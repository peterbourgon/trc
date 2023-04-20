package trchttp

import (
	"context"
	"net/http"
	"time"

	"github.com/peterbourgon/trc"
)

// NewTraceFunc is a function that produces a new trace in a provided context.
// It's implemented by the trace collector.
type NewTraceFunc func(context.Context, string) (context.Context, trc.Trace)

// CategorizeFunc is a function that produces a category string from an HTTP
// request. It's meant to be provided by callers.
type CategorizeFunc func(*http.Request) string

// Middleware creates a trace for each request served by the handler. The trace
// category is determined by passing the request to the get category function.
// Basic metadata, such as method, path, duration, and response code, is
// recorded in the trace.
//
// This is meant as a convenience for simple use cases. Users who want different
// or more sophisticated behavior should implement their own middlewares.
func Middleware(create NewTraceFunc, categorize CategorizeFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, tr := create(r.Context(), categorize(r))
			defer tr.Finish()

			tr.Tracef("%s %s %s", r.RemoteAddr, r.Method, r.URL.String())

			iw := newInterceptor(w)

			defer func(b time.Time) {
				code := iw.Code()
				sent := iw.Written()
				took := humanizeduration(time.Since(b))
				tr.LazyTracef("HTTP %d, %dB, %s", code, sent, took)
			}(time.Now())

			w = iw
			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		})
	}
}

type interceptor struct {
	http.ResponseWriter

	code int
	n    int
}

func newInterceptor(w http.ResponseWriter) *interceptor {
	return &interceptor{ResponseWriter: w}
}

func (i *interceptor) WriteHeader(code int) {
	if i.code == 0 {
		i.code = code
	}
	i.ResponseWriter.WriteHeader(code)
}

func (i *interceptor) Write(p []byte) (int, error) {
	n, err := i.ResponseWriter.Write(p)
	i.n += n
	return n, err
}

func (i *interceptor) Code() int {
	if i.code == 0 {
		return http.StatusOK
	}
	return i.code
}

func (i *interceptor) Written() int {
	return i.n
}
