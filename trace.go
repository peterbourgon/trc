package trc

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
)

// Trace is a collection of events and metadata for an operation, typically a
// request, in a program. Traces should normally represent ephemeral and
// short-lived operations, and should be accessed through a context value.
//
// Implementations are expected to be safe for concurrent use.
type Trace interface {
	ID() string
	Category() string
	Started() time.Time
	Duration() time.Duration
	Tracef(format string, args ...any)
	LazyTracef(format string, args ...any)
	Errorf(format string, args ...any)
	LazyErrorf(format string, args ...any)
	Finish()
	Finished() bool
	Errored() bool
	Events() []Event
}

//
//
//

// Traces implements [sort.Interface], ordering traces with more recent start
// timestamps before traces with older start timestamps.
type Traces []Trace

// Less implements [sort.Interface].
func (trs Traces) Less(i, j int) bool { return trs[i].Started().After(trs[j].Started()) }

// Swap implements [sort.Interface].
func (trs Traces) Swap(i, j int) { trs[i], trs[j] = trs[j], trs[i] }

// Len implements [sort.Interface].
func (trs Traces) Len() int { return len(trs) }

//
//
//

// coreTrace is the default, mutable implementation of a trace. Trace IDs are
// ULIDs, using a default monotonic source of entropy. Traces can contain up to
// a maximum number of events, defined by SetMaxEvents.
type coreTrace struct {
	mtx       sync.Mutex
	id        string
	category  string
	start     time.Time
	errored   bool
	finished  bool
	duration  time.Duration
	events    []Event
	truncated int
}

var _ Trace = (*coreTrace)(nil)

// newCoreTrace creates and starts a new trace with the given category.
func newCoreTrace(category string) *coreTrace {
	now := time.Now().UTC()
	id := ulid.MustNew(ulid.Timestamp(now), traceIDEntropy).String()
	return &coreTrace{
		id:       id,
		category: category,
		start:    now,
	}
}

// ID implements Trace.
func (tr *coreTrace) ID() string {
	return tr.id // immutable
}

// Category implements Trace.
func (tr *coreTrace) Category() string {
	return tr.category // immutable
}

// Start implements Trace.
func (tr *coreTrace) Started() time.Time {
	return tr.start // immutable
}

// Duration implements Trace.
func (tr *coreTrace) Duration() time.Duration {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if tr.finished {
		return tr.duration
	}

	return time.Since(tr.start)
}

// Tracef implements Trace.
func (tr *coreTrace) Tracef(format string, args ...any) {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if tr.finished {
		return
	}

	switch {
	case len(tr.events) >= getCoreTraceMaxEvents():
		tr.truncated++
	default:
		tr.events = append(tr.events, newEvent(format, args...))
	}
}

// LazyTracef implements Trace.
func (tr *coreTrace) LazyTracef(format string, args ...any) {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if tr.finished {
		return
	}

	switch {
	case len(tr.events) >= getCoreTraceMaxEvents():
		tr.truncated++
	default:
		tr.events = append(tr.events, newLazyEvent(format, args...))
	}
}

// Errorf implements Trace.
func (tr *coreTrace) Errorf(format string, args ...any) {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if tr.finished {
		return
	}

	tr.errored = true

	switch {
	case len(tr.events) >= getCoreTraceMaxEvents():
		tr.truncated++
	default:
		tr.events = append(tr.events, newErrorEvent(format, args...))
	}
}

// LazyErrorf implements Trace.
func (tr *coreTrace) LazyErrorf(format string, args ...any) {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if tr.finished {
		return
	}

	tr.errored = true

	switch {
	case len(tr.events) >= getCoreTraceMaxEvents():
		tr.truncated++
	default:
		tr.events = append(tr.events, newLazyErrorEvent(format, args...))
	}
}

// Finish implements Trace.
func (tr *coreTrace) Finish() {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if tr.finished {
		return
	}

	tr.finished = true
	tr.duration = time.Since(tr.start)
}

// Finished implements Trace.
func (tr *coreTrace) Finished() bool {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	return tr.finished
}

// Errored implements Trace.
func (tr *coreTrace) Errored() bool {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	return tr.errored
}

// Events implements Trace.
func (tr *coreTrace) Events() []Event {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	events := make([]Event, len(tr.events))
	copy(events, tr.events)

	if tr.truncated > 0 {
		events = append(events, newEvent("(truncated event count %d)", tr.truncated))
	}

	return events
}

// SetMaxEvents sets the maximum number of events that will be stored in a
// default [trc.Trace] as produced by [NewTrace], [FromContext], etc. Once this
// limit is reached, additional events increment a "truncated" counter in the
// trace, the value of which is reported in a single, final event.
//
// By default, the maximum number of events per trace is 1000. The minimum value
// is 1, and the maximum value is 10000.
func SetMaxEvents(n int) {
	switch {
	case n < coreTraceMaxEventsMin:
		n = coreTraceMaxEventsMin
	case n > coreTraceMaxEventsMax:
		n = coreTraceMaxEventsMax
	}
	atomic.StoreUint64(&coreTraceMaxEvents, uint64(n))
}

const (
	coreTraceMaxEventsMin = 1
	coreTraceMaxEventsDef = 1000
	coreTraceMaxEventsMax = 10000
)

var (
	coreTraceMaxEvents = uint64(coreTraceMaxEventsDef)
	traceIDEntropy     = ulid.DefaultEntropy()
)

func getCoreTraceMaxEvents() int {
	return int(atomic.LoadUint64(&coreTraceMaxEvents))
}
