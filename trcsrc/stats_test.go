package trcsrc_test

import (
	"context"
	"math/rand"
	"testing"

	"github.com/peterbourgon/trc/trcsrc"
	"github.com/peterbourgon/trc/trcstore"
)

func TestStatsMerge(t *testing.T) {
	t.Parallel()

	seed := int64(12345)
	rng := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	categories := []string{"foo", "bar", "baz", "quux"}

	sourceCount := 5
	sources := make([]*trcsrc.Source, sourceCount)
	for i := range sources {
		sources[i] = trcsrc.NewDefaultSource()
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

	responses := make([]*trcsrc.SelectResponse, len(sources))
	for i := range sources {
		res, err := sources[i].Select(ctx, &trcsrc.SelectRequest{})
		AssertNoError(t, err)
		responses[i] = res
	}

	var merged trcsrc.SelectStats
	for _, res := range responses {
		merged.Merge(res.Stats)
	}

	overall := merged.Overall()
	AssertEqual(t, 0, overall.ActiveCount)
	AssertEqual(t, 0, overall.ErroredCount)
	AssertEqual(t, len(trcstore.DefaultBucketing), len(overall.BucketCounts))
	AssertEqual(t, traceCount, overall.TotalCount())
}
