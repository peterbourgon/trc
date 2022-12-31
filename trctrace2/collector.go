package trctrace

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcds"
)

type Collector struct {
	categories *trcds.RingBuffers[trc.Trace]
}

var _ Searcher = (*Collector)(nil)

func NewCollector(maxPerCategory int) *Collector {
	return &Collector{
		categories: trcds.NewRingBuffers[trc.Trace](maxPerCategory),
	}
}

func (c *Collector) NewTrace(ctx context.Context, category string) (context.Context, trc.Trace) {
	if tr, ok := trc.MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}

	ctx, tr := trc.NewTrace(ctx, category)
	c.categories.GetOrCreate(category).Add(tr)
	return ctx, tr
}

func (c *Collector) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	_, tr, finish := trc.Region(ctx, "Collector Search")
	defer finish()

	begin := time.Now()
	stopwatch := trc.NewStopwatch()
	defer func() { tr.Tracef("%s", stopwatch) }()

	if err := req.Normalize(); err != nil {
		return nil, fmt.Errorf("sanitize request: %w", err)
	}

	var overall trc.Traces // TODO: allocs
	for cat, rb := range c.categories.GetAll() {
		if err := rb.Walk(func(tr trc.Trace) error {
			overall = append(overall, tr)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("gathering traces (%s): %w", cat, err)
		}
	}

	total := len(overall)

	walkTime := stopwatch.Lap("walk")

	stats := NewStatsFrom(req.Bucketing, overall)

	statsTime := stopwatch.Lap("stats")

	var allowed trc.Traces
	for _, tr := range overall {
		if req.Allow(tr) {
			allowed = append(allowed, tr)
		}
	}

	matched := len(allowed)

	evalTime := stopwatch.Lap("eval")

	sort.Sort(allowed)
	if len(allowed) > req.Limit {
		allowed = allowed[:req.Limit]
	}

	selected := make([]*trc.StaticTrace, len(allowed))
	for i := range allowed {
		selected[i] = trc.NewStaticTrace(allowed[i])
	}

	stopwatch.Lap("select")

	{
		if n := time.Duration(len(overall)); n > 0 {
			tr.Tracef(
				"total trace count %d: walk %d ns/trace, stats %d ns/trace, eval %d ns/trace",
				total,
				walkTime/n,
				statsTime/n,
				evalTime/n,
			)
		}
	}

	duration := time.Since(begin)

	return &SearchResponse{
		Request:  req,
		Stats:    stats,
		Total:    total,
		Matched:  matched,
		Selected: selected,
		Problems: nil,
		Duration: duration,
	}, nil
}
