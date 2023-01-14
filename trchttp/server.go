package trchttp

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
)

// ResponseData is what the server returns to requests.
type ResponseData struct {
	Remotes  []string            `json:"remotes,omitempty"`
	Request  *trc.SearchRequest  `json:"request"`
	Response *trc.SearchResponse `json:"response"`
}

// Server wraps a searcher and provides a JSON API and HTML UI for queries.
// The API can be consumed by the client type also in this package, to allow
// remote searching of traces.
type Server struct {
	searcher trc.Searcher
}

// NewServer returns a server wrapping the given searcher.
func NewServer(searcher trc.Searcher) *Server {
	return &Server{
		searcher: searcher,
	}
}

// ServeHTTP implements http.Handler, serving either a JSON API or an HTML web
// UI based on the request's Accept header. Callers can force the JSON API
// response by providing a `json` query parameter.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, tr, finish := trc.Region(r.Context(), "trchttp.Server.ServeHTTP")
	defer finish()

	var (
		begin    = time.Now()
		req, prs = parseSearchRequest(ctx, r)
		urlquery = r.URL.Query()
		remotes  = urlquery["r"]
		searcher = s.searcher
	)

	if len(remotes) > 0 {
		var multi trc.MultiSearcher
		for _, r := range remotes {
			multi = append(multi, NewClient(http.DefaultClient, r))
		}
		tr.Tracef("searching remotes %v", remotes)
		searcher = multi
	}

	res, err := searcher.Search(ctx, req)
	if err != nil {
		tr.Errorf("search error: %v", err)
		prs = append(prs, err.Error())
		res = &trc.SearchResponse{} // default
	}

	res.Problems = append(prs, res.Problems...)
	res.Duration = time.Since(begin)

	tr.Tracef("total=%d matched=%d selected=%d duration=%s", res.Total, res.Matched, len(res.Selected), res.Duration)

	renderResponse(ctx, w, r, assets, "traces.html", templateFuncs, &ResponseData{
		Remotes:  remotes,
		Request:  req,
		Response: res,
	})
}

//
//
//

func parseSearchRequest(ctx context.Context, r *http.Request) (*trc.SearchRequest, []string) {
	var (
		tr       = trc.FromContext(ctx)
		isJSON   = strings.Contains(r.Header.Get("content-type"), "application/json")
		urlquery = r.URL.Query()
		n        = parseRange(urlquery.Get("n"), strconv.Atoi, 1, 10, 250)
		min      = parseDefault(urlquery.Get("min"), parseDurationPointer, nil)
		bs       = parseBucketing(urlquery["b"]) // can be nil, no problem
		q        = urlquery.Get("q")
		req      = &trc.SearchRequest{}
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
		req = &trc.SearchRequest{
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

	for _, problem := range req.Normalize() {
		prs = append(prs, problem)
		tr.Tracef("normalize search request: %s", problem)
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