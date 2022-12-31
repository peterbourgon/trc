package trctrace_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/peterbourgon/trc/trctrace"
)

func TestQueryResponseMerge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	seed := int64(1234)
	rng := rand.New(rand.NewSource(seed))
	c1 := generateCollector(t, rng, "a", "b", "c")
	c2 := generateCollector(t, rng, "b", "c")
	c3 := generateCollector(t, rng, "c")
	all := trctrace.MultiQueryer{
		trctrace.NewOriginQueryer("c1", c1),
		trctrace.NewOriginQueryer("c2", c2),
		trctrace.NewOriginQueryer("c3", c3),
	}

	for _, q := range []trctrace.Queryer{c1, c2, c3, all} {
		res, err := q.Query(ctx, &trctrace.QueryRequest{Category: "category 1"})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("origins=%v total=%d matched=%d selected=%d", res.Origins, res.Total, res.Matched, len(res.Selected))
	}
}

func generateCollector(t *testing.T, rng *rand.Rand, keys ...string) *trctrace.Collector {
	t.Helper()

	collector := trctrace.NewCollector(100)
	for i := 0; i < 100; i++ {
		category := fmt.Sprintf("category %d", rng.Intn(3))
		_, tr := collector.NewTrace(context.Background(), category)
		tr.Tracef("key %s", keys[rng.Intn(len(keys))])
		tr.Finish()
	}

	return collector
}
