package trc

import (
	"encoding/json"
	"strings"
	"time"
)

type StaticTrace struct {
	StaticID       string         `json:"id"`
	StaticCategory string         `json:"category"`
	StaticStarted  time.Time      `json:"start"`
	StaticFinished bool           `json:"finished"`
	StaticErrored  bool           `json:"errored"`
	StaticDuration DurationString `json:"duration"`
	StaticEvents   []StaticEvent  `json:"events"`
}

var _ Trace = (*StaticTrace)(nil)

// NewStaticTrace constructs a static copy of the provided trace, including a
// copy of all of the current trace events.
func NewStaticTrace(tr Trace) *StaticTrace {
	return &StaticTrace{
		StaticID:       tr.ID(),
		StaticCategory: tr.Category(),
		StaticStarted:  tr.Started(),
		StaticFinished: tr.Finished(),
		StaticErrored:  tr.Errored(),
		StaticDuration: DurationString(tr.Duration()),
		StaticEvents:   toStaticEvents(tr.Events()),
	}
}

// ID implements Trace.
func (tr *StaticTrace) ID() string { return tr.StaticID }

// Category implements Trace.
func (tr *StaticTrace) Category() string { return tr.StaticCategory }

// Start implements Trace.
func (tr *StaticTrace) Started() time.Time { return tr.StaticStarted }

// Finished implements Trace.
func (tr *StaticTrace) Finished() bool { return tr.StaticFinished }

// Errored implements Trace.
func (tr *StaticTrace) Errored() bool { return tr.StaticErrored }

// Duration implements Trace.
func (tr *StaticTrace) Duration() time.Duration { return time.Duration(tr.StaticDuration) }

// Finish implements Trace, but does nothing.
func (tr *StaticTrace) Finish() { /* no-op */ }

// Error implements Trace, but does nothing.
func (tr *StaticTrace) Error() { /* no-op */ }

// Tracef implements Trace, but does nothing.
func (tr *StaticTrace) Tracef(format string, args ...any) { /* no-op */ }

// LazyTracef implements Trace, but does nothing.
func (tr *StaticTrace) LazyTracef(format string, args ...any) { /* no-op */ }

// Errorf implements Trace, but does nothing.
func (tr *StaticTrace) Errorf(format string, args ...any) { /* no-op */ }

// LazyErrorf implements Trace, but does nothing.
func (tr *StaticTrace) LazyErrorf(format string, args ...any) { /* no-op */ }

// Events implements Trace.
func (tr *StaticTrace) Events() []Event { return toTraceEvents(tr.StaticEvents) }

//
//
//

type StaticEvent struct {
	StaticWhen    time.Time     `json:"when"`
	StaticWhat    string        `json:"what"`
	StaticStack   []StaticFrame `json:"stack"`
	StaticIsError bool          `json:"is_error,omitempty"`
}

var _ Event = (*StaticEvent)(nil)

func (sev StaticEvent) When() time.Time { return sev.StaticWhen }
func (sev StaticEvent) What() string    { return sev.StaticWhat }
func (sev StaticEvent) Stack() []Frame  { return toTraceCallStack(sev.StaticStack) }
func (sev StaticEvent) IsError() bool   { return sev.StaticIsError }

func toStaticEvents(evs []Event) []StaticEvent {
	sevs := make([]StaticEvent, len(evs))
	for i, ev := range evs {
		sevs[i] = StaticEvent{
			StaticWhen:    ev.When(),
			StaticWhat:    ev.What(),
			StaticStack:   toStaticCallStack(ev.Stack()),
			StaticIsError: ev.IsError(),
		}
	}
	return sevs
}

func toTraceEvents(sevs []StaticEvent) []Event {
	evs := make([]Event, len(sevs))
	for i := range sevs {
		evs[i] = sevs[i]
	}
	return evs
}

//
//
//

func toTraceCallStack(scs []StaticFrame) []Frame {
	cs := make([]Frame, len(scs))
	for i := range scs {
		cs[i] = scs[i]
	}
	return cs
}

func toStaticCallStack(cs []Frame) []StaticFrame {
	scs := make([]StaticFrame, len(cs))
	for i, c := range cs {
		scs[i] = StaticFrame{
			StaticFunction: c.Function(),
			StaticFileLine: c.FileLine(),
		}
	}
	return scs
}

//
//
//

var _ Frame = (*StaticFrame)(nil)

type StaticFrame struct {
	StaticFunction string `json:"function"`
	StaticFileLine string `json:"fileline"`
}

func (c StaticFrame) Function() string { return c.StaticFunction }
func (c StaticFrame) FileLine() string { return c.StaticFileLine }

//
//
//

// DurationString is a [time.Duration] that JSON marshals as a string rather
// than int64 nanoseconds.
type DurationString time.Duration

// MarshalJSON implements [json.Marshaler].
func (d *DurationString) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(*d).String())
}

// UnmarshalJSON implements [json.Marshaler].
func (d *DurationString) UnmarshalJSON(data []byte) error {
	if dur, err := time.ParseDuration(strings.Trim(string(data), `"`)); err == nil {
		*d = DurationString(dur)
		return nil
	}
	return json.Unmarshal(data, (*time.Duration)(d))
}
