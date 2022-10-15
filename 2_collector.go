package trc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"time"
)

// Collector maintains a set of traces and logs, grouped by category, in memory.
//
// Most users are likely to be better served by `package eztrc` and shouldn't
// need to construct and maintain a collector directly.
type Collector struct {
	traces *ringBuffers[Trace]
	logs   *ringBuffers[Log]
}

// NewCollector returns a new collector with the given configuration.
func NewCollector(cfg CollectorConfig) *Collector {
	cfg.sanitize()
	return &Collector{
		traces: newRingBuffers[Trace](cfg.MaxTraces),
		logs:   newRingBuffers[Log](cfg.MaxLogs),
	}
}

// NewDefaultCollector returns a new collector with the default configuration.
func NewDefaultCollector() *Collector {
	return NewCollector(CollectorConfig{})
}

// CollectorConfig defines the configuration options for a collector.
type CollectorConfig struct {
	// MaxTraces is how many traces the collector will keep, newest-first, for
	// each category. The default value is 1000.
	MaxTraces int

	// MaxLogs is how many logs the collector will keep, newest-first, for each
	// category. The default value is 10000.
	MaxLogs int
}

func (cfg *CollectorConfig) sanitize() {
	if cfg.MaxTraces <= 0 {
		cfg.MaxTraces = 1000
	}
	if cfg.MaxLogs <= 0 {
		cfg.MaxLogs = 10000
	}
}

//
//
//

// NewTrace creates a new trace with the given category, and saves it to the
// collector. If a trace already exists in the context, it becomes "shadowed" by
// the new trace.
func (c *Collector) NewTrace(ctx context.Context, category string) (context.Context, Trace) {
	ctx, tr := NewTrace(ctx, category)
	c.traces.getOrCreate(category).add(tr)
	return ctx, tr
}

// GetOrCreateTrace returns the trace from the context, if it exists. Otherwise,
// it creates and returns a new trace via NewTrace.
func (c *Collector) GetOrCreateTrace(ctx context.Context, category string) (context.Context, Trace) {
	if tr, ok := MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}
	return c.NewTrace(ctx, category)
}

// Traces returns all traces stored in the collector, newest first.
func (c *Collector) Traces() Traces {
	trs, _, _ := c.TraceQuery(TraceFilter{}, 0)
	return trs
}

// TraceQuery returns the n most recent traces matching f, and a count of
// all matching traces. If n is zero, all matching traces are returned.
func (c *Collector) TraceQuery(f TraceFilter, n int) (Traces, int, error) {
	var res Traces

	for _, trs := range c.traces.getAll() {
		trs.walk(func(tr Trace) error {
			if f.Allow(tr) {
				res = append(res, tr)
			}
			return nil
		})
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].Start().After(res[j].Start())
	})

	total := len(res)
	if n > 0 && total > n {
		res = res[:n]
	}

	return res, total, nil
}

// TraceStats returns summary statistics for all traces in the collector.
func (c *Collector) TraceStats(bucketing []time.Duration) TraceStats {
	incrementIf := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}

	newBuckets := func() []TraceBucket {
		buckets := make([]TraceBucket, len(bucketing))
		for i := range buckets {
			buckets[i].MinDuration = bucketing[i]
		}
		return buckets
	}

	stats := TraceStats{
		Bucketing: bucketing,
		Overall:   TraceCategory{Name: "overall", Buckets: newBuckets()},
	}

	observe := func(tr Trace, cats ...*TraceCategory) error {
		var (
			duration     = tr.Duration()
			addActive    = incrementIf(tr.Active())
			addErrored   = incrementIf(tr.Errored())
			addSucceeded = incrementIf(tr.Succeeded())
		)
		for _, cat := range cats {
			cat.NumActive += addActive
			cat.NumErrored += addErrored
			cat.NumOverall += 1
			for idx, bkt := range cat.Buckets {
				if duration >= bkt.MinDuration {
					bkt.NumActive += addActive
					bkt.NumErrored += addErrored
					bkt.NumSucceeded += addSucceeded
					bkt.NumOverall += 1
					cat.Buckets[idx] = bkt
				}
			}
		}
		return nil
	}

	for name, rb := range c.traces.getAll() {
		thisCategory := TraceCategory{Name: name, Buckets: newBuckets()}
		rb.walk(func(tr Trace) error { return observe(tr, &thisCategory, &stats.Overall) })
		stats.Categories = append(stats.Categories, thisCategory)
	}

	sort.Slice(stats.Categories, func(i, j int) bool {
		return stats.Categories[i].Name < stats.Categories[j].Name
	})

	return stats
}

//
//
//

// Logf adds a log event to the collector. Arguments are evaluated immediately.
func (c *Collector) Logf(category, format string, args ...interface{}) {
	lg := NewCoreLog(category, MakeEvent(format, args...))
	c.logs.getOrCreate(category).add(lg)
}

// LazyLogf adds a lazy log event to the collector. Arguments are stored for an
// indeterminate length of time, and are evaluated from multiple goroutines, and
// so must be safe for concurrent access.
func (c *Collector) LazyLogf(category, format string, args ...interface{}) {
	lg := NewCoreLog(category, MakeLazyEvent(format, args...))
	c.logs.getOrCreate(category).add(lg)
}

// Logs returns all logs in the collector, newest first.
func (c *Collector) Logs() Logs {
	lgs, _ := c.LogQuery(LogFilter{}, 0)
	return lgs
}

// LogQuery returns the n most recent logs matching f, and the total number
// of logs that matched overall.
func (c *Collector) LogQuery(f LogFilter, n int) (Logs, int) {
	var res Logs

	// Collector maintains logs by category, so walk those and get matches.
	for _, lgs := range c.logs.getAll() {
		lgs.walk(func(lg Log) error {
			if f.Allow(lg) {
				res = append(res, lg)
			}
			return nil
		})
	}

	// Each chunk was newest-to-oldest, so make the whole slice that way.
	sort.Slice(res, func(i, j int) bool {
		return res[i].Event().When.After(res[j].Event().When)
	})

	// Limit the returned values.
	total := len(res)
	if n > 0 && total > n {
		res = res[:n]
	}

	return res, total
}

// LogStats returns summary statistics for all logs in the collector.
func (c *Collector) LogStats() LogStats {
	var stats LogStats

	for name, rb := range c.logs.getAll() {
		head, tail, count := rb.stats()
		switch {
		case count == 0:
			stats.Categories = append(stats.Categories, LogCategory{
				Name:  name,
				Count: count,
			})

		case count > 0:
			newest := head.Event().When
			oldest := tail.Event().When
			stats.Categories = append(stats.Categories, LogCategory{
				Name:   name,
				Count:  count,
				Newest: newest,
				Oldest: oldest,
				Rate:   calcRate(count, newest, oldest),
			})
			stats.Overall.Count += count
			stats.Overall.Newest = pickNewest(stats.Overall.Newest, newest)
			stats.Overall.Oldest = pickOldest(stats.Overall.Oldest, oldest)
		}
	}

	stats.Overall.Rate = calcRate(stats.Overall.Count, stats.Overall.Newest, stats.Overall.Oldest)

	sort.Slice(stats.Categories, func(i, j int) bool {
		return stats.Categories[i].Name < stats.Categories[j].Name
	})

	return stats
}

//
//
//

// TraceStats captures summary statistics for log events across all categories.
type TraceStats struct {
	Bucketing  []time.Duration
	Categories []TraceCategory
	Overall    TraceCategory
}

// GetCategory returns stats for the named category, if it exists.
func (s *TraceStats) GetCategory(name string) (TraceCategory, bool) {
	for _, c := range s.Categories {
		if c.Name == name {
			return c, true
		}
	}
	return TraceCategory{}, false
}

// TraceCategory collects summary statistics for traces in a single category.
type TraceCategory struct {
	Name       string
	NumActive  int
	Buckets    []TraceBucket
	NumErrored int
	NumOverall int
}

// ClassName returns a hopefully-unique string suitable as a CSS class name.
func (c *TraceCategory) ClassName() string {
	h := sha256.Sum256([]byte(c.Name))
	s := hex.EncodeToString(h[:])
	return "category-" + s
}

// TraceBucket collects statistics for a subset of traces in a single category
// with durations equal to or greater than the minimum duration of the bucket.
type TraceBucket struct {
	MinDuration  time.Duration
	NumActive    int
	NumSucceeded int
	NumErrored   int
	NumOverall   int
}

// ClassName returns a hopefully-unique string suitable as a CSS class name.
// Note that the string only reflects the bucket and not e.g. category names.
func (b *TraceBucket) ClassName() string {
	return fmt.Sprintf("min-%s", b.MinDuration.Truncate(time.Millisecond))
}

// TraceFilter captures parameters that can select specific trace events.
type TraceFilter struct {
	IDs         []string
	Category    string
	Active      bool
	Finished    bool
	Succeeded   bool
	Errored     bool
	MinDuration *time.Duration
	Query       string         // user input
	Regexp      *regexp.Regexp // what's actually used
}

func (f *TraceFilter) CanHighlight() bool {
	var (
		noIDs          = len(f.IDs) == 0
		hasCategory    = f.Category != ""
		hasActive      = f.Active
		hasErrored     = f.Errored
		hasMinDuration = f.MinDuration != nil
	)
	return noIDs && (hasCategory || hasActive || hasErrored || hasMinDuration)
}

// IsZero returns true if the filter has no constraints.
func (f *TraceFilter) IsZero() bool {
	if hasIDs := len(f.IDs) > 0; hasIDs {
		return false
	}
	if hasCategory := f.Category != ""; hasCategory {
		return false
	}
	if hasActive := f.Active; hasActive {
		return false
	}
	if hasFinished := f.Finished; hasFinished {
		return false
	}
	if hasSucceeded := f.Succeeded; hasSucceeded {
		return false
	}
	if hasErrored := f.Errored; hasErrored {
		return false
	}
	if hasMinDuration := f.MinDuration != nil; hasMinDuration {
		return false
	}
	if hasQuery := f.Regexp != nil; hasQuery {
		return false
	}
	return true
}

// Allow returns true if the given trace satisfies the filter.
func (f *TraceFilter) Allow(tr Trace) bool {
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

	if f.Active && !tr.Active() {
		return false
	}

	if f.Finished && !tr.Finished() {
		return false
	}

	if f.Succeeded && !tr.Succeeded() {
		return false
	}

	if f.Errored && !tr.Errored() {
		return false
	}

	if f.MinDuration != nil && (!tr.Finished() || tr.Duration() < *f.MinDuration) {
		return false
	}

	if f.Regexp != nil {
		if matchedSomething := func() bool {
			if f.Regexp.MatchString(tr.ID()) {
				return true
			}

			if f.Regexp.MatchString(tr.Category()) {
				return true
			}

			for _, ev := range tr.Events() {
				if ev.MatchRegexp(f.Regexp) {
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

// LogFilter captures parameters that can select specific log events.
type LogFilter struct {
	Category string
	Query    string         // user input
	Regexp   *regexp.Regexp // what's actually used
}

// IsZero returns true if the filter has no constraints.
func (f LogFilter) IsZero() bool {
	return f.Category == "" && f.Regexp == nil
}

// Allow returns true if the given log satisfies the filter.
func (f *LogFilter) Allow(lg Log) bool {
	if f.Category != "" && f.Category != lg.Category() {
		return false
	}

	if f.Regexp != nil && !lg.MatchRegexp(f.Regexp) {
		return false
	}

	return true
}

// LogStats captures summary statistics for log events across all categories.
type LogStats struct {
	Categories []LogCategory
	Overall    LogCategory
}

// GetCategory returns stats for the named category, if it exists.
func (s *LogStats) GetCategory(name string) (LogCategory, bool) {
	for _, c := range s.Categories {
		if c.Name == name {
			return c, true
		}
	}
	return LogCategory{}, false
}

// LogCategory captures summary statistic for log events in a category.
type LogCategory struct {
	Name   string
	Count  int
	Oldest time.Time
	Newest time.Time
	Rate   float64
}

// ClassName returns a hopefully-unique string for the category, which is
// suitable for use as a CSS class name.
func (c *LogCategory) ClassName() string {
	h := sha256.Sum256([]byte(c.Name))
	s := hex.EncodeToString(h[:])
	return "category-" + s
}

//
//
//

func calcRate(count int, newest, oldest time.Time) float64 {
	if oldest.After(newest) {
		oldest, newest = newest, oldest
	}

	d := newest.Sub(oldest).Seconds()
	if d == 0 {
		return 0.0
	}

	return float64(count) / float64(d)
}

func pickNewest(current, candidate time.Time) time.Time {
	if current.IsZero() && !candidate.IsZero() {
		return candidate
	}

	if !current.IsZero() && !candidate.IsZero() && candidate.After(current) {
		return candidate
	}

	return current
}

func pickOldest(current, candidate time.Time) time.Time {
	if current.IsZero() && !candidate.IsZero() {
		return candidate
	}

	if !current.IsZero() && !candidate.IsZero() && candidate.Before(current) {
		return candidate
	}

	return current
}
