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

func Get(ctx context.Context, category string) (context.Context, trc.Trace) {
	if tr, ok := trc.MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}
	return New(ctx, category)
}

// Tracef adds a new event to the trace in the context (via FromContext).
// Arguments are evaulated immediately.
func Tracef(ctx context.Context, format string, args ...any) {
	trc.FromContext(ctx).Tracef(format, args...)
}

// LazyTracef adds a new event to the trace in the context (via FromContext).
// Arguments are evaluated lazily, when the event is read by a client. Arguments
// may be stored for an indeterminste amount of time, and may be evaluated by
// multiple goroutines, and therefore must be safe for concurrent access.
func LazyTracef(ctx context.Context, format string, args ...any) {
	trc.FromContext(ctx).LazyTracef(format, args...)
}

// Errorf adds a new event to the trace in the context (via FromContext), and
// marks the trace as errored. Arguments are evaluted immediately.
func Errorf(ctx context.Context, format string, args ...any) {
	trc.FromContext(ctx).Errorf(format, args...)
}

// LazyErrorf adds a new event to the trace in the context (via FromContext),
// and marks the trace as errored. Arguments are evaluated lazily, when the
// event is read by a client. Arguments may be stored for an indeterminste
// amount of time, and may be evaluated by multiple goroutines, and therefore
// must be safe for concurrent access.
func LazyErrorf(ctx context.Context, format string, args ...any) {
	trc.FromContext(ctx).LazyErrorf(format, args...)
}
