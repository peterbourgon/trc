package trcstore_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/peterbourgon/trc"
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
