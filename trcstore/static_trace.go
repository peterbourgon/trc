package trcstore

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
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

var _ trc.Trace = (*StaticTrace)(nil)

// NewStaticTrace constructs a static copy of the provided trace, including a
// copy of all of the current trace events.
func NewStaticTrace(tr trc.Trace) *StaticTrace {
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
func (tr *StaticTrace) Events() []trc.Event { return toTraceEvents(tr.StaticEvents) }

//
//
//

type StaticEvent struct {
	StaticWhen    time.Time       `json:"when"`
	StaticWhat    string          `json:"what"`
	StaticStack   StaticCallStack `json:"stack"`
	StaticIsError bool            `json:"is_error,omitempty"`
}

var _ trc.Event = (*StaticEvent)(nil)

func (sev StaticEvent) When() time.Time      { return sev.StaticWhen }
func (sev StaticEvent) What() string         { return sev.StaticWhat }
func (sev StaticEvent) Stack() trc.CallStack { return toTraceCallStack(sev.StaticStack) }
func (sev StaticEvent) IsError() bool        { return sev.StaticIsError }

func toStaticEvents(evs []trc.Event) []StaticEvent {
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

func toTraceEvents(sevs []StaticEvent) []trc.Event {
	evs := make([]trc.Event, len(sevs))
	for i := range sevs {
		evs[i] = sevs[i]
	}
	return evs
}

//
//
//

type StaticCallStack []StaticCall

func toTraceCallStack(scs StaticCallStack) trc.CallStack {
	cs := make(trc.CallStack, len(scs))
	for i := range scs {
		cs[i] = scs[i]
	}
	return cs
}

func toStaticCallStack(cs trc.CallStack) StaticCallStack {
	scs := make(StaticCallStack, len(cs))
	for i, c := range cs {
		scs[i] = StaticCall{
			StaticFunction: c.Function(),
			StaticFileLine: c.FileLine(),
		}
	}
	return scs
}

//
//
//

var _ trc.Call = (*StaticCall)(nil)

type StaticCall struct {
	StaticFunction string `json:"function"`
	StaticFileLine string `json:"fileline"`
}

func (c StaticCall) Function() string { return c.StaticFunction }
func (c StaticCall) FileLine() string { return c.StaticFileLine }

//
//
//

type DurationString time.Duration

func (d *DurationString) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(*d).String())
}

func (d *DurationString) UnmarshalJSON(data []byte) error {
	if dur, err := time.ParseDuration(strings.Trim(string(data), `"`)); err == nil {
		*d = DurationString(dur)
		return nil
	}
	return json.Unmarshal(data, (*time.Duration)(d))
}
