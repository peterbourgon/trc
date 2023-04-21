package trc

import (
	"context"
	"fmt"
	"runtime/trace"
	"strings"
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

//
//
//

// Prefix wraps the trace with the provided prefix.
func Prefix(tr Trace, format string, args ...any) Trace {
	format = strings.TrimSpace(format)

	if format == "" {
		return tr
	}

	return &prefixedTrace{
		Trace:  tr,
		format: format + " ",
		args:   args,
	}
}

// prefixedTrace decorates a trace and adds a user-supplied prefix to each event.
// This can be useful to show important regions of execution without needing to
// inspect full call stacks.
type prefixedTrace struct {
	Trace
	format string
	args   []any
}

// Tracef implements Trace, adding a prefix to the provided format string.
func (ptr *prefixedTrace) Tracef(format string, args ...any) {
	ptr.Trace.Tracef(ptr.format+format, append(ptr.args, args...)...)
}

// LazyTracef implements Trace, adding a prefix to the provided format string.
func (ptr *prefixedTrace) LazyTracef(format string, args ...any) {
	ptr.Trace.LazyTracef(ptr.format+format, append(ptr.args, args...)...)
}

//
//
//

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
