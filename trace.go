package trc

import (
	"strings"
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
	// and args. Args are stored in their raw form and evaulated lazily, when
	// the event is first read. Callers should be very careful to ensure that
	// args passed to lazy-evaluated events will remain valid beyond the scope
	// of the call.
	LazyTracef(format string, args ...any)

	// Errorf adds an error event to the trace, with the given format string and
	// args. It marks the trace as errored. Args are evaluated immediately.
	Errorf(format string, args ...any)

	// LazyErrorf adds an error event to the trace, with the given format string
	// and args. It marks the trace as errored. Args are stored in their raw
	// form and evaulated lazily, when the event is first read. Callers should
	// be very careful to ensure that args passed to lazy-evaluated events will
	// remain valid beyond the scope of the call.
	LazyErrorf(format string, args ...any)

	// Finish marks the trace as finished. Once finished, a trace is "frozen",
	// and any method that would modify the trace becomes a no-op.
	Finish()

	// Finished returns true if Finish has been called.
	Finished() bool

	// Errored returns true if Errorf or LazyErrorf has been called.
	Errored() bool

	// Events returns all of the events collected by the trace, newest first.
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

// Frame is a single call frame in an event's call stack.
type Frame struct {
	Function string
	FileLine string
}

// CompactFileLine returns a more compact representation of the file and line.
func (fr Frame) CompactFileLine() string {
	file, line, _ := strings.Cut(fr.FileLine, ":")
	prefix, suffix := pkgPrefix(fr.Function), pathSuffix(file)
	if prefix == "" {
		file = suffix
	} else {
		file = prefix + "/" + suffix
	}
	return file + ":" + line
}

func pkgPrefix(funcName string) string {
	const pathSep = "/"
	end := strings.LastIndex(funcName, pathSep)
	if end == -1 {
		return ""
	}
	return funcName[:end]
}

func pathSuffix(path string) string {
	const pathSep = "/"
	lastSep := strings.LastIndex(path, pathSep)
	if lastSep == -1 {
		return path
	}
	return path[strings.LastIndex(path[:lastSep], pathSep)+1:]
}
