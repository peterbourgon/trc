package eztrc

import (
	"context"
	"net/http"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
)

var (
	Tracef     = trc.Tracef
	LazyTracef = trc.LazyTracef
	Errorf     = trc.Errorf
	LazyErrorf = trc.LazyErrorf
	Region     = trc.Region
)

var collector = trc.NewCollector(1000)

func Collector() *trc.Collector {
	return collector
}

func SetCollector(c *trc.Collector) {
	if c == nil {
		panic("nil trc.Collector provided to SetCollector")
	}
	collector = c
}

func Handler() http.Handler {
	return trchttp.NewServer(Collector())
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
