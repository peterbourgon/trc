package trcstore_test

import (
	"context"
	"math/rand"
	"testing"

	"github.com/peterbourgon/trc/trcstore"
)

func TestStatsMerge(t *testing.T) {
	t.Parallel()

	seed := int64(12345)
	rng := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	categories := []string{"foo", "bar", "baz", "quux"}

	collectorCount := 5
	collectors := make([]*trcstore.Collector, collectorCount)
	for i := range collectors {
		collectors[i] = trcstore.NewDefaultCollector()
	}

	traceCount := 1024
	for i := 0; i < traceCount; i++ {
		collector := collectors[rng.Intn(len(collectors))]
		category := categories[rng.Intn(len(categories))]
		_, tr := collector.NewTrace(ctx, category)
		for i := 0; i < 1+rng.Intn(10); i++ {
			tr.Tracef("event %d", i+1)
		}
		tr.Finish()
	}

	responses := make([]*trcstore.SearchResponse, len(collectors))
	for i := range collectors {
		res, err := collectors[i].Search(ctx, &trcstore.SearchRequest{})
		AssertNoError(t, err)
		responses[i] = res
	}

	var merged trcstore.Stats
	for _, res := range responses {
		merged.Merge(res.Stats)
	}

	overall := merged.Overall()
	AssertEqual(t, 0, overall.ActiveCount)
	AssertEqual(t, 0, overall.ErroredCount)
	AssertEqual(t, len(trcstore.DefaultBucketing), len(overall.BucketCount))
	AssertEqual(t, traceCount, overall.TotalCount())
}
