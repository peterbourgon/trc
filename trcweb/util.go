package trcweb

import (
	"net/http"
	"sort"
	"time"

	"github.com/peterbourgon/trc"
)

const maxRequestBodySizeBytes = 1 * 1024 * 1024 // 1MB

func parseFilter(r *http.Request) trc.Filter {
	urlquery := r.URL.Query()
	return trc.Filter{
		Sources:     urlquery["source"],
		IDs:         urlquery["id"],
		Category:    urlquery.Get("category"),
		IsActive:    urlquery.Has("active"),
		IsFinished:  urlquery.Has("finished"),
		MinDuration: parseDefault(urlquery.Get("min"), parseDurationPointer, nil),
		IsSuccess:   urlquery.Has("success"),
		IsErrored:   urlquery.Has("errored"),
		Query:       urlquery.Get("q"),
	}
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

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

var _ HTTPClient = (*http.Client)(nil)
