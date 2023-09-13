package trc

import (
	"time"
)

type StreamTrace struct {
	TraceSource      string        `json:"source"`
	TraceID          string        `json:"id"`
	TraceCategory    string        `json:"category"`
	TraceStarted     time.Time     `json:"started"`
	TraceDuration    time.Duration `json:"duration"`
	TraceDurationStr string        `json:"duration_str"`
	TraceDurationSec float64       `json:"duration_sec"`
	TraceFinished    bool          `json:"finished,omitempty"`
	TraceErrored     bool          `json:"errored,omitempty"`
	TraceEvents      []Event       `json:"events,omitempty"`
}

var _ Trace = (*StreamTrace)(nil) // needs to be passed to Filter.Allow

func NewStreamTrace(tr Trace) *StreamTrace {
	// Active stream traces include only the most recent event. This is to allow
	// subscribers to stream complete traces with all trace events, by filtering
	// for IsFinished true; or to stream individual events as they occur, by not
	// filtering for IsFinished. In both cases, the final stream trace for a
	// given trace ID will always include all events.
	//
	// Also, stream trace events don't include stacks. This is an optimization
	// to reduce stream trace size, based on the assumption that stacks are far
	// less useful to stream subscribers.
	var (
		isActive         = !tr.Finished()
		detail, isDetail = tr.(interface{ EventsDetail(int, bool) []Event })
		events           []Event
	)
	switch {
	case isActive && isDetail:
		events = detail.EventsDetail(1, false)
	case !isActive && isDetail:
		events = detail.EventsDetail(-1, false)
	case isActive && !isDetail:
		events = tr.Events()
		events = events[len(events)-1:]
		for i := range events {
			events[i].Stack = events[i].Stack[:0]
		}
	case !isActive && !isDetail:
		events = tr.Events()
		for i := range events {
			events[i].Stack = events[i].Stack[:0]
		}
	}

	duration := tr.Duration()

	return &StreamTrace{
		TraceSource:      tr.Source(),
		TraceID:          tr.ID(),
		TraceCategory:    tr.Category(),
		TraceStarted:     tr.Started(),
		TraceDuration:    duration,
		TraceDurationStr: duration.String(),
		TraceDurationSec: duration.Seconds(),
		TraceFinished:    tr.Finished(),
		TraceErrored:     tr.Errored(),
		TraceEvents:      events,
	}
}

func (st *StreamTrace) ID() string                            { return st.TraceID }
func (st *StreamTrace) Source() string                        { return st.TraceSource }
func (st *StreamTrace) Category() string                      { return st.TraceCategory }
func (st *StreamTrace) Started() time.Time                    { return st.TraceStarted }
func (st *StreamTrace) Tracef(format string, args ...any)     {}
func (st *StreamTrace) LazyTracef(format string, args ...any) {}
func (st *StreamTrace) Errorf(format string, args ...any)     {}
func (st *StreamTrace) LazyErrorf(format string, args ...any) {}
func (st *StreamTrace) Finish()                               {}
func (st *StreamTrace) Finished() bool                        { return st.TraceFinished }
func (st *StreamTrace) Errored() bool                         { return st.TraceErrored }
func (st *StreamTrace) Duration() time.Duration               { return st.TraceDuration }
func (st *StreamTrace) Events() []Event                       { return st.TraceEvents }
