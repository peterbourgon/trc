package trc

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

//
//
//

type TraceCollector struct {
	c *collector[Trace]
}

func NewTraceCollector() *TraceCollector {
	return &TraceCollector{
		c: newCollector[Trace](DefaultTraceCollectorMaxEvents),
	}
}

func (c *TraceCollector) NewTrace(ctx context.Context, category string) (context.Context, Trace) {
	if tr, ok := MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}

	ctx, tr := NewTrace(ctx, category)
	c.c.add(category, tr)
	return ctx, tr
}

func (c *TraceCollector) CopyTrace(tr Trace, newCategory string) error {
	if tr.Category() == newCategory {
		return fmt.Errorf("trace is already in the requested category")
	}

	c.c.add(newCategory, tr)

	tr.Tracef("CopyTrace added trace to category %q", newCategory)
	tr.Tracef("debug: %v", c.c.debug())

	return nil
}

func (c *TraceCollector) QueryTraces(ctx context.Context, req *QueryTracesRequest) (*QueryTracesResponse, error) {
	tr := FromContext(ctx)

	tr.Tracef("debug: %v", c.c.debug())

	if err := req.Sanitize(); err != nil {
		return nil, fmt.Errorf("sanitize request: %w", err)
	}

	var begin = time.Now()
	var overall Traces
	var allowed Traces
	{
		for cat, rb := range c.c.groups.getAll() {
			if err := rb.walk(func(tr Trace) error {
				overall = append(overall, tr)
				if req.Allow(tr) {
					allowed = append(allowed, tr)
				}
				return nil
			}); err != nil {
				return nil, fmt.Errorf("gathering traces (%s): %w", cat, err)
			}
		}
	}
	matched := len(allowed)
	took := time.Since(begin)
	perTrace := time.Duration(float64(took) / float64(len(overall)))

	tr.Tracef("evaluated %d, matched %d, took %s, %s/trace", len(overall), matched, took, perTrace)

	stats := newTraceQueryStats(req, overall)

	tr.Tracef("computed stats")

	sort.Sort(allowed)
	if len(allowed) > req.Limit {
		allowed = allowed[:req.Limit]
	}

	selected := make([]*StaticTrace, len(allowed))
	for i := range allowed {
		selected[i] = NewTraceStatic(allowed[i])
	}

	tr.Tracef("selected %d", len(selected))

	return &QueryTracesResponse{
		Request:  req,
		Stats:    stats,
		Matched:  matched,
		Selected: selected,
		Problems: nil,
	}, nil
}

func (c *TraceCollector) Subscribe(ctx context.Context, ch chan<- Trace) error {
	return c.c.stream.subscribe(ch)
}

func (c *TraceCollector) Unsubscribe(ctx context.Context, ch chan<- Trace) (sends, drops uint64, _ error) {
	return c.c.stream.unsubscribe(ch)
}

func (c *TraceCollector) Subscription(ctx context.Context, ch chan<- Trace) (sends, drops uint64, err error) {
	return c.c.stream.stats(ch)
}

//
//
//

/*
type TraceCollector struct {
	byCategory *ringBuffers[Trace]
}

func NewDefaultTraceCollector() *TraceCollector {
	return NewTraceCollector(DefaultTraceCollectorMaxEvents)
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

func (tc *TraceCollector) QueryTrace(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	tr := FromContext(ctx)

	if err := req.Sanitize(); err != nil {
		return nil, fmt.Errorf("trace query request: %w", err)
	}

	var begin = time.Now()
	var overall Traces
	var allowed Traces
	{
		for cat, rb := range tc.byCategory.getAll() {
			if err := rb.walk(func(tr Trace) error {
				overall = append(overall, tr)
				if req.Allow(tr) {
					allowed = append(allowed, tr)
				}
				return nil
			}); err != nil {
				return nil, fmt.Errorf("gathering traces (%s): %w", cat, err)
			}
		}
	}
	matched := len(allowed)
	took := time.Since(begin)
	perTrace := time.Duration(float64(took) / float64(len(overall)))

	tr.Tracef("evaluated %d, matched %d, took %s, %s/trace", len(overall), matched, took, perTrace)

	stats := newTraceQueryStats(req, overall)

	tr.Tracef("computed stats")

	sort.Sort(allowed)
	if len(allowed) > req.Limit {
		allowed = allowed[:req.Limit]
	}

	selected := make([]*StaticTrace, len(allowed))
	for i := range allowed {
		selected[i] = NewTraceStatic(allowed[i])
	}

	tr.Tracef("selected %d", len(selected))

	return &QueryResponse{
		Request:  req,
		Stats:    stats,
		Matched:  matched,
		Selected: selected,
		Problems: nil,
	}, nil
}
*/

//
//
//

type QueryTracesRequest struct {
	IDs         []string        `json:"ids,omitempty"`
	Category    string          `json:"category,omitempty"`
	IsActive    bool            `json:"is_active,omitempty"`
	IsFinished  bool            `json:"is_finished,omitempty"`
	IsSucceeded bool            `json:"is_succeeded,omitempty"`
	IsErrored   bool            `json:"is_errored,omitempty"`
	MinDuration *time.Duration  `json:"min_duration,omitempty"`
	Bucketing   []time.Duration `json:"bucketing,omitempty"`
	Search      string          `json:"search"`
	Regexp      *regexp.Regexp  `json:"-"`
	Limit       int             `json:"limit,omitempty"`
}

func (req *QueryTracesRequest) String() string {
	req.Sanitize()

	var parts []string
	if len(req.IDs) > 0 {
		parts = append(parts, fmt.Sprintf("id=%v", req.IDs))
	}

	if req.Category != "" {
		parts = append(parts, fmt.Sprintf("category=%q", req.Category))
	}

	if req.IsActive {
		parts = append(parts, "active")
	}

	if req.IsFinished {
		parts = append(parts, "finished")
	}

	if req.IsSucceeded {
		parts = append(parts, "succeeded")
	}

	if req.IsErrored {
		parts = append(parts, "errored")
	}

	if req.MinDuration != nil {
		parts = append(parts, fmt.Sprintf("min=%s", req.MinDuration.String()))
	}

	if req.Bucketing != nil {
		parts = append(parts, fmt.Sprintf("bucketing=%v", req.Bucketing))
	}

	if req.Regexp != nil {
		parts = append(parts, fmt.Sprintf("regexp=%q", req.Regexp.String()))
	}

	if req.Limit > 0 {
		parts = append(parts, fmt.Sprintf("limit=%d", req.Limit))
	}

	if len(parts) <= 0 {
		return "*"
	}

	return strings.Join(parts, " ")
}

func (req *QueryTracesRequest) Sanitize() error {
	if req.Bucketing == nil {
		req.Bucketing = defaultBucketing
	}

	switch {
	case req.Regexp != nil && req.Search == "":
		req.Search = req.Regexp.String()
	case req.Regexp == nil && req.Search != "":
		re, err := regexp.Compile(req.Search)
		if err != nil {
			return fmt.Errorf("%q: %w", req.Search, err)
		}
		req.Regexp = re
	}

	switch {
	case req.Limit <= 0:
		req.Limit = traceQueryLimitDef
	case req.Limit < traceQueryLimitMin:
		req.Limit = traceQueryLimitMin
	case req.Limit > traceQueryLimitMax:
		req.Limit = traceQueryLimitMax
	}

	return nil
}

func (req *QueryTracesRequest) Allow(tr Trace) bool {
	if len(req.IDs) > 0 {
		var found bool
		for _, id := range req.IDs {
			if id == tr.ID() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if req.Category != "" && tr.Category() != req.Category {
		return false
	}

	if req.IsActive && !tr.Active() {
		return false
	}

	if req.IsFinished && !tr.Finished() {
		return false
	}

	if req.IsSucceeded && !tr.Succeeded() {
		return false
	}

	if req.IsErrored && !tr.Errored() {
		return false
	}

	if req.MinDuration != nil {
		if tr.Active() { // we assert that a min duration query param excludes active traces
			return false
		}
		if tr.Duration() < *req.MinDuration {
			return false
		}
	}

	if req.Regexp != nil {
		if matchedSomething := func() bool {
			if req.Regexp.MatchString(tr.ID()) {
				return true
			}
			if req.Regexp.MatchString(tr.Category()) {
				return true
			}
			for _, ev := range tr.Events() {
				if ev.MatchRegexp(req.Regexp) {
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

type QueryTracesResponse struct {
	Request  *QueryTracesRequest `json:"request"`
	Origins  []string            `json:"origins,omitempty"`
	Stats    *TraceStats         `json:"stats"`
	Matched  int                 `json:"matched"`
	Selected []*StaticTrace      `json:"selected"`
	Problems []string            `json:"problems,omitempty"`
	Duration time.Duration       `json:"duration"`
}

func NewQueryTracesResponse(req *QueryTracesRequest, selected Traces) *QueryTracesResponse {
	return &QueryTracesResponse{
		Request: req,
		Stats:   newTraceQueryStats(req, selected),
	}
}

func (res *QueryTracesResponse) Merge(other *QueryTracesResponse) error {
	if res.Request == nil {
		return fmt.Errorf("invalid response: missing request")
	}

	res.Origins = mergeStringSlices(res.Origins, other.Origins)
	if err := mergeTraceQueryStats(res.Stats, other.Stats); err != nil {
		return fmt.Errorf("merge stats: %w", err)
	}

	res.Matched += other.Matched

	res.Selected = append(res.Selected, other.Selected...)

	res.Problems = append(res.Problems, other.Problems...)

	res.Duration = ifThenElse(res.Duration > other.Duration, res.Duration, other.Duration)

	return nil

}

//
//
//

// TraceStats is a summary view of a set of traces. It's returned as
// part of a trace collector's query response, and in that case represents all
// traces in the collector, with bucketing as specified in the query.
type TraceStats struct {
	Request    *QueryTracesRequest   `json:"request"` // TODO: find a way to remove
	Categories []*TraceCategoryStats `json:"categories"`
}

func newTraceQueryStats(req *QueryTracesRequest, trs Traces) *TraceStats {
	// Group the traces into stats buckets by category.
	byCategory := map[string]*TraceCategoryStats{}
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
			st = newTraceQueryCategoryStats(req, category)
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
	flattened := make([]*TraceCategoryStats, 0, len(byCategory))
	for _, sts := range byCategory {
		flattened = append(flattened, sts)
	}
	sort.Slice(flattened, func(i, j int) bool {
		return flattened[i].Name < flattened[j].Name
	})

	// That'll do.
	return &TraceStats{
		Request:    req,
		Categories: flattened,
	}
}

// Overall returns stats for a synthetic category representing all traces.
func (ts *TraceStats) Overall() (*TraceCategoryStats, error) {
	overall := &TraceCategoryStats{
		Name:    "overall",
		Buckets: newTraceQueryCategoryStatsBuckets(ts.Request),
	}
	for _, cat := range ts.Categories {
		if err := mergeTraceQueryCategoryStats(overall, cat); err != nil {
			return nil, fmt.Errorf("merge %q: %w", cat.Name, err)
		}
	}
	return overall, nil
}

// Bucketing is the set of durations by which finished traces are grouped.
func (ts *TraceStats) Bucketing() []time.Duration {
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

func mergeTraceQueryStats(dst, src *TraceStats) error {
	m := map[string]*TraceCategoryStats{}
	for _, c := range dst.Categories {
		m[c.Name] = c
	}

	for _, c := range src.Categories {
		target, ok := m[c.Name]
		if !ok {
			m[c.Name] = c
			continue
		}
		if err := mergeTraceQueryCategoryStats(target, c); err != nil {
			return fmt.Errorf("category %q: %w", c.Name, err)
		}
	}

	flat := make([]*TraceCategoryStats, 0, len(m))
	for _, s := range m {
		flat = append(flat, s)
	}
	sort.Slice(flat, func(i, j int) bool {
		return flat[i].Name < flat[j].Name
	})
	dst.Categories = flat

	return nil
}

//
//
//

// TraceCategoryStats is a summary view of traces in a given category.
type TraceCategoryStats struct {
	Name         string                      `json:"name"`
	Buckets      []*TraceCategoryBucketStats `json:"buckets"`
	IsQueried    bool                        `json:"is_queried,omitempty"`
	NumSucceeded int                         `json:"num_succeeded"` //  succeeded
	NumErrored   int                         `json:"num_errored"`   // +  errored
	NumFinished  int                         `json:"num_finished"`  // = finished -> finished
	NumActive    int                         `json:"num_active"`    //               + active
	NumTotal     int                         `json:"num_total"`     //               =  total
	Oldest       time.Time                   `json:"oldest"`
	Newest       time.Time                   `json:"newest"`
}

func newTraceQueryCategoryStats(req *QueryTracesRequest, name string) *TraceCategoryStats {
	return &TraceCategoryStats{
		Name:      name,
		Buckets:   newTraceQueryCategoryStatsBuckets(req),
		IsQueried: req.Category == name,
	}
}

func mergeTraceQueryCategoryStats(dst, src *TraceCategoryStats) error {
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

func newTraceQueryCategoryStatsBuckets(req *QueryTracesRequest) []*TraceCategoryBucketStats {
	res := make([]*TraceCategoryBucketStats, len(req.Bucketing))
	for i := range req.Bucketing {
		res[i] = &TraceCategoryBucketStats{
			MinDuration: req.Bucketing[i],
			IsQueried:   req.MinDuration != nil && *req.MinDuration == req.Bucketing[i],
		}
	}
	return res
}

func mergeTraceQueryCategoryStatsBuckets(dst, src []*TraceCategoryBucketStats) error {
	if len(dst) == 0 {
		dst = make([]*TraceCategoryBucketStats, len(src))
		copy(dst, src)
		return nil
	}

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

// TraceCategoryBucketStats is a summary view of traces in a given category
// with a duration greater than or equal to the specified minimum duration.
type TraceCategoryBucketStats struct {
	MinDuration  time.Duration `json:"min_duration"`
	IsQueried    bool          `json:"is_queried,omitempty"`
	NumSucceeded int           `json:"num_succeeded"`
	NumErrored   int           `json:"num_errored"`
	NumFinished  int           `json:"num_finished"`
	NumActive    int           `json:"num_active"`
	NumTotal     int           `json:"num_total"`
	Oldest       time.Time     `json:"oldest"`
	Newest       time.Time     `json:"newest"`
}

func mergeTraceQueryCategoryBucketStats(dst, src *TraceCategoryBucketStats) error {
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

func incrIf(dst *int, when bool) {
	if when {
		*dst++
	}
}

func olderOf(dst *time.Time, src time.Time) {
	if dst.IsZero() || src.Before(*dst) {
		*dst = src
	}
}

func newerOf(dst *time.Time, src time.Time) {
	if dst.IsZero() || src.After(*dst) {
		*dst = src
	}
}

func mergeStringSlices(a, b []string) []string {
	m := map[string]struct{}{}
	for _, s := range a {
		m[s] = struct{}{}
	}
	for _, s := range b {
		m[s] = struct{}{}
	}
	r := make([]string, 0, len(m))
	for s := range m {
		r = append(r, s)
	}
	sort.Strings(r)
	return r
}

func ifThenElse[T any](cond bool, yes, not T) T {
	if cond {
		return yes
	}
	return not
}

//
//
//

const (
	traceQueryLimitMin = 1
	traceQueryLimitDef = 10
	traceQueryLimitMax = 1000

	DefaultTraceCollectorMaxEvents = 1000
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
