package trcstore

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcringbuf"
)

type Store struct {
	categories *trcringbuf.RingBuffers[trc.Trace]
	newTrace   func(ctx context.Context, category string) (context.Context, trc.Trace)
}

type StoreConfig struct {
	NewTraceFunc         func(ctx context.Context, category string) (context.Context, trc.Trace)
	MaxTracesPerCategory int
}

const DefaultMaxTracesPerCategory = 1000

func NewStore() *Store {
	return NewStoreConfig(StoreConfig{})
}

func NewStoreConfig(cfg StoreConfig) *Store {
	if cfg.NewTraceFunc == nil {
		cfg.NewTraceFunc = trc.NewTrace
	}
	if cfg.MaxTracesPerCategory <= 0 {
		cfg.MaxTracesPerCategory = DefaultMaxTracesPerCategory
	}
	return &Store{
		categories: trcringbuf.NewRingBuffers[trc.Trace](cfg.MaxTracesPerCategory),
		newTrace:   cfg.NewTraceFunc,
	}
}

func (s *Store) NewTrace(ctx context.Context, category string) (context.Context, trc.Trace) {
	if tr, ok := trc.MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}

	ctx, tr := s.newTrace(ctx, category)
	s.categories.GetOrCreate(category).Add(tr)
	return ctx, tr
}

func (s *Store) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	begin := time.Now()

	_, tr, finish := trc.Region(ctx, "Collector.Search")
	defer finish()

	_, _, _ = begin, tr, finish

	problems := req.Normalize()
	for _, problem := range problems {
		tr.Tracef("normalize search request: %v", problem)
	}

	var overall trc.Traces // TODO: allocs
	for cat, rb := range s.categories.GetAll() {
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
		if req.Allow(tr) {
			allowed = append(allowed, tr)
		}
	}

	matched := len(allowed)

	sort.Sort(allowed)
	if len(allowed) > req.Limit {
		allowed = allowed[:req.Limit]
	}

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
