package trc_test

import (
	"context"
	"testing"

	"github.com/peterbourgon/trc"
)

func TestSearchScenarios(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := trc.NewDefaultCollector()

	var id1 string
	{
		_, tr := src.NewTrace(ctx, "category-a")
		id1 = tr.ID()
		tr.Tracef("event 1 (foo)")
		tr.Tracef("event 2 (bar)")
		tr.Finish()
	}

	var id2 string
	{
		_, tr := src.NewTrace(ctx, "category-a")
		id2 = tr.ID()
		tr.Errorf("event 3 (baz)")
		tr.Finish()
	}

	var id3 string
	{
		_, tr := src.NewTrace(ctx, "category-b")
		id3 = tr.ID()
		tr.ID()
		tr.Tracef("event 4 (foo)")
		tr.Tracef("event 5 (bar)")
		tr.Tracef("event 6 (baz)")
		tr.Finish()
	}

	{
		res, err := src.Search(ctx, &trc.SearchRequest{})
		AssertNoError(t, err)
		AssertEqual(t, 3, res.TotalCount)
		AssertEqual(t, 3, res.MatchCount)
		AssertEqual(t, 3, len(res.Traces))
		AssertEqual(t, id3, res.Traces[0].ID())
		AssertEqual(t, id2, res.Traces[1].ID())
		AssertEqual(t, id1, res.Traces[2].ID())
	}

	{
		res, err := src.Search(ctx, &trc.SearchRequest{Filter: trc.Filter{IsErrored: true}})
		AssertNoError(t, err)
		AssertEqual(t, 3, res.TotalCount)
		AssertEqual(t, 1, res.MatchCount)
		AssertEqual(t, 1, len(res.Traces))
		AssertEqual(t, id2, res.Traces[0].ID())
	}

	{
		res, err := src.Search(ctx, &trc.SearchRequest{Filter: trc.Filter{Query: "foo"}})
		AssertNoError(t, err)
		AssertEqual(t, 3, res.TotalCount)
		AssertEqual(t, 2, res.MatchCount)
		AssertEqual(t, 2, len(res.Traces))
		AssertEqual(t, id3, res.Traces[0].ID())
		AssertEqual(t, id1, res.Traces[1].ID())
	}

	{
		res, err := src.Search(ctx, &trc.SearchRequest{Filter: trc.Filter{Query: "event (1|3)"}})
		AssertNoError(t, err)
		AssertEqual(t, "event (1|3)", res.Request.Filter.Query)
		AssertEqual(t, 3, res.TotalCount)
		AssertEqual(t, 2, res.MatchCount)
		AssertEqual(t, 2, len(res.Traces))
		AssertEqual(t, id2, res.Traces[0].ID())
		AssertEqual(t, id1, res.Traces[1].ID())
	}

	{
		res, err := src.Search(ctx, &trc.SearchRequest{Filter: trc.Filter{Query: "event (1"}})
		AssertNoError(t, err)
		AssertEqual(t, 1, len(res.Problems))
		AssertEqual(t, "", res.Request.Filter.Query)
	}
}

func TestCollectorResize(t *testing.T) {
	t.Parallel()

	var (
		ctx      = context.Background()
		src      = trc.NewDefaultCollector()
		category = "my category"
		count    = 100
		ids      = []string{}
	)

	for i := 0; i < count; i++ {
		_, tr := src.NewTrace(ctx, category)
		ids = append(ids, tr.ID())
		tr.Tracef("some event")
		tr.Finish()
	}

	{
		res, err := src.Search(ctx, &trc.SearchRequest{Limit: count}) // request all count traces
		AssertNoError(t, err)                                         //
		AssertEqual(t, count, res.TotalCount)                         // we get them all
		AssertEqual(t, count, len(res.Traces))                        //
		AssertEqual(t, ids[len(ids)-1], res.Traces[0].ID())           // first trace in the result is the most recent
		AssertEqual(t, ids[0], res.Traces[len(res.Traces)-1].ID())    // last trace in the result is the oldest
	}

	fewer := count / 3
	src.SetCategorySize(fewer)

	{
		res, err := src.Search(ctx, &trc.SearchRequest{Limit: count})           // request the same count traces
		AssertNoError(t, err)                                                   //
		AssertEqual(t, fewer, res.TotalCount)                                   // but we get fewer, since we truncated each category
		AssertEqual(t, fewer, len(res.Traces))                                  //
		AssertEqual(t, ids[len(ids)-1], res.Traces[0].ID())                     // first trace in the result is still the most recent
		AssertEqual(t, ids[len(ids)-fewer], res.Traces[len(res.Traces)-1].ID()) // last trace in the result "moves up" as older traces were dropped
	}
}
