// Package eztrc is how most programs are expected to use traces.
//
// Typically, each instance of a program will maintain a single set of traces,
// grouped by category. This package provides a singleton (process global) trace
// collector, and helper functions that interact with that collector, to
// facilitate this common use case.
//
// The most typical usage in application code is producing a trace event, e.g.
//
//	eztrc.Tracef(ctx, "my trace event %d", i)
//
// See the examples directory for more information.
package eztrc

import (
	"context"
	"net/http"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
)

// Region is an alias for [trc.Region].
func Region(ctx context.Context, format string, args ...any) (context.Context, trc.Trace, func()) {
	return trc.Region(ctx, format, args...)
}

// Prefix is an alias for [trc.Prefix].
func Prefix(ctx context.Context, format string, args ...any) (context.Context, trc.Trace) {
	return trc.Prefix(ctx, format, args...)
}

// FromContext is an alias for [trc.FromContext].
func FromContext(ctx context.Context) trc.Trace {
	return trc.FromContext(ctx)
}

// MaybeFromContext is an alias for [trc.MaybeFromContext].
func MaybeFromContext(ctx context.Context) (trc.Trace, bool) {
	return trc.MaybeFromContext(ctx)
}

var collector = trc.NewDefaultCollector()

// Collector returns the singleton trace collector in the package.
func Collector() *trc.Collector {
	return collector
}

// Handler returns an HTTP handler serving the singleton trace collector.
func Handler() http.Handler {
	return trchttp.NewServer(collector)
}

// Middleware returns an HTTP middleware that adds a trace to the singleton
// trace collector for each received request. The category is determined by the
// categorize function.
func Middleware(categorize func(*http.Request) string) func(http.Handler) http.Handler {
	return trchttp.Middleware(collector.NewTrace, categorize)
}

// New creates a new trace in the singleton trace collector, injects that trace
// into the given context, and returns a new derived context containing the
// trace, as well as the trace itself.
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
