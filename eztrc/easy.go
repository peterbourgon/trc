package eztrc

import (
	"context"
	"net/http"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
	"github.com/peterbourgon/trc/trcstore"
)

var (
	Region = trc.Region
	Prefix = trc.Prefix
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

// Get is really GetOrCreate: if a trace exists in the context, Get adds an
// event reflecting the provided category and returns the context and the trace
// directly. Otherwise, Get creates a new trace in the context via [New].
func Get(ctx context.Context, category string) (context.Context, trc.Trace) {
	if tr, ok := trc.MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}
	return New(ctx, category)
}

// Tracef adds a new event to the trace in the context.
// Arguments are evaulated immediately.
func Tracef(ctx context.Context, format string, args ...any) {
	trc.FromContext(ctx).Tracef(format, args...)
}

// LazyTracef adds a new event to the trace in the context.
// Arguments are evaluated lazily.
func LazyTracef(ctx context.Context, format string, args ...any) {
	trc.FromContext(ctx).LazyTracef(format, args...)
}

// Errorf adds a new event to the trace in the context.
// Arguments are evaulated immediately.
func Errorf(ctx context.Context, format string, args ...any) {
	trc.FromContext(ctx).Errorf(format, args...)
}

// LazyErrorf adds a new event to the trace in the context.
// Arguments are evaluated lazily.
func LazyErrorf(ctx context.Context, format string, args ...any) {
	trc.FromContext(ctx).LazyErrorf(format, args...)
}
