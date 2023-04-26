package trc

import (
	"context"
	"sort"
	"time"

	"github.com/peterbourgon/trc/internal/trcringbuf"
)

// Collector maintains recent traces, in memory, grouped by category.
//
// Each unique category observed by NewTrace creates a persistent ring buffer of traces
// with a fixed size.
type Collector struct {
	categories *trcringbuf.RingBuffers[Trace]
	newTrace   func(ctx context.Context, category string) (context.Context, Trace)
}

// CollectorConfig defines the configuration parameters for a collector.
type CollectorConfig struct {
	// NewTrace is called by the collector to create a new trace for a given
	// category. If nil, the default constructor is NewTrace.
	NewTrace func(ctx context.Context, category string) (context.Context, Trace)

	// MaxTracesPerCategory specifies how many recent traces are maintained in
	// the collector for each unique category. The default value is 1000, the
	// minimum value is 10, and the maximum value is 10000.
	MaxTracesPerCategory int
}

const (
	tracesPerCategoryMin = 10
	tracesPerCategoryDef = 1000
	tracesPerCategoryMax = 10000
)

// NewCollector returns a trace collector based on the provided config.
func NewCollector(cfg CollectorConfig) *Collector {
	if cfg.NewTrace == nil {
		cfg.NewTrace = NewTrace
	}
	switch {
	case cfg.MaxTracesPerCategory <= 0:
		cfg.MaxTracesPerCategory = tracesPerCategoryDef
	case cfg.MaxTracesPerCategory < tracesPerCategoryMin:
		cfg.MaxTracesPerCategory = tracesPerCategoryMin
	case cfg.MaxTracesPerCategory > tracesPerCategoryMax:
		cfg.MaxTracesPerCategory = tracesPerCategoryMax
	}
	return &Collector{
		categories: trcringbuf.NewRingBuffers[Trace](cfg.MaxTracesPerCategory),
		newTrace:   cfg.NewTrace,
	}
}

// NewDefaultCollector is a convenience function that calls NewCollector with a
// zero value config, yielding a collector with default config parameters.
func NewDefaultCollector() *Collector {
	return NewCollector(CollectorConfig{})
}

// NewTrace creates and returns a new trace in the given context, with the given
// category, via the NewTrace function provided in the config. The trace is
// saved in the collector according to its category.
func (c *Collector) NewTrace(ctx context.Context, category string) (context.Context, Trace) {
	if tr, ok := MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}

	ctx, tr := c.newTrace(ctx, category)
	c.categories.GetOrCreate(category).Add(tr)
	return ctx, tr
}

// Search all of the traces in the collector.
func (c *Collector) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	begin := time.Now()
	ctx, tr, finish := Region(ctx, "trc.Collector.Search")
	defer finish()

	req.Normalize(ctx)

	var (
		stats   = newSearchStats(req.Bucketing)
		total   int
		allowed Traces
	)
	for _, rb := range c.categories.GetAll() {
		trs := rb.Get()
		stats.observe(trs)
		total += len(trs)
		for _, tr := range trs {
			if req.Allow(ctx, tr) {
				allowed = append(allowed, tr)
			}
		}
	}

	tr.Tracef("gathered traces")

	matched := len(allowed)
	sort.Sort(allowed)
	if len(allowed) > req.Limit {
		allowed = allowed[:req.Limit]
	}

	selected := make([]*SelectedTrace, len(allowed))
	for i, tr := range allowed {
		selected[i] = NewSelectedTrace(tr)
	}

	return &SearchResponse{
		Stats:    stats,
		Total:    total,
		Matched:  matched,
		Selected: selected,
		Duration: time.Since(begin),
	}, nil
}

// Resize all of the per-category ring buffers to the provided capacity,
// truncating older traces when necessary.
func (c *Collector) Resize(ctx context.Context, maxTracesPerCategory int) {
	c.categories.Resize(maxTracesPerCategory)
}

func (c *Collector) SetNewTrace(ctx context.Context, newTrace func(ctx context.Context, category string) (context.Context, Trace)) {
	c.newTrace = newTrace
}
