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
		begin := time.Now()

		ctx, tr, finish := trc.Region(r.Context(), "ServeHTTP")
		defer finish()

		req, problems := parseSearchRequest(ctx, r)

		urlquery := r.URL.Query()
		isLocal := urlquery.Has("local")
		t := urlquery.Get("t")
		tgt, ok := index[t]
		switch {
		case ok:
			// great
		case !ok && isLocal:
			tgt = cfg.Local
		case !ok && !isLocal:
			tgt = cfg.Default
		}

		tr.Tracef("target=%v", tgt.Name)

		res, err := tgt.Searcher.Search(ctx, req)
		if err != nil {
			tr.Errorf("search error: %v", err)
			problems = append(problems, err.Error())
			res = &trctrace.SearchResponse{} // default
		}

		res.Problems = append(problems, res.Problems...)
		res.Duration = time.Since(begin)

		tr.Tracef("total=%d matched=%d selected=%d duration=%s", res.Total, res.Matched, len(res.Selected), res.Duration)

		Render(ctx, w, r, assets, "traces2.html", templateFuncs, &ResponseData{
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

/*
type Server struct {
	//
}

func NewServerOver(targets ...Target) http.Handler {
	if len(targets) <= 0 {
		panic("no targets")
	}
	s := &Server{}
	m := TargetMiddleware(targets...)(s)
	return m
}

//func NewServer(origin string, s trctrace.Searcher) *Server {
//	return &Server{
//		target: Target{
//			Origin:   origin,
//			Searcher: s,
//		},
//	}
//}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, tr, finish := trc.Region(r.Context(), "trctracehttp.Server.ServeHTTP")
	defer finish()

	begin := time.Now()

	origins := getOrigins(ctx)
	target := getTarget(ctx)
	if target.Searcher == nil {
		http.Error(w, "no targets configured", http.StatusInternalServerError)
		return
	}
	log.Printf("### ServeHTTP target=%v", target)

	req, problems := parseSearchRequest(ctx, r)

	tr.Tracef("starting search")

	res, err := target.Searcher.Search(ctx, req)
	if err != nil {
		tr.Errorf("search error: %v", err)
		problems = append(problems, err.Error())
		res = &trctrace.SearchResponse{Request: req} // default
	}

	tr.Tracef("search complete")

	res.Origins = origins
	res.Problems = append(problems, res.Problems...)
	res.Duration = time.Since(begin)
	res.ServedBy = target.Origin
	if len(res.DataFrom) <= 0 {
		res.DataFrom = append(res.DataFrom, target.Origin)
		for i := range res.Selected {
			res.Selected[i].Origin = target.Origin
		}
	}

	tr.Tracef("total=%d matched=%d selected=%d duration=%s", res.Total, res.Matched, len(res.Selected), res.Duration)

	Render(ctx, w, r, assets, "traces2.html", templateFuncs, res)
}

//
//
//

func TargetMiddleware(targets ...Target) func(http.Handler) http.Handler {
	// TODO: validate targets

	names := make([]string, len(targets))
	index := make(map[string]Target, len(targets))
	for i, t := range targets {
		names[i] = t.Origin
		index[t.Origin] = t
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			tr := trc.FromContext(ctx)

			origin := r.URL.Query().Get("origin")
			tr.Tracef("query origin %q", origin)

			target, ok := index[origin]
			if !ok {
				target = targets[0]
			}

			ctx = putTarget(ctx, target)
			ctx = putOrigins(ctx, names)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

//
//
//

func putTarget(ctx context.Context, t Target) context.Context {
	return context.WithValue(ctx, targetContextKey{}, t)
}

func getTarget(ctx context.Context) Target {
	t, ok := ctx.Value(targetContextKey{}).(Target)
	if ok {
		return t
	}
	return Target{}
}

func putOrigins(ctx context.Context, origins []string) context.Context {
	return context.WithValue(ctx, originsContextKey{}, origins)
}

func getOrigins(ctx context.Context) []string {
	origins := ctx.Value(originsContextKey{}).([]string)
	return origins
}

func putSearcher(ctx context.Context, s trctrace.Searcher) context.Context {
	return context.WithValue(ctx, searcherContextKey{}, s)
}

func getSearcher(ctx context.Context, def trctrace.Searcher) trctrace.Searcher {
	if s, ok := ctx.Value(searcherContextKey{}).(trctrace.Searcher); ok {
		log.Printf("### getSearcher got Searcher")
		return s
	}
	log.Printf("### getSearcher not get Searcher")
	return def
}

func putTargetOrigins(ctx context.Context, target Target, origins []string) context.Context {
	return context.WithValue(ctx, targetOriginsContextKey{}, targetOriginsContextVal{
		Target:  target,
		Origins: origins,
	})
}

func getTargetOrigins(ctx context.Context) (Target, []string, bool) {
	to, ok := ctx.Value(targetOriginsContextKey{}).(targetOriginsContextVal)
	if !ok {
		return Target{}, nil, false
	}
	return to.Target, to.Origins, true
}

type (
	targetContextKey        struct{}
	originsContextKey       struct{}
	searcherContextKey      struct{}
	targetOriginsContextKey struct{}
	targetOriginsContextVal struct {
		Target  Target
		Origins []string
	}
)
*/

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
		problems = append(problems, err.Error())
		tr.Errorf(err.Error())
	}

	tr.Tracef("parsed search request %s", req)

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

func firstOf(strs ...string) string {
	for _, s := range strs {
		if s != "" {
			return s
		}
	}
	return ""
}
