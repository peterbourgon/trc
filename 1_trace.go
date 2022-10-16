package trc

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
)

// Trace is an interface describing metadata related to something that happened,
// typically an event or request. A common use case is to create a new trace for
// each incoming request to an HTTP server.
//
// Traces should represent ephemeral, short-lived events, and should be accessed
// through a context object. If this doesn't describe your use case, consider
// using the Log type instead.
//
// Implementations of Trace are expected to be safe for concurrent access.
type Trace interface {
	// ID should return a process-unique identifier for the trace.
	ID() string

	// Category should return the user-supplied category of the trace.
	Category() string

	// Start should return the time the trace was created, preferably UTC.
	Start() time.Time

	// Active represents whether or not the trace is still ongoing. It should
	// return true if and only if Finish has not yet been called.
	Active() bool

	// Finished represents whether or not the trace is still ongoing. It's
	// essentially a convenience method opposite to Active. It should return
	// true if and only if Finish has been called.
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
	Tracef(format string, args ...interface{})

	// LazyTracef should capture the arguments without evaluating them, and add
	// a corresponding event to the trace. If the trace is finished, this method
	// should have no effect.
	LazyTracef(format string, args ...interface{})

	// Errorf should behave like Tracef, but also mark the trace as "errored",
	// typically with a boolean flag. If the trace is finished, this method
	// should have no effect.
	Errorf(format string, args ...interface{})

	// LazyErrorf should behave like LazyTracef, but also mark the trace as
	// "errored", typically with a boolean flag. If the trace is finished, this
	// method should have no effect.
	LazyErrorf(format string, args ...interface{})

	// Events should return the immutable events collected by the trace so far.
	Events() []Event
}

//
//
//

// Traces is a collection of traces ordered by start time, newest-first.
type Traces []Trace

// Less implements sort.Interface by start time, newest-first.
func (trs Traces) Less(i, j int) bool { return trs[i].Start().After(trs[j].Start()) }

// Swap implements sort.Interface.
func (trs Traces) Swap(i, j int) { trs[i], trs[j] = trs[j], trs[i] }

// Len implements sort.Interface.
func (trs Traces) Len() int { return len(trs) }

//
//
//

// TraceCore is the default, mutable implementation of the Trace interface.
type TraceCore struct {
	mtx       sync.Mutex
	id        string
	start     time.Time
	category  string
	events    []Event
	truncated int
	errored   bool
	finished  bool
	duration  time.Duration
}

// NewTraceCore creates a new TraceCore with the given category.
func NewTraceCore(category string) *TraceCore {
	now := time.Now().UTC()
	id := ulid.MustNew(ulid.Timestamp(now), traceIDEntropy).String()
	return &TraceCore{
		id:       id,
		start:    now,
		category: category,
	}
}

// Tracef implements Trace.
func (ctr *TraceCore) Tracef(format string, args ...interface{}) {
	ctr.mtx.Lock()
	defer ctr.mtx.Unlock()

	if ctr.finished {
		return
	}

	switch {
	case len(ctr.events) >= getTraceCoreMaxEvents():
		ctr.truncated++
	default:
		ctr.events = append(ctr.events, MakeEvent(format, args...))
	}
}

// LazyTracef implements Trace.
func (ctr *TraceCore) LazyTracef(format string, args ...interface{}) {
	ctr.mtx.Lock()
	defer ctr.mtx.Unlock()

	if ctr.finished {
		return
	}

	switch {
	case len(ctr.events) >= getTraceCoreMaxEvents():
		ctr.truncated++
	default:
		ctr.events = append(ctr.events, MakeLazyEvent(format, args...))
	}
}

// Errorf implements Trace.
func (ctr *TraceCore) Errorf(format string, args ...interface{}) {
	ctr.mtx.Lock()
	defer ctr.mtx.Unlock()

	if ctr.finished {
		return
	}

	ctr.errored = true

	switch {
	case len(ctr.events) >= getTraceCoreMaxEvents():
		ctr.truncated++
	default:
		ctr.events = append(ctr.events, MakeEvent(format, args...))
	}
}

// LazyErrorf implements Trace.
func (ctr *TraceCore) LazyErrorf(format string, args ...interface{}) {
	ctr.mtx.Lock()
	defer ctr.mtx.Unlock()

	if ctr.finished {
		return
	}

	ctr.errored = true

	switch {
	case len(ctr.events) >= getTraceCoreMaxEvents():
		ctr.truncated++
	default:
		ctr.events = append(ctr.events, MakeLazyEvent(format, args...))
	}
}

// Finish implements Trace.
func (ctr *TraceCore) Finish() {
	ctr.mtx.Lock()
	defer ctr.mtx.Unlock()

	if ctr.finished {
		return
	}

	ctr.finished = true
	ctr.duration = time.Since(ctr.start)
}

// ID implements Trace.
func (ctr *TraceCore) ID() string {
	return ctr.id // immutable
}

// Start implements Trace.
func (ctr *TraceCore) Start() time.Time {
	return ctr.start // immutable
}

// Category implements Trace.
func (ctr *TraceCore) Category() string {
	return ctr.category // immutable
}

// Active implements Trace.
func (ctr *TraceCore) Active() bool {
	return !ctr.Finished()
}

// Finished implements Trace.
func (ctr *TraceCore) Finished() bool {
	ctr.mtx.Lock()
	defer ctr.mtx.Unlock()

	return ctr.finished
}

// Succeeded implements Trace.
func (ctr *TraceCore) Succeeded() bool {
	ctr.mtx.Lock()
	defer ctr.mtx.Unlock()

	return ctr.finished && !ctr.errored
}

// Errored implements Trace.
func (ctr *TraceCore) Errored() bool {
	ctr.mtx.Lock()
	defer ctr.mtx.Unlock()

	return ctr.finished && ctr.errored
}

// Duration implements Trace.
func (ctr *TraceCore) Duration() time.Duration {
	ctr.mtx.Lock()
	defer ctr.mtx.Unlock()

	if ctr.finished {
		return ctr.duration
	}

	return time.Since(ctr.start)
}

// Events implements Trace.
func (ctr *TraceCore) Events() []Event {
	ctr.mtx.Lock()
	defer ctr.mtx.Unlock()

	events := make([]Event, len(ctr.events))
	copy(events, ctr.events)

	if ctr.truncated > 0 {
		events = append(events, MakeEvent("(truncated event count %d)", ctr.truncated))
	}

	return events
}

//
//
//

// SetTraceCoreMaxEvents sets the maximum number of events that will be stored
// in a core trace produced by this package. Past this limit, new events will
// increment a "truncated" counter in the trace. The value of that counter is
// represented by a single, final trace event.
//
// The default value is 1000, the minimum is 1, and the maximum is 10000.
func SetTraceCoreMaxEvents(n int) {
	switch {
	case n < traceCoreMaxEventsMin:
		n = traceCoreMaxEventsMin
	case n > traceCoreMaxEventsMax:
		n = traceCoreMaxEventsMax
	}
	atomic.StoreUint64(&traceCoreMaxEvents, uint64(n))
}

const (
	traceCoreMaxEventsMin = 1
	traceCoreMaxEventsDef = 1000
	traceCoreMaxEventsMax = 10000
)

var (
	traceCoreMaxEvents = uint64(traceCoreMaxEventsDef)
	traceIDEntropy     = ulid.DefaultEntropy()
)

func getTraceCoreMaxEvents() int {
	return int(atomic.LoadUint64(&traceCoreMaxEvents))
}

//
//
//

// PrefixedTrace decorates a Trace such that all messages are prefixed with a
// given string. This can be useful to show important stages or sub-sections of
// a call stack in traces without needing to inspect call stacks.
//
//	func process(ctx context.Context, i int, vs []string) error {
//	    ctx = Prefixf(ctx, "[process %02d]", i)
//	    eztrc.Tracef(ctx, "doing something")     // [process 01] doing something
//	    ...
//	    for _, v := range vs {
//	        ctx = Prefixf(ctx, "<%s>", v)
//	        eztrc.Tracef(ctx, "inner loop")      // [process 01] <abc> inner loop
//	        ...
type PrefixedTrace struct {
	Trace
	prefix string
}

// PrefixTracef wraps the trace and prefixes all events with the format string.
func PrefixTracef(tr Trace, format string, args ...interface{}) Trace {
	prefix := strings.TrimSpace(fmt.Sprintf(format, args...))
	if prefix == "" {
		return tr
	}

	return &PrefixedTrace{
		Trace:  tr,
		prefix: prefix + " ",
	}
}

// PrefixContextf decorates the trace in the context, if it exists, with PrefixTracef.
func PrefixContextf(ctx context.Context, format string, args ...interface{}) context.Context {
	tr, ok := MaybeFromContext(ctx)
	if !ok {
		return ctx
	}

	prefix := strings.TrimSpace(fmt.Sprintf(format, args...))
	if prefix == "" {
		return ctx
	}

	ptr := &PrefixedTrace{Trace: tr, prefix: prefix + " "}
	return context.WithValue(ctx, traceContextVal, ptr)
}

// Tracef implements Trace.
func (ptr *PrefixedTrace) Tracef(format string, args ...interface{}) {
	ptr.Trace.Tracef(ptr.prefix+format, args...)
}

// LazyTracef implements Trace.
func (ptr *PrefixedTrace) LazyTracef(format string, args ...interface{}) {
	ptr.Trace.LazyTracef(ptr.prefix+format, args...)
}

// Errorf implements Trace.
func (ptr *PrefixedTrace) Errorf(format string, args ...interface{}) {
	ptr.Trace.Errorf(ptr.prefix+format, args...)
}

// LazyErrorf implements Trace.
func (ptr *PrefixedTrace) LazyErrorf(format string, args ...interface{}) {
	ptr.Trace.LazyErrorf(ptr.prefix+format, args...)
}
