package trc

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
)

// Trace is a collection of events and metadata for an operation, typically a
// request, in a program. Traces should normally represent ephemeral and
// short-lived events, and should be accessed through a context object.
//
// Implementations of Trace are expected to be safe for concurrent use.
type Trace interface {
	// ID should return a unique identifier for the trace.
	ID() string

	// Category should return the user-supplied category of the trace.
	Category() string

	// Start should return the time the trace was created, preferably UTC.
	Start() time.Time

	// Active represents whether or not the trace is still ongoing. It should
	// return true if and only if Finish has not yet been called. Once finished,
	// a trace should not be re-started.
	Active() bool

	// Finished represents whether or not the trace is still ongoing. It's
	// essentially a convenience method opposite to Active. It should return
	// true if and only if Finish has been called. Once finished, a trace should
	// not be re-started.
	Finished() bool

	// Succeeded represents whether a trace has finished without any errors. It
	// should return true if and only if Finish has been called, without any
	// calls to Error-class methods having been made beforehand.
	Succeeded() bool

	// Errored represents whether a trace has finished with one or more errors.
	// It should return true if and only if Finish has been called, with one or
	// more calls to Error-class methods having been made beforehand.
	Errored() bool

	// Duration represents the lifetime of the trace. If the trace is active, it
	// should return the time since the start time. If the trace is finished, it
	// should return the difference between the start time and the time Finish
	// was called.
	Duration() time.Duration

	// Finish marks the trace as completed. Once called, the trace should be
	// "frozen" and immutable. Subsequent calls to Trace- or Error-class methods
	// should be no-ops.
	Finish()

	// Tracef should immediately (synchromously) evaluate the provided
	// arguments, and add a corresponding event to the trace. If the trace is
	// finished, this method should have no effect.
	Tracef(format string, args ...any)

	// LazyTracef should capture the arguments without evaluating them, and add
	// a corresponding event to the trace. If the trace is finished, this method
	// should have no effect.
	LazyTracef(format string, args ...any)

	// Errorf should behave like Tracef, but also mark the trace as "errored",
	// typically with a boolean flag. If the trace is finished, this method
	// should have no effect.
	Errorf(format string, args ...any)

	// LazyErrorf should behave like LazyTracef, but also mark the trace as
	// "errored", typically with a boolean flag. If the trace is finished, this
	// method should have no effect.
	LazyErrorf(format string, args ...any)

	// Events should return the events collected by the trace so far.
	//
	// Implementations should ensure that the returned slice is safe for
	// concurrent use. Events themselves are expected to be immutable, so this
	// typically means that implementations should create and return a new slice
	// for each caller.
	Events() []*Event
}

//
//
//

type traces []Trace

func (trs traces) Less(i, j int) bool { return trs[i].Start().After(trs[j].Start()) }
func (trs traces) Swap(i, j int)      { trs[i], trs[j] = trs[j], trs[i] }
func (trs traces) Len() int           { return len(trs) }

//
//
//

// coreTrace is the default, mutable implementation of a trace, used by the
// package and the collector. Trace IDs are ULIDs, using a default monotonic
// source of entropy. Traces can contain up to a max number of events defined by
// SetCoreTraceMaxEvents.
type coreTrace struct {
	mtx       sync.Mutex
	uri       string
	id        string
	category  string
	start     time.Time
	errored   bool
	finished  bool
	duration  time.Duration
	events    []*Event
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
		tr.events = append(tr.events, NewEvent(format, args...))
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
		tr.events = append(tr.events, NewLazyEvent(format, args...))
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
		tr.events = append(tr.events, NewErrorEvent(format, args...))
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
		tr.events = append(tr.events, NewLazyErrorEvent(format, args...))
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

func (tr *coreTrace) URI() string {
	return tr.uri
}

// ID implements Trace.
func (tr *coreTrace) ID() string {
	return tr.id // immutable
}

// Start implements Trace.
func (tr *coreTrace) Start() time.Time {
	return tr.start // immutable
}

// Category implements Trace.
func (tr *coreTrace) Category() string {
	return tr.category // immutable
}

// Active implements Trace.
func (tr *coreTrace) Active() bool {
	return !tr.Finished()
}

// Finished implements Trace.
func (tr *coreTrace) Finished() bool {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	return tr.finished
}

// Succeeded implements Trace.
func (tr *coreTrace) Succeeded() bool {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	return tr.finished && !tr.errored
}

// Errored implements Trace.
func (tr *coreTrace) Errored() bool {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	return tr.finished && tr.errored
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

// Events implements Trace.
func (tr *coreTrace) Events() []*Event {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	events := make([]*Event, len(tr.events))
	copy(events, tr.events)

	if tr.truncated > 0 {
		events = append(events, NewEvent("(truncated event count %d)", tr.truncated))
	}

	return events
}

// MarshalJSON implements json.Marshaler for the trace by first converting it to
// a static trace.
func (tr *coreTrace) MarshalJSON() ([]byte, error) {
	return json.Marshal(newStaticTrace(tr))
}

//
//
//

// SetDefaultMaxEvents sets the maximum number of events that will be stored
// in a CoreTrace. Once this limit is reached, additional events increment a
// "truncated" counter in the trace, the value of which is reported in a single,
// final event.
//
// The default value is 1000, the minimum is 1, and the maximum is 10000.
func SetDefaultMaxEvents(n int) {
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

//
//
//

// StaticTrace is an immutable "copy" of a trace and its events, which, unlike
// the core trace, can be serialized. Although static trace implements the trace
// interface, the interfaces which would normally mutate the trace are no-ops.
type StaticTrace struct {
	// Via records the source(s) of the trace, which is useful when aggregating
	// traces from multiple collectors into a single result.
	Via []string `json:"via,omitempty"`

	StaticID        string         `json:"id"`
	StaticCategory  string         `json:"category"`
	StaticStart     time.Time      `json:"start"`
	StaticActive    bool           `json:"active"`
	StaticFinished  bool           `json:"finished"`
	StaticSucceeded bool           `json:"succeeded"`
	StaticErrored   bool           `json:"errored"`
	StaticDuration  durationString `json:"duration"`
	StaticEvents    []*Event       `json:"events"`
}

var _ Trace = (*StaticTrace)(nil)

// newStaticTrace constructs a static copy of the provided trace, including a
// copy of all of the current trace events.
func newStaticTrace(tr Trace) *StaticTrace {
	return &StaticTrace{
		StaticID:        tr.ID(),
		StaticCategory:  tr.Category(),
		StaticStart:     tr.Start(),
		StaticActive:    tr.Active(),
		StaticFinished:  tr.Finished(),
		StaticSucceeded: tr.Succeeded(),
		StaticErrored:   tr.Errored(),
		StaticDuration:  durationString(tr.Duration()),
		StaticEvents:    tr.Events(),
	}
}

// ID implements Trace.
func (tr *StaticTrace) ID() string { return tr.StaticID }

// Category implements Trace.
func (tr *StaticTrace) Category() string { return tr.StaticCategory }

// Start implements Trace.
func (tr *StaticTrace) Start() time.Time { return tr.StaticStart }

// Active implements Trace.
func (tr *StaticTrace) Active() bool { return tr.StaticActive }

// Finished implements Trace.
func (tr *StaticTrace) Finished() bool { return tr.StaticFinished }

// Succeeded implements Trace.
func (tr *StaticTrace) Succeeded() bool { return tr.StaticSucceeded }

// Errored implements Trace.
func (tr *StaticTrace) Errored() bool { return tr.StaticErrored }

// Duration implements Trace.
func (tr *StaticTrace) Duration() time.Duration { return time.Duration(tr.StaticDuration) }

// Finish implements Trace, but does nothing.
func (tr *StaticTrace) Finish() { /* no-op */ }

// Tracef implements Trace, but does nothing.
func (tr *StaticTrace) Tracef(format string, args ...any) { /* no-op */ }

// LazyTracef implements Trace, but does nothing.
func (tr *StaticTrace) LazyTracef(format string, args ...any) { /* no-op */ }

// Errorf implements Trace, but does nothing.
func (tr *StaticTrace) Errorf(format string, args ...any) { /* no-op */ }

// LazyErrorf implements Trace, but does nothing.
func (tr *StaticTrace) LazyErrorf(format string, args ...any) { /* no-op */ }

// Events implements Trace.
func (tr *StaticTrace) Events() []*Event { return tr.StaticEvents }

// durationString is a time.Duration which JSON marshals as a string.
type durationString time.Duration

// MarshalJSON implements json.Marshaler.
func (d *durationString) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(*d).String())
}

// UnmarshalJSON implements json.Marshaler.
func (d *durationString) UnmarshalJSON(data []byte) error {
	if dur, err := time.ParseDuration(strings.Trim(string(data), `"`)); err == nil {
		*d = durationString(dur)
		return nil
	}
	return json.Unmarshal(data, (*time.Duration)(d))
}

//
//
//

// prefixedTrace decorates a trace and adds a user-supplied prefix to each event.
// This can be useful to show important regions of execution without needing to
// inspect full call stacks.
type prefixedTrace struct {
	Trace
	format string
	args   []any
}

// PrefixTrace wraps the trace with the provided prefix.
func Prefix(tr Trace, format string, args ...any) Trace {
	format = strings.TrimSpace(format)

	if format == "" {
		return tr
	}

	return &prefixedTrace{
		Trace:  tr,
		format: format + " ",
		args:   args,
	}
}

// Tracef implements Trace, adding a prefix to the provided format string.
func (ptr *prefixedTrace) Tracef(format string, args ...any) {
	ptr.Trace.Tracef(ptr.format+format, append(ptr.args, args...)...)
}

// LazyTracef implements Trace, adding a prefix to the provided format string.
func (ptr *prefixedTrace) LazyTracef(format string, args ...any) {
	ptr.Trace.LazyTracef(ptr.format+format, append(ptr.args, args...)...)
}

// Errorf implements Trace, adding a prefix to the provided format string.
func (ptr *prefixedTrace) Errorf(format string, args ...any) {
	ptr.Trace.Errorf(ptr.format+format, append(ptr.args, args...)...)
}

// LazyErrorf implements Trace, adding a prefix to the provided format string.
func (ptr *prefixedTrace) LazyErrorf(format string, args ...any) {
	ptr.Trace.LazyErrorf(ptr.format+format, append(ptr.args, args...)...)
}
