package trctrace

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
)

// Stats collects summary statistics for a group of traces. It's meant to model
// the summary table at the top of the HTML user interface. Stats are typically
// produced from a single collector via the stats builder, and can be merged
// together in order to support distributed searching.
type Stats struct {
	Bucketing  []time.Duration `json:"bucketing"`
	Categories []CategoryStats `json:"categories"`
}

func (s *Stats) IsZero() bool {
	return len(s.Bucketing) == 0 && len(s.Categories) == 0
}

func CombineStats(a, b Stats) Stats {
	switch {
	case a.IsZero() && b.IsZero():
		return a

	case a.IsZero() && !b.IsZero():
		return b

	case !a.IsZero() && b.IsZero():
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
		target.Merge(c)         // TODO: if/when it changes, error checking
		index[c.Name] = target  // TODO: pointers?
	}

	categories := make([]CategoryStats, 0, len(index))
	for _, c := range index {
		a.Categories = append(a.Categories, c) // TODO: allocs
	}

	return Stats{
		Bucketing:  a.Bucketing,
		Categories: categories,
	}
}

func (s *Stats) Overall() *CategoryStats {
	overall := NewCategory("overall", s.Bucketing)
	for _, c := range s.Categories {
		overall.Merge(c)
	}
	return overall
}

//
//
//

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
}

func NewCategory(name string, bucketing []time.Duration) *CategoryStats {
	return &CategoryStats{
		Name:      name,
		NumBucket: make([]uint64, len(bucketing)),
	}
}

func (c *CategoryStats) IsZero() bool {
	var (
		zeroName       = c.Name == ""
		zeroNumActive  = c.NumActive == 0
		zeroNumBucket  = len(c.NumBucket) == 0
		zeroNumFailed  = c.NumFailed == 0
		zeroOldest     = c.Oldest.IsZero()
		zeroNewest     = c.Newest.IsZero()
		zeroEverything = zeroName && zeroNumActive && zeroNumBucket && zeroNumFailed && zeroOldest && zeroNewest
	)
	return zeroEverything
}

func (cs *CategoryStats) Observe(tr trc.Trace, bucketing []time.Duration) {
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
			switch {
			case bucket <= duration:
				cs.NumBucket[i]++
			case bucket > duration:
				break
			}
		}

	case failed:
		cs.NumFailed++
	}

	cs.Oldest = olderOf(cs.Oldest, start)

	cs.Newest = newerOf(cs.Newest, start)
}

func (cs *CategoryStats) Merge(other CategoryStats) {
	if other.IsZero() {
		return
	}

	if cs.IsZero() {
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
}

func (cs *CategoryStats) NumTotal() uint64 {
	var total uint64
	total += cs.NumActive
	for _, n := range cs.NumBucket {
		total += n
	}
	total += cs.NumFailed
	return total
}

func (cs *CategoryStats) Rate() float64 {
	span := cs.Newest.Sub(cs.Oldest).Seconds()
	if span <= 0 {
		return 0
	}
	return float64(cs.NumTotal()) / span
}

//
//
//

// StatsBuilder produces summary statistics for a group of traces incrementally.
// It's meant to be used by a collector when executing a search request.
type StatsBuilder struct {
	bucketing  []time.Duration
	byCategory map[string]*CategoryStats
}

func NewStatsBuilder(bucketing []time.Duration) *StatsBuilder {
	return &StatsBuilder{
		bucketing:  bucketing,
		byCategory: map[string]*CategoryStats{},
	}
}

func (sb *StatsBuilder) Observe(tr trc.Trace) {
	category := tr.Category()
	cs, ok := sb.byCategory[category]
	if !ok {
		cs = NewCategory(category, sb.bucketing)
		sb.byCategory[category] = cs
	}
	cs.Observe(tr, sb.bucketing)
}

func (sb *StatsBuilder) Stats() *Stats {
	categories := make([]CategoryStats, 0, len(sb.byCategory))
	for _, cs := range sb.byCategory {
		categories = append(categories, *cs)
	}
	sort.Slice(categories, func(i, j int) bool {
		return strings.Compare(categories[i].Name, categories[j].Name) < 0
	})
	return &Stats{
		Bucketing:  sb.bucketing,
		Categories: categories,
	}
}

//
//
//

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

func normalizeBucketing(bucketing []time.Duration) []time.Duration {
	sort.Slice(bucketing, func(i, j int) bool {
		return bucketing[i] < bucketing[j]
	})

	for len(bucketing) > 0 && bucketing[0] < 0 {
		bucketing = bucketing[1:]
	}

	if len(bucketing) <= 0 || bucketing[0] != 0 {
		bucketing = append([]time.Duration{0}, bucketing...)
	}

	return bucketing
}
