package eztrc

import (
	"context"
	"net/http"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
	"github.com/peterbourgon/trc/trcstore"
)

var (
	Tracef     = trc.Tracef
	LazyTracef = trc.LazyTracef
	Errorf     = trc.Errorf
	LazyErrorf = trc.LazyErrorf
	Region     = trc.Region
	Prefix     = trc.Prefix
)

var store = trcstore.NewStore()

func Handler() http.Handler {
	return trchttp.NewServer(store)
}

func Middleware(categorize trchttp.CategorizeFunc) func(http.Handler) http.Handler {
	return trchttp.Middleware(store.NewTrace, categorize)
}

func New(ctx context.Context, category string) (context.Context, trc.Trace) {
	return store.NewTrace(ctx, category)
}

func Get(ctx context.Context, category string) (context.Context, trc.Trace) {
	if tr, ok := trc.MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}
	return New(ctx, category)
}
