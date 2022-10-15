package trchttp

import (
	"net/http"
	"time"

	"github.com/peterbourgon/trc"
)

// Middleware decorates an HTTP handler, creating a trace in the collector for
// each incoming request. The trace category is determined by passing the HTTP
// request to the getCategory function. Basic metadata, like method, path,
// duration, and response code, is recorded.
//
// This is meant primarily as an example, and convenience function for simple
// use cases. Users who want different or more sophisticated behavior should
// implement their own middlewares.
func Middleware(c *trc.Collector, getCategory func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, tr := c.GetOrCreateTrace(r.Context(), getCategory(r))
			defer tr.Finish()

			tr.Tracef("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)

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
