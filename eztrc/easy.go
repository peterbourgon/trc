package eztrc

import (
	"context"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trctrace"
)

var (
	Tracef     = trc.Tracef
	LazyTracef = trc.LazyTracef
	Errorf     = trc.Errorf
	LazyErrorf = trc.LazyErrorf
	Region     = trc.Region
)

var collector = trctrace.NewCollector(1000)

func Collector() *trctrace.Collector {
	return collector
}

func SetCollector(c *trctrace.Collector) {
	if c == nil {
		panic("nil trctrace.Collector provided to SetCollector")
	}
	collector = c
}

func New(ctx context.Context, category string) (context.Context, trc.Trace) {
	return collector.NewTrace(ctx, category)
}

func Get(ctx context.Context, category string) (context.Context, trc.Trace) {
	if tr, ok := trc.MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}
	return New(ctx, category)
}
