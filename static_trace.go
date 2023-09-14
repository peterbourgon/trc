package trc

import (
	"encoding/json"
	"time"
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

// NewSearchTrace produces a static trace
func NewSearchTrace(tr Trace) *StaticTrace {
	duration := tr.Duration()
	return &StaticTrace{
		TraceSource:      tr.Source(),
		TraceID:          tr.ID(),
		TraceCategory:    tr.Category(),
		TraceStarted:     tr.Started(),
		TraceDuration:    duration,
		TraceDurationStr: duration.String(),
		TraceDurationSec: duration.Seconds(),
		TraceFinished:    tr.Finished(),
		TraceErrored:     tr.Errored(),
		TraceEvents:      tr.Events(),
	}
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
	return &StaticTrace{
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

func (st *StaticTrace) ID() string                            { return st.TraceID }
func (st *StaticTrace) Source() string                        { return st.TraceSource }
func (st *StaticTrace) Category() string                      { return st.TraceCategory }
func (st *StaticTrace) Started() time.Time                    { return st.TraceStarted }
func (st *StaticTrace) Tracef(format string, args ...any)     {}
func (st *StaticTrace) LazyTracef(format string, args ...any) {}
func (st *StaticTrace) Errorf(format string, args ...any)     {}
func (st *StaticTrace) LazyErrorf(format string, args ...any) {}
func (st *StaticTrace) Finish()                               {}
func (st *StaticTrace) Finished() bool                        { return st.TraceFinished }
func (st *StaticTrace) Errored() bool                         { return st.TraceErrored }
func (st *StaticTrace) Duration() time.Duration               { return st.TraceDuration }
func (st *StaticTrace) Events() []Event                       { return st.TraceEvents }

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

func (st *StaticTrace) Dump() string {
	buf, _ := json.MarshalIndent(st, "", "    ")
	return string(buf)
}

type EventStats struct {
	Index      int
	Count      int
	Cumulative time.Duration
	Duration   time.Duration
	Percent    float64 // 0..100
}

func (st *StaticTrace) EventStats(eventIndex int) EventStats {
	stats := EventStats{
		Index: eventIndex,
		Count: len(st.TraceEvents),
	}

	if eventIndex < 0 {
		return stats
	}

	if eventIndex >= len(st.TraceEvents) {
		lastEvent := st.TraceEvents[len(st.TraceEvents)-1]
		lastEventWhen := lastEvent.When
		endEventWhen := st.TraceStarted.Add(st.TraceDuration)

		stats.Cumulative = st.TraceDuration
		stats.Duration = endEventWhen.Sub(lastEventWhen)
		stats.Percent = 100 * float64(stats.Duration) / float64(st.TraceDuration)

		return stats
	}

	event := st.TraceEvents[eventIndex]
	eventWhen := event.When
	previousEventWhen := st.TraceStarted
	if eventIndex > 0 {
		previousEventWhen = st.TraceEvents[eventIndex-1].When
	}

	stats.Cumulative = eventWhen.Sub(st.TraceStarted)
	stats.Duration = eventWhen.Sub(previousEventWhen)
	stats.Percent = 100 * float64(stats.Duration) / float64(st.TraceDuration)

	return stats
}

type RenderEvent struct {
	IsStart, IsEnd bool
	Index          int
	When           time.Time
	Delta          time.Duration
	DeltaPercent   float64
	Cumulative     time.Duration
	What           string
	IsError        bool
	Stack          []Frame
}

func (st *StaticTrace) RenderEvents() []RenderEvent {
	var events []RenderEvent

	// Synthetic "start" event.
	events = append(events, RenderEvent{
		IsStart: true,
		Index:   -1,
		When:    st.TraceStarted,
		What:    "start",
	})

	// Actual trace events.
	prev := st.TraceStarted
	for i, ev := range st.TraceEvents {
		delta := ev.When.Sub(prev)
		events = append(events, RenderEvent{
			Index:        i,
			When:         ev.When,
			Delta:        delta,
			DeltaPercent: 100 * float64(delta) / float64(st.TraceDuration),
			Cumulative:   ev.When.Sub(st.TraceStarted),
			What:         ev.What,
			IsError:      ev.IsError,
			Stack:        ev.Stack,
		})
		prev = ev.When
	}

	// Synthetic "end" event.
	when := st.TraceStarted.Add(st.TraceDuration)
	delta := when.Sub(prev)
	what := iff(st.TraceFinished, "finished", "active...")
	events = append(events, RenderEvent{
		IsEnd:        true,
		Index:        len(st.TraceEvents),
		When:         when,
		Delta:        delta,
		DeltaPercent: 100 * float64(delta) / float64(st.TraceDuration),
		Cumulative:   st.TraceDuration,
		What:         what,
	})

	return events
}
