package trctrace

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
	Query       string          `json:"query,omitempty"`
	Regexp      *regexp.Regexp  `json:"-"`
	Limit       int             `json:"limit,omitempty"`
}

func (req *SearchRequest) String() string {
	var tokens []string

	if len(req.IDs) > 0 {
		tokens = append(tokens, fmt.Sprintf("ids=%v", req.IDs))
	}

	if req.Category != "" {
		tokens = append(tokens, fmt.Sprintf("category=%q", req.Category))
	}

	if req.IsActive {
		tokens = append(tokens, "active")
	}

	if len(req.Bucketing) > 0 {
		tokens = append(tokens, fmt.Sprintf("bucketing=%v", req.Bucketing))
	}

	if req.MinDuration != nil {
		tokens = append(tokens, fmt.Sprintf("min=%s", req.MinDuration))
	}

	if req.IsFailed {
		tokens = append(tokens, "failed")
	}

	if req.Query != "" {
		tokens = append(tokens, fmt.Sprintf("query=%s", req.Query))
	}

	if req.Regexp != nil {
		tokens = append(tokens, fmt.Sprintf("regexp=%s", req.Regexp.String()))
	}

	if req.Limit != 0 {
		tokens = append(tokens, fmt.Sprintf("limit=%d", req.Limit))
	}

	return strings.Join(tokens, " ")
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

	urlquery.Set("local", "true")

	r.URL.RawQuery = urlquery.Encode()

	r.Header.Set("accept", "application/json")

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
		if tr.Active() || tr.Errored() { // we assert that a min duration excludes active and failed traces
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

func (req *SearchRequest) QueryParams(keyvals ...string) template.URL {
	values := url.Values{}

	/*
		if len(req.IDs) > 0 {
			values["id"] = req.IDs
		}

		if req.Category != "" {
			values.Set("category", req.Category)
		}

		if req.IsActive {
			values.Set("active", "true")
		}

		if len(req.Bucketing) > 0 {
			if !reflect.DeepEqual(req.Bucketing, DefaultBucketing) {
				for _, b := range req.Bucketing {
					values.Add("b", b.String())
				}
			}
		}

		if req.MinDuration != nil {
			values.Set("min", req.MinDuration.String())
		}

		if req.IsFailed {
			values.Set("failed", "true")
		}
	*/

	if req.Regexp != nil {
		values.Set("q", req.Regexp.String())
	}

	if req.Limit > 0 {
		values.Set("n", strconv.Itoa(req.Limit))
	}

	for i := 0; i < len(keyvals); i += 2 {
		key := keyvals[i]
		val := keyvals[i+1]
		if key == "category" && val == "overall" {
			continue
		}
		values.Set(key, val)
	}

	return template.URL(values.Encode())
}

//
//
//

type SearchResponse struct {
	Sources  []trc.Source       `json:"sources,omitempty"`
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
	begin := time.Now()
	tr := trc.FromContext(ctx)

	type tuple struct {
		id  string
		res *SearchResponse
		err error
	}

	// Scatter.
	tuplec := make(chan tuple, len(ms))
	for i, s := range ms {
		go func(id string, s Searcher) {
			ctx, _ := trc.PrefixTraceContext(ctx, "<%s>", id)
			res, err := s.Search(ctx, req)
			tuplec <- tuple{id, res, err}
		}(strconv.Itoa(i+1), s)
	}

	tr.Tracef("scattered request count %d", len(ms))

	// Gather.
	aggregate := &SearchResponse{}
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
			//tr.Tracef("%s: success", t.id)
			aggregate.Sources = append(aggregate.Sources, t.res.Sources...)
			aggregate.Stats = CombineStats(aggregate.Stats, t.res.Stats)
			aggregate.Total += t.res.Total
			aggregate.Matched += t.res.Matched
			aggregate.Selected = append(aggregate.Selected, t.res.Selected...) // needs sort+limit
			aggregate.Problems = append(aggregate.Problems, t.res.Problems...)
		case t.res != nil && t.err != nil: // weird
			tr.Tracef("%s: weird: valid result (accepting it) with error: %v", t.id, t.err)
			aggregate.Sources = append(aggregate.Sources, t.res.Sources...)
			aggregate.Stats = CombineStats(aggregate.Stats, t.res.Stats)
			aggregate.Total += t.res.Total
			aggregate.Matched += t.res.Matched
			aggregate.Selected = append(aggregate.Selected, t.res.Selected...) // needs sort+limit
			aggregate.Problems = append(aggregate.Problems, t.res.Problems...)
			aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("got valid search response with error (%v) -- weird", t.err))
		}
	}

	tr.Tracef("gathered responses")

	sort.Slice(aggregate.Sources, func(i, j int) bool {
		return aggregate.Sources[i].Name < aggregate.Sources[j].Name
	})

	// At this point, the aggregate response has all of the raw data it's ever
	// gonna get. We need to do a little bit of post-processing. First, we need
	// to sort all of the selected traces by start time, and then limit them by
	// the requested limit.

	sort.Slice(aggregate.Selected, func(i, j int) bool {
		return aggregate.Selected[i].Start().After(aggregate.Selected[j].Start())
	})

	if len(aggregate.Selected) > req.Limit {
		aggregate.Selected = aggregate.Selected[:req.Limit]
	}

	// Duration is also defined across all individual requests.
	aggregate.Duration = time.Since(begin)

	// That should be it.
	return aggregate, nil
}
