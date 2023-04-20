package trccoll

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcringbuf"
	"github.com/peterbourgon/trc/internal/trcstatic"
)

type Collector struct {
	categories *trcringbuf.RingBuffers[trc.Trace]
}

func NewCollector(maxTracesPerCategory int) *Collector {
	return &Collector{
		categories: trcringbuf.NewRingBuffers[trc.Trace](maxTracesPerCategory),
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

func (c *Collector) Select(ctx context.Context, allow func(trc.Trace) error) ([]trc.Trace, error) {
	var selected []trc.Trace

	for category, rb := range c.categories.GetAll() {
		if err := rb.Walk(func(tr trc.Trace) error {
			if allow(tr) == nil {
				selected = append(selected, tr)
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("category %s: walk traces: %w", category, err)
		}
	}

	sort.Slice(selected, func(i, j int) bool {
		return selected[i].Start().After(selected[j].Start())
	})

	return selected, nil
}

func (c *Collector) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	begin := time.Now()

	_, tr, finish := trc.Region(ctx, "trc.Collector.Search")
	defer finish()

	_, _, _ = begin, tr, finish

	problems := req.Normalize()
	for _, problem := range problems {
		tr.Tracef("normalize search request: %v", problem)
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
	tr.Tracef("walked all traces, total count %d", total)

	stats := newStatsFrom(req.Bucketing, overall)
	tr.Tracef("calculated stats")

	var allowed trc.Traces
	for _, tr := range overall {
		if req.allow(tr) {
			allowed = append(allowed, tr)
		}
	}

	matched := len(allowed)

	sort.Sort(allowed)
	if len(allowed) > req.Limit {
		allowed = allowed[:req.Limit]
	}

	selected := make([]*trcstatic.StaticTrace, len(allowed))
	for i := range allowed {
		selected[i] = trcstatic.NewStaticTrace(allowed[i])
	}

	tr.Tracef("matched %d, selected %d", matched, len(selected))

	return &SearchResponse{
		Stats:    stats,
		Total:    total,
		Matched:  matched,
		Selected: selected,
		Problems: problems,
		Duration: time.Since(begin),
	}, nil
}
