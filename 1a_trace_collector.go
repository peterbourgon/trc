package trc

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"time"
)

const (
	traceQueryLimitMin = 1
	traceQueryLimitDef = 10
	traceQueryLimitMax = 1000
)

var defaultBucketing = []time.Duration{
	0 * time.Second,
	1 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	1000 * time.Millisecond,
}

//
//
//

type TraceCollector struct {
	byCategory *ringBuffers[Trace]
}

func NewTraceCollector(max int) *TraceCollector {
	return &TraceCollector{
		byCategory: newRingBuffers[Trace](max),
	}
}

func (tc *TraceCollector) NewTrace(ctx context.Context, category string) (context.Context, Trace) {
	ctx, tr := NewTrace(ctx, category)
	tc.byCategory.getOrCreate(category).add(tr)
	return ctx, tr
}

func (tc *TraceCollector) GetOrCreateTrace(ctx context.Context, category string) (context.Context, Trace) {
	if tr, ok := MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}
	return tc.NewTrace(ctx, category)
}

func (tc *TraceCollector) TraceQuery(ctx context.Context, req *TraceQueryRequest) (*TraceQueryResponse, error) {
	tr := FromContext(ctx)
	req.sanitize()

	var overall, allowed Traces
	{
		for cat, rb := range tc.byCategory.getAll() {
			if err := rb.walk(func(tr Trace) error {
				overall = append(overall, tr)
				if req.allow(tr) {
					allowed = append(allowed, tr)
				}
				return nil
			}); err != nil {
				return nil, fmt.Errorf("gathering traces (%s): %w", cat, err)
			}
		}
	}
	matched := len(allowed)
	tr.Tracef("fetched traces: overall %d, matched %d", len(overall), matched)

	stats := newTraceCollectorStats(overall, req.Bucketing)
	tr.Tracef("computed stats")

	selected := allowed
	sort.Sort(selected)
	if len(selected) > req.Limit {
		selected = selected[:req.Limit]
	}
	tr.Tracef("selected %d", len(selected))

	return &TraceQueryResponse{
		Stats:    stats,
		Matched:  matched,
		Selected: selected,
	}, nil
}

//
//
//

// TraceQueryRequest collects the parameters used to query a trace collector.
type TraceQueryRequest struct {
	IDs         []string
	Category    string
	IsActive    bool
	IsFinished  bool
	IsSucceeded bool
	IsErrored   bool
	MinDuration *time.Duration // minimum
	Bucketing   []time.Duration
	Search      *regexp.Regexp
	Limit       int
}

func (req *TraceQueryRequest) sanitize() {
	if req.Bucketing == nil {
		req.Bucketing = defaultBucketing
	}

	switch {
	case req.Limit <= 0:
		req.Limit = traceQueryLimitDef
	case req.Limit < traceQueryLimitMin:
		req.Limit = traceQueryLimitMin
	case req.Limit > traceQueryLimitMax:
		req.Limit = traceQueryLimitMax
	}
}

func (f *TraceQueryRequest) allow(tr Trace) bool {
	if len(f.IDs) > 0 {
		var found bool
		for _, id := range f.IDs {
			if id == tr.ID() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if f.Category != "" && tr.Category() != f.Category {
		return false
	}

	if f.IsActive && !tr.Active() {
		return false
	}

	if f.IsFinished && !tr.Finished() {
		return false
	}

	if f.IsSucceeded && !tr.Succeeded() {
		return false
	}

	if f.IsErrored && !tr.Errored() {
		return false
	}

	if f.MinDuration != nil && tr.Duration() < *f.MinDuration {
		return false
	}

	if f.Search != nil {
		if matchedSomething := func() bool {
			if f.Search.MatchString(tr.ID()) {
				return true
			}
			if f.Search.MatchString(tr.Category()) {
				return true
			}
			for _, ev := range tr.Events() {
				if ev.MatchRegexp(f.Search) {
					return true
				}
			}
			return false
		}(); !matchedSomething {
			return false
		}
	}

	return true
}

//
//
//

// TraceQueryResponse represents the results of a trace query.
type TraceQueryResponse struct {
	Stats    *TraceCollectorStats
	Matched  int
	Selected Traces
	Problems []string
}

//
//
//

// TraceCollectorStats is a summary view of a set of traces. It's returned as
// part of a trace collector's query response, and in that case represents all
// traces in the collector, with bucketing as specified by the query.
type TraceCollectorStats struct {
	Categories []*TraceQueryCategoryStats
}

func newTraceCollectorStats(trs Traces, bucketing []time.Duration) *TraceCollectorStats {
	// Group the traces into stats buckets by category.
	byCategory := map[string]*TraceQueryCategoryStats{}
	for _, tr := range trs {
		var (
			category  = tr.Category()
			start     = tr.Start()
			duration  = tr.Duration()
			succeeded = tr.Succeeded()
			errored   = tr.Errored()
			finished  = tr.Finished()
			active    = tr.Active()
		)

		// If the bucket doesn't exist yet, create it.
		st, ok := byCategory[category]
		if !ok {
			st = newTraceQueryCategoryStats(category, bucketing)
			byCategory[category] = st
		}

		// Update the counters for the category.
		incrIf(&st.NumSucceeded, succeeded)
		incrIf(&st.NumErrored, errored)
		incrIf(&st.NumFinished, finished)
		incrIf(&st.NumActive, active)
		incrIf(&st.NumTotal, true)
		olderOf(&st.Oldest, start)
		newerOf(&st.Newest, start)

		// Update the counters for each bucket that the trace satisfies.
		for _, b := range st.Buckets {
			if duration >= b.MinDuration {
				incrIf(&b.NumSucceeded, succeeded)
				incrIf(&b.NumErrored, errored)
				incrIf(&b.NumFinished, finished)
				incrIf(&b.NumActive, active)
				incrIf(&b.NumTotal, true)
				olderOf(&b.Oldest, start)
				newerOf(&b.Newest, start)
			}
		}
	}

	// Flatten the per-category stats into a slice.
	flattened := make([]*TraceQueryCategoryStats, 0, len(byCategory))
	for _, sts := range byCategory {
		flattened = append(flattened, sts)
	}
	sort.Slice(flattened, func(i, j int) bool {
		return flattened[i].Name < flattened[j].Name
	})

	// That'll do.
	return &TraceCollectorStats{
		Categories: flattened,
	}
}

// Overall returns stats for a synthetic category representing all traces.
func (ts *TraceCollectorStats) Overall() (*TraceQueryCategoryStats, error) {
	overall := &TraceQueryCategoryStats{
		Name: "overall",
	}
	for _, cat := range ts.Categories {
		if err := mergeTraceQueryCategoryStats(overall, cat); err != nil {
			return nil, fmt.Errorf("merge %q: %w", cat.Name, err)
		}
	}
	return overall, nil
}

// Bucketing is the set of durations by which finished traces are grouped.
func (ts *TraceCollectorStats) Bucketing() []time.Duration {
	if len(ts.Categories) == 0 {
		return defaultBucketing
	}
	cat := ts.Categories[0] // TODO: assumes bucketing is consistent

	bucketing := make([]time.Duration, len(cat.Buckets))
	for i, b := range cat.Buckets {
		bucketing[i] = b.MinDuration
	}
	return bucketing
}

//
//
//

// TraceCategoryStats is a summary view of traces in a given category.
type TraceQueryCategoryStats struct {
	Name         string
	Buckets      []*TraceQueryCategoryBucketStats
	NumSucceeded int //  succeeded
	NumErrored   int // +  errored
	NumFinished  int // = finished -> finished
	NumActive    int //               + active
	NumTotal     int //               =  total
	Oldest       time.Time
	Newest       time.Time
}

func newTraceQueryCategoryStats(name string, bucketing []time.Duration) *TraceQueryCategoryStats {
	return &TraceQueryCategoryStats{
		Name:    name,
		Buckets: newTraceQueryCategoryStatsBuckets(bucketing),
	}
}

func mergeTraceQueryCategoryStats(dst, src *TraceQueryCategoryStats) error {
	if dst.Name != src.Name {
		return fmt.Errorf("name: want %q, have %q", dst.Name, src.Name)
	}

	dst.NumSucceeded += src.NumSucceeded
	dst.NumErrored += src.NumErrored
	dst.NumFinished += src.NumFinished
	dst.NumActive += src.NumActive
	dst.NumTotal += src.NumTotal

	if dst.Oldest.IsZero() || src.Oldest.Before(dst.Oldest) {
		dst.Oldest = src.Oldest
	}

	if dst.Newest.IsZero() || src.Newest.After(dst.Newest) {
		dst.Newest = src.Newest
	}

	if err := mergeTraceQueryCategoryStatsBuckets(dst.Buckets, src.Buckets); err != nil {
		return fmt.Errorf("merge buckets: %w", err)
	}

	return nil
}

//
//
//

func newTraceQueryCategoryStatsBuckets(bucketing []time.Duration) []*TraceQueryCategoryBucketStats {
	res := make([]*TraceQueryCategoryBucketStats, len(bucketing))
	for i := range bucketing {
		res[i] = &TraceQueryCategoryBucketStats{MinDuration: bucketing[i]}
	}
	return res
}

func mergeTraceQueryCategoryStatsBuckets(dst, src []*TraceQueryCategoryBucketStats) error {
	if len(dst) != len(src) {
		return fmt.Errorf("length mismatch: dst %d, src %d", len(dst), len(src))
	}

	for i := range dst {
		if err := mergeTraceQueryCategoryBucketStats(dst[i], src[i]); err != nil {
			return fmt.Errorf("bucket %d/%d (%s): %w", i+1, len(dst), dst[i].MinDuration, err)
		}
	}

	return nil
}

// TraceQueryCategoryBucketStats is a summary view of traces in a given category
// with a duration greater than or equal to the specified minimum duration.
type TraceQueryCategoryBucketStats struct {
	MinDuration  time.Duration
	NumSucceeded int
	NumErrored   int
	NumFinished  int
	NumActive    int
	NumTotal     int
	Oldest       time.Time
	Newest       time.Time
}

func mergeTraceQueryCategoryBucketStats(dst, src *TraceQueryCategoryBucketStats) error {
	if dst.MinDuration != src.MinDuration {
		return fmt.Errorf("min duration: want %s, have %s", dst.MinDuration, src.MinDuration)
	}

	dst.NumSucceeded += src.NumSucceeded
	dst.NumErrored += src.NumErrored
	dst.NumFinished += src.NumFinished
	dst.NumActive += src.NumActive
	dst.NumTotal += src.NumTotal

	if dst.Oldest.IsZero() || src.Oldest.Before(dst.Oldest) {
		dst.Oldest = src.Oldest
	}

	if dst.Newest.IsZero() || src.Newest.After(dst.Newest) {
		dst.Newest = src.Newest
	}

	return nil
}

//
//
//

func incrIf(dst *int, when bool) {
	if when {
		*dst++
	}
}

func olderOf(dst *time.Time, src time.Time) {
	if src.Before(*dst) {
		*dst = src
	}
}

func newerOf(dst *time.Time, src time.Time) {
	if src.After(*dst) {
		*dst = src
	}
}
