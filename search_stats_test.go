package trc_test

import (
	"context"
	"math/rand"
	"testing"

	"github.com/peterbourgon/trc"
)

func TestSearchStatsMerge(t *testing.T) {
	t.Parallel()

	seed := int64(12345)
	rng := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	categories := []string{"foo", "bar", "baz", "quux"}

	sourceCount := 5
	sources := make([]*trc.Collector, sourceCount)
	for i := range sources {
		sources[i] = trc.NewDefaultCollector()
	}

	traceCount := 1024
	for i := 0; i < traceCount; i++ {
		src := sources[rng.Intn(len(sources))]
		cat := categories[rng.Intn(len(categories))]
		_, tr := src.NewTrace(ctx, cat)
		for i := 0; i < 1+rng.Intn(10); i++ {
			tr.Tracef("event %d", i+1)
		}
		tr.Finish()
	}

	responses := make([]*trc.SearchResponse, len(sources))
	for i := range sources {
		res, err := sources[i].Search(ctx, &trc.SearchRequest{})
		AssertNoError(t, err)
		responses[i] = res
	}

	var merged trc.SearchStats
	for _, res := range responses {
		merged.Merge(res.Stats)
	}

	overall := merged.Overall()
	AssertEqual(t, 0, overall.ActiveCount)
	AssertEqual(t, 0, overall.ErroredCount)
	AssertEqual(t, len(trc.DefaultBucketing), len(overall.BucketCounts))
	AssertEqual(t, traceCount, overall.TotalCount())
}
