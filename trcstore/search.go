package trcstore

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
)

// Searcher describes the ability to search over a collection of traces.
// It's implemented by the [Collector] type.
type Searcher interface {
	Search(context.Context, *SearchRequest) (*SearchResponse, error)
}

// SearchRequest defines a set of parameters that select a subset of traces from
// a collection of traces. All fields are optional; the zero value of a search
// request is valid, and matches all traces.
type SearchRequest struct {
	// IDs selects traces with any of the provided ID strings.
	IDs []string `json:"ids"`

	// Category selects traces in the provided category.
	Category string `json:"category"`

	// IsActive selects traces that are active.
	IsActive bool `json:"is_active"`

	// Bucketing defines the time buckets that finished and non-errored traces
	// are grouped by in the stats value returned in the search response. The
	// default value is DefaultBucketing.
	Bucketing []time.Duration `json:"bucketing"`

	// MinDuration selects traces that are finished, non-errored, and which have
	// at least the provided duration.
	MinDuration *time.Duration `json:"min_duration"`

	// IsErrored selects traces that are finished and errored.
	IsErrored bool `json:"is_errored"`

	// Query selects traces with events or stack frames that match the given
	// query string, which is evaluated as a regexp. If the query string is not
	// a valid regexp, the field will be ignored.
	Query string `json:"query"`

	// Limit defines the maximum number of traces that will be returned in the
	// search response. The default value is 10. The minimum is 1, and the
	// maximum is 250.
	Limit int `json:"limit"`

	// Problems encountered when parsing and/or evaluating the search request.
	// This field should generally not be set by callers.
	Problems []string `json:"problems,omitempty"`

	regexp *regexp.Regexp
}

const (
	searchLimitMin = 1
	searchLimitDef = 10
	searchLimitMax = 250
)

// DefaultBucketing are the default buckets used to group finished and
// non-errored traces in the stats returned as part of a search response.
var DefaultBucketing = []time.Duration{
	0 * time.Millisecond,
	1 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	1000 * time.Millisecond,
}

// Normalize enforces constraints, limits, etc. on relevant fields of the search
// request, and compiles the query string to a regexp. This method should be
// called before a search request is consumed.
func (req *SearchRequest) Normalize(ctx context.Context) {
	if req.Bucketing == nil {
		req.Bucketing = DefaultBucketing
	}
	sort.Slice(req.Bucketing, func(i, j int) bool {
		return req.Bucketing[i] < req.Bucketing[j]
	})
	if req.Bucketing[0] != 0 {
		req.Bucketing = append([]time.Duration{0}, req.Bucketing...)
	}

	switch {
	case req.regexp != nil && req.Query == "":
		req.Query = req.regexp.String()
	case req.regexp == nil && req.Query != "":
		re, err := regexp.Compile(req.Query)
		switch {
		case err == nil:
			req.regexp = re
		case err != nil:
			req.Query = ""
			req.Problems = append(req.Problems, fmt.Sprintf("query ignored: %v", err))
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
}

// QueryValues returns a set of URL query parameters that, if passed to the
// server handler, should reproduce the search request. It is assumed that the
// defaults are consistent between the instance which invokes this method, and
// the instance serving the request.
func (req *SearchRequest) QueryValues() url.Values {
	values := url.Values{}
	if len(req.IDs) > 0 {
		values["ids"] = req.IDs
	}
	if req.Category != "" {
		values.Set("category", req.Category)
	}
	if req.IsActive {
		values.Set("active", "true")
	}
	if !reflect.DeepEqual(req.Bucketing, DefaultBucketing) {
		strs := make([]string, len(req.Bucketing))
		for i := range req.Bucketing {
			strs[i] = req.Bucketing[i].String()
		}
		values["b"] = strs
	}
	if req.MinDuration != nil {
		values.Set("min", req.MinDuration.String())
	}
	if req.IsErrored {
		values.Set("errored", "true")
	}
	if req.Query != "" {
		values.Set("q", req.Query)
	}
	if req.Limit != searchLimitDef {
		values.Set("n", strconv.Itoa(req.Limit))
	}
	return values
}

// String returns a representation of the search request that elides default
// values and is suitable for use in a trace event or log statement.
func (req SearchRequest) String() string {
	var elems []string
	for k, vs := range req.QueryValues() {
		elems = append(elems, fmt.Sprintf("%s=%v", k, vs))
	}
	return "{" + strings.Join(elems, " ") + "}"
}

// Allow returns true if the search request matches the provided trace.
func (req *SearchRequest) Allow(ctx context.Context, tr trc.Trace) bool {
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

	if req.IsErrored && !(tr.Finished() && tr.Errored()) {
		return false
	}

	if req.regexp != nil {
		for _, ev := range tr.Events() {
			if req.regexp.MatchString(ev.What) {
				return true
			}
			for _, c := range ev.Stack {
				if req.regexp.MatchString(c.Function) {
					return true
				}
				if req.regexp.MatchString(c.FileLine) {
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

// SearchResponse is the result of a search.
type SearchResponse struct {
	Stats    *Stats         `json:"stats"`
	Sources  []string       `json:"sources,omitempty"`
	Total    int            `json:"total"`
	Matched  int            `json:"matched"`
	Selected []*SearchTrace `json:"selected"`
	Problems []string       `json:"problems,omitempty"`
	Duration time.Duration  `json:"duration"`
}

//
//
//

// MultiSearcher allows multiple searchers to be queried as one. Search requests
// are scattered to each individual searcher concurrently, and responses are
// gathered and merged into a single response.
type MultiSearcher []Searcher

// Search makes concurrent search requests to each of the individual searchers,
// and merges the search responses into a single aggregate result.
func (ms MultiSearcher) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	tr := trc.Get(ctx)
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
			ctx, _ := trc.Prefix(ctx, "<%s>", id)
			res, err := s.Search(ctx, req)
			tuplec <- tuple{id, res, err}
		}(strconv.Itoa(i+1), s)
	}

	tr.Tracef("scattered request count %d", len(ms))

	// Gather.
	aggregate := &SearchResponse{
		Stats: NewStats(req.Bucketing),
	}
	for i := 0; i < cap(tuplec); i++ {
		t := <-tuplec
		switch {
		case t.res == nil && t.err == nil: // weird
			tr.Tracef("%s: weird: no result, no error", t.id)
			aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("%s: weird: empty response", t.id))
		case t.res == nil && t.err != nil: // error case
			tr.Tracef("%s: error: %v", t.id, t.err)
			aggregate.Problems = append(aggregate.Problems, t.err.Error())
		case t.res != nil && t.err == nil: // success case
			aggregate.Stats.Merge(t.res.Stats)
			aggregate.Sources = append(aggregate.Sources, t.res.Sources...)
			aggregate.Total += t.res.Total
			aggregate.Matched += t.res.Matched
			aggregate.Selected = append(aggregate.Selected, t.res.Selected...) // needs sort+limit
			aggregate.Problems = append(aggregate.Problems, t.res.Problems...)
		case t.res != nil && t.err != nil: // weird
			tr.Tracef("%s: weird: valid result (accepting it) with error: %v", t.id, t.err)
			aggregate.Stats.Merge(t.res.Stats)
			aggregate.Sources = append(aggregate.Sources, t.res.Sources...)
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
		return aggregate.Selected[i].Started.After(aggregate.Selected[j].Started)
	})
	if len(aggregate.Selected) > req.Limit {
		aggregate.Selected = aggregate.Selected[:req.Limit]
	}

	// Fix up the sources.
	sort.Strings(aggregate.Sources)

	// Duration is also defined across all individual requests.
	aggregate.Duration = time.Since(begin)

	// That should be it.
	return aggregate, nil
}

//
//
//

// SearchTrace is a "snapshot" of a trace returned by a search.
type SearchTrace struct {
	Source   string        `json:"source"`
	ID       string        `json:"id"`
	Category string        `json:"category"`
	Started  time.Time     `json:"started"`
	Finished bool          `json:"finished"`
	Errored  bool          `json:"errored"`
	Duration time.Duration `json:"duration"`
	Events   []trc.Event   `json:"events"`
}

// NewSearchTrace returns an immutable "snapshot" of the given trace.
func NewSearchTrace(tr trc.Trace) *SearchTrace {
	return &SearchTrace{
		Source:   tr.Source(),
		ID:       tr.ID(),
		Category: tr.Category(),
		Started:  tr.Started(),
		Finished: tr.Finished(),
		Errored:  tr.Errored(),
		Duration: tr.Duration(),
		Events:   tr.Events(),
	}
}
