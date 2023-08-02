// Package eztrc provides an easy-to-use API for typical use cases. Most
// applications should only need to import this package.
//
// A global [Collector] maintains recent traces, grouped by category, in memory.
// Applications should create a new trace in that collector for each e.g.
// request they process. The [Middleware] helper provides a simple decorator for
// HTTP handlers which does this work.
//
// Traces are always created within a context. Application code should "log" by
// adding events to the trace in the context. Helpers like [Get] can retrieve
// the current trace from a context, and helpers like [Tracef] can log events
// directly to the trace in a context.
//
// Traces may be viewed, queried, etc. via the [Handler], which provides an HTTP
// interface to the global collector. Applications should install this handler
// to their internal or debug HTTP server, on a route of their choice, e.g.
// /traces.
//
// See the [examples] for more complete example applications.
//
// [examples]: https://github.com/peterbourgon/trc/tree/main/_examples
package eztrc

import (
	"context"
	"net/http"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcsrc"
	"github.com/peterbourgon/trc/trcweb"
)

var source = trcsrc.NewDefaultCollector()

var handler = trcweb.NewServer(source)

// Source returns the global [trcsrc.NewDefaultSource].
func Source() *trcsrc.Collector {
	return source
}

// Handler returns a [trcweb.Server] for the global trace collector.
func Handler() http.Handler {
	return handler
}

// Middleware returns a [trcweb.Middleware] which adds a trace to the global
// trace collector for each received request. The category is determined by the
// provided categorize function.
func Middleware(categorize func(*http.Request) string) func(http.Handler) http.Handler {
	return trcweb.Middleware(source.NewTrace, categorize)
}

// New creates a new trace in the global trace collector, injects that trace
// into the given context, and returns a derived context containing the new
// trace, as well as the new trace itself.
func New(ctx context.Context, category string) (context.Context, trc.Trace) {
	return source.NewTrace(ctx, category)
}

// Region calls [trc.Region].
func Region(ctx context.Context, name string) (context.Context, trc.Trace, func()) {
	return trc.Region(ctx, name)
}

// Prefix calls [trc.Prefix].
func Prefix(ctx context.Context, format string, args ...any) (context.Context, trc.Trace) {
	return trc.Prefix(ctx, format, args...)
}

// Get calls [trc.Get].
func Get(ctx context.Context) trc.Trace {
	return trc.Get(ctx)
}

// MaybeGet calls [trc.MaybeGet].
func MaybeGet(ctx context.Context) (trc.Trace, bool) {
	return trc.MaybeGet(ctx)
}

// Tracef adds a new normal event to the trace in the context.
// Arguments are evaluated immediately.
func Tracef(ctx context.Context, format string, args ...any) {
	trc.Get(ctx).Tracef(format, args...)
}

// LazyTracef adds a new normal event to the trace in the context.
// Arguments are evaluated lazily.
func LazyTracef(ctx context.Context, format string, args ...any) {
	trc.Get(ctx).LazyTracef(format, args...)
}

// Errorf adds a new error event to the trace in the context.
// Arguments are evaluated immediately.
func Errorf(ctx context.Context, format string, args ...any) {
	trc.Get(ctx).Errorf(format, args...)
}

// LazyErrorf adds a new error event to the trace in the context.
// Arguments are evaluated lazily.
func LazyErrorf(ctx context.Context, format string, args ...any) {
	trc.Get(ctx).LazyErrorf(format, args...)
}
