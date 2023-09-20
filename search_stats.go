package trc

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// SearchStats are statistics over the complete set of traces that were queried
// as part of a search request.
type SearchStats struct {
	Bucketing  []time.Duration           `json:"bucketing"`
	Categories map[string]*CategoryStats `json:"categories"`
}

// NewSearchStats creates a new and empty search stats, with the given time
// buckets for grouping finished traces.
func NewSearchStats(bucketing []time.Duration) *SearchStats {
	return &SearchStats{
		Bucketing:  bucketing,
		Categories: map[string]*CategoryStats{},
	}
}

// IsZero returns true if the stats are empty.
func (ss *SearchStats) IsZero() bool {
	if ss == nil {
		return true
	}

	if len(ss.Bucketing) <= 0 {
		return true
	}

	return false
}

// Observe the given traces into the search stats.
func (ss *SearchStats) Observe(ctx context.Context, trs ...Trace) {
	for _, tr := range trs {
		category := tr.Category()
		cs, ok := ss.Categories[category]
		if !ok {
			cs = NewCategoryStats(category, ss.Bucketing)
			ss.Categories[category] = cs
		}

		if o, ok := tr.(interface {
			ObserveStats(*CategoryStats, []time.Duration) bool
		}); ok && o.ObserveStats(cs, ss.Bucketing) {
			continue
		}

		cs.EventCount += len(tr.Events())

		var (
			traceStarted  = tr.Started()
			traceFinished = tr.Finished()
			traceErrored  = tr.Errored()
			isActive      = !traceFinished
			isBucket      = traceFinished && !traceErrored
			isErrored     = traceFinished && traceErrored
		)
		switch {
		case isActive:
			cs.ActiveCount++
		case isBucket:
			duration := tr.Duration()
			for i, bucket := range ss.Bucketing {
				if bucket > duration {
					break
				}
				cs.BucketCounts[i]++
			}
		case isErrored:
			cs.ErroredCount++
		}

		cs.Oldest = olderOf(cs.Oldest, traceStarted)
		cs.Newest = newerOf(cs.Newest, traceStarted)
	}
}

// Merge the other stats into this one.
func (ss *SearchStats) Merge(other *SearchStats) {
	if other.IsZero() {
		return
	}

	if ss.IsZero() {
		*ss = *other
		return
	}

	if dst, src := len(ss.Bucketing), len(other.Bucketing); dst != src {
		panic(fmt.Errorf("bad merge: inconsistent buckets: %d vs. %d", dst, src))
	}

	for category, theirs := range other.Categories {
		ours, ok := ss.Categories[category]
		if !ok {
			cp := *theirs
			ss.Categories[category] = &cp
			continue
		}
		ours.Merge(theirs)
	}
}

// Overall returns a synthetic category stats representing all categories.
func (ss *SearchStats) Overall() *CategoryStats {
	overall := NewCategoryStats("overall", ss.Bucketing)
	var tracerate, eventrate float64
	for _, sc := range ss.Categories {
		overall.Merge(sc)
		tracerate += sc.TraceRate()
		eventrate += sc.EventRate()
	}
	overall.tracerate = tracerate
	overall.eventrate = eventrate
	return overall
}

// AllCategories returns category stats for all known categories, as well as the
// synthetic Overall category.
func (ss *SearchStats) AllCategories() []*CategoryStats {
	slice := make([]*CategoryStats, 0, len(ss.Categories)+1)
	for _, cs := range ss.Categories {
		slice = append(slice, cs)
	}
	sort.Slice(slice, func(i, j int) bool {
		return slice[i].Category < slice[j].Category
	})
	slice = append(slice, ss.Overall())
	return slice
}

//
//
//

// CategoryStats represents statistics for all traces in a specific category.
type CategoryStats struct {
	Category     string    `json:"category"`
	EventCount   int       `json:"event_count"`
	ActiveCount  int       `json:"active_count"`
	BucketCounts []int     `json:"bucket_counts"`
	ErroredCount int       `json:"errored_count"`
	Oldest       time.Time `json:"oldest"`
	Newest       time.Time `json:"newest"`

	tracerate float64
	eventrate float64
}

// NewCategoryStats returns an empty category stats for the given category, and
// with the given bucketing.
func NewCategoryStats(category string, bucketing []time.Duration) *CategoryStats {
	return &CategoryStats{
		Category:     category,
		BucketCounts: make([]int, len(bucketing)),
	}
}

// IsZero returns true if the stats are not properly initialized or empty.
func (cs *CategoryStats) IsZero() bool {
	if cs == nil {
		return true
	}

	var bucketCounts int
	for _, bc := range cs.BucketCounts {
		bucketCounts += bc
	}

	var (
		zeroCategory     = cs.Category == ""
		zeroActiveCount  = cs.ActiveCount == 0
		zeroBucketCounts = bucketCounts == 0
		zeroErroredCount = cs.ErroredCount == 0
		zeroOldest       = cs.Oldest.IsZero()
		zeroNewest       = cs.Newest.IsZero()
		zeroEverything   = zeroCategory && zeroActiveCount && zeroBucketCounts && zeroErroredCount && zeroOldest && zeroNewest
	)
	return zeroEverything
}

// TotalCount returns the total number of traces in the category.
func (cs *CategoryStats) TotalCount() int {
	var total int
	total += cs.ActiveCount
	if len(cs.BucketCounts) > 0 {
		total += cs.BucketCounts[0]
	}
	total += cs.ErroredCount
	return total
}

// TraceRate is an approximate measure of traces per second in this category.
func (cs *CategoryStats) TraceRate() (r float64) {
	if cs.tracerate != 0 {
		return cs.tracerate
	}

	defer func() {
		cs.tracerate = r
	}()

	var (
		total      = cs.TotalCount()
		delta      = time.Since(cs.Oldest)
		totalZero  = total <= 0
		deltaZero  = delta <= 0
		newestZero = cs.Newest.IsZero()
		oldestZero = cs.Oldest.IsZero()
		isZero     = totalZero || deltaZero || newestZero || oldestZero
	)
	if isZero {
		return 0
	}

	return float64(total) / float64(delta.Seconds())
}

// EventRate is an approximate measure of events per second in this category.
func (cs *CategoryStats) EventRate() (r float64) {
	if cs.eventrate != 0 {
		return cs.eventrate
	}

	defer func() {
		cs.eventrate = r
	}()

	var (
		total      = cs.EventCount
		delta      = time.Since(cs.Oldest)
		totalZero  = total <= 0
		deltaZero  = delta <= 0
		newestZero = cs.Newest.IsZero()
		oldestZero = cs.Oldest.IsZero()
		isZero     = totalZero || deltaZero || newestZero || oldestZero
	)
	if isZero {
		return 0
	}

	return float64(total) / float64(delta.Seconds())
}

// Merge the other category stats into this one.
func (cs *CategoryStats) Merge(other *CategoryStats) {
	if other.IsZero() {
		return
	}

	if cs.IsZero() {
		*cs = *other
		return
	}

	// Overall merges stats from different categories together, so we can't
	// assert that category names must be the same.

	if dst, src := len(cs.BucketCounts), len(other.BucketCounts); dst != src {
		panic(fmt.Errorf("bad merge: inconsistent buckets: %d vs. %d", dst, src))
	}

	cs.ActiveCount += other.ActiveCount

	for i := range cs.BucketCounts {
		cs.BucketCounts[i] += other.BucketCounts[i]
	}

	cs.ErroredCount += other.ErroredCount

	cs.Oldest = olderOf(cs.Oldest, other.Oldest)
	cs.Newest = newerOf(cs.Newest, other.Newest)

	cs.tracerate = cs.TraceRate() + other.TraceRate()
	cs.eventrate = cs.EventRate() + other.EventRate()
}

//
//
//

func olderOf(a, b time.Time) time.Time {
	switch {
	case !a.IsZero() && !b.IsZero():
		if a.Before(b) {
			return a
		}
		return b
	case !a.IsZero() && b.IsZero():
		return a
	case a.IsZero() && !b.IsZero():
		return b
	case a.IsZero() && b.IsZero():
		return time.Time{}
	default:
		panic("unreachable")
	}
}

func newerOf(a, b time.Time) time.Time {
	switch {
	case !a.IsZero() && !b.IsZero():
		if a.After(b) {
			return a
		}
		return b
	case !a.IsZero() && b.IsZero():
		return a
	case a.IsZero() && !b.IsZero():
		return b
	case a.IsZero() && b.IsZero():
		return time.Time{}
	default:
		panic("unreachable")
	}
}
