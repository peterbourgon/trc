package trctrace

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/peterbourgon/trc"
)

type Searcher interface {
	Search(context.Context, *SearchRequest) (*SearchResponse, error)
}

//
//
//

type SearchRequest struct {
	IDs         []string        `json:"ids,omitempty"`
	Category    string          `json:"category,omitempty"`
	IsActive    bool            `json:"is_active,omitempty"`
	Bucketing   []time.Duration `json:"bucketing,omitempty"`
	MinDuration *time.Duration  `json:"min_duration,omitempty"`
	IsFailed    bool            `json:"is_failed,omitempty"`
	Query       string          `json:"query"`
	Regexp      *regexp.Regexp  `json:"-"`
	Limit       int             `json:"limit,omitempty"`
}

func (req *SearchRequest) Normalize() error {
	if req.Bucketing == nil {
		req.Bucketing = DefaultBucketing
	}

	switch {
	case req.Regexp != nil && req.Query == "":
		req.Query = req.Regexp.String()
	case req.Regexp == nil && req.Query != "":
		re, err := regexp.Compile(req.Query)
		if err != nil {
			return fmt.Errorf("%q: %w", req.Query, err)
		}
		req.Regexp = re
	}

	switch {
	case req.Limit <= 0:
		req.Limit = queryLimitDef
	case req.Limit < queryLimitMin:
		req.Limit = queryLimitMin
	case req.Limit > queryLimitMax:
		req.Limit = queryLimitMax
	}

	return nil
}

func (req *SearchRequest) HTTPRequest(ctx context.Context, baseurl string) (*http.Request, error) {
	if err := req.Normalize(); err != nil {
		return nil, fmt.Errorf("normalize query request: %w", err)
	}

	r, err := http.NewRequestWithContext(ctx, "GET", baseurl, nil)
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}

	urlquery := r.URL.Query()

	if req.Limit > 0 {
		urlquery.Set("n", strconv.Itoa(req.Limit))
	}

	for _, id := range req.IDs {
		urlquery.Add("id", id)
	}

	if req.Category != "" {
		urlquery.Set("category", req.Category)
	}

	if req.IsActive {
		urlquery.Set("active", "true")
	}

	if req.Bucketing != nil {
		for _, b := range req.Bucketing {
			urlquery.Add("b", b.String())
		}
	}

	if req.MinDuration != nil {
		urlquery.Set("min", req.MinDuration.String())
	}

	if req.IsFailed {
		urlquery.Set("failed", "true")
	}

	if req.Regexp != nil {
		urlquery.Set("q", req.Regexp.String())
	}

	urlquery.Set("json", "true")

	r.URL.RawQuery = urlquery.Encode()

	return r, nil
}

func (req *SearchRequest) Allow(tr trc.Trace) bool {
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

	if req.IsActive && !tr.Active() {
		return false
	}

	if req.MinDuration != nil {
		if tr.Active() { // we assert that a min duration excludes active traces
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
		if matchedSomething := func() bool {
			if req.Regexp.MatchString(tr.ID()) {
				return true
			}
			if req.Regexp.MatchString(tr.Category()) {
				return true
			}
			for _, ev := range tr.Events() {
				if ev.MatchRegexp(req.Regexp) {
					return true
				}
			}
			return false
		}(); !matchedSomething {
			return false
		}
	}

	return true
}

//
//
//

type SearchResponse struct {
	Request  *SearchRequest     `json:"request"`
	Origins  []string           `json:"origins,omitempty"`
	Stats    Stats              `json:"stats"`
	Total    int                `json:"total"`
	Matched  int                `json:"matched"`
	Selected []*trc.StaticTrace `json:"selected"`
	Problems []string           `json:"problems,omitempty"`
	Duration time.Duration      `json:"duration"`
}

//
//
//

type MultiSearcher []Searcher

func (ms MultiSearcher) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	// Mark the start time.
	begin := time.Now()

	// We'll scatter/gather over our searchers.
	type tuple struct {
		res *SearchResponse
		err error
	}

	// Scatter.
	tuplec := make(chan tuple, len(ms))
	for _, s := range ms {
		go func(s Searcher) {
			res, err := s.Search(ctx, req)
			tuplec <- tuple{res, err}
		}(s)
	}

	// Gather.
	aggregate := &SearchResponse{Request: req}
	for i := 0; i < cap(tuplec); i++ {
		t := <-tuplec
		switch {
		case t.res != nil: // success
			aggregate.Origins = append(aggregate.Origins, t.res.Origins...)
			aggregate.Stats = CombineStats(aggregate.Stats, t.res.Stats)
			aggregate.Total += t.res.Total
			aggregate.Matched += t.res.Matched
			aggregate.Selected = append(aggregate.Selected, t.res.Selected...) // needs sort+limit
			aggregate.Problems = append(aggregate.Problems, t.res.Problems...)
			if t.err != nil { // weird!!
				aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("got valid search response (origin %v) with error (%v) -- weird", t.res.Origins, t.err))
			}

		case t.res == nil: // error
			if t.err == nil { // weird!!
				t.err = fmt.Errorf("got invalid search response (zero value) with no error -- weird")
			}
			aggregate.Problems = append(aggregate.Problems, t.err.Error())
		}
	}

	// At this point, the aggregate search response has collected as much data
	// as it's gonna get. That data is correct *except* for the selected traces.
	// They need to be sorted and limited, same as the search method on the
	// collector does.
	sort.Slice(aggregate.Selected, func(i, j int) bool {
		return aggregate.Selected[i].Start().Before(aggregate.Selected[j].Start())
	})
	if len(aggregate.Selected) > req.Limit {
		aggregate.Selected = aggregate.Selected[:req.Limit]
	}

	// Mark the overall duration.
	aggregate.Duration = time.Since(begin)

	// Done.
	return aggregate, nil
}
