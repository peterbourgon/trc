package eztrc

import (
	"context"
	"net/http"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
)

// Region is an alias for [trc.Region].
var Region = trc.Region

// Prefix is an alias for [trc.Prefix].
var Prefix = trc.Prefix

var collector = trc.NewDefaultCollector()

// Collector returns the singleton trace collector used by the package.
func Collector() *trc.Collector {
	return collector
}

// Handler returns an HTTP handler serving from the singleton trace collector.
func Handler() http.Handler {
	return trchttp.NewServer(collector)
}

// Middleware returns an HTTP middleware that adds a trace to the singleton
// trace collector for each received request. The category is determined by the
// categorize function.
func Middleware(categorize trchttp.CategorizeFunc) func(http.Handler) http.Handler {
	return trchttp.Middleware(collector.NewTrace, categorize)
}

// New creates a new trace in the package global trace collector, injects it
// into the given context, and returns a new derived context containing the
// trace, and the trace itself.
func New(ctx context.Context, category string) (context.Context, trc.Trace) {
	return collector.NewTrace(ctx, category)
}

// Get is really GetOrCreate: if a trace exists in the context, Get adds an
// event reflecting the provided category and returns the context and the trace
// directly. Otherwise, Get creates a new trace via [New].
func Get(ctx context.Context, category string) (context.Context, trc.Trace) {
	if tr, ok := trc.MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}
	return New(ctx, category)
}

// Tracef adds a new event to the trace in the context.
// Arguments are evaluated immediately.
func Tracef(ctx context.Context, format string, args ...any) {
	trc.FromContext(ctx).Tracef(format, args...)
}

// LazyTracef adds a new event to the trace in the context.
// Arguments are evaluated lazily.
func LazyTracef(ctx context.Context, format string, args ...any) {
	trc.FromContext(ctx).LazyTracef(format, args...)
}

// Errorf adds a new error event to the trace in the context.
// Arguments are evaluated immediately.
func Errorf(ctx context.Context, format string, args ...any) {
	trc.FromContext(ctx).Errorf(format, args...)
}

// LazyErrorf adds a new error event to the trace in the context.
// Arguments are evaluated lazily.
func LazyErrorf(ctx context.Context, format string, args ...any) {
	trc.FromContext(ctx).LazyErrorf(format, args...)
}
