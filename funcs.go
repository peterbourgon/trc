package trc

import (
	"context"
	"time"
)

type traceContextKey struct{}

var traceContextVal traceContextKey

// NewTrace creates a new trace with the given category, and injects it to the
// given context. If the context already contained a trace, it becomes
// "shadowed" by the new one.
func NewTrace(ctx context.Context, category string) (context.Context, Trace) {
	tr := NewCoreTrace(category)
	return ToContext(ctx, tr), tr
}

// FromContext returns the trace in the context, if it exists. If not, an orphan
// trace is created and returned.
//
// Orphan traces are usually bugs, so this function is meant as a convenience
// for situations where a context is reliably known to contain a trace.
func FromContext(ctx context.Context) Trace {
	if tr, ok := MaybeFromContext(ctx); ok {
		return tr
	}

	return NewCoreTrace("(orphan)")
}

// MaybeFromContext returns the trace in the context, if it exists.
func MaybeFromContext(ctx context.Context) (Trace, bool) {
	tr, ok := ctx.Value(traceContextVal).(Trace)
	return tr, ok
}

// ToContext derives a new context from the given context, containing the given
// trace. If the context already contained a trace, it becomes "shadowed" by the
// new trace.
func ToContext(ctx context.Context, tr Trace) context.Context {
	return context.WithValue(ctx, traceContextVal, tr)
}

// Tracef adds a new event to the trace in the context (via FromContext).
// The arguments are evaulated immediately.
func Tracef(ctx context.Context, format string, args ...any) {
	FromContext(ctx).Tracef(format, args...)
}

// LazyTracef adds a new event to the trace in the context (via FromContext).
// Arguments are stored for an indeterminate length of time and are evaluated
// from multiple goroutines, so they must be safe for concurrent access.
func LazyTracef(ctx context.Context, format string, args ...any) {
	FromContext(ctx).LazyTracef(format, args...)
}

// Errorf adds a new event to the trace in the context (via FromContext), and
// marks the trace as errored. The arguments are evaluted immediately.
func Errorf(ctx context.Context, format string, args ...any) {
	FromContext(ctx).Errorf(format, args...)
}

// LazyErrorf adds a new event to the trace in the context (via FromContext),
// and marks the trace as errored. Arguments are stored for an indeterminate
// length of time and are evaluated from multiple goroutines, so they must be
// safe for concurrent access.
func LazyErrorf(ctx context.Context, format string, args ...any) {
	FromContext(ctx).LazyErrorf(format, args...)
}

// Region is a convenience function for more detailed tracing of regions of
// code, typically functions. Typical usage is as follows.
//
//	func foo(ctx context.Context, id int) {
//		ctx, tr, finish := trc.Region(ctx, "foo %d", id)
//		defer finish()
//		...
//	}
//
// Region produces hierarchical trace events as follows.
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
// Region may incur non-negligable costs to performance, and is meant to be used
// deliberately and sparingly. It explicitly should not be applied "by default"
// to code via e.g. code generation.
func Region(ctx context.Context, format string, args ...any) (context.Context, Trace, func()) {
	begin := time.Now()
	inputTrace := FromContext(ctx)
	outputTrace := PrefixTrace(inputTrace, "· ")
	outputContext := ToContext(ctx, outputTrace)

	inputTrace.LazyTracef("→ "+format, args...)
	finish := func() {
		took := time.Since(begin)
		inputTrace.LazyTracef("← "+format+" [%s]", append(args, took.String())...)
	}

	return outputContext, outputTrace, finish
}
