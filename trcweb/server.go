package trcweb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcsrc"
)

type Server struct {
	sel trcsrc.Selecter
}

func NewServer(sel trcsrc.Selecter) *Server {
	return &Server{
		sel: sel,
	}
}

type SelectData struct {
	Request  trcsrc.SelectRequest  `json:"request"`
	Response trcsrc.SelectResponse `json:"response"`
	Problems []error               `json:"problems,omitempty"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		ctx    = r.Context()
		tr     = trc.Get(ctx)
		isJSON = strings.Contains(r.Header.Get("content-type"), "application/json")
		data   = SelectData{}
	)

	switch {
	case isJSON:
		body := http.MaxBytesReader(w, r.Body, maxRequestBodySizeBytes)
		if err := json.NewDecoder(body).Decode(&data.Request); err != nil {
			tr.Errorf("decode JSON request failed, using defaults (%v)", err)
			data.Problems = append(data.Problems, fmt.Errorf("decode JSON request: %w", err))
		}

	default:
		urlquery := r.URL.Query()
		data.Request = trcsrc.SelectRequest{
			Bucketing: parseBucketing(urlquery["b"]), // nil is OK
			Filter: trcsrc.Filter{
				Sources:     urlquery["source"],
				IDs:         urlquery["id"],
				Category:    urlquery.Get("category"),
				IsActive:    urlquery.Has("active"),
				IsFinished:  urlquery.Has("finished"),
				MinDuration: parseDefault(urlquery.Get("min"), parseDurationPointer, nil),
				IsSuccess:   urlquery.Has("success"),
				IsErrored:   urlquery.Has("errored"),
				Query:       urlquery.Get("q"),
			},
			Limit: parseRange(urlquery.Get("n"), strconv.Atoi, trcsrc.SelectRequestLimitMin, trcsrc.SelectRequestLimitDefault, trcsrc.SelectRequestLimitMax),
		}
	}

	data.Problems = append(data.Problems, data.Request.Normalize()...)

	res, err := s.sel.Select(ctx, &data.Request)
	if err != nil {
		data.Problems = append(data.Problems, fmt.Errorf("execute select request: %w", err))
	} else {
		data.Response = *res
	}

	for _, problem := range data.Response.Problems {
		data.Problems = append(data.Problems, fmt.Errorf("response: %s", problem))
	}

	renderResponse(ctx, w, r, assets, "traces.html", nil, data)
}

const maxRequestBodySizeBytes = 1 * 1024 * 1024 // 1MB

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
