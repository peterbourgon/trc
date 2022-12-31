package trctracehttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
	trctrace "github.com/peterbourgon/trc/trctrace2"
)

type Server struct {
	origin string
	local  trctrace.Searcher
	global trctrace.Searcher
}

func NewServer(origin string, local, global trctrace.Searcher) *Server {
	return &Server{
		origin: origin,
		local:  local,
		global: global,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, tr, finish := trc.Region(r.Context(), "ServeHTTP")
	defer finish()

	var (
		begin    = time.Now()
		urlquery = r.URL.Query()
		problems = []string{}
	)

	var target trctrace.Searcher
	switch {
	case urlquery.Has("local"):
		target = s.local
	default:
		target = s.global
	}

	req, err := parseSearchRequest(r)
	if err != nil {
		problems = append(problems, err.Error())
		tr.Errorf("parse search request: %v", err)
		req = &trctrace.SearchRequest{} // default
	}

	// TODO: content length, transfer encoding?
	if contentType := r.Header.Get("content-type"); strings.Contains(contentType, "application/json") {
		tr.Tracef("request content type %s, parsing search request from body", contentType)
		err := json.NewDecoder(r.Body).Decode(req)
		if err != nil {
			err = fmt.Errorf("parse request body: %w", err)
			problems = append(problems, err.Error())
			tr.Errorf(err.Error())
		}
	}

	if err := req.Normalize(); err != nil {
		err = fmt.Errorf("normalize request: %w", err)
		problems = append(problems, err.Error())
		tr.Errorf(err.Error())
	}

	res, err := target.Search(ctx, req)
	if err != nil {
		err = fmt.Errorf("perform search: %w", err)
		problems = append(problems, err.Error())
		tr.Errorf(err.Error())
		res = &trctrace.SearchResponse{Request: req} // default
	}

	res.Duration = time.Since(begin)
	res.Origins = append(res.Origins, s.origin)
	res.Problems = append(problems, res.Problems...)

	tr.Tracef("search: total %d, matched %d, selected %d, duration %s", res.Total, res.Matched, len(res.Selected), res.Duration)

	// trchttp.Render(ctx, w, r, assets, "traces.html", templateFuncs, &HTTPQueryData{
	// PageTitle:        pageTitle,
	// AvailableOrigins: origins,
	// ResponseOrigin:   origin,
	// Request:          req,
	// Response:         res,
	// })

	w.Header().Set("content-type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(res)
}

func parseSearchRequest(r *http.Request) (*trctrace.SearchRequest, error) {
	var (
		urlquery    = r.URL.Query()
		limit       = trchttp.ParseDefault(urlquery.Get("n"), strconv.Atoi, 10)
		minDuration = trchttp.ParseDefault(urlquery.Get("min"), trchttp.ParseDurationPointer, nil)
		bucketing   = parseBucketing(urlquery["b"]) // can be nil, no problem
		query       = urlquery.Get("q")
	)

	req := &trctrace.SearchRequest{
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
