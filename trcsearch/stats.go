package trcsearch

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
)

// Stats collects summary statistics for a group of traces. It's meant to model
// the summary table at the top of the HTML user interface. Stats are typically
// produced from a single collector, and can be merged together to support
// distributed searching.
type Stats struct {
	Bucketing  []time.Duration `json:"bucketing"`
	Categories []CategoryStats `json:"categories"`
}

func NewStatsFrom(bucketing []time.Duration, traces []trc.Trace) Stats {
	byCategory := map[string]*CategoryStats{}
	for _, tr := range traces {
		category := tr.Category()
		cs, ok := byCategory[category]
		if !ok {
			cs = newCategoryStats(category, bucketing)
			byCategory[category] = cs
		}
		cs.observe(tr, bucketing)
	}

	categories := make([]CategoryStats, 0, len(byCategory))
	for _, cs := range byCategory {
		categories = append(categories, *cs)
	}

	sort.Slice(categories, func(i, j int) bool {
		return strings.Compare(categories[i].Name, categories[j].Name) < 0
	})

	return Stats{
		Bucketing:  bucketing,
		Categories: categories,
	}
}

func (s *Stats) isZero() bool {
	return len(s.Bucketing) == 0 && len(s.Categories) == 0
}

// Overall returns a composite CategoryStats representing all categories.
func (s *Stats) Overall() *CategoryStats {
	overall := newCategoryStats("overall", s.Bucketing)
	rateSum := float64(0.0)
	for _, c := range s.Categories {
		overall.merge(c)
		rateSum += c.Rate
	}
	overall.Rate = rateSum
	return overall
}

// CategoryStats are summary statistics for a group of traces in a single
// category. It's meant to model a single row in the summary table at the top of
// the HTML user interface. Category stats are always part of a parent summary
// stats struct, and can also be merged.
type CategoryStats struct {
	Name      string    `json:"name"`
	NumActive uint64    `json:"num_active"` // active
	NumBucket []uint64  `json:"num_bucket"` // not active and not errored, by minimum duration
	NumFailed uint64    `json:"num_failed"` // not active and errored
	Oldest    time.Time `json:"oldest"`
	Newest    time.Time `json:"newest"`
	Rate      float64   `json:"rate"`
}

func newCategoryStats(name string, bucketing []time.Duration) *CategoryStats {
	return &CategoryStats{
		Name:      name,
		NumBucket: make([]uint64, len(bucketing)),
	}
}

func (c *CategoryStats) isZero() bool {
	var (
		zeroName       = c.Name == ""
		zeroNumActive  = c.NumActive == 0
		zeroNumBucket  = len(c.NumBucket) == 0
		zeroNumFailed  = c.NumFailed == 0
		zeroOldest     = c.Oldest.IsZero()
		zeroNewest     = c.Newest.IsZero()
		zeroRate       = c.Rate == 0
		zeroEverything = zeroName && zeroNumActive && zeroNumBucket && zeroNumFailed && zeroOldest && zeroNewest && zeroRate
	)
	return zeroEverything
}

func (cs *CategoryStats) observe(tr trc.Trace, bucketing []time.Duration) {
	var (
		start  = tr.Start()
		active = tr.Active()
		bucket = !tr.Active() && tr.Succeeded()
		failed = !tr.Active() && !tr.Succeeded()
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

	case failed:
		cs.NumFailed++
	}

	cs.Oldest = olderOf(cs.Oldest, start)
	cs.Newest = newerOf(cs.Newest, start)
	cs.Rate = calcRate(cs.NumTotal(), cs.Newest.Sub(cs.Oldest))
}

func (cs *CategoryStats) merge(other CategoryStats) {
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

	cs.NumFailed += other.NumFailed

	cs.Oldest = olderOf(cs.Oldest, other.Oldest)
	cs.Newest = newerOf(cs.Newest, other.Newest)
	cs.Rate = calcRate(cs.NumTotal(), cs.Newest.Sub(cs.Oldest))
}

// NumTotal returns the total number of traces represented in the category.
func (cs *CategoryStats) NumTotal() uint64 {
	var total uint64
	total += cs.NumActive
	if len(cs.NumBucket) > 0 {
		total += cs.NumBucket[0] // always 0s
	}
	total += cs.NumFailed
	return total
}

func calcRate(n uint64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / d.Seconds()
}

func combineStats(a, b Stats) Stats {
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
	index := map[string]CategoryStats{}            // duplicates not possible
	for _, c := range slice {
		target := index[c.Name] // can be zero
		target.merge(c)         // TODO: if/when it changes, error checking
		index[c.Name] = target  // TODO: pointers?
	}

	categories := make([]CategoryStats, 0, len(index))
	for _, c := range index {
		categories = append(categories, c) // TODO: allocs
	}

	sort.Slice(categories, func(i, j int) bool {
		return categories[i].Name < categories[j].Name
	})

	return Stats{
		Bucketing:  a.Bucketing,
		Categories: categories,
	}
}

var ErrBadMerge = fmt.Errorf("bad merge")

func badMerge(what string, dst, src any) error {
	return fmt.Errorf("%w: %s: %v ↯ %v", ErrBadMerge, what, dst, src)
}
