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
	"github.com/peterbourgon/trc/trcstore"
	"golang.org/x/exp/slices"
)

// Server implements a JSON API and HTML UI over a [trcstore.Searcher].
type Server struct {
	searcher trcstore.Searcher
}

// NewServer returns a server wrapping the given searcher.
func NewServer(searcher trcstore.Searcher) *Server {
	return &Server{
		searcher: searcher,
	}
}

// ServeHTTP implements [http.Handler], serving a JSON API or an HTML UI, based
// on the request's Accept header. Callers can force the JSON API response by
// providing a `json` query parameter.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	begin := time.Now()

	ctx, tr, finish := trc.Region(r.Context(), "trchttp.Server.ServeHTTP")
	defer finish()

	req := parseSearchRequest(ctx, r)
	res, err := s.searcher.Search(ctx, req)
	if err != nil {
		tr.LazyErrorf("search error: %v", err)
		res = &trcstore.SearchResponse{} // default
		res.Problems = append(res.Problems, err.Error())
	}

	res.Duration = time.Since(begin)

	tr.LazyTracef("total=%d matched=%d selected=%d duration=%s", res.Total, res.Matched, len(res.Selected), res.Duration)

	renderResponse(ctx, w, r, assets, "traces.html", templateFuncs, &SearchResponseData{
		Request:  req,
		Response: res,
	})
}

//
//
//

// SearchResponseData is what the server returns to search requests. It
// basically just couples the request and response into a single data structure,
// allowing e.g. templates to access information from both.
type SearchResponseData struct {
	Request  *trcstore.SearchRequest  `json:"req"`
	Response *trcstore.SearchResponse `json:"res"`
}

// Problems returns the aggregate problems from the request and response.
func (d *SearchResponseData) Problems() []string {
	var s []string
	if p := d.Request.Problems; len(p) > 0 {
		s = append(s, p...)
	}
	if p := d.Response.Problems; len(p) > 0 {
		s = append(s, p...)
	}
	sort.Strings(s)
	s = slices.Compact(s)
	return s
}

func parseSearchRequest(ctx context.Context, r *http.Request) *trcstore.SearchRequest {
	var (
		tr     = trc.Get(ctx)
		isJSON = strings.Contains(r.Header.Get("content-type"), "application/json")
	)

	var req trcstore.SearchRequest
	switch {
	case isJSON:
		tr.LazyTracef("parsing search request from JSON request body")
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			req.Problems = append(req.Problems, err.Error())
			tr.LazyErrorf("parse JSON request body: %v", err)
		}

	default:
		var (
			urlquery = r.URL.Query()
			n        = parseRange(urlquery.Get("n"), strconv.Atoi, 1, 10, 250) // these limits are from trcstore
			min      = parseDefault(urlquery.Get("min"), parseDurationPointer, nil)
			bs       = parseBucketing(urlquery["b"]) // can be nil, no problem
			q        = urlquery.Get("q")
		)
		tr.LazyTracef("parsing search request from URL %q", urlquery.Encode())
		req = trcstore.SearchRequest{
			Sources:     urlquery["source"],
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

	tr.LazyTracef("parsed search request %s", req)

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

func parseDurationPointer(s string) (*time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, err
	}
	return &d, nil
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
