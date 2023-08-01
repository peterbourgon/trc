package trcsrc_test

import (
	"context"
	"testing"

	"github.com/peterbourgon/trc/trcsrc"
)

func TestSourceResize(t *testing.T) {
	var (
		ctx      = context.Background()
		src      = trcsrc.NewDefaultSource()
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
		res, err := src.Select(ctx, &trcsrc.SelectRequest{Limit: count}) // request all count traces
		AssertNoError(t, err)                                            //
		AssertEqual(t, count, res.TotalCount)                            // we get them all
		AssertEqual(t, count, len(res.Traces))                           //
		AssertEqual(t, ids[len(ids)-1], res.Traces[0].ID)                // first trace in the result is the most recent
		AssertEqual(t, ids[0], res.Traces[len(res.Traces)-1].ID)         // last trace in the result is the oldest
	}

	fewer := count / 3
	src.SetCategorySize(fewer)

	{
		res, err := src.Select(ctx, &trcsrc.SelectRequest{Limit: count})      // request the same count traces
		AssertNoError(t, err)                                                 //
		AssertEqual(t, fewer, res.TotalCount)                                 // but we get fewer, since we truncated each category
		AssertEqual(t, fewer, len(res.Traces))                                //
		AssertEqual(t, ids[len(ids)-1], res.Traces[0].ID)                     // first trace in the result is still the most recent
		AssertEqual(t, ids[len(ids)-fewer], res.Traces[len(res.Traces)-1].ID) // last trace in the result "moves up" as older traces were dropped
	}
}
