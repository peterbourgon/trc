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

type Server struct {
	Origin string
	Local  trctrace.Searcher
	Global trctrace.Searcher
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, tr, finish := trc.Region(r.Context(), "ServeHTTP")
	defer finish()

	var (
		begin         = time.Now()
		req, problems = parseSearchRequest(ctx, r)
		isGlobal      = s.Global != nil && !r.URL.Query().Has("local")
	)

	var target trctrace.Searcher
	switch {
	case isGlobal:
		tr.Tracef("global search")
		target = s.Global
	default:
		tr.Tracef("local search")
		target = s.Local
	}

	res, err := target.Search(ctx, req)
	if err != nil {
		tr.Errorf("search failed: %v", err)
		problems = append(problems, err.Error())
		res = &trctrace.SearchResponse{Request: req} // default
	} else {
		tr.Tracef("search finished")
	}

	res.Duration = time.Since(begin)

	res.Problems = append(problems, res.Problems...)

	for _, tr := range res.Selected {
		if tr.Origin == "" {
			tr.Origin = s.Origin
		}
	}

	if len(res.Origins) <= 0 {
		res.Origins = append(res.Origins, s.Origin)
	}

	sort.Strings(res.Origins)

	tr.Tracef("origins %d, total %d, matched %d, selected %d, duration %s", len(res.Origins), res.Total, res.Matched, len(res.Selected), res.Duration)

	Render(ctx, w, r, assets, "traces2.html", templateFuncs, res)
}

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
		problems    = []string{}
	)

	switch {
	case isJSON:
		tr.Tracef("parsing search request from JSON request body")
		if err := json.NewDecoder(r.Body).Decode(req); err != nil {
			err = fmt.Errorf("parse JSON request body: %w", err)
			problems = append(problems, err.Error())
			tr.Errorf(err.Error())
		}
	default:
		tr.Tracef("parsing search request from URL")
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
		problems = append(problems, err.Error())
		tr.Errorf(err.Error())
	}

	return req, problems
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
