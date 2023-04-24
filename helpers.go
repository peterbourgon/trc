package trc

import (
	"context"
	"fmt"
	"runtime/trace"
	"strings"
	"time"
)

// Region provides more detailed tracing of regions of code, usually functions.
// It prefixes trace events with the provided format and args, and produces a
// standard library [runtime/trace.Region].
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
// Region can significantly impact performance. Use it sparingly.
func Region(ctx context.Context, format string, args ...any) (context.Context, Trace, func()) {
	begin := time.Now()
	inputTrace := FromContext(ctx)
	outputContext, outputTrace := Prefix(ctx, "·")
	region := trace.StartRegion(outputContext, fmt.Sprintf(format, args...))

	inputTrace.LazyTracef("→ "+format, args...)
	finish := func() {
		took := time.Since(begin)
		inputTrace.LazyTracef("← "+format+" [%s]", append(args, took.String())...)
		region.End()
	}

	return outputContext, outputTrace, finish
}

// Prefix decorates the trace in the context such that every trace event will be
// prefixed by the string specified by format and args. The args are evaluated
// lazily, meaning they are re-evaluated with each call to e.g. Tracef.
func Prefix(ctx context.Context, format string, args ...any) (context.Context, Trace) {
	original := FromContext(ctx)

	format = strings.TrimSpace(format)
	if format == "" {
		return ctx, original
	}

	prefixed := &prefixTrace{
		Trace:  original,
		format: format + " ",
		args:   args,
	}

	return ToContext(ctx, prefixed), prefixed
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
