package trc

import (
	"context"
	"fmt"
	"runtime/trace"
	"strings"
	"time"
)

// NewTrace creates a new default trace with the provided category, and injects
// it to the context. If the context already contained a trace, it becomes
// "shadowed" by the new one.
func NewTrace(ctx context.Context, category string) (context.Context, Trace) {
	tr := newCoreTrace(category)
	return ToContext(ctx, tr), tr
}

type traceContextKey struct{}

var traceContextVal traceContextKey

// FromContext returns the trace in the context, if it exists. If not, an orphan
// trace is created and returned. Note that orphan traces are usually bugs, and
// so this function is meant as a convenience for situations where a context is
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

// ToContext injects the given trace into the context, returning a new context
// with the provided trace. If the context already contained a trace, it becomes
// "shadowed" by the new trace.
func ToContext(ctx context.Context, tr Trace) context.Context {
	return context.WithValue(ctx, traceContextVal, tr)
}

//
//
//

func Prefix(ctx context.Context, format string, args ...any) (context.Context, Trace) {
	original := FromContext(ctx)

	format = strings.TrimSpace(format)
	if format == "" {
		return ctx, original
	}

	prefixed := &prefixedTrace{
		Trace:  original,
		format: format + " ",
		args:   args,
	}

	return ToContext(ctx, prefixed), prefixed
}

type prefixedTrace struct {
	Trace
	format string
	args   []any
}

func (ptr *prefixedTrace) Tracef(format string, args ...any) {
	ptr.Trace.Tracef(ptr.format+format, append(ptr.args, args...)...)
}

func (ptr *prefixedTrace) LazyTracef(format string, args ...any) {
	ptr.Trace.LazyTracef(ptr.format+format, append(ptr.args, args...)...)
}

func (ptr *prefixedTrace) Errorf(format string, args ...any) {
	ptr.Trace.Errorf(ptr.format+format, append(ptr.args, args...)...)
}

func (ptr *prefixedTrace) LazyErrorf(format string, args ...any) {
	ptr.Trace.LazyErrorf(ptr.format+format, append(ptr.args, args...)...)
}

//
//
//

// Region is a convenience function that provides more detailed tracing of
// regions of code, usually functions. It also produces a standard library
// [runtime/trace.Region], which can be useful when analyzing program execution
// with go tool trace.
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
	outputContext, outputTrace := Prefix(ctx, "· ")
	region := trace.StartRegion(outputContext, fmt.Sprintf(format, args...))

	inputTrace.LazyTracef("→ "+format, args...)
	finish := func() {
		took := time.Since(begin)
		inputTrace.LazyTracef("← "+format+" [%s]", append(args, took.String())...)
		region.End()
	}

	return outputContext, outputTrace, finish
}
