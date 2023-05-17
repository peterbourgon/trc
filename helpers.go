package trc

import (
	"context"
	"runtime/trace"
	"strings"
	"time"
)

// Put the given trace into the context, and return a new context containing
// that trace, as well as the trace itself. If the context already contained a
// trace, it becomes "shadowed" by the new trace.
func Put(ctx context.Context, tr Trace) (context.Context, Trace) {
	return context.WithValue(ctx, traceContextVal, tr), tr
}

// Get the trace from the context, if it exists. If not, an "orphan" trace is
// created and returned (but not injected into the context).
func Get(ctx context.Context) Trace {
	if tr, ok := MaybeGet(ctx); ok {
		return tr
	}

	return newCoreTrace("", "(orphan)")
}

// MaybeGet returns the trace in the context, if it exists, with true as the
// second return value. If not, a nil trace is returned, with false as the
// second return value.
func MaybeGet(ctx context.Context) (Trace, bool) {
	tr, ok := ctx.Value(traceContextVal).(Trace)
	return tr, ok
}

// Region provides more detailed tracing of regions of code, usually functions,
// which is visible in the trace event "what" text. It decorates the trace in
// the context by annotating events with the provided name, and also creates a
// standard library [runtime/trace.Region] with the same name.
//
// Typical usage is as follows.
//
//	func foo(ctx context.Context, id int) {
//	    ctx, tr, finish := trc.Region(ctx, "foo")
//	    defer finish()
//	    ...
//	}
//
// This produces hierarchical trace events as follows.
//
//	→ foo
//	· trace event in foo
//	· another event in foo
//	· → bar
//	· · something in bar
//	· ← bar [1.23ms]
//	· final event in foo
//	← foo [2.34ms]
//
// Region can significantly impact performance. Use it sparingly.
func Region(ctx context.Context, name string) (context.Context, Trace, func()) {
	begin := time.Now()
	inputTrace := Get(ctx)
	outputContext, outputTrace := Prefix(ctx, "·")
	region := trace.StartRegion(outputContext, name)

	inputTrace.LazyTracef("→ " + name)
	finish := func() {
		took := time.Since(begin)
		inputTrace.LazyTracef("← "+name+" [%s]", took.String())
		region.End()
	}

	return outputContext, outputTrace, finish
}

// Prefix decorates the trace in the context such that every trace event will be
// prefixed with the string specified by format and args. Those args are not
// evaluated when Prefix is called, but are instead prefixed to the format and
// args of trace events made against the returned trace.
func Prefix(ctx context.Context, format string, args ...any) (context.Context, Trace) {
	original := Get(ctx)

	format = strings.TrimSpace(format)
	if format == "" {
		return ctx, original
	}

	prefixed := &prefixTrace{
		Trace:  original,
		format: format + " ",
		args:   args,
	}

	return Put(ctx, prefixed)
}

type prefixTrace struct {
	Trace

	format string
	args   []any
}

func (ptr *prefixTrace) Tracef(format string, args ...any) {
	ptr.Trace.Tracef(ptr.format+format, append(ptr.args, args...)...)
}

func (ptr *prefixTrace) LazyTracef(format string, args ...any) {
	ptr.Trace.LazyTracef(ptr.format+format, append(ptr.args, args...)...)
}

func (ptr *prefixTrace) Errorf(format string, args ...any) {
	ptr.Trace.Errorf(ptr.format+format, append(ptr.args, args...)...)
}

func (ptr *prefixTrace) LazyErrorf(format string, args ...any) {
	ptr.Trace.LazyErrorf(ptr.format+format, append(ptr.args, args...)...)
}

// SetMaxEvents tries to set the max events for a specific trace, by checking if
// the trace implements the method SetMaxEvents(int), and, if so, calling that
// method with the given max events value. Returns the given trace, and a
// boolean representing whether or not the call was successful.
func SetMaxEvents(tr Trace, maxEvents int) (Trace, bool) {
	m, ok := tr.(interface{ SetMaxEvents(int) })
	if !ok {
		return tr, false
	}
	m.SetMaxEvents(maxEvents)
	return tr, true
}
