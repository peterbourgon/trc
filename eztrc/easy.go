package eztrc

import (
	"context"
	"net/http"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trccoll"
	"github.com/peterbourgon/trc/trchttp"
)

var (
	Tracef     = trc.Tracef
	LazyTracef = trc.LazyTracef
	Errorf     = trc.Errorf
	LazyErrorf = trc.LazyErrorf
	Region     = trc.Region
	Prefix     = trc.Prefix
)

var collector = trccoll.NewCollector(1000)

func ResetCollector(maxTracesPerCategory int) {
	collector = trccoll.NewCollector(maxTracesPerCategory)
}

func Handler() http.Handler {
	return trchttp.NewServer(collector)
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
