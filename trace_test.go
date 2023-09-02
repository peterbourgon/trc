package trc_test

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"reflect"
	"sync"
	"testing"

	"github.com/peterbourgon/trc"
)

// TraceTest performs basic validation of trace implementations.
func TraceTest(t *testing.T, constructor trc.NewTraceFunc) {
	t.Parallel()

	t.Helper()

	ctx := context.Background()

	t.Run("Unique ID", func(t *testing.T) {
		t.Parallel()

		index := map[string]bool{}
		for i := 0; i < 10; i++ {
			_, tr := constructor(ctx, "src", "foo")
			if _, ok := index[tr.ID()]; ok {
				t.Errorf("duplicate ID %s", tr.ID())
			}
			index[tr.ID()] = true
		}
	})

	t.Run("Errored", func(t *testing.T) {
		t.Parallel()

		_, tr := constructor(ctx, "src", "foo")
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
		t.Parallel()

		_, tr := constructor(ctx, "src", "foo")
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
		t.Parallel()

		_, tr := constructor(ctx, "src", "foo")
		a := []int{1, 2, 3}
		tr.Tracef("a=%v", a)
		tr.Finish()
		a[0] = 0
		if want, have := "a=[1 2 3]", tr.Events()[0].What; want != have {
			t.Errorf("want %s, have %s", want, have)
		}
	})

	t.Run("Lazy events", func(t *testing.T) {
		t.Parallel()

		_, tr := constructor(ctx, "src", "foo")
		a := []int{1, 2, 3}
		tr.LazyTracef("a=%v", a)
		tr.Finish()
		a[0] = 0
		if want, have := "a=[0 2 3]", tr.Events()[0].What; want != have {
			t.Errorf("want %s, have %s", want, have)
		}
	})

	t.Run("Error event", func(t *testing.T) {
		t.Parallel()

		_, tr := constructor(ctx, "src", "foo")
		tr.Errorf("this is an error")
		tr.Finish()
		AssertEqual(t, true, tr.Errored())
	})

	t.Run("optional SetMaxEvents", func(t *testing.T) {
		t.Parallel()

		_, tr := constructor(ctx, "src", "foo")
		defer tr.Finish()
		m, ok := tr.(interface{ SetMaxEvents(int) })
		if !ok {
			t.Skipf("%T doesn't have a SetMaxEvents method", tr)
		}
		max, extra := 32, 17
		m.SetMaxEvents(max)
		for i := 0; i < max+extra; i++ {
			tr.Tracef("event %d", i+1)
		}
		events := tr.Events()
		have := len(events)
		want1, want2 := max, max+1 // we can have an extra "truncated" event
		if !(have == want1 || have == want2) {
			t.Errorf("events: want either %d or %d, have %d", want1, want2, have)
		}
	})

	t.Run("Concurrency", func(t *testing.T) {
		t.Parallel()

		workers := 100
		_, tr := constructor(ctx, "src", "foo")
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				switch r := rand.Float64(); {
				case r < 0.05:
					tr.Errorf("trace event %d", rand.Intn(100))
				case r < 0.10:
					tr.LazyErrorf("trace event %d", rand.Intn(100))
				case r < 0.75:
					tr.Tracef("trace event %d", rand.Intn(100))
				default:
					tr.LazyTracef("trace event %d", rand.Intn(100))
				}
				_ = tr.ID()
				_ = tr.Source()
				_ = tr.Category()
				_ = tr.Started()
				_ = tr.Finished()
				_ = tr.Errored()
				_ = tr.Duration()
				_ = tr.Events()
			}()
		}
		wg.Wait()
		tr.Finish()
		AssertEqual(t, workers, len(tr.Events()))
	})
}

func TestCoreTrace(t *testing.T) {
	TraceTest(t, trc.New)
}

func TestTraceContext(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		ctx0 := context.Background()
		if _, ok := trc.MaybeGet(ctx0); ok {
			t.Fatalf("unexpectedly got trace from fresh context")
		}
	})

	t.Run("same", func(t *testing.T) {
		ctx, tr1 := trc.New(context.Background(), "src", "1")
		tr2, ok := trc.MaybeGet(ctx)
		if !ok {
			t.Fatalf("MaybeFromContext failed to return a trace")
		}
		if want, have := tr1.ID(), tr2.ID(); want != have {
			t.Fatalf("ID: want %s, have %s", want, have)
		}
	})

	t.Run("scoping", func(t *testing.T) {
		ctx, tr0 := trc.New(context.Background(), "src", "a")
		func(ctx context.Context) {
			ctx, tr1 := trc.New(ctx, "src", "b")
			if tr0.ID() == tr1.ID() {
				t.Errorf("tr0 %s == tr1 %s, bad", tr0.ID(), tr1.ID())
			}

			tr2 := trc.Get(ctx)
			if tr1.ID() != tr2.ID() {
				t.Errorf("tr1 %s != tr2 %s, bad", tr1.ID(), tr2.ID())
			}

			{
				ctx, tr3 := trc.New(ctx, "src", "c")
				fmt.Fprint(io.Discard, ctx, tr3)
			}

			tr4 := trc.Get(ctx)
			if tr4.ID() != tr2.ID() {
				t.Errorf("tr4 %s != tr2 %s, bad", tr4.ID(), tr2.ID())
			}
		}(ctx)
	})
}
