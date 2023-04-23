package trc

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/peterbourgon/trc/internal/trcringbuf"
)

type Collector struct {
	categories *trcringbuf.RingBuffers[Trace]
	newTrace   func(ctx context.Context, category string) (context.Context, Trace)
}

type CollectorConfig struct {
	NewTraceFunc         func(ctx context.Context, category string) (context.Context, Trace)
	MaxTracesPerCategory int
}

const DefaultMaxTracesPerCategory = 1000

func NewCollector() *Collector {
	return NewCollectorConfig(CollectorConfig{})
}

func NewCollectorConfig(cfg CollectorConfig) *Collector {
	if cfg.NewTraceFunc == nil {
		cfg.NewTraceFunc = NewTrace
	}
	if cfg.MaxTracesPerCategory <= 0 {
		cfg.MaxTracesPerCategory = DefaultMaxTracesPerCategory
	}
	return &Collector{
		categories: trcringbuf.NewRingBuffers[Trace](cfg.MaxTracesPerCategory),
		newTrace:   cfg.NewTraceFunc,
	}
}

func (s *Collector) NewTrace(ctx context.Context, category string) (context.Context, Trace) {
	if tr, ok := MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}

	ctx, tr := s.newTrace(ctx, category)
	s.categories.GetOrCreate(category).Add(tr)
	return ctx, tr
}

func (s *Collector) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	begin := time.Now()

	_, tr, finish := Region(ctx, "Collector.Search")
	defer finish()

	_, _, _ = begin, tr, finish

	problems := req.Normalize()
	for _, problem := range problems {
		tr.Tracef("normalize search request: %v", problem)
	}

	var overall Traces // TODO: allocs
	for cat, rb := range s.categories.GetAll() {
		if err := rb.Walk(func(tr Trace) error {
			overall = append(overall, tr)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("gathering traces (%s): %w", cat, err)
		}
	}

	total := len(overall)

	tr.Tracef("walked all traces, total count %d", total)

	stats := newSearchStatsFrom(req.Bucketing, overall)

	tr.Tracef("calculated stats")

	tr.Tracef("selecting traces...")

	var allowed Traces
	{
		_, _, finish := Region(ctx, "Collector.Search:Allow")
		for _, tr := range overall {
			if req.Allow(ctx, tr) {
				allowed = append(allowed, tr)
			}
		}
		finish()
	}

	matched := len(allowed)

	tr.Tracef("sorting traces...")

	sort.Sort(allowed)
	if len(allowed) > req.Limit {
		allowed = allowed[:req.Limit]
	}

	tr.Tracef("limiting traces...")

	selected := make([]*SelectedTrace, len(allowed))
	for i := range allowed {
		selected[i] = NewSelectedTrace(allowed[i])
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
