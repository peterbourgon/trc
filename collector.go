package trc

import (
	"context"
	"sort"
	"time"

	"github.com/peterbourgon/trc/internal/trcringbuf"
	"github.com/peterbourgon/trc/internal/trcutil"
)

// Collector maintains a set of traces in memory, grouped by category.
type Collector struct {
	source     string
	newTrace   NewTraceFunc
	broker     *Broker
	decorators []DecoratorFunc
	categories *trcringbuf.RingBuffers[Trace]
}

var _ Searcher = (*Collector)(nil)

// NewTraceFunc describes a function that produces a new trace with a specific
// source and category, and which is decorated by the given decorators. It
// returns a context containing the new trace, as well as the new trace itself.
type NewTraceFunc func(ctx context.Context, source string, category string, decorators ...DecoratorFunc) (context.Context, Trace)

// NewDefaultCollector returns a new collector with the source "default" and
// using [New] to produce new traces.
func NewDefaultCollector() *Collector {
	return NewCollector(CollectorConfig{
		Source:   "default",
		NewTrace: New,
	})
}

// CollectorConfig captures the configuration parameters for a collector.
type CollectorConfig struct {
	// Source is used as the source for all traces created within the collector.
	// If not provided, the "default" source is used.
	Source string

	// NewTrace is used to construct the traces in the collector. If not
	// provided, the [New] function is used.
	NewTrace NewTraceFunc

	// Decorators are applied to every new trace created in the collector.
	Decorators []DecoratorFunc

	// Broker is used for streaming traces and events. If not provided, a new
	// broker will be constructed and used.
	Broker *Broker
}

// NewCollector returns a new collector with the provided config.
func NewCollector(cfg CollectorConfig) *Collector {
	if cfg.Source == "" {
		cfg.Source = "default"
	}

	if cfg.NewTrace == nil {
		cfg.NewTrace = New
	}

	if cfg.Broker == nil {
		cfg.Broker = NewBroker()
	}

	return &Collector{
		source:     cfg.Source,
		newTrace:   cfg.NewTrace,
		broker:     cfg.Broker,
		decorators: cfg.Decorators,
		categories: trcringbuf.NewRingBuffers[Trace](1000),
	}
}

// SetSourceName sets the source used by the collector.
//
// The method returns its receiver to allow for builder-style construction.
func (c *Collector) SetSourceName(name string) *Collector {
	c.source = name
	return c
}

// SetNewTrace sets the new trace function used by the collector.
//
// The method returns its receiver to allow for builder-style construction.
func (c *Collector) SetNewTrace(newTrace NewTraceFunc) *Collector {
	c.newTrace = newTrace
	return c
}

// SetDecorators completely resets the decorators used by the collector.
//
// The method returns its receiver to allow for builder-style construction.
func (c *Collector) SetDecorators(decorators ...DecoratorFunc) *Collector {
	c.decorators = decorators
	return c
}

// SetCategorySize resets the max size of each category in the collector. If any
// categories are currently larger than the given capacity, they will be reduced
// by dropping old traces. The default capacity is 1000.
//
// The method returns its receiver to allow for builder-style construction.
func (c *Collector) SetCategorySize(cap int) *Collector {
	for _, droppedTrace := range c.categories.Resize(cap) {
		maybeFree(droppedTrace)
	}
	return c
}

// NewTrace produces a new trace in the collector with the given category,
// injects it into the given context, and returns a new derived context
// containing the trace, as well as the new trace itself.
func (c *Collector) NewTrace(ctx context.Context, category string) (context.Context, Trace) {
	if tr, ok := MaybeGet(ctx); ok {
		tr.LazyTracef("(+ %s)", category)
		return ctx, tr
	}

	ctx, tr := c.newTrace(ctx, c.source, category, publishDecorator(c.broker))

	for _, d := range c.decorators {
		tr = d(tr)
	}

	if droppedTrace, didDrop := c.categories.GetOrCreate(category).Add(tr); didDrop {
		maybeFree(droppedTrace)
	}

	return Put(ctx, tr)
}

// Search the collector for traces, according to the provided search request.
func (c *Collector) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	var (
		tr            = Get(ctx)
		begin         = time.Now()
		normalizeErrs = req.Normalize()
		stats         = NewSearchStats(req.Bucketing)
		totalCount    = 0
		matchCount    = 0
		traces        = []*StaticTrace{}
	)

	for _, ringBuf := range c.categories.GetAll() { // TODO: could do these concurrently
		var categoryTraces []*StaticTrace
		ringBuf.Walk(func(candidate Trace) error {
			// Every candidate trace should be observed.
			stats.Observe(candidate)
			totalCount++

			// If we already have the max number of traces from this category,
			// then we won't select any more. We do this first, because it's
			// cheaper than checking allow.
			if len(categoryTraces) >= req.Limit {
				return nil
			}

			// If the filter won't allow this trace, then we won't select it.
			if !req.Filter.Allow(candidate) {
				return nil
			}

			// Otherwise, collect a static copy of the trace.
			categoryTraces = append(categoryTraces, NewSearchTrace(candidate).TrimStacks(req.StackDepth))
			matchCount++
			return nil
		})
		traces = append(traces, categoryTraces...)
	}

	// Sort most recent first.
	sort.Sort(staticTracesNewestFirst(traces))

	// Take only the most recent traces as per the limit.
	if len(traces) > req.Limit {
		traces = traces[:req.Limit]
	}

	tr.LazyTracef("%s -> total %d, matched %d, returned %d", c.source, totalCount, matchCount, len(traces))

	return &SearchResponse{
		Request:    req,
		Sources:    []string{c.source},
		TotalCount: totalCount,
		MatchCount: matchCount,
		Traces:     traces,
		Stats:      stats,
		Problems:   trcutil.FlattenErrors(normalizeErrs...),
		Duration:   time.Since(begin),
	}, nil
}

// Stream traces matching the filter to the channel, returning when the context
// is canceled. See [Broker.Stream] for more details.
func (c *Collector) Stream(ctx context.Context, f Filter, ch chan<- Trace) (StreamStats, error) {
	return c.broker.Stream(ctx, f, ch)
}

// StreamStats returns statistics about a currently active subscription.
func (c *Collector) StreamStats(ctx context.Context, ch chan<- Trace) (StreamStats, error) {
	return c.broker.StreamStats(ctx, ch)
}

//
//
//

func maybeFree(tr Trace) {
	if f, ok := tr.(interface{ Free() }); ok {
		f.Free()
	}
}
