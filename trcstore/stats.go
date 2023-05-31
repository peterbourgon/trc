package trcstore

import (
	"fmt"
	"sort"
	"time"

	"github.com/peterbourgon/trc"
)

// Stats is a summary of all of the traces queried by a search request. Traces
// are first grouped by category, and then by result: either active, succeeded,
// or errored. Successful traces are further grouped by duration according to
// the bucketing parameter. That is, a bucket of duration D will "contain" all
// successful traces with a duration of at least D.
type Stats struct {
	Bucketing  []time.Duration
	Categories map[string]*StatsCategory
}

// NewStats constructs an empty stats value with the provided bucketing.
func NewStats(bucketing []time.Duration) *Stats {
	return &Stats{
		Bucketing:  bucketing,
		Categories: map[string]*StatsCategory{},
	}
}

// Observe the given traces into the stats value.
func (s *Stats) Observe(trs ...trc.Trace) {
	for _, tr := range trs {
		category := tr.Category()
		sc, ok := s.Categories[category]
		if !ok {
			sc = NewStatsCategory(category, s.Bucketing)
			s.Categories[category] = sc
		}

		var (
			start         = tr.Started()
			traceFinished = tr.Finished()
			traceErrored  = tr.Errored()
			isActive      = !traceFinished
			isBucket      = traceFinished && !traceErrored
			isErrored     = traceFinished && traceErrored
		)

		switch {
		case isActive:
			sc.ActiveCount++

		case isBucket:
			duration := tr.Duration()
			for i, bucket := range s.Bucketing {
				if bucket > duration {
					break
				}
				sc.BucketCount[i]++
			}

		case isErrored:
			sc.ErroredCount++
		}

		sc.Oldest = olderOf(sc.Oldest, start)
		sc.Newest = newerOf(sc.Newest, start)
	}
}

// IsZero returns true if the stats value is not property initialized.
func (s *Stats) IsZero() bool {
	if s == nil {
		return true
	}

	if len(s.Bucketing) <= 0 {
		return true
	}

	if len(s.Categories) <= 0 {
		return true
	}

	return false
}

// Merge the other stats into this one. Merging stats with inconsistent
// bucketing will panic.
func (s *Stats) Merge(other *Stats) {
	if other.IsZero() {
		return
	}

	if s.IsZero() {
		*s = *other
		return
	}

	if dst, src := len(s.Bucketing), len(other.Bucketing); dst != src {
		panic(badMerge("buckets", dst, src))
	}

	for category, theirs := range other.Categories {
		ours, ok := s.Categories[category]
		if !ok {
			cp := *theirs
			s.Categories[category] = &cp
			continue
		}
		ours.Merge(theirs)
	}
}

// Overall returns a synthetic StatsCategory for all traces in the stats.
func (s *Stats) Overall() *StatsCategory {
	overall := NewStatsCategory("overall", s.Bucketing)
	var rate float64
	for _, sc := range s.Categories {
		overall.Merge(sc)
		rate += sc.Rate()
	}
	overall.rate = rate
	return overall
}

// AllCategories returns a slice of per-category stats, sorted by category name,
// and including (as the last element) a synthetic "overall" category.
func (s *Stats) AllCategories() []*StatsCategory {
	slice := make([]*StatsCategory, 0, len(s.Categories)+1)
	for _, sc := range s.Categories {
		slice = append(slice, sc)
	}
	sort.Slice(slice, func(i, j int) bool {
		return slice[i].Category < slice[j].Category
	})
	slice = append(slice, s.Overall())
	return slice
}

//
//
//

// StatsCategory represents summary statistics for traces in a given category.
type StatsCategory struct {
	Category     string    `json:"category"`
	ActiveCount  int       `json:"active_count"`
	BucketCount  []int     `json:"bucket_count"`
	ErroredCount int       `json:"errored_count"`
	Oldest       time.Time `json:"oldest"`
	Newest       time.Time `json:"newest"`

	rate float64 // special case for overall pseudo-category
}

// NewStatsCategory constructs an empty category stats value with the given
// category name and bucketing.
func NewStatsCategory(category string, bucketing []time.Duration) *StatsCategory {
	return &StatsCategory{
		Category:    category,
		BucketCount: make([]int, len(bucketing)),
	}
}

// TotalCount is the total number of traces in this category.
func (sc *StatsCategory) TotalCount() int {
	var total int
	total += sc.ActiveCount
	if len(sc.BucketCount) > 0 {
		total += sc.BucketCount[0]
	}
	total += sc.ErroredCount
	return total
}

// Rate is the approximate number of traces per second seen by this category.
// The first call to rate that returns a non-zero value will store that value,
// and subsequent calls will return the stored value.
func (sc *StatsCategory) Rate() (r float64) {
	if sc.rate != 0 {
		return sc.rate
	}

	defer func() {
		sc.rate = r
	}()

	// if isFew := sc.Newest.Sub(sc.Oldest) < time.Second; isFew {
	// return 1
	// }

	var (
		total      = sc.TotalCount()
		delta      = sc.Newest.Sub(sc.Oldest)
		totalZero  = total <= 0
		deltaZero  = delta <= 0
		newestZero = sc.Newest.IsZero()
		oldestZero = sc.Oldest.IsZero()
		isZero     = totalZero || deltaZero || newestZero || oldestZero
	)
	if isZero {
		return 0
	}

	return float64(total) / float64(delta.Seconds())
}

// IsZero returns true if the stats are not properly initialized or empty.
func (sc *StatsCategory) IsZero() bool {
	if sc == nil {
		return true
	}

	var bucketCounts int
	for _, bc := range sc.BucketCount {
		bucketCounts += bc
	}

	var (
		zeroCategory     = sc.Category == ""
		zeroActiveCount  = sc.ActiveCount == 0
		zeroBucketCounts = bucketCounts == 0
		zeroErroredCount = sc.ErroredCount == 0
		zeroOldest       = sc.Oldest.IsZero()
		zeroNewest       = sc.Newest.IsZero()
		zeroEverything   = zeroCategory && zeroActiveCount && zeroBucketCounts && zeroErroredCount && zeroOldest && zeroNewest
	)
	return zeroEverything
}

// Merge the other category stats into this one. Merging stats with inconsistent
// bucketing will panic.
func (sc *StatsCategory) Merge(other *StatsCategory) {
	if other.IsZero() {
		return
	}

	if sc.IsZero() {
		*sc = *other
		return
	}

	ourRate := sc.Rate()
	theirRate := other.Rate()
	mergedRate := ourRate + theirRate

	// Overall merges stats from different categories together, so we can't
	// assert that category names must be the same.

	if dst, src := len(sc.BucketCount), len(other.BucketCount); dst != src {
		panic(badMerge("buckets", dst, src))
	}

	sc.ActiveCount += other.ActiveCount

	for i := range sc.BucketCount {
		sc.BucketCount[i] += other.BucketCount[i]
	}

	sc.ErroredCount += other.ErroredCount

	sc.Oldest = olderOf(sc.Oldest, other.Oldest)
	sc.Newest = newerOf(sc.Newest, other.Newest)

	sc.rate = mergedRate
}

//
//
//

// ErrBadMerge indicates a problem when merging values together. Typically, this
// is a programmer error.
var ErrBadMerge = fmt.Errorf("bad merge")

func badMerge(what string, dst, src any) error {
	return fmt.Errorf("%w: %s: %v ↯ %v", ErrBadMerge, what, dst, src)
}

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
