package trc_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/peterbourgon/trc"
)

func TestCollectorBasics(t *testing.T) {
	ctx := context.Background()

	t.Run("Constructor", func(t *testing.T) {
		var count atomic.Uint64

		collector := trc.NewCollector(trc.CollectorConfig{
			Constructor: func(ctx context.Context, category string) (context.Context, trc.Trace) {
				count.Add(1)
				return trc.NewTrace(ctx, category)
			},
		})

		n := 5

		for i := 0; i < n; i++ {
			_, tr := collector.NewTrace(ctx, "foo")
			tr.Finish()
		}

		AssertEqual(t, n, int(count.Load()))
	})

	t.Run("MaxTracesPerCategory", func(t *testing.T) {
		max := 5

		collector := trc.NewCollector(trc.CollectorConfig{
			MaxTracesPerCategory: max,
		})

		for i := 0; i < max+3; i++ {
			_, tr := collector.NewTrace(ctx, "foo")
			tr.Finish()
		}

		res, err := collector.Search(ctx, &trc.SearchRequest{
			Limit: 2 * max,
		})
		AssertNoError(t, err)
		AssertEqual(t, max, len(res.Selected))
	})

	t.Run("Resize", func(t *testing.T) {
		max := 10

		collector := trc.NewCollector(trc.CollectorConfig{
			MaxTracesPerCategory: max,
		})

		for i := 0; i < 2*max; i++ {
			_, tr := collector.NewTrace(ctx, "foo")
			tr.Finish()
		}

		res, err := collector.Search(ctx, &trc.SearchRequest{})
		AssertNoError(t, err)
		AssertEqual(t, max, len(res.Selected))

		max /= 2
		collector.Resize(ctx, max)

		res, err = collector.Search(ctx, &trc.SearchRequest{})
		AssertNoError(t, err)
		AssertEqual(t, max, len(res.Selected))
	})
}

func TestCollectorSearch(t *testing.T) {
	collector := trc.NewDefaultCollector()
	ids := testLoadCorpus(t, collector.NewTrace)
	testSearchCorpus(t, collector, ids)
}
