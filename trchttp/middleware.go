package trchttp

import (
	"context"
	"net/http"
	"time"

	"github.com/peterbourgon/trc"
)

type NewTracer interface {
	NewTrace(context.Context, string) (context.Context, trc.Trace)
}

// Middleware decorates an HTTP handler and creates a trace for each incoming
// request via the provided NewTracer. The category is determined by passing the
// HTTP request to the getCategory function. Basic request metadata, like
// method, path, duration, and response code, is recorded in the trace.
//
// This is meant as a convenience function for basic use cases. Users who want
// different or more sophisticated behavior should implement their own
// middlewares.
//
// TODO: use a config, default getCategory
func Middleware(nt NewTracer, getCategory func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, tr := nt.NewTrace(r.Context(), getCategory(r))
			defer tr.Finish()

			tr.Tracef("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)

			for k, vs := range r.Header {
				for _, v := range vs {
					tr.Tracef("> %s: %v", k, v)
				}
			}

			iw := newInterceptor(w)
			defer func(b time.Time) {
				code := iw.Code()
				sent := iw.Written()
				took := humanize(time.Since(b))
				tr.Tracef("HTTP %d, %dB, %s", code, sent, took)
			}(time.Now())

			w = iw
			r = r.WithContext(ctx)
			next.ServeHTTP(w, r)
		})
	}
}

//
//
//

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
