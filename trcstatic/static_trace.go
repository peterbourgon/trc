package trcstatic

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
)

type StaticTrace struct {
	StaticID        string         `json:"id"`
	StaticCategory  string         `json:"category"`
	StaticStart     time.Time      `json:"start"`
	StaticActive    bool           `json:"active"`
	StaticFinished  bool           `json:"finished"`
	StaticSucceeded bool           `json:"succeeded"`
	StaticErrored   bool           `json:"errored"`
	StaticDuration  DurationString `json:"duration"`
	StaticEvents    []*trc.Event   `json:"events"`
}

var _ trc.Trace = (*StaticTrace)(nil)

// NewStaticTrace constructs a static copy of the provided trace, including a
// copy of all of the current trace events.
func NewStaticTrace(tr trc.Trace) *StaticTrace {
	return &StaticTrace{
		StaticID:        tr.ID(),
		StaticCategory:  tr.Category(),
		StaticStart:     tr.Start(),
		StaticActive:    tr.Active(),
		StaticFinished:  tr.Finished(),
		StaticSucceeded: tr.Succeeded(),
		StaticErrored:   tr.Errored(),
		StaticDuration:  DurationString(tr.Duration()),
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
func (tr *StaticTrace) Events() []*trc.Event { return tr.StaticEvents }

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
