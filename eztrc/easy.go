package eztrc

import (
	"context"
	"net/http"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trctrace"
	"github.com/peterbourgon/trc/trctrace/trctracehttp"
)

var (
	Tracef     = trc.Tracef
	LazyTracef = trc.LazyTracef
	Errorf     = trc.Errorf
	LazyErrorf = trc.LazyErrorf
	Region     = trc.Region
)

//
//

var collector = trctrace.NewCollector(
	trc.Source{
		Name: "local",
	},
	1000,
) // TODO

func Collector() *trctrace.Collector {
	return collector
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

//
//
//

func Handler(localName string, otherTargets ...*trctracehttp.Target) http.Handler {
	s, err := trctracehttp.NewServer(trctracehttp.ServerConfig{
		Local: &trctracehttp.Target{
			Name:     localName,
			Searcher: collector,
		},
		Other: otherTargets,
	})
	if err != nil {
		panic(err)
	}
	return s
}
