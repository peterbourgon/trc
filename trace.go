package trc

import (
	"strings"
	"time"
)

// Trace is a collection of metadata and events for a single operation,
// typically a request, in a program. Traces are normally accessed through a
// context, and maintained in a [Collector].
//
// [New] produces a default implementation of a Trace which is suitable for most
// use cases. Consumers can extend that implementation via [DecoratorFunc], or
// provide their own implementation entirely. Trace implementations must be safe
// for concurrent use.
//
// Note that traces are typically created for every operation, but are accessed
// only upon explicit request, for example when an operator is diagnosing a
// problem. Consequently, traces are written far more often than they are read.
// Implementations should keep this access pattern in mind, and optimize for
// writes rather than reads.
//
// Trace implementations may optionally implement SetMaxEvents(int), to allow
// callers to modify the maximum number of events that will be stored in the
// trace. This method, if it exists, is called by [SetMaxEvents].
//
// Trace implementations may optionally implement Free(), to release any
// resources claimed by the trace to an e.g. [sync.Pool]. This method, if it
// exists, is called by the [Collector] when a trace is dropped.
type Trace interface {
	// ID returns an identifier for the trace which should be automatically
	// generated during construction, and should be unique within a given
	// instance.
	ID() string

	// Source returns a human-readable string representing the origin of the
	// trace, which is typically the instance of the program where the trace was
	// constructed.
	Source() string

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
	// args passed to this method will remain valid indefinitely.
	LazyTracef(format string, args ...any)

	// Errorf adds an error event to the trace, with the given format string and
	// args. It marks the trace as errored. Args are evaluated immediately.
	Errorf(format string, args ...any)

	// LazyErrorf adds an error event to the trace, with the given format string
	// and args. It marks the trace as errored. Args are stored in their raw
	// form and evaulated lazily, when the event is first read. Callers should
	// be very careful to ensure that args passed to this method will remain
	// valid indefinitely.
	LazyErrorf(format string, args ...any)

	// Finish marks the trace as finished. Once finished, a trace is "frozen",
	// and any method that would modify the trace becomes a no-op.
	Finish()

	// Finished returns true if Finish has been called.
	Finished() bool

	// Errored returns true if Errorf or LazyErrorf has been called.
	Errored() bool

	// Events returns all of the events collected by the trace, oldest to
	// newest. Events are produced by Tracef, LazyTracef, Errorf, and
	// LazyErrorf.
	Events() []Event
}

// Event is a traced event, similar to a log event, which is created in the
// context of a specific trace, via methods like Tracef.
type Event struct {
	When    time.Time `json:"when"`
	What    string    `json:"what"`
	Stack   []Frame   `json:"stack,omitempty"`
	IsError bool      `json:"is_error,omitempty"`
}

// Frame is a single call frame in an event's call stack.
type Frame struct {
	Function string `json:"function"`
	FileLine string `json:"fileline"`
}

// CompactFileLine returns a human-readable representation of the file and line,
// intended to be used in user-facing interfaces.
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

// Freer can optionally be implemented by traces. It might be called by the
// collector when the trace is dropped from a ring buffer. It's meant as an
// optimization to e.g. return the trace to a pool.
type Freer interface {
	Free()
}
