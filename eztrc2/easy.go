package eztrc

import (
	"context"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
)

var collector = trc.NewTraceCollector(100)

func Collector() *trc.TraceCollector { return collector }

var TraceHandler = trchttp.TraceCollectorHandler(collector)

func Create(ctx context.Context, category string) (_ context.Context, finish func()) {
	ctx, tr := collector.NewTrace(ctx, category)
	tr.Tracef("> %s", category)
	return ctx, func() { tr.Tracef("< %s", category); tr.Finish() }
}

func Get(ctx context.Context) trc.Trace {
	return trc.FromContext(ctx)
}

func GetOrCreate(ctx context.Context, category string) (_ context.Context, finish func()) {
	if tr, ok := trc.MaybeFromContext(ctx); ok {
		tr.Tracef("(> %s)", category)
		return ctx, func() { tr.Tracef("(< %s)", category) }
	}
	return Create(ctx, category)
}

func Tracef(ctx context.Context, format string, args ...interface{}) {
	trc.Tracef(ctx, format, args...)
}

func LazyTracef(ctx context.Context, format string, args ...interface{}) {
	trc.LazyTracef(ctx, format, args...)
}

func Errorf(ctx context.Context, format string, args ...interface{}) {
	trc.Errorf(ctx, format, args...)
}

func LazyErrorf(ctx context.Context, format string, args ...interface{}) {
	trc.LazyErrorf(ctx, format, args...)
}

func Logf(category string, format string, args ...interface{}) {
	//
}

func LazyLogf(category string, format string, args ...interface{}) {
}
