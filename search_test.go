package trc_test

import (
	"context"
	"math/rand"
	"testing"

	"github.com/peterbourgon/trc"
)

func testLoadCorpus(t *testing.T, create func(ctx context.Context, category string) (context.Context, trc.Trace)) []string {
	t.Helper()

	var (
		ctx           = context.Background()
		seed          = int64(1234)
		rng           = rand.New(rand.NewSource(seed))
		categories    = []string{"cat1", "cat2", "cat3"}
		traceCount    = 1000
		eventMin      = 1
		eventMax      = 100
		activePercent = 0.03
		ids           = []string{}
	)

	for i := 0; i < traceCount; i++ {
		category := categories[rng.Intn(len(categories))]
		_, tr := create(ctx, category)
		eventCount := eventMin + rng.Intn(eventMax-eventMin)
		for j := 0; j < eventCount; j++ {
			tr.Tracef("event %d/%d", j+1, eventCount)
		}
		if rng.Float64() > activePercent {
			tr.Finish()
		}
		ids = append(ids, tr.ID())
	}

	return ids
}

func testSearchCorpus(t *testing.T, s trc.Searcher, ids []string) {
	t.Helper()

	var (
		ctx           = context.Background()
		totalCat1     = 342
		totalCat2     = 338
		totalCat3     = 320
		totalOverall  = 342 + 338 + 320
		activeCat1    = 9
		activeCat2    = 10
		activeCat3    = 13
		activeOverall = 9 + 10 + 13
	)

	t.Run("totals", func(t *testing.T) {
		res, err := s.Search(ctx, &trc.SearchRequest{})
		AssertNoError(t, err)
		totals := map[string]int{}
		active := map[string]int{}
		for _, c := range res.Stats.AllCategories() {
			totals[c.Name] = int(c.NumTotal())
			active[c.Name] = int(c.NumActive)
		}
		ExpectEqual(t, totals["cat1"], totalCat1)
		ExpectEqual(t, totals["cat2"], totalCat2)
		ExpectEqual(t, totals["cat3"], totalCat3)
		ExpectEqual(t, totals["overall"], totalOverall)
		ExpectEqual(t, active["cat1"], activeCat1)
		ExpectEqual(t, active["cat2"], activeCat2)
		ExpectEqual(t, active["cat3"], activeCat3)
		ExpectEqual(t, active["overall"], activeOverall)
	})

	t.Run("limit 1000 oldest", func(t *testing.T) {
		res, err := s.Search(ctx, &trc.SearchRequest{
			Limit: totalOverall,
		})
		AssertNoError(t, err)
		AssertEqual(t, ids[0], res.Selected[len(res.Selected)-1].ID())
	})

	t.Run("limit 1 newest", func(t *testing.T) {
		res, err := s.Search(ctx, &trc.SearchRequest{
			Limit: 1,
		})
		AssertNoError(t, err)
		AssertEqual(t, ids[len(ids)-1], res.Selected[0].ID())
	})

	t.Run("category search", func(t *testing.T) {
		res, err := s.Search(ctx, &trc.SearchRequest{
			Category: "cat2",
		})
		AssertNoError(t, err)
		AssertEqual(t, totalCat2, res.Matched)
	})

	t.Run("active search", func(t *testing.T) {
		res, err := s.Search(ctx, &trc.SearchRequest{
			IsActive: true,
		})
		AssertNoError(t, err)
		AssertEqual(t, activeOverall, res.Matched)
	})
}
