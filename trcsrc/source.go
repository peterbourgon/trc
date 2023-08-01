package trcsrc

import (
	"context"
	"sort"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcringbuf"
)

type Source struct {
	name       string
	newTrace   NewTraceFunc
	categories *trcringbuf.RingBuffers[trc.Trace]
}

var _ Selecter = (*Source)(nil)

func NewSource(name string, newTrace NewTraceFunc) *Source {
	return &Source{
		name:       name,
		newTrace:   newTrace,
		categories: trcringbuf.NewRingBuffers[trc.Trace](defaultCategorySize),
	}
}

func NewDefaultSource() *Source {
	return NewSource("default", trc.New)
}

type NewTraceFunc func(ctx context.Context, source string, category string) (context.Context, trc.Trace)

const defaultCategorySize = 1000

func (src *Source) SetSourceName(name string) *Source {
	src.name = name
	return src
}

func (src *Source) SetNewTrace(newTrace NewTraceFunc) *Source {
	src.newTrace = newTrace
	return src
}

func (src *Source) SetCategorySize(cap int) *Source {
	for _, droppedTrace := range src.categories.Resize(cap) {
		maybeFree(droppedTrace)
	}
	return src
}

func (src *Source) NewTrace(ctx context.Context, category string) (context.Context, trc.Trace) {
	if tr, ok := trc.MaybeGet(ctx); ok {
		tr.LazyTracef("(+ %s)", category)
		return ctx, tr
	}

	ctx, tr := src.newTrace(ctx, src.name, category)

	if droppedTrace, didDrop := src.categories.GetOrCreate(category).Add(tr); didDrop {
		maybeFree(droppedTrace)
	}

	return ctx, tr
}

func (src *Source) Select(ctx context.Context, req *SelectRequest) (*SelectResponse, error) {
	var (
		tr            = trc.Get(ctx)
		begin         = time.Now()
		normalizeErrs = req.Normalize()
		totalCount    = 0
		matchCount    = 0
		traces        = []*SelectedTrace{}
		stats         = NewSelectStats(req.Bucketing)
	)

	for _, ringBuf := range src.categories.GetAll() { // TODO: could do these concurrently
		var categoryTraces []*SelectedTrace
		ringBuf.Walk(func(candidate trc.Trace) error {
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
			categoryTraces = append(categoryTraces, NewSelectedTrace(candidate))
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
		Sources:    []string{src.name},
		TotalCount: totalCount,
		MatchCount: matchCount,
		Traces:     traces,
		Stats:      stats,
		Problems:   flattenErrors(normalizeErrs...),
		Duration:   time.Since(begin),
	}, nil
}

func maybeFree(tr trc.Trace) {
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
