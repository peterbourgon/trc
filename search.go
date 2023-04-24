package trc

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Searcher describes the ability to search over a collection of traces.
type Searcher interface {
	Search(context.Context, *SearchRequest) (*SearchResponse, error)
}

// SearchRequest specifies parameters that identify a subset of traces.
type SearchRequest struct {
	IDs         []string        `json:"ids,omitempty"`
	Category    string          `json:"category,omitempty"`
	IsActive    bool            `json:"is_active,omitempty"`
	Bucketing   []time.Duration `json:"bucketing,omitempty"`
	MinDuration *time.Duration  `json:"min_duration,omitempty"`
	IsFailed    bool            `json:"is_failed,omitempty"`
	Query       string          `json:"query,omitempty"`
	Regexp      *regexp.Regexp  `json:"-"`
	Limit       int             `json:"limit,omitempty"`
}

// Normalize ensures the request is valid, returning any problems encountered.
func (req *SearchRequest) Normalize() (problems []string) {
	if req.Bucketing == nil {
		req.Bucketing = DefaultBucketing
	}

	switch {
	case req.Regexp != nil && req.Query == "":
		req.Query = req.Regexp.String()
	case req.Regexp == nil && req.Query != "":
		re, err := regexp.Compile(req.Query)
		switch {
		case err == nil:
			req.Regexp = re
		case err != nil:
			problems = append(problems, err.Error())
		}
	}

	switch {
	case req.Limit <= 0:
		req.Limit = searchLimitDef
	case req.Limit < searchLimitMin:
		req.Limit = searchLimitMin
	case req.Limit > searchLimitMax:
		req.Limit = searchLimitMax
	}

	return problems
}

// String returns a representation of the search request that elides default
// values and is suitable for use in a trace event or log statement.
func (req *SearchRequest) String() string {
	var elems []string
	if len(req.IDs) > 0 {
		elems = append(elems, fmt.Sprintf("ids=%v", req.IDs))
	}
	if req.Category != "" {
		elems = append(elems, fmt.Sprintf("category=%q", req.Category))
	}
	if req.IsActive {
		elems = append(elems, "active")
	}
	if !reflect.DeepEqual(req.Bucketing, DefaultBucketing) {
		strs := make([]string, len(req.Bucketing))
		for i := range req.Bucketing {
			strs[i] = req.Bucketing[i].String()
		}
		elems = append(elems, fmt.Sprintf("bucketing=%v", strs))
	}
	if req.MinDuration != nil {
		elems = append(elems, fmt.Sprintf("min=%s", req.MinDuration))
	}
	if req.IsFailed {
		elems = append(elems, "failed")
	}
	if req.Query != "" {
		elems = append(elems, fmt.Sprintf("q=%q", req.Query))
	}
	if req.Limit > 0 {
		elems = append(elems, fmt.Sprintf("limit=%d", req.Limit))
	}
	return "{" + strings.Join(elems, " ") + "}"
}

// Allow returns true if the search request matches the provided trace.
func (req *SearchRequest) Allow(ctx context.Context, tr Trace) bool {
	if len(req.IDs) > 0 {
		var found bool
		for _, id := range req.IDs {
			if id == tr.ID() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if req.Category != "" && tr.Category() != req.Category {
		return false
	}

	if req.IsActive && tr.Finished() {
		return false
	}

	if req.MinDuration != nil {
		if !tr.Finished() || tr.Errored() { // we assert that a min duration excludes active and failed traces
			return false
		}
		if tr.Duration() < *req.MinDuration {
			return false
		}
	}

	if req.IsFailed && !(tr.Finished() && tr.Errored()) {
		return false
	}

	if req.Regexp != nil {
		for _, ev := range tr.Events() {
			if req.Regexp.MatchString(ev.What()) {
				return true
			}
			for _, c := range ev.Stack() {
				if req.Regexp.MatchString(c.Function()) {
					return true
				}
				if req.Regexp.MatchString(c.FileLine()) {
					return true
				}
			}
		}
		return false
	}

	return true
}

//
//
//

// SearchResponse is the result of performing a search request.
type SearchResponse struct {
	Stats    SearchStats      `json:"stats"`
	Total    int              `json:"total"`
	Matched  int              `json:"matched"`
	Selected []*SelectedTrace `json:"selected"`
	Problems []string         `json:"problems,omitempty"`
	Duration time.Duration    `json:"duration"`
}

// Sources returns all of the unique "Via" sources among all of the selected
// traces in the response.
func (res *SearchResponse) Sources() []string {
	index := map[string]struct{}{}
	for _, x := range res.Selected {
		for _, s := range x.Via {
			index[s] = struct{}{}
		}
	}
	slice := make([]string, 0, len(index))
	for s := range index {
		slice = append(slice, s)
	}
	sort.Strings(slice)
	return slice
}

//
//
//

// MultiSearcher allows multiple distinct searchers to be queried as one,
// scattering the search request to each of them, and gathering and merging
// their responses into a single response.
type MultiSearcher []Searcher

// Search implements Searcher, by making concurrent search requests, and
// gathering results into a single response.
func (ms MultiSearcher) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	tr := FromContext(ctx)
	begin := time.Now()

	type tuple struct {
		id  string
		res *SearchResponse
		err error
	}

	// Scatter.
	tuplec := make(chan tuple, len(ms))
	for i, s := range ms {
		go func(id string, s Searcher) {
			ctx, _ := Prefix(ctx, "<%s>", id)
			res, err := s.Search(ctx, req)
			tuplec <- tuple{id, res, err}
		}(strconv.Itoa(i+1), s)
	}

	tr.Tracef("scattered request count %d", len(ms))

	// Gather.
	aggregate := &SearchResponse{
		Stats: SearchStats{
			Bucketing: req.Bucketing,
		},
	}
	for i := 0; i < cap(tuplec); i++ {
		t := <-tuplec
		switch {
		case t.res == nil && t.err == nil: // weird
			tr.Tracef("%s: weird: no result, no error", t.id)
			aggregate.Problems = append(aggregate.Problems, "got nil search response with nil error -- weird")
		case t.res == nil && t.err != nil: // error case
			tr.Tracef("%s: error: %v", t.id, t.err)
			aggregate.Problems = append(aggregate.Problems, t.err.Error())
		case t.res != nil && t.err == nil: // success case
			aggregate.Stats = combineStats(aggregate.Stats, t.res.Stats)
			aggregate.Total += t.res.Total
			aggregate.Matched += t.res.Matched
			aggregate.Selected = append(aggregate.Selected, t.res.Selected...) // needs sort+limit
			aggregate.Problems = append(aggregate.Problems, t.res.Problems...)
		case t.res != nil && t.err != nil: // weird
			tr.Tracef("%s: weird: valid result (accepting it) with error: %v", t.id, t.err)
			aggregate.Stats = combineStats(aggregate.Stats, t.res.Stats)
			aggregate.Total += t.res.Total
			aggregate.Matched += t.res.Matched
			aggregate.Selected = append(aggregate.Selected, t.res.Selected...) // needs sort+limit
			aggregate.Problems = append(aggregate.Problems, t.res.Problems...)
			aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("got valid search response with error (%v) -- weird", t.err))
		}
	}

	tr.Tracef("gathered responses")

	// At this point, the aggregate response has all of the raw data it's ever
	// gonna get. We need to do a little bit of post-processing. First, we need
	// to sort all of the selected traces by start time, and then limit them by
	// the requested limit.
	sort.Slice(aggregate.Selected, func(i, j int) bool {
		return aggregate.Selected[i].Started().After(aggregate.Selected[j].Started())
	})
	if len(aggregate.Selected) > req.Limit {
		aggregate.Selected = aggregate.Selected[:req.Limit]
	}

	// Duration is also defined across all individual requests.
	aggregate.Duration = time.Since(begin)

	// That should be it.
	return aggregate, nil
}