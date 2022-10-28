package eztrc

import (
	"context"
	"fmt"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
)

var collector = trc.NewTraceCollector()

// Collector returns the default global collector used by package eztrc.
func Collector() *trc.TraceCollector { return collector }

// TracesHandler is an HTTP handler that serves basic HTML and JSON
// representations of the traces in the default collector.
var TracesHandler = trchttp.TracesHandler(collector)

// Create a new trace with the given category in the default collector. Return a
// context containing that trace, and a function that should be called when the
// trace is finished.
//
// Create will always produce a new trace. If a trace already exists in the
// context, the new trace will begin and end within the scope of the
// pre-existing trace.
func Create(ctx context.Context, category string) (_ context.Context, finish func()) {
	ctx, tr := collector.NewTrace(ctx, category)
	tr.Tracef("> %s", category)
	return ctx, func() { tr.Tracef("< %s", category); tr.Finish() }
}

// Get a trace from the context. If a trace doesn't exist in the context, a new
// trace will be created on the background context in the undefined category.
func Get(ctx context.Context) trc.Trace {
	return trc.FromContext(ctx)
}

// GetOrCreate will return the existing trace in the context, if it exists.
// Otherwise, it will create and return a new trace. If there is a pre-existing
// trace, it's annotated with the given category when this function is called,
// and again when finish is invoked.
func GetOrCreate(ctx context.Context, category string) (_ context.Context, finish func()) {
	if tr, ok := trc.MaybeFromContext(ctx); ok {
		tr.Tracef("(> %s)", category)
		return ctx, func() { tr.Tracef("(< %s)", category) }
	}
	return Create(ctx, category)
}

// Tracef is a convenience function that calls trc.Tracef.
func Tracef(ctx context.Context, format string, args ...interface{}) {
	trc.Tracef(ctx, format, args...)
}

// LazyTracef is a convenience function that calls trc.LazyTracef. Arguments are
// stored for an indeterminate length of time and are evaluated from multiple
// goroutines, so they must be safe for concurrent access.
func LazyTracef(ctx context.Context, format string, args ...interface{}) {
	trc.LazyTracef(ctx, format, args...)
}

// Errorf is a convenience function that calls trc.Errorf.
func Errorf(ctx context.Context, format string, args ...interface{}) {
	trc.Errorf(ctx, format, args...)
}

// LazyErrorf is a convenience function that calls trc.LazyErrorf. Arguments are
// stored for an indeterminate length of time and are evaluated from multiple
// goroutines, so they must be safe for concurrent access.
func LazyErrorf(ctx context.Context, format string, args ...interface{}) {
	trc.LazyErrorf(ctx, format, args...)
}

func CopyTrace(ctx context.Context, newCategory string) error {
	tr, ok := trc.MaybeFromContext(ctx)
	if !ok {
		return fmt.Errorf("no trace in context")
	}

	Tracef(ctx, "CopyTrace existing trace has %d events", len(tr.Events()))

	return collector.CopyTrace(tr, newCategory)
}
