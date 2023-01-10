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
	"github.com/peterbourgon/trc/trctrace"
)

type ServerConfig struct {
	// Local represents the server instance itself. Typically, the local
	// searcher is implemented by a singleton collector.
	//
	// Required.
	Local *Target

	// Other targets are made available for searches.
	//
	// Optional.
	Other []*Target

	// Default is the target which should be used when no target is explicitly
	// specified by the user.
	//
	// Optional. By default, the local target is used.
	Default *Target
}

func (cfg *ServerConfig) sanitize() error {
	if cfg.Local == nil {
		return fmt.Errorf("local target is required")
	}

	if cfg.Default == nil {
		cfg.Default = cfg.Local
	}

	return nil
}

type Server struct {
	targetIndex      map[string]*Target
	localTarget      *Target
	defaultTarget    *Target
	availableTargets []string
}

// NewServer returns a server, which implements an HTTP API over the targets
// provided in the config. That API can be consumed by the client type, also
// provided in this package.
func NewServer(cfg ServerConfig) (*Server, error) {
	if err := cfg.sanitize(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	names := make([]string, 1+len(cfg.Other))
	index := make(map[string]*Target, 1+len(cfg.Other))
	for i, t := range append([]*Target{cfg.Local}, cfg.Other...) {
		names[i] = t.name
		index[t.name] = t
	}

	return &Server{
		targetIndex:      index,
		localTarget:      cfg.Local,
		defaultTarget:    cfg.Default,
		availableTargets: names,
	}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, tr, finish := trc.Region(r.Context(), "ServeHTTP")
	defer finish()

	var (
		begin      = time.Now()
		req, prs   = parseSearchRequest(ctx, r)
		urlquery   = r.URL.Query()
		hasLocal   = urlquery.Has("local")
		targetName = urlquery.Get("t")
	)

	target, ok := s.targetIndex[targetName]
	switch {
	case ok:
		// great
	case !ok && hasLocal:
		target = s.localTarget
	case !ok && !hasLocal:
		target = s.defaultTarget
	}

	tr.Tracef("target=%v", target.name)

	res, err := target.searcher.Search(ctx, req)
	if err != nil {
		tr.Errorf("search error: %v", err)
		prs = append(prs, err.Error())
		res = &trctrace.SearchResponse{} // default
	}

	res.Problems = append(prs, res.Problems...)
	res.Duration = time.Since(begin)

	tr.Tracef("total=%d matched=%d selected=%d duration=%s", res.Total, res.Matched, len(res.Selected), res.Duration)

	Render(ctx, w, r, assets, "traces.html", templateFuncs, &ResponseData{
		Target:   target.name,
		Targets:  s.availableTargets,
		Request:  req,
		Response: res,
	})
}

//
//
//

type ResponseData struct {
	Target   string                   `json:"target,omitempty"`
	Targets  []string                 `json:"targets,omitempty"`
	Request  *trctrace.SearchRequest  `json:"request"`
	Response *trctrace.SearchResponse `json:"response"`
}

//
//
//

func parseSearchRequest(ctx context.Context, r *http.Request) (*trctrace.SearchRequest, []string) {
	var (
		tr       = trc.FromContext(ctx)
		isJSON   = strings.Contains(r.Header.Get("content-type"), "application/json")
		urlquery = r.URL.Query()
		n        = parseRange(urlquery.Get("n"), strconv.Atoi, 1, 10, 250)
		min      = parseDefault(urlquery.Get("min"), parseDurationPointer, nil)
		bs       = parseBucketing(urlquery["b"]) // can be nil, no problem
		q        = urlquery.Get("q")
		req      = &trctrace.SearchRequest{}
		prs      = []string{}
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
			Bucketing:   bs,
			MinDuration: min,
			IsFailed:    urlquery.Has("failed"),
			Query:       q,
			Limit:       n,
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

func parseRange[T int](s string, parse func(string) (T, error), min, def, max T) T {
	v, err := parse(s)
	switch {
	case err != nil:
		return def
	case err == nil && v < min:
		return min
	case err == nil && v > max:
		return max
	default:
		return v
	}
}

func parseDurationPointer(s string) (*time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}
