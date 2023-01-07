package eztrc

import (
	"context"

	"github.com/peterbourgon/trc"
	trctrace "github.com/peterbourgon/trc/trctrace2"
)

var collector = trctrace.NewCollector(trc.Source{Name: "local"}, 1000) // TODO

func Collector() *trctrace.Collector { return collector }

//var QueryHandler = trctrace.NewHTTPQueryHandler(collector)

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
