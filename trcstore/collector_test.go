package trcstore_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcdebug"
	"github.com/peterbourgon/trc/trcstore"
)

func TestCollectorBasics(t *testing.T) {
	ctx := context.Background()

	t.Run("Constructor", func(t *testing.T) {
		var count atomic.Uint64

		collector := trcstore.NewCollector(trcstore.CollectorConfig{
			Constructor: func(ctx context.Context, source, category string) (context.Context, trc.Trace) {
				count.Add(1)
				return trc.New(ctx, source, category)
			},
		})

		n := 5

		for i := 0; i < n; i++ {
			_, tr := collector.NewTrace(ctx, "foo")
			tr.Finish()
		}

		AssertEqual(t, n, int(count.Load()))
	})

	t.Run("CategorySize+Resize", func(t *testing.T) {
		max := 32

		collector := trcstore.NewCollector(trcstore.CollectorConfig{
			CategorySize: max,
		})

		for i := 0; i < 2*max; i++ {
			_, tr := collector.NewTrace(ctx, "foo")
			tr.Finish()
		}

		res, err := collector.Search(ctx, &trcstore.SearchRequest{
			Limit: 2 * max,
		})
		AssertNoError(t, err)
		AssertEqual(t, max, len(res.Selected))

		max /= 2
		collector.Resize(ctx, max)

		res, err = collector.Search(ctx, &trcstore.SearchRequest{
			Limit: 2 * max,
		})
		AssertNoError(t, err)
		AssertEqual(t, max, len(res.Selected))
	})

	t.Run("SetSource", func(t *testing.T) {
		collector := trcstore.NewCollector(trcstore.CollectorConfig{
			Source: "abc",
		})

		{
			_, tr := collector.NewTrace(ctx, "foo")
			tr.Finish()
			AssertEqual(t, "abc", tr.Source())
		}

		collector.SetSource("xyz")

		{
			_, tr := collector.NewTrace(ctx, "foo")
			tr.Finish()
			AssertEqual(t, "xyz", tr.Source())
		}
	})
}

func TestCollectorDropActive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	categorySize := 3
	collector := trcstore.NewCollector(trcstore.CollectorConfig{CategorySize: categorySize})

	defer func(lostBefore uint64) {
		lostAfter := trcdebug.CoreTraceLostCount.Load()
		t.Logf("lost: %d -> %d", lostBefore, lostAfter)
	}(trcdebug.CoreTraceLostCount.Load())

	var traces []trc.Trace
	for i := 0; i < 5*categorySize; i++ {
		_, tr := collector.NewTrace(ctx, "foo")
		tr.Tracef("trace %d", i+1)
		traces = append(traces, tr)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j, tr := range traces {
				tr.Tracef("trace %d", j+1)
			}
		}()
	}
	wg.Wait()

	for i, tr := range traces {
		tr.Finish()
		want := fmt.Sprintf("trace %d", i+1)
		bad := map[string]int{}
		for _, ev := range tr.Events() {
			if have := ev.What; want != have {
				bad[have]++
			}
		}
		if len(bad) > 0 {
			var lines []string
			for k, v := range bad {
				lines = append(lines, fmt.Sprintf("%q (x%d)", k, v))
			}
			t.Errorf("want %q, have additional %s", want, strings.Join(lines, ", "))
		}
	}
}
