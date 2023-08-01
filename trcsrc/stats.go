package trcsrc

import (
	"fmt"
	"sort"
	"time"

	"github.com/peterbourgon/trc"
)

type SelectStats struct {
	Bucketing  []time.Duration           `json:"bucketing"`
	Categories map[string]*CategoryStats `json:"categories"`
}

func NewSelectStats(bucketing []time.Duration) *SelectStats {
	return &SelectStats{
		Bucketing:  bucketing,
		Categories: map[string]*CategoryStats{},
	}
}

func (ss *SelectStats) IsZero() bool {
	if ss == nil {
		return true
	}

	if len(ss.Bucketing) <= 0 {
		return true
	}

	if len(ss.Categories) <= 0 {
		return true
	}

	return false
}

func (ss *SelectStats) Observe(trs ...trc.Trace) {
	for _, tr := range trs {
		category := tr.Category()
		cs, ok := ss.Categories[category]
		if !ok {
			cs = NewCategoryStats(category, ss.Bucketing)
			ss.Categories[category] = cs
		}

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

func (ss *SelectStats) Merge(other *SelectStats) {
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

func (ss *SelectStats) Overall() *CategoryStats {
	overall := NewCategoryStats("overall", ss.Bucketing)
	var rate float64
	for _, sc := range ss.Categories {
		overall.Merge(sc)
		rate += sc.Rate()
	}
	overall.rate = rate
	return overall
}

func (ss *SelectStats) AllCategories() []*CategoryStats {
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

type CategoryStats struct {
	Category     string    `json:"category"`
	ActiveCount  int       `json:"active_count"`
	BucketCounts []int     `json:"bucket_counts"`
	ErroredCount int       `json:"errored_count"`
	Oldest       time.Time `json:"oldest"`
	Newest       time.Time `json:"newest"`

	rate float64
}

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

func (cs *CategoryStats) TotalCount() int {
	var total int
	total += cs.ActiveCount
	if len(cs.BucketCounts) > 0 {
		total += cs.BucketCounts[0]
	}
	total += cs.ErroredCount
	return total
}

func (cs *CategoryStats) Rate() (r float64) {
	if cs.rate != 0 {
		return cs.rate
	}

	defer func() {
		cs.rate = r
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

func (cs *CategoryStats) Merge(other *CategoryStats) {
	if other.IsZero() {
		return
	}

	if cs.IsZero() {
		*cs = *other
		return
	}

	ourRate := cs.Rate()
	theirRate := other.Rate()
	mergedRate := ourRate + theirRate

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

	cs.rate = mergedRate
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
