package trcstore

import (
	"context"
	"sort"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcringbuf"
)

// Collector maintains recent traces in memory, grouped by category, and allows
// those traces to be queried by operators via the [Searcher] interface. Each
// unique category creates a persistent and fixed-size ring buffer in the
// collector, so care should be taken to limit the total number of categories.
type Collector struct {
	source      string
	constructor func(ctx context.Context, source, category string) (context.Context, trc.Trace)
	categories  *trcringbuf.RingBuffers[trc.Trace]
}

var _ Searcher = (*Collector)(nil)

// CollectorConfig defines the configuration parameters for a collector.
type CollectorConfig struct {
	// Source is assigned to all traces constructed within the collector.
	// Optional. By default, source is an empty string, which means it is not
	// set and generally ignored.
	Source string

	// Constructor is invoked by the NewTrace method to create a new trace.
	// Optional. By default, the New function from package trc is used.
	Constructor func(ctx context.Context, source, category string) (context.Context, trc.Trace)

	// CategorySize specifies the size of each per-category ring buffer in the
	// collector. Optional. By default, the category size is 1000. The minimum
	// is 1, and the maximum is 10000.
	CategorySize int
}

const (
	categorySizeMin = 1
	categorySizeDef = 1000
	categorySizeMax = 10000
)

// NewCollector returns an empty trace collector based on the provided config.
func NewCollector(cfg CollectorConfig) *Collector {
	if cfg.Constructor == nil {
		cfg.Constructor = trc.New
	}

	switch {
	case cfg.CategorySize <= 0:
		cfg.CategorySize = categorySizeDef
	case cfg.CategorySize < categorySizeMin:
		cfg.CategorySize = categorySizeMin
	case cfg.CategorySize > categorySizeMax:
		cfg.CategorySize = categorySizeMax
	}

	return &Collector{
		source:      cfg.Source,
		constructor: cfg.Constructor,
		categories:  trcringbuf.NewRingBuffers[trc.Trace](cfg.CategorySize),
	}
}

// NewDefaultCollector is a convenience function that calls NewCollector with a
// zero value config, returning a collector with a default configuration.
func NewDefaultCollector() *Collector {
	return NewCollector(CollectorConfig{})
}

// Resize all of the per-category ring buffers in the collector to the provided
// capacity, truncating older traces when necessary.
func (c *Collector) Resize(ctx context.Context, categorySize int) {
	dropped := c.categories.Resize(categorySize)
	for _, tr := range dropped {
		if f, ok := tr.(interface{ Free() }); ok {
			f.Free()
		}
	}
}

// SetSource sets the source string assigned to traces created in the collector.
// Callers should ensure this method is never called concurrently with other
// methods on the collector, especially NewTrace and Search.
func (c *Collector) SetSource(source string) {
	c.source = source
}

// NewTrace creates a new trace with the provided category, injects it to the
// provided context, and returns the new derived context and the new trace. The
// trace is also stored in the collector, in a ring buffer corresponding to the
// provided category.
func (c *Collector) NewTrace(ctx context.Context, category string) (context.Context, trc.Trace) {
	if tr, ok := trc.MaybeGet(ctx); ok {
		tr.LazyTracef("(+ %s)", category)
		return ctx, tr
	}

	ctx, tr := c.constructor(ctx, c.source, category)

	dropped, ok := c.categories.GetOrCreate(category).Add(tr)
	if ok {
		if f, ok := dropped.(interface{ Free() }); ok {
			f.Free()
		}
	}

	return ctx, tr
}

// Search over the traces in the collector.
func (c *Collector) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	begin := time.Now()
	tr := trc.Get(ctx)

	req.Normalize(ctx) // it's possible for e.g. tests to call Search directly

	var (
		stats    = NewStats(req.Bucketing)
		total    int
		selected []*SearchTrace
	)
	for _, rb := range c.categories.GetAll() {
		var categorySelected []*SearchTrace
		rb.Walk(func(tr trc.Trace) error {
			// Every trace should update the total, and be observed by stats.
			total++
			stats.Observe(tr)

			// If we already have the max number of traces from this category,
			// then we won't select any more. We do this first, because it's
			// cheaper than checking allow.
			if len(categorySelected) >= req.Limit {
				return nil
			}

			// If the request won't allow this trace, then we won't select it.
			if !req.Allow(ctx, tr) {
				return nil
			}

			// Otherwise, collect a static copy of the trace.
			categorySelected = append(categorySelected, NewSearchTrace(tr))
			return nil
		})
		selected = append(selected, categorySelected...)
	}

	matched := len(selected)

	sort.Slice(selected, func(i, j int) bool {
		return selected[i].Started.After(selected[j].Started)
	})
	if len(selected) > req.Limit {
		selected = selected[:req.Limit]
	}

	tr.LazyTracef("categories=%d traces=%d matched=%d selected=%d", len(stats.Categories), total, matched, len(selected))

	var sources []string
	if c.source != "" {
		sources = []string{c.source} // TODO: is this right?
	}

	return &SearchResponse{
		Sources:  sources,
		Stats:    stats,
		Total:    total,
		Matched:  matched,
		Selected: selected,
		Problems: nil, // no problems from a direct search against a collector
		Duration: time.Since(begin),
	}, nil
}
