package trcstream

import (
	"time"

	"github.com/peterbourgon/trc"
)

type StreamTrace struct {
	TraceID          string        `json:"id"`
	TraceSource      string        `json:"source"`
	TraceCategory    string        `json:"category"`
	TraceStarted     time.Time     `json:"started"`
	TraceDuration    time.Duration `json:"duration"`
	TraceDurationStr string        `json:"duration_str"`
	TraceDurationSec float64       `json:"duration_sec"`
	TraceFinished    bool          `json:"finished,omitempty"`
	TraceErrored     bool          `json:"errored,omitempty"`
	TraceEvents      []trc.Event   `json:"events,omitempty"`
}

var _ trc.Trace = (*StreamTrace)(nil) // needs to be passed to Filter.Allow

func NewStreamTrace(tr trc.Trace) *StreamTrace {
	events := tr.Events()

	// Active stream traces include only the most recent event.
	if !tr.Finished() && len(events) > 0 {
		events = events[len(events)-1:]
	}

	// All stream trace events have no stacks.
	for i := range events {
		events[i].Stack = events[i].Stack[:0]
	}

	duration := tr.Duration()

	return &StreamTrace{
		TraceID:          tr.ID(),
		TraceSource:      tr.Source(),
		TraceCategory:    tr.Category(),
		TraceStarted:     tr.Started(),
		TraceFinished:    tr.Finished(),
		TraceErrored:     tr.Errored(),
		TraceDuration:    duration,
		TraceDurationStr: duration.String(),
		TraceDurationSec: duration.Seconds(),
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
func (st *StreamTrace) Events() []trc.Event                   { return st.TraceEvents }
