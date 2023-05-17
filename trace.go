package trc

import (
	"time"
)

// Trace is a collection of metadata and events for a single operation,
// typically a request, in a program. Traces are normally accessed through a
// context, and may be stored in an e.g.
// [github.com/peterbourgon/trc/trcstore.Collector].
//
// The package provides a default implementation which is suitable for most use
// cases. Consumers can extend that default implementation with e.g. decorators,
// or provide their own implementation entirely. Implementations must be safe
// for concurrent use.
//
// Note that traces are created for every traced operation, but are accessed
// only when operators explicitly ask for them, for example when diagnosing a
// problem. Consequently, traces are written far more often than they are read.
// Implementations should keep this access pattern in mind, and optimize for
// writes rather than reads.
//
// Trace implementations may optionally implement SetMaxEvents(int), to allow
// callers to modify the maximum number of events that will be stored in the
// trace. This method, if it exists, is called by e.g. [SetMaxEvents].
//
// Trace implementations may optionally implement Free(), to release any
// resources claimed by the trace to an e.g. [sync.Pool]. This method, if it
// exists, is called by e.g. [github.com/peterbourgon/trc/trcstore.Collector],
// when a trace is dropped.
type Trace interface {
	// Source returns a human-readable string representing the origin of the
	// trace, which is typically the instance of the program where the trace was
	// constructed.
	Source() string

	// ID returns a unique identifier for the trace, which should be generated
	// by the trace constructor. By default, ID is a ULID.
	ID() string

	// Category returns the category of the trace, which should be provided by
	// the caller when the trace is created.
	Category() string

	// Started returns when the trace was created, preferably in UTC.
	Started() time.Time

	// Duration returns how long the trace is (or was) active, which is the time
	// between the started timestamp and when the trace was finished. If the
	// trace is still active, it returns the time since the started timestamp.
	Duration() time.Duration

	// Tracef adds a normal event to the trace, with the given format string and
	// args. Args are evaluated immediately.
	Tracef(format string, args ...any)

	// LazyTracef adds a normal event to the trace, with the given format string
	// and args. Args are evaulated lazily, when the event is read.
	LazyTracef(format string, args ...any)

	// Errorf adds an error event to the trace, with the given format string and
	// args. It marks the trace as errored. Args are evaluated immediately.
	Errorf(format string, args ...any)

	// LazyErrorf adds an error event to the trace, with the given format string
	// and args. It marks the trace as errored. Args are evaluated lazily, when
	// the event is read.
	LazyErrorf(format string, args ...any)

	// Finish marks the trace as finished. Once finished, a trace is "frozen",
	// and any method that would modify the trace becomes a no-op.
	Finish()

	// Finished returns true if Finish has been called.
	Finished() bool

	// Errored returns true if Errorf or LazyErrorf has been called.
	Errored() bool

	// Events returns all of the events collected from calls to e.g. Tracef.
	Events() []Event
}

// Event is a traced event, similar to a log event, which is created in the
// context of a specific trace, via methods like Tracef.
type Event struct {
	When    time.Time
	What    string
	Stack   []Frame
	IsError bool
}

// Frame represents a single call in a call stack.
type Frame struct {
	Function string
	FileLine string
}
