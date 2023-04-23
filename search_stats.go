package trc

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// SearchStats collects summary statistics for traces returned by a search.
// Stats are typically produced from a single collector, but can be merged
// together to support distributed searching.
type SearchStats struct {
	Bucketing  []time.Duration       `json:"bucketing"`
	Categories []SearchStatsCategory `json:"categories"`
}

func newSearchStatsFrom(bucketing []time.Duration, traces []Trace) SearchStats {
	byCategory := map[string]*SearchStatsCategory{}
	for _, tr := range traces {
		category := tr.Category()
		cs, ok := byCategory[category]
		if !ok {
			cs = newSearchStatsCategory(category, bucketing)
			byCategory[category] = cs
		}
		cs.observe(tr, bucketing)
	}

	categories := make([]SearchStatsCategory, 0, len(byCategory))
	for _, cs := range byCategory {
		categories = append(categories, *cs)
	}

	sort.Slice(categories, func(i, j int) bool {
		return strings.Compare(categories[i].Name, categories[j].Name) < 0
	})

	return SearchStats{
		Bucketing:  bucketing,
		Categories: categories,
	}
}

func (s *SearchStats) isZero() bool {
	return len(s.Bucketing) == 0 && len(s.Categories) == 0
}

// Overall returns a composite SearchStatsCategory representing all categories.
func (s *SearchStats) Overall() *SearchStatsCategory {
	overall := newSearchStatsCategory("overall", s.Bucketing)
	rateSum := float64(0.0)
	for _, c := range s.Categories {
		overall.merge(c)
		rateSum += c.Rate
	}
	overall.Rate = rateSum
	return overall
}

//
//
//

// SearchStatsCategory are summary statistics for a group of traces in a single
// category. Category stats are always part of a parent summary stats, and can
// also be merged.
type SearchStatsCategory struct {
	Name       string    `json:"name"`
	NumActive  uint64    `json:"num_active"`  // active
	NumBucket  []uint64  `json:"num_bucket"`  // not active and not errored, by minimum duration
	NumErrored uint64    `json:"num_errored"` // not active and errored
	Oldest     time.Time `json:"oldest"`
	Newest     time.Time `json:"newest"`
	Rate       float64   `json:"rate"`
}

func newSearchStatsCategory(name string, bucketing []time.Duration) *SearchStatsCategory {
	return &SearchStatsCategory{
		Name:      name,
		NumBucket: make([]uint64, len(bucketing)),
	}
}

func (c *SearchStatsCategory) isZero() bool {
	var (
		zeroName       = c.Name == ""
		zeroNumActive  = c.NumActive == 0
		zeroNumBucket  = len(c.NumBucket) == 0
		zeroNumFailed  = c.NumErrored == 0
		zeroOldest     = c.Oldest.IsZero()
		zeroNewest     = c.Newest.IsZero()
		zeroRate       = c.Rate == 0
		zeroEverything = zeroName && zeroNumActive && zeroNumBucket && zeroNumFailed && zeroOldest && zeroNewest && zeroRate
	)
	return zeroEverything
}

func (cs *SearchStatsCategory) observe(tr Trace, bucketing []time.Duration) {
	var (
		start    = tr.Started()
		finished = tr.Finished()
		active   = !finished
		bucket   = finished && !tr.Errored()
		errored  = finished && tr.Errored()
	)

	switch {
	case active:
		cs.NumActive++

	case bucket:
		duration := tr.Duration()
		for i, bucket := range bucketing {
			if bucket > duration {
				break
			}
			cs.NumBucket[i]++
		}

	case errored:
		cs.NumErrored++
	}

	cs.Oldest = olderOf(cs.Oldest, start)
	cs.Newest = newerOf(cs.Newest, start)
	cs.Rate = calcRate(cs.NumTotal(), cs.Newest.Sub(cs.Oldest))
}

func (cs *SearchStatsCategory) merge(other SearchStatsCategory) {
	if other.isZero() {
		return
	}

	if cs.isZero() {
		*cs = other
		return
	}

	// TODO: compare names in an e.g. MergeStrict

	if dst, src := len(cs.NumBucket), len(other.NumBucket); dst != src {
		panic(badMerge("buckets", dst, src)) // TODO: MergeSafe
	}

	cs.NumActive += other.NumActive

	for i := range cs.NumBucket {
		cs.NumBucket[i] += other.NumBucket[i]
	}

	cs.NumErrored += other.NumErrored

	cs.Oldest = olderOf(cs.Oldest, other.Oldest)
	cs.Newest = newerOf(cs.Newest, other.Newest)
	cs.Rate = calcRate(cs.NumTotal(), cs.Newest.Sub(cs.Oldest))
}

// NumTotal returns the total number of traces represented in the category.
func (cs *SearchStatsCategory) NumTotal() uint64 {
	var total uint64
	total += cs.NumActive
	if len(cs.NumBucket) > 0 {
		total += cs.NumBucket[0] // always 0s
	}
	total += cs.NumErrored
	return total
}

//
//
//

func calcRate(n uint64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / d.Seconds()
}

func combineStats(a, b SearchStats) SearchStats {
	switch {
	case a.isZero() && b.isZero():
		return a

	case a.isZero() && !b.isZero():
		return b

	case !a.isZero() && b.isZero():
		return a
	}

	// Merging two non-zero summary stats requires identical bucketing.
	if dst, src := len(a.Bucketing), len(b.Bucketing); dst != src {
		panic(badMerge("bucketing", dst, src)) // TODO: MergeSafe
	}
	for i := range a.Bucketing {
		if dst, src := a.Bucketing[i], b.Bucketing[i]; dst != src {
			panic(badMerge(fmt.Sprintf("bucketing %d/%d", i+1, len(a.Bucketing)), dst, src)) // TODO: error?
		}
	}

	slice := append(a.Categories, b.Categories...) // duplicates are possible
	index := map[string]SearchStatsCategory{}      // duplicates not possible
	for _, c := range slice {
		target := index[c.Name] // can be zero
		target.merge(c)         // TODO: if/when it changes, error checking
		index[c.Name] = target  // TODO: pointers?
	}

	categories := make([]SearchStatsCategory, 0, len(index))
	for _, c := range index {
		categories = append(categories, c) // TODO: allocs
	}

	sort.Slice(categories, func(i, j int) bool {
		return categories[i].Name < categories[j].Name
	})

	return SearchStats{
		Bucketing:  a.Bucketing,
		Categories: categories,
	}
}

var ErrBadMerge = fmt.Errorf("bad merge")

func badMerge(what string, dst, src any) error {
	return fmt.Errorf("%w: %s: %v ↯ %v", ErrBadMerge, what, dst, src)
}

var DefaultBucketing = []time.Duration{
	0 * time.Millisecond,
	1 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	1000 * time.Millisecond,
}

const (
	searchLimitMin = 1
	searchLimitDef = 10
	searchLimitMax = 1000
)

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
