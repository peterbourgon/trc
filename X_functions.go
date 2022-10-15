package trc

import "context"

// NewTrace creates a new trace with the given category, and injects it to the
// given context. If the context already contained a trace, it becomes
// "shadowed" by the new one.
func NewTrace(ctx context.Context, category string) (context.Context, Trace) {
	tr := NewTraceCore(category)
	return ToContext(ctx, tr), tr
}

// FromContext returns the trace in the context, if it exists. Otherwise, an
// orphan trace is created and returned.
//
// This function is meant as a convenience for situations where a context is
// reliably known to contain a trace. Otherwise, prefer MaybeFromContext.
func FromContext(ctx context.Context) Trace {
	if tr, ok := MaybeFromContext(ctx); ok {
		return tr
	}

	return NewTraceCore("(orphan)")
}

// MaybeFromContext returns the trace in the context, if it exists.
func MaybeFromContext(ctx context.Context) (Trace, bool) {
	tr, ok := ctx.Value(traceContextVal).(Trace)
	return tr, ok
}

// ToContext derives a new context from the given context containing the given
// trace. If the context already contained a trace, it becomes "shadowed" by the
// new trace.
func ToContext(ctx context.Context, tr Trace) context.Context {
	return context.WithValue(ctx, traceContextVal, tr)
}

// Tracef adds a new event to the trace in the context (via FromContext).
// The arguments are evaulated immediately.
func Tracef(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).Tracef(format, args...)
}

// LazyTracef adds a new event to the trace in the context (via FromContext).
// Arguments are stored for an indeterminate length of time and are evaluated
// from multiple goroutines, so they must be safe for concurrent access.
func LazyTracef(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).LazyTracef(format, args...)
}

// Errorf adds a new event to the trace in the context (via FromContext), and
// marks the trace as errored. The arguments are evaluted immediately.
func Errorf(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).Errorf(format, args...)
}

// LazyErrorf adds a new event to the trace in the context (via FromContext),
// and marks the trace as errored. Arguments are stored for an indeterminate
// length of time and are evaluated from multiple goroutines, so they must be
// safe for concurrent access.
func LazyErrorf(ctx context.Context, format string, args ...interface{}) {
	FromContext(ctx).LazyErrorf(format, args...)
}

type traceContextKey struct{}

var traceContextVal traceContextKey
