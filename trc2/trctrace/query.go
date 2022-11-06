package trctrace

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	trc "github.com/peterbourgon/trc/trc2"
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

func ParseBucketing(strs []string) []time.Duration {
	if len(strs) <= 0 {
		return defaultBucketing
	}

	var ds []time.Duration
	for _, s := range strs {
		if d, err := time.ParseDuration(s); err == nil {
			ds = append(ds, d)
		}
	}

	sort.Slice(ds, func(i, j int) bool {
		return ds[i] < ds[j]
	})

	if ds[0] != 0 {
		ds = append([]time.Duration{0}, ds...)
	}

	return ds
}

const (
	queryLimitMin = 1
	queryLimitDef = 10
	queryLimitMax = 1000
)

type QueryRequest struct {
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

func (req *QueryRequest) Sanitize() error {
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
		req.Limit = queryLimitDef
	case req.Limit < queryLimitMin:
		req.Limit = queryLimitMin
	case req.Limit > queryLimitMax:
		req.Limit = queryLimitMax
	}

	return nil
}

func (req *QueryRequest) String() string {
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

func (req *QueryRequest) Allow(tr trc.Trace) bool {
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

type QueryResponse struct {
	Request  *QueryRequest      `json:"request"`
	Origins  []string           `json:"origins,omitempty"`
	Stats    *QueryStats        `json:"stats"`
	Matched  int                `json:"matched"`
	Selected []*trc.StaticTrace `json:"selected"`
	Problems []string           `json:"problems,omitempty"`
	Duration time.Duration      `json:"duration"`
}

func NewQueryResponse(req *QueryRequest, selected trc.Traces) *QueryResponse {
	return &QueryResponse{
		Request: req,
		Stats:   newQueryStats(req, selected),
	}
}

func (res *QueryResponse) Merge(other *QueryResponse) error {
	if res.Request == nil {
		return fmt.Errorf("invalid response: missing request")
	}

	res.Origins = mergeStringSlices(res.Origins, other.Origins)

	if err := mergeQueryStats(res.Stats, other.Stats); err != nil {
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

// QueryStats is a summary view of a set of traces. It's returned as
// part of a trace collector's query response, and in that case represents all
// traces in the collector, with bucketing as specified in the query.
type QueryStats struct {
	Request    *QueryRequest    `json:"request"` // TODO: find a way to remove
	Categories []*CategoryStats `json:"categories"`
}

func newQueryStats(req *QueryRequest, trs trc.Traces) *QueryStats {
	// Group the traces into stats buckets by category.
	byCategory := map[string]*CategoryStats{}
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
			st = newCategoryStats(req, category)
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
	flattened := make([]*CategoryStats, 0, len(byCategory))
	for _, sts := range byCategory {
		flattened = append(flattened, sts)
	}
	sort.Slice(flattened, func(i, j int) bool {
		return flattened[i].Name < flattened[j].Name
	})

	// That'll do.
	return &QueryStats{
		Request:    req,
		Categories: flattened,
	}
}

// Overall returns stats for a synthetic category representing all traces.
func (s *QueryStats) Overall() (*CategoryStats, error) {
	overall := &CategoryStats{
		Name:    "overall",
		Buckets: newBucketStats(s.Request),
	}
	for _, cat := range s.Categories {
		if err := mergeCategoryStats(overall, cat); err != nil {
			return nil, fmt.Errorf("merge %q: %w", cat.Name, err)
		}
	}
	return overall, nil
}

// Bucketing is the set of durations by which finished traces are grouped.
func (s *QueryStats) Bucketing() []time.Duration {
	if len(s.Categories) == 0 {
		return defaultBucketing
	}
	cat := s.Categories[0] // TODO: assumes bucketing is consistent

	bucketing := make([]time.Duration, len(cat.Buckets))
	for i, b := range cat.Buckets {
		bucketing[i] = b.MinDuration
	}
	return bucketing
}

func mergeQueryStats(dst, src *QueryStats) error {
	m := map[string]*CategoryStats{}
	for _, c := range dst.Categories {
		m[c.Name] = c
	}

	for _, c := range src.Categories {
		target, ok := m[c.Name]
		if !ok {
			m[c.Name] = c
			continue
		}
		if err := mergeCategoryStats(target, c); err != nil {
			return fmt.Errorf("category %q: %w", c.Name, err)
		}
	}

	flat := make([]*CategoryStats, 0, len(m))
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

// CategoryStats is a summary view of traces in a given category.
type CategoryStats struct {
	Name         string         `json:"name"`
	Buckets      []*BucketStats `json:"buckets"`
	IsQueried    bool           `json:"is_queried,omitempty"`
	NumSucceeded int            `json:"num_succeeded"` //  succeeded
	NumErrored   int            `json:"num_errored"`   // +  errored
	NumFinished  int            `json:"num_finished"`  // = finished -> finished
	NumActive    int            `json:"num_active"`    //               + active
	NumTotal     int            `json:"num_total"`     //               =  total
	Oldest       time.Time      `json:"oldest"`
	Newest       time.Time      `json:"newest"`
}

func newCategoryStats(req *QueryRequest, name string) *CategoryStats {
	return &CategoryStats{
		Name:      name,
		Buckets:   newBucketStats(req),
		IsQueried: req.Category == name,
	}
}

func mergeCategoryStats(dst, src *CategoryStats) error {
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

	if err := combineBucketStats(dst.Buckets, src.Buckets); err != nil {
		return fmt.Errorf("merge buckets: %w", err)
	}

	return nil
}

//
//
//

func newBucketStats(req *QueryRequest) []*BucketStats {
	res := make([]*BucketStats, len(req.Bucketing))
	for i := range req.Bucketing {
		res[i] = &BucketStats{
			MinDuration: req.Bucketing[i],
			IsQueried:   req.MinDuration != nil && *req.MinDuration == req.Bucketing[i],
		}
	}
	return res
}

func combineBucketStats(dst, src []*BucketStats) error {
	if len(dst) == 0 {
		dst = make([]*BucketStats, len(src))
		copy(dst, src)
		return nil
	}

	if len(dst) != len(src) {
		return fmt.Errorf("length mismatch: dst %d, src %d", len(dst), len(src))
	}

	for i := range dst {
		if err := mergeBucketStats(dst[i], src[i]); err != nil {
			return fmt.Errorf("bucket %d/%d (%s): %w", i+1, len(dst), dst[i].MinDuration, err)
		}
	}

	return nil
}

// BucketStats is a summary view of traces in a given category
// with a duration greater than or equal to the specified minimum duration.
type BucketStats struct {
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

func mergeBucketStats(dst, src *BucketStats) error {
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

func ifThenElse[T any](condition bool, affirmative, negative T) T {
	if condition {
		return affirmative
	}
	return negative
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
