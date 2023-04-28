package trchttp

import (
	"context"
	"net/http"
	"time"

	"github.com/peterbourgon/trc"
)

// CreateFunc is a function that produces a new trace in a provided context.
// Typically, callers should use the NewTrace method of a collector.
type CreateFunc func(context.Context, string) (context.Context, trc.Trace)

// CategorizeFunc is a function that produces a category string from an HTTP
// request. The total number of categories in a given program is expected to be
// relatively small, approximately O(10) or fewer.
type CategorizeFunc func(*http.Request) string

// Middleware decorates an HTTP handler by creating a trace for each request.
// The trace category is determined by the categorize function. Basic metadata,
// such as method, path, duration, and response code, is recorded in the trace.
//
// This is meant as a convenience for simple use cases. Users who want different
// or more sophisticated behavior should implement their own middlewares.
func Middleware(create CreateFunc, categorize CategorizeFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, tr := create(r.Context(), categorize(r))
			defer tr.Finish()

			tr.Tracef("%s %s %s", r.RemoteAddr, r.Method, r.URL.String())

			iw := newInterceptor(w)

			defer func(b time.Time) {
				code := iw.Code()
				sent := humanizebytes(iw.Written())
				took := humanizeduration(time.Since(b))
				tr.LazyTracef("HTTP %d, %s, %s", code, sent, took)
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
