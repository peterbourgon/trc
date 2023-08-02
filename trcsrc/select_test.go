package trcsrc_test

import (
	"context"
	"testing"

	"github.com/peterbourgon/trc/trcsrc"
)

func TestSelectScenarios(t *testing.T) {
	ctx := context.Background()
	src := trcsrc.NewDefaultCollector()

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
		res, err := src.Select(ctx, &trcsrc.SelectRequest{})
		AssertNoError(t, err)
		AssertEqual(t, 3, res.TotalCount)
		AssertEqual(t, 3, res.MatchCount)
		AssertEqual(t, 3, len(res.Traces))
		AssertEqual(t, id3, res.Traces[0].ID)
		AssertEqual(t, id2, res.Traces[1].ID)
		AssertEqual(t, id1, res.Traces[2].ID)
	}

	{
		res, err := src.Select(ctx, &trcsrc.SelectRequest{Filter: trcsrc.Filter{IsErrored: true}})
		AssertNoError(t, err)
		AssertEqual(t, 3, res.TotalCount)
		AssertEqual(t, 1, res.MatchCount)
		AssertEqual(t, 1, len(res.Traces))
		AssertEqual(t, id2, res.Traces[0].ID)
	}

	{
		res, err := src.Select(ctx, &trcsrc.SelectRequest{Filter: trcsrc.Filter{Query: "foo"}})
		AssertNoError(t, err)
		AssertEqual(t, 3, res.TotalCount)
		AssertEqual(t, 2, res.MatchCount)
		AssertEqual(t, 2, len(res.Traces))
		AssertEqual(t, id3, res.Traces[0].ID)
		AssertEqual(t, id1, res.Traces[1].ID)
	}

	{
		res, err := src.Select(ctx, &trcsrc.SelectRequest{Filter: trcsrc.Filter{Query: "event (1|3)"}})
		AssertNoError(t, err)
		AssertEqual(t, "event (1|3)", res.Request.Filter.Query)
		AssertEqual(t, 3, res.TotalCount)
		AssertEqual(t, 2, res.MatchCount)
		AssertEqual(t, 2, len(res.Traces))
		AssertEqual(t, id2, res.Traces[0].ID)
		AssertEqual(t, id1, res.Traces[1].ID)
	}

	{
		res, err := src.Select(ctx, &trcsrc.SelectRequest{Filter: trcsrc.Filter{Query: "event (1"}})
		AssertNoError(t, err)
		AssertEqual(t, 1, len(res.Problems))
		AssertEqual(t, "", res.Request.Filter.Query)
	}
}
