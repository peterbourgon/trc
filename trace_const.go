package trc

import (
	"encoding/json"
	"strings"
	"time"
)

// ConstTrace is a "snapshot" of a trace which can be e.g. JSON serialized but
// cannot be modified. It's used when sending traces over the wire.
type ConstTrace struct {
	ConstID       string         `json:"id"`
	ConstCategory string         `json:"category"`
	ConstStarted  time.Time      `json:"start"`
	ConstFinished bool           `json:"finished"`
	ConstErrored  bool           `json:"errored"`
	ConstDuration DurationString `json:"duration"`
	ConstEvents   []ConstEvent   `json:"events"`
}

var _ Trace = (*ConstTrace)(nil)

// NewConstTrace constructs a static copy of the provided trace, including a
// copy of all of the current trace events.
func NewConstTrace(tr Trace) *ConstTrace {
	return &ConstTrace{
		ConstID:       tr.ID(),
		ConstCategory: tr.Category(),
		ConstStarted:  tr.Started(),
		ConstFinished: tr.Finished(),
		ConstErrored:  tr.Errored(),
		ConstDuration: DurationString(tr.Duration()),
		ConstEvents:   toConstEvents(tr.Events()),
	}
}

// ID implements Trace.
func (tr *ConstTrace) ID() string { return tr.ConstID }

// Category implements Trace.
func (tr *ConstTrace) Category() string { return tr.ConstCategory }

// Start implements Trace.
func (tr *ConstTrace) Started() time.Time { return tr.ConstStarted }

// Finished implements Trace.
func (tr *ConstTrace) Finished() bool { return tr.ConstFinished }

// Errored implements Trace.
func (tr *ConstTrace) Errored() bool { return tr.ConstErrored }

// Duration implements Trace.
func (tr *ConstTrace) Duration() time.Duration { return time.Duration(tr.ConstDuration) }

// Finish implements Trace, but does nothing.
func (tr *ConstTrace) Finish() { /* no-op */ }

// Error implements Trace, but does nothing.
func (tr *ConstTrace) Error() { /* no-op */ }

// Tracef implements Trace, but does nothing.
func (tr *ConstTrace) Tracef(format string, args ...any) { /* no-op */ }

// LazyTracef implements Trace, but does nothing.
func (tr *ConstTrace) LazyTracef(format string, args ...any) { /* no-op */ }

// Errorf implements Trace, but does nothing.
func (tr *ConstTrace) Errorf(format string, args ...any) { /* no-op */ }

// LazyErrorf implements Trace, but does nothing.
func (tr *ConstTrace) LazyErrorf(format string, args ...any) { /* no-op */ }

// Events implements Trace.
func (tr *ConstTrace) Events() []Event { return toTraceEvents(tr.ConstEvents) }

//
//
//

// ConstEvent is a snapshot of a trace event, which can be e.g. JSON serialized.
type ConstEvent struct {
	ConstWhen    time.Time    `json:"when"`
	ConstWhat    string       `json:"what"`
	ConstStack   []ConstFrame `json:"stack"`
	ConstIsError bool         `json:"is_error,omitempty"`
}

var _ Event = (*ConstEvent)(nil)

func (cev ConstEvent) When() time.Time { return cev.ConstWhen }
func (cev ConstEvent) What() string    { return cev.ConstWhat }
func (cev ConstEvent) Stack() []Frame  { return toTraceFrames(cev.ConstStack) }
func (cev ConstEvent) IsError() bool   { return cev.ConstIsError }

func toConstEvents(evs []Event) []ConstEvent {
	cevs := make([]ConstEvent, len(evs))
	for i, ev := range evs {
		cevs[i] = ConstEvent{
			ConstWhen:    ev.When(),
			ConstWhat:    ev.What(),
			ConstStack:   toConstFrames(ev.Stack()),
			ConstIsError: ev.IsError(),
		}
	}
	return cevs
}

func toTraceEvents(cevs []ConstEvent) []Event {
	evs := make([]Event, len(cevs))
	for i := range cevs {
		evs[i] = cevs[i]
	}
	return evs
}

func toTraceFrames(cfs []ConstFrame) []Frame {
	fs := make([]Frame, len(cfs))
	for i := range cfs {
		fs[i] = cfs[i]
	}
	return fs
}

func toConstFrames(cs []Frame) []ConstFrame {
	cfs := make([]ConstFrame, len(cs))
	for i, c := range cs {
		cfs[i] = ConstFrame{
			StaticFunction: c.Function(),
			StaticFileLine: c.FileLine(),
		}
	}
	return cfs
}

var _ Frame = (*ConstFrame)(nil)

type ConstFrame struct {
	StaticFunction string `json:"function"`
	StaticFileLine string `json:"fileline"`
}

func (c ConstFrame) Function() string { return c.StaticFunction }
func (c ConstFrame) FileLine() string { return c.StaticFileLine }

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
