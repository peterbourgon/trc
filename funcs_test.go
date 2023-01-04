package trc_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/peterbourgon/trc"
)

func TestFromContext(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		ctx0 := context.Background()
		if _, ok := trc.MaybeFromContext(ctx0); ok {
			t.Fatalf("unexpectedly got trace from fresh context")
		}
	})

	t.Run("same", func(t *testing.T) {
		ctx, tr1 := trc.NewTrace(context.Background(), "1")
		tr2, ok := trc.MaybeFromContext(ctx)
		if !ok {
			t.Fatalf("MaybeFromContext failed to return a trace")
		}
		if want, have := tr1.ID(), tr2.ID(); want != have {
			t.Fatalf("ID: want %s, have %s", want, have)
		}
	})

	t.Run("scoping", func(t *testing.T) {
		ctx, tr0 := trc.NewTrace(context.Background(), "a")
		func(ctx context.Context) {
			ctx, tr1 := trc.NewTrace(ctx, "b")
			if tr0.ID() == tr1.ID() {
				t.Errorf("tr0 %s == tr1 %s, bad", tr0.ID(), tr1.ID())
			}

			tr2 := trc.FromContext(ctx)
			if tr1.ID() != tr2.ID() {
				t.Errorf("tr1 %s != tr2 %s, bad", tr1.ID(), tr2.ID())
			}

			{
				ctx, tr3 := trc.NewTrace(ctx, "c")
				fmt.Fprint(io.Discard, ctx, tr3)
			}

			tr4 := trc.FromContext(ctx)
			if tr4.ID() != tr2.ID() {
				t.Errorf("tr4 %s != tr2 %s, bad", tr4.ID(), tr2.ID())
			}
		}(ctx)
	})
}
