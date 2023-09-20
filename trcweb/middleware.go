package trcweb

import (
	"context"
	"net/http"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcutil"
)

// Middleware decorates an HTTP handler by creating a trace for each request via
// the constructor function. The trace category is determined by the categorize
// function. Basic metadata, such as method, path, duration, and response code,
// is recorded in the trace.
//
// This is meant as a convenience for simple use cases. Users who want different
// or more sophisticated behavior should implement their own middlewares.
func Middleware(
	constructor func(context.Context, string) (context.Context, trc.Trace),
	categorize func(*http.Request) string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, tr := constructor(r.Context(), categorize(r))
			defer tr.Finish()

			tr.LazyTracef("%s %s %s", r.RemoteAddr, r.Method, r.URL.String())

			for _, header := range []string{"User-Agent", "Accept", "Content-Type", "X-TRC-ID"} {
				if val := r.Header.Get(header); val != "" {
					tr.LazyTracef("%s: %s", header, val)
				}
			}

			iw := newInterceptor(w)

			defer func(b time.Time) {
				code := iw.Code()
				sent := trcutil.HumanizeBytes(iw.Written())
				took := trcutil.HumanizeDuration(time.Since(b))
				tr.LazyTracef("HTTP %d, %s, %s", code, sent, took)
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

	flush func()
	code  int
	n     int
}

func newInterceptor(w http.ResponseWriter) *interceptor {
	flush := func() {}
	if f, ok := w.(http.Flusher); ok {
		flush = f.Flush
	}
	return &interceptor{ResponseWriter: w, flush: flush}
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

func (i *interceptor) Flush() {
	i.flush()
}
