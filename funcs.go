package trc

import (
	"context"
	"fmt"
	"runtime/trace"
	"time"
)

type traceContextKey struct{}

var traceContextVal traceContextKey

// NewTrace creates a new trace with the provided category, and injects it to
// the context. If the context already contained a trace, it becomes "shadowed"
// by the new one.
func NewTrace(ctx context.Context, category string) (context.Context, Trace) {
	tr := newCoreTrace(category)
	return ToContext(ctx, tr), tr
}

// FromContext returns the trace in the context, if it exists. If not, an orphan
// trace is created and returned. Note that orphan traces are usually bugs, and
// so This function is meant as a convenience for situations where a context is
// reliably known to contain a trace.
func FromContext(ctx context.Context) Trace {
	if tr, ok := MaybeFromContext(ctx); ok {
		return tr
	}

	return newCoreTrace("(orphan)")
}

// MaybeFromContext returns the trace in the context, if it exists.
func MaybeFromContext(ctx context.Context) (Trace, bool) {
	tr, ok := ctx.Value(traceContextVal).(Trace)
	return tr, ok
}

// ToContext injects the trace into the context via [context.WithValue]. If the
// context already contained a trace, it becomes shadowed by the new trace.
func ToContext(ctx context.Context, tr Trace) context.Context {
	return context.WithValue(ctx, traceContextVal, tr)
}

// Tracef adds a new event to the trace in the context (via FromContext).
// Arguments are evaulated immediately.
func Tracef(ctx context.Context, format string, args ...any) {
	FromContext(ctx).Tracef(format, args...)
}

// LazyTracef adds a new event to the trace in the context (via FromContext).
// Arguments are evaluated lazily, when the event is read by a client. Arguments
// may be stored for an indeterminste amount of time, and may be evaluated by
// multiple goroutines, and therefore must be safe for concurrent access.
func LazyTracef(ctx context.Context, format string, args ...any) {
	FromContext(ctx).LazyTracef(format, args...)
}

// Errorf adds a new event to the trace in the context (via FromContext), and
// marks the trace as errored. Arguments are evaluted immediately.
func Errorf(ctx context.Context, format string, args ...any) {
	FromContext(ctx).Errorf(format, args...)
}

// LazyErrorf adds a new event to the trace in the context (via FromContext),
// and marks the trace as errored. Arguments are evaluated lazily, when the
// event is read by a client. Arguments may be stored for an indeterminste
// amount of time, and may be evaluated by multiple goroutines, and therefore
// must be safe for concurrent access.
func LazyErrorf(ctx context.Context, format string, args ...any) {
	FromContext(ctx).LazyErrorf(format, args...)
}

// Region is a convenience function that provides more detailed tracing of
// regions of code, usually functions. It also produces a standard library
// runtime/trace region, which can be useful when e.g. analyzing program
// execution with `go tool trace`.
//
// Typical usage is as follows.
//
//	func foo(ctx context.Context, id int) {
//	    ctx, tr, finish := trc.Region(ctx, "foo %d", id)
//	    defer finish()
//	    ...
//	}
//
// This produces hierarchical trace events as follows.
//
//	→ foo 42
//	· trace event in foo
//	· another event in foo
//	· → bar
//	· · something in bar
//	· ← bar [1.23ms]
//	· final event in foo
//	← foo 42 [2.34ms]
//
// Region can significantly impact performance, use it sparingly.
func Region(ctx context.Context, format string, args ...any) (context.Context, Trace, func()) {
	begin := time.Now()
	inputTrace := FromContext(ctx)
	outputTrace := Prefix(inputTrace, "· ")
	outputContext := ToContext(ctx, outputTrace)
	region := trace.StartRegion(ctx, fmt.Sprintf(format, args...))

	inputTrace.LazyTracef("→ "+format, args...)
	finish := func() {
		took := time.Since(begin)
		inputTrace.LazyTracef("← "+format+" [%s]", append(args, took.String())...)
		region.End()
	}

	return outputContext, outputTrace, finish
}
