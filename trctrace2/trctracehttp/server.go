package trctracehttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
	trctrace "github.com/peterbourgon/trc/trctrace2"
)

type Target struct {
	Name     string
	Searcher trctrace.Searcher
}

type ResponseData struct {
	Target   string                   `json:"target,omitempty"`
	Targets  []string                 `json:"targets,omitempty"`
	Request  *trctrace.SearchRequest  `json:"request"`
	Response *trctrace.SearchResponse `json:"response"`
}

type ServerConfig struct {
	Local   *Target
	Other   []*Target
	Default *Target
}

func (cfg *ServerConfig) Validate() error {
	if cfg.Local == nil {
		return fmt.Errorf("local target is required")
	}

	if cfg.Default == nil {
		cfg.Default = cfg.Local
	}

	return nil
}

func NewServer(cfg ServerConfig) (http.Handler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	names := make([]string, 1+len(cfg.Other))
	index := make(map[string]*Target, 1+len(cfg.Other))
	for i, t := range append([]*Target{cfg.Local}, cfg.Other...) {
		names[i] = t.Name
		index[t.Name] = t
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, tr, finish := trc.Region(r.Context(), "ServeHTTP")
		defer finish()

		var (
			begin    = time.Now()
			req, prs = parseSearchRequest(ctx, r)
			urlquery = r.URL.Query()
			hasLocal = urlquery.Has("local")
			t        = urlquery.Get("t")
		)

		tgt, ok := index[t]
		switch {
		case ok:
			// great
		case !ok && hasLocal:
			tgt = cfg.Local
		case !ok && !hasLocal:
			tgt = cfg.Default
		}

		tr.Tracef("target=%v", tgt.Name)

		res, err := tgt.Searcher.Search(ctx, req)
		if err != nil {
			tr.Errorf("search error: %v", err)
			prs = append(prs, err.Error())
			res = &trctrace.SearchResponse{} // default
		}

		res.Problems = append(prs, res.Problems...)
		res.Duration = time.Since(begin)

		tr.Tracef("total=%d matched=%d selected=%d duration=%s", res.Total, res.Matched, len(res.Selected), res.Duration)

		Render(ctx, w, r, assets, "traces.html", templateFuncs, &ResponseData{
			Target:   tgt.Name,
			Targets:  names,
			Request:  req,
			Response: res,
		})
	}), nil
}

//
//
//

func parseSearchRequest(ctx context.Context, r *http.Request) (*trctrace.SearchRequest, []string) {
	var (
		tr          = trc.FromContext(ctx)
		isJSON      = strings.Contains(r.Header.Get("content-type"), "application/json")
		urlquery    = r.URL.Query()
		limit       = parseDefault(urlquery.Get("n"), strconv.Atoi, 10)
		minDuration = parseDefault(urlquery.Get("min"), parseDurationPointer, nil)
		bucketing   = parseBucketing(urlquery["b"]) // can be nil, no problem
		query       = urlquery.Get("q")
		req         = &trctrace.SearchRequest{}
		prs         = []string{}
	)

	switch {
	case isJSON:
		tr.Tracef("parsing search request from JSON request body")
		if err := json.NewDecoder(r.Body).Decode(req); err != nil {
			err = fmt.Errorf("parse JSON request body: %w", err)
			prs = append(prs, err.Error())
			tr.Errorf(err.Error())
		}

	default:
		tr.Tracef("parsing search request from URL %q", urlquery.Encode())
		req = &trctrace.SearchRequest{
			IDs:         urlquery["id"],
			Category:    urlquery.Get("category"),
			IsActive:    urlquery.Has("active"),
			Bucketing:   bucketing,
			MinDuration: minDuration,
			IsFailed:    urlquery.Has("failed"),
			Query:       query,
			Limit:       limit,
		}
	}

	if err := req.Normalize(); err != nil {
		err = fmt.Errorf("normalize request: %w", err)
		prs = append(prs, err.Error())
		tr.Errorf(err.Error())
	}

	tr.Tracef("parsed search request %s", req)

	return req, prs
}

func parseBucketing(bs []string) []time.Duration {
	if len(bs) <= 0 {
		return nil
	}

	var ds []time.Duration
	for _, s := range bs {
		if d, err := time.ParseDuration(s); err == nil {
			ds = append(ds, d)
		}
	}

	sort.Slice(ds, func(i, j int) bool {
		return ds[i] < ds[j]
	})

	if len(ds) <= 0 {
		return nil
	}

	if ds[0] != 0 {
		ds = append([]time.Duration{0}, ds...)
	}

	return ds
}

func parseDefault[T any](s string, parse func(string) (T, error), def T) T {
	if v, err := parse(s); err == nil {
		return v
	}
	return def
}

func parseDurationPointer(s string) (*time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}
