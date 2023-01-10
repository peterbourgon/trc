package trctrace

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/peterbourgon/trc"
)

type MultiSearcher []Searcher

func (ms MultiSearcher) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	tr := trc.FromContext(ctx)
	begin := time.Now()

	type tuple struct {
		id  string
		res *SearchResponse
		err error
	}

	// Scatter.
	tuplec := make(chan tuple, len(ms))
	for i, s := range ms {
		go func(id string, s Searcher) {
			ctx, _ := trc.PrefixTraceContext(ctx, "<%s>", id)
			res, err := s.Search(ctx, req)
			tuplec <- tuple{id, res, err}
		}(strconv.Itoa(i+1), s)
	}

	tr.Tracef("scattered request count %d", len(ms))

	// Gather.
	aggregate := &SearchResponse{}
	for i := 0; i < cap(tuplec); i++ {
		t := <-tuplec
		switch {
		case t.res == nil && t.err == nil: // weird
			tr.Tracef("%s: weird: no result, no error", t.id)
			aggregate.Problems = append(aggregate.Problems, "got nil search response with nil error -- weird")
		case t.res == nil && t.err != nil: // error case
			tr.Tracef("%s: error: %v", t.id, t.err)
			aggregate.Problems = append(aggregate.Problems, t.err.Error())
		case t.res != nil && t.err == nil: // success case
			//tr.Tracef("%s: success", t.id)
			aggregate.Sources = append(aggregate.Sources, t.res.Sources...)
			aggregate.Stats = CombineStats(aggregate.Stats, t.res.Stats)
			aggregate.Total += t.res.Total
			aggregate.Matched += t.res.Matched
			aggregate.Selected = append(aggregate.Selected, t.res.Selected...) // needs sort+limit
			aggregate.Problems = append(aggregate.Problems, t.res.Problems...)
		case t.res != nil && t.err != nil: // weird
			tr.Tracef("%s: weird: valid result (accepting it) with error: %v", t.id, t.err)
			aggregate.Sources = append(aggregate.Sources, t.res.Sources...)
			aggregate.Stats = CombineStats(aggregate.Stats, t.res.Stats)
			aggregate.Total += t.res.Total
			aggregate.Matched += t.res.Matched
			aggregate.Selected = append(aggregate.Selected, t.res.Selected...) // needs sort+limit
			aggregate.Problems = append(aggregate.Problems, t.res.Problems...)
			aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("got valid search response with error (%v) -- weird", t.err))
		}
	}

	tr.Tracef("gathered responses")

	sort.Slice(aggregate.Sources, func(i, j int) bool {
		return aggregate.Sources[i].Name < aggregate.Sources[j].Name
	})

	// At this point, the aggregate response has all of the raw data it's ever
	// gonna get. We need to do a little bit of post-processing. First, we need
	// to sort all of the selected traces by start time, and then limit them by
	// the requested limit.

	sort.Slice(aggregate.Selected, func(i, j int) bool {
		return aggregate.Selected[i].Start().After(aggregate.Selected[j].Start())
	})

	if len(aggregate.Selected) > req.Limit {
		aggregate.Selected = aggregate.Selected[:req.Limit]
	}

	// Duration is also defined across all individual requests.
	aggregate.Duration = time.Since(begin)

	// That should be it.
	return aggregate, nil
}
