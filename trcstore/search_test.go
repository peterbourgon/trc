package trcstore_test

import (
	"context"
	"math/rand"
	"strconv"
	"testing"

	"github.com/peterbourgon/trc/trcstore"
)

func TestMultiSearch(t *testing.T) {
	t.Parallel()

	seed := int64(12345)
	rng := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	categories := []string{"foo", "bar", "baz", "quux"}

	collectorCount := 5
	collectors := make([]*trcstore.Collector, collectorCount)
	for i := range collectors {
		collectors[i] = trcstore.NewCollector(trcstore.CollectorConfig{Source: strconv.Itoa(i + 1)})
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

	ms := make(trcstore.MultiSearcher, len(collectors))
	for i := range collectors {
		ms[i] = collectors[i]
	}

	res, err := ms.Search(ctx, &trcstore.SearchRequest{})
	AssertNoError(t, err)

	AssertEqual(t, len(ms), len(res.Sources))
	AssertEqual(t, traceCount, res.Total)
}
