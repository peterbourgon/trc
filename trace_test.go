package trc_test

import (
	"context"
	"fmt"
	"io"
	"reflect"
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

func TestCoreTrace(t *testing.T) {
	TraceTest(t, trc.NewTrace)
}

func TraceTest(t *testing.T, constructor func(ctx context.Context, category string) (context.Context, trc.Trace)) {
	t.Helper()

	ctx := context.Background()

	t.Run("Unique ID", func(t *testing.T) {
		index := map[string]bool{}
		for i := 0; i < 10; i++ {
			_, tr := constructor(ctx, "foo")
			if _, ok := index[tr.ID()]; ok {
				t.Errorf("duplicate ID %s", tr.ID())
			}
			index[tr.ID()] = true
		}
	})

	t.Run("Errored", func(t *testing.T) {
		_, tr := constructor(ctx, "foo")
		if want, have := false, tr.Errored(); want != have {
			t.Errorf("Trace was marked Errored without error event")
		}
		tr.Errorf("err")
		if want, have := true, tr.Errored(); want != have {
			t.Errorf("Errorf didn't mark Errored immediately")
		}
		tr.Finish()
		if want, have := true, tr.Errored(); want != have {
			t.Errorf("Errorf didn't mark Errored after finish")
		}
	})

	t.Run("Finish prevents updates", func(t *testing.T) {
		_, tr := constructor(ctx, "foo")
		tr.Tracef("first")
		tr.LazyTracef("second")
		f1 := tr.Finished()
		e1 := tr.Events()

		tr.Finish()

		f2 := tr.Finished()
		d1 := tr.Duration()
		tr.Tracef("should be no-op")
		tr.LazyTracef("should be no-op")
		tr.Errorf("should not error")
		tr.LazyErrorf("should not error")
		d2 := tr.Duration()
		e2 := tr.Events()

		if f1 == true {
			t.Errorf("Finished returned true before Finish was called")
		}

		if f2 == false {
			t.Errorf("Finished returned false after Finish was called")
		}

		if d1 != d2 {
			t.Errorf("Duration changed after Finish was called")
		}

		if want, have := false, tr.Errored(); want != have {
			t.Errorf("Errored was changed after Finish was called")
		}

		if !reflect.DeepEqual(e1, e2) {
			t.Errorf("Events changed after Finish was called")
		}
	})

	t.Run("Normal events", func(t *testing.T) {
		_, tr := constructor(ctx, "foo")
		a := []int{1, 2, 3}
		tr.Tracef("a=%v", a)
		tr.Finish()
		a[0] = 0
		if want, have := "a=[1 2 3]", tr.Events()[0].What(); want != have {
			t.Errorf("want %s, have %s", want, have)
		}
	})

	t.Run("Lazy events", func(t *testing.T) {
		_, tr := constructor(ctx, "foo")
		a := []int{1, 2, 3}
		tr.LazyTracef("a=%v", a)
		tr.Finish()
		a[0] = 0
		if want, have := "a=[0 2 3]", tr.Events()[0].What(); want != have {
			t.Errorf("want %s, have %s", want, have)
		}
	})
}
