package trc

import (
	"context"
	"sort"
	"time"

	"github.com/peterbourgon/trc/internal/trcringbuf"
)

type Collector struct {
	source     string
	newTrace   NewTraceFunc
	decorators []DecoratorFunc
	categories *trcringbuf.RingBuffers[Trace]
}

type NewTraceFunc func(ctx context.Context, source string, category string) (context.Context, Trace)

func NewDefaultCollector() *Collector {
	return NewCollector("default", New)
}

func NewCollector(source string, newTrace NewTraceFunc, decorators ...DecoratorFunc) *Collector {
	return &Collector{
		source:     source,
		newTrace:   newTrace,
		decorators: decorators,
		categories: trcringbuf.NewRingBuffers[Trace](defaultCategorySize),
	}
}

const defaultCategorySize = 1000

func (c *Collector) SetSourceName(name string) *Collector {
	c.source = name
	return c
}

func (c *Collector) SetNewTrace(newTrace NewTraceFunc) *Collector {
	c.newTrace = newTrace
	return c
}

func (c *Collector) SetDecorators(decorators ...DecoratorFunc) *Collector {
	c.decorators = decorators
	return c
}

func (c *Collector) SetCategorySize(cap int) *Collector {
	for _, droppedTrace := range c.categories.Resize(cap) {
		maybeFree(droppedTrace)
	}
	return c
}

func (c *Collector) NewTrace(ctx context.Context, category string) (context.Context, Trace) {
	if tr, ok := MaybeGet(ctx); ok {
		tr.LazyTracef("(+ %s)", category)
		return ctx, tr
	}

	ctx, tr := c.newTrace(ctx, c.source, category)

	if len(c.decorators) > 0 {
		for _, d := range c.decorators {
			tr = d(tr)
		}
		ctx, tr = Put(ctx, tr)
	}

	if droppedTrace, didDrop := c.categories.GetOrCreate(category).Add(tr); didDrop {
		maybeFree(droppedTrace)
	}

	return ctx, tr
}

func (c *Collector) Select(ctx context.Context, req *SelectRequest) (*SelectResponse, error) {
	var (
		tr            = Get(ctx)
		begin         = time.Now()
		normalizeErrs = req.Normalize()
		stats         = NewSelectStats(req.Bucketing)
		totalCount    = 0
		matchCount    = 0
		traces        = []*SelectedTrace{}
	)

	for _, ringBuf := range c.categories.GetAll() { // TODO: could do these concurrently
		var categoryTraces []*SelectedTrace
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
			categoryTraces = append(categoryTraces, NewSelectedTrace(candidate).TrimStacks(req.StackDepth))
			matchCount++
			return nil
		})
		traces = append(traces, categoryTraces...)
	}

	// Sort most recent first.
	sort.Slice(traces, func(i, j int) bool {
		return traces[i].Started.After(traces[j].Started)
	})

	// Take only the most recent traces as per the limit.
	if len(traces) > req.Limit {
		traces = traces[:req.Limit]
	}

	tr.LazyTracef("%s -> total count %d, match count %d, trace count %d", req.String(), totalCount, matchCount, len(traces))

	return &SelectResponse{
		Request:    req,
		Sources:    []string{c.source},
		TotalCount: totalCount,
		MatchCount: matchCount,
		Traces:     traces,
		Stats:      stats,
		Problems:   flattenErrors(normalizeErrs...),
		Duration:   time.Since(begin),
	}, nil
}

func maybeFree(tr Trace) {
	if f, ok := tr.(interface{ Free() }); ok {
		f.Free()
	}
}

func flattenErrors(errs ...error) []string {
	if len(errs) <= 0 {
		return nil
	}
	strs := make([]string, len(errs))
	for i := range errs {
		strs[i] = errs[i].Error()
	}
	return strs
}
