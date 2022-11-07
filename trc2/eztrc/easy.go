package eztrc

import (
	"context"

	trc "github.com/peterbourgon/trc/trc2"
	"github.com/peterbourgon/trc/trc2/trctrace"
)

var collector = trctrace.NewCollector(1000) // TODO

func Collector() *trctrace.Collector { return collector }

var QueryHandler = trctrace.NewHTTPQueryHandler(collector)

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

var (
	Tracef     = trc.Tracef
	LazyTracef = trc.LazyTracef
	Errorf     = trc.Errorf
	LazyErrorf = trc.LazyErrorf
	Region     = trc.Region
)
