package trchttp

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/unixtransport"
)

// Server implements a JSON API and HTML UI over a [trc.Searcher].
type Server struct {
	searcher Searcher
	client   HTTPClient
}

// Searcher describes anything that can serve trace search requests. It's a
// consumer contract for the server. The typical implementation is
// [trc.Collector].
type Searcher interface {
	Search(context.Context, *trc.SearchRequest) (*trc.SearchResponse, error)
}

// NewServer returns a server wrapping the given searcher.
func NewServer(searcher Searcher) *Server {
	var transport http.Transport
	unixtransport.Register(&transport)
	client := &http.Client{Transport: &transport}

	return &Server{
		searcher: searcher,
		client:   client,
	}
}

// ServeHTTP implements http.Handler, serving either a JSON API or an HTML web
// UI based on the request's Accept header. Callers can force the JSON API
// response by providing a `json` query parameter.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	begin := time.Now()

	ctx, tr, finish := trc.Region(r.Context(), "trchttp.Server.ServeHTTP")
	defer finish()

	req := parseSearchRequest(ctx, r)
	res, err := s.searcher.Search(ctx, req)
	if err != nil {
		tr.Errorf("search error: %v", err)
		res = &trc.SearchResponse{} // default
		res.Problems = append(res.Problems, err.Error())
	}

	res.Duration = time.Since(begin)

	tr.Tracef("total=%d matched=%d selected=%d duration=%s", res.Total, res.Matched, len(res.Selected), res.Duration)

	renderResponse(ctx, w, r, assets, "traces.html", templateFuncs, &SearchResponse{
		Request:  req,
		Response: res,
	})
}

//
//
//

// SearchResponse is what the server returns to search requests.
type SearchResponse struct {
	Request  *trc.SearchRequest  `json:"request"`
	Response *trc.SearchResponse `json:"response"`
}

func (res *SearchResponse) Problems() []string {
	var s []string
	if p := res.Request.Problems; len(p) > 0 {
		s = append(s, p...)
	}
	if p := res.Response.Problems; len(p) > 0 {
		s = append(s, p...)
	}
	return s
}

func parseSearchRequest(ctx context.Context, r *http.Request) *trc.SearchRequest {
	var (
		tr       = trc.FromContext(ctx)
		isJSON   = strings.Contains(r.Header.Get("content-type"), "application/json")
		urlquery = r.URL.Query()
		n        = parseRange(urlquery.Get("n"), strconv.Atoi, 1, 10, 250)
		min      = parseDefault(urlquery.Get("min"), parseDurationPointer, nil)
		bs       = parseBucketing(urlquery["b"]) // can be nil, no problem
		q        = urlquery.Get("q")
	)

	var req trc.SearchRequest
	switch {
	case isJSON:
		tr.Tracef("parsing search request from JSON request body")
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			req.Problems = append(req.Problems, err.Error())
			tr.Errorf("parse JSON request body: %v", err)
		}

	default:
		tr.Tracef("parsing search request from URL %q", urlquery.Encode())
		req = trc.SearchRequest{
			IDs:         urlquery["id"],
			Category:    urlquery.Get("category"),
			IsActive:    urlquery.Has("active"),
			Bucketing:   bs,
			MinDuration: min,
			IsErrored:   urlquery.Has("errored"),
			Query:       q,
			Limit:       n,
		}
	}

	req.Normalize(ctx)

	tr.Tracef("parsed search request %s", req)

	return &req
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
