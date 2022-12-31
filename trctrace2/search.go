package trctrace

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
)

type Searcher interface {
	Search(context.Context, *SearchRequest) (*SearchResponse, error)
}

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

func ParseSearchRequest(r *http.Request) (*SearchRequest, error) {
	var (
		urlquery    = r.URL.Query()
		limit       = trchttp.ParseDefault(urlquery.Get("n"), strconv.Atoi, 10)
		minDuration = trchttp.ParseDefault(urlquery.Get("min"), trchttp.ParseDurationPointer, nil)
		bucketing   = parseBucketing(urlquery["b"])
		query       = urlquery.Get("q")
	)

	req := &SearchRequest{
		IDs:         urlquery["id"],
		Category:    urlquery.Get("category"),
		IsActive:    urlquery.Has("active"),
		Bucketing:   bucketing,
		MinDuration: minDuration,
		IsFailed:    urlquery.Has("failed"),
		Query:       query,
		Limit:       limit,
	}

	if err := req.Normalize(); err != nil {
		return nil, err
	}

	return req, nil
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

func (req *SearchRequest) MakeHTTPRequest(ctx context.Context, baseurl string) (*http.Request, error) {
	if err := req.Normalize(); err != nil {
		return nil, fmt.Errorf("sanitize query request: %w", err)
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
