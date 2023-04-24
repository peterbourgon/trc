package trc

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/peterbourgon/trc/internal/trcringbuf"
)

// Collector maintains recent traces, in memory, grouped by category.
type Collector struct {
	categories *trcringbuf.RingBuffers[Trace]
	newTrace   func(ctx context.Context, category string) (context.Context, Trace)
}

// CollectorConfig defines the configuration parameters for a collector.
type CollectorConfig struct {
	// NewTraceFunc is called by the collector to create a new trace for a given
	// category. If nil, the default constructor is [NewTrace].
	NewTraceFunc func(ctx context.Context, category string) (context.Context, Trace)

	// MaxTracesPerCategory specifies how many traces are maintained in the
	// collector for each unique category. If zero, the default value is 1000.
	MaxTracesPerCategory int
}

const defaultMaxTracesPerCategory = 1000

// NewCollector is a convenience function that calls NewCollectorConfig with a
// zero value config, yielding a collector with default config parameters.
func NewCollector() *Collector {
	return NewCollectorConfig(CollectorConfig{})
}

// NewCollectorConfig returns a trace collector based on the provided config.
func NewCollectorConfig(cfg CollectorConfig) *Collector {
	if cfg.NewTraceFunc == nil {
		cfg.NewTraceFunc = NewTrace
	}
	if cfg.MaxTracesPerCategory <= 0 {
		cfg.MaxTracesPerCategory = defaultMaxTracesPerCategory
	}
	return &Collector{
		categories: trcringbuf.NewRingBuffers[Trace](cfg.MaxTracesPerCategory),
		newTrace:   cfg.NewTraceFunc,
	}
}

// NewTrace creates and returns a new trace in the given context, with the given
// category, via the NewTraceFunc provided in the config. The trace is saved in
// the collector by its category.
func (s *Collector) NewTrace(ctx context.Context, category string) (context.Context, Trace) {
	if tr, ok := MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}

	ctx, tr := s.newTrace(ctx, category)
	s.categories.GetOrCreate(category).Add(tr)
	return ctx, tr
}

// Search all of the traces in the collector.
func (s *Collector) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	begin := time.Now()
	_, tr, finish := Region(ctx, "Collector.Search")
	defer finish()

	problems := req.Normalize()
	for _, problem := range problems {
		tr.Tracef("normalize search request: %v", problem)
	}

	tr.Tracef("walking traces...")

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

	tr.Tracef("walked %d, calculating stats...", total)

	stats := newSearchStatsFrom(req.Bucketing, overall)

	tr.Tracef("category count %d, finding matching traces...", len(stats.Categories))

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

	tr.Tracef("matched %d, sorting and limiting...", matched)

	sort.Sort(allowed)
	if len(allowed) > req.Limit {
		allowed = allowed[:req.Limit]
	}

	selected := make([]*SelectedTrace, len(allowed))
	for i := range allowed {
		selected[i] = NewSelectedTrace(allowed[i])
	}

	tr.Tracef("selected %d", len(selected))

	return &SearchResponse{
		Stats:    stats,
		Total:    total,
		Matched:  matched,
		Selected: selected,
		Problems: problems,
		Duration: time.Since(begin),
	}, nil
}
