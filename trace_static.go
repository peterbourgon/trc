package trc

import (
	"runtime"
	"sync"
	"time"

	"github.com/peterbourgon/trc/internal/trcdebug"
)

// StaticTrace is a "snapshot" of a trace which can be sent over the wire.
type StaticTrace struct {
	TraceSource      string        `json:"source"`
	TraceID          string        `json:"id"`
	TraceCategory    string        `json:"category"`
	TraceStarted     time.Time     `json:"started"`
	TraceDuration    time.Duration `json:"duration"`
	TraceDurationStr string        `json:"duration_str,omitempty"`
	TraceDurationSec float64       `json:"duration_sec,omitempty"`
	TraceFinished    bool          `json:"finished,omitempty"`
	TraceErrored     bool          `json:"errored,omitempty"`
	TraceEvents      []Event       `json:"events,omitempty"`
}

var _ Trace = (*StaticTrace)(nil) // needs to be passed to Filter.Allow

var staticTracePool = sync.Pool{
	New: func() any {
		trcdebug.StaticTraceAllocCount.Add(1)
		return &StaticTrace{}
	},
}

func newStaticTrace() *StaticTrace {
	trcdebug.StaticTraceNewCount.Add(1)
	st := staticTracePool.Get().(*StaticTrace)

	runtime.SetFinalizer(st, func(st *StaticTrace) {
		trcdebug.StaticTraceFreeCount.Add(1)
		staticTracePool.Put(st)
	})

	st.TraceSource = ""
	st.TraceID = ""
	st.TraceCategory = ""
	st.TraceStarted = time.Time{}
	st.TraceDuration = 0
	st.TraceDurationStr = ""
	st.TraceDurationSec = 0
	st.TraceFinished = false
	st.TraceErrored = false
	st.TraceEvents = st.TraceEvents[:0]

	return st
}

// NewSearchTrace produces a static trace intended for a search response.
func NewSearchTrace(tr Trace) *StaticTrace {
	st := newStaticTrace()
	st.TraceSource = tr.Source()
	st.TraceID = tr.ID()
	st.TraceCategory = tr.Category()
	st.TraceStarted = tr.Started()
	st.TraceDuration = tr.Duration()
	st.TraceFinished = tr.Finished()
	st.TraceErrored = tr.Errored()
	st.TraceEvents = tr.Events()
	return st
}

// NewStreamTrace produces a static trace meant for streaming. If the trace is
// active, only the most recent event is included. Also, stacks are removed from
// every event.
func NewStreamTrace(tr Trace) *StaticTrace {
	var (
		isActive          = !tr.Finished()
		detail, canDetail = tr.(interface{ EventsDetail(int, bool) []Event })
		events            = []Event{}
	)
	switch {
	case canDetail && isActive:
		events = detail.EventsDetail(1, false)
	case canDetail && !isActive:
		events = detail.EventsDetail(-1, false)
	case !canDetail && isActive:
		events = tr.Events()
		events = events[len(events)-1:]
		for i := range events {
			events[i].Stack = events[i].Stack[:0]
		}
	case !canDetail && !isActive:
		events = tr.Events()
		for i := range events {
			events[i].Stack = events[i].Stack[:0]
		}
	}

	duration := tr.Duration()

	st := newStaticTrace()
	st.TraceSource = tr.Source()
	st.TraceID = tr.ID()
	st.TraceCategory = tr.Category()
	st.TraceStarted = tr.Started()
	st.TraceDuration = duration
	st.TraceDurationStr = duration.String()
	st.TraceDurationSec = duration.Seconds()
	st.TraceFinished = tr.Finished()
	st.TraceErrored = tr.Errored()
	st.TraceEvents = events

	return st
}

// ID implements the Trace interface.
func (st *StaticTrace) ID() string { return st.TraceID }

// Source implements the Trace interface.
func (st *StaticTrace) Source() string { return st.TraceSource }

// Category implements the Trace interface.
func (st *StaticTrace) Category() string { return st.TraceCategory }

// Started implements the Trace interface.
func (st *StaticTrace) Started() time.Time { return st.TraceStarted }

// Tracef implements the Trace interface.
func (st *StaticTrace) Tracef(format string, args ...any) {}

// LazyTracef implements the Trace interface.
func (st *StaticTrace) LazyTracef(format string, args ...any) {}

// Errorf implements the Trace interface.
func (st *StaticTrace) Errorf(format string, args ...any) {}

// LazyErrorf implements the Trace interface.
func (st *StaticTrace) LazyErrorf(format string, args ...any) {}

// Finish implements the Trace interface.
func (st *StaticTrace) Finish() {}

// Finished implements the Trace interface.
func (st *StaticTrace) Finished() bool { return st.TraceFinished }

// Errored implements the Trace interface.
func (st *StaticTrace) Errored() bool { return st.TraceErrored }

// Duration implements the Trace interface.
func (st *StaticTrace) Duration() time.Duration { return st.TraceDuration }

// Events implements the Trace interface.
func (st *StaticTrace) Events() []Event { return st.TraceEvents }

// TrimStacks reduces the stacks of every event in the trace based on depth. A
// depth of 0 means "no change" -- to remove stacks, use a depth of -1.
func (st *StaticTrace) TrimStacks(depth int) *StaticTrace {
	if depth == 0 {
		return st // zero value (0) means don't do anything
	}
	if depth < 0 {
		depth = 0 // negative value means remove all stacks
	}
	for i, ev := range st.TraceEvents {
		if len(ev.Stack) > depth {
			ev.Stack = ev.Stack[:depth]
			st.TraceEvents[i] = ev
		}
	}
	return st
}

//
//
//

type staticTracesNewestFirst []*StaticTrace

func (sts staticTracesNewestFirst) Len() int { return len(sts) }

func (sts staticTracesNewestFirst) Swap(i, j int) { sts[i], sts[j] = sts[j], sts[i] }

func (sts staticTracesNewestFirst) Less(i, j int) bool {
	var (
		iStarted = sts[i].Started()
		jStarted = sts[j].Started()
	)
	switch {
	case iStarted.After(jStarted):
		return true
	case iStarted.Before(jStarted):
		return false
	default:
		return sts[i].ID() > sts[j].ID()
	}

}
