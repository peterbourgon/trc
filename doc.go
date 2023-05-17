// Package trc provides in-process request tracing, an efficient alternative to
// logging. The package is inspired by https://golang.org/x/net/trace, much
// gratitude to those authors.
//
// The basic idea is to "log" to a value in the context — known as a [Trace] —
// rather than a destination like stdout or a file on disk. Traces are created,
// assigned a category, and injected to the context for each e.g. request served
// by the application, making them available to user code.
//
// The most recent traces are typically maintained in-memory, usually grouped
// into per-category ring buffers. The complete set of traces, across all ring
// buffers, are exposed via an HTTP interface. Operators access the application
// "logs" by querying that HTTP interface. That interface is fairly rich,
// allowing users to select traces by category, minimum duration, successful vs.
// errored, and so on.
//
// There are a few caveats. This approach is only suitable for applications that
// do their work in the context of a trace-related operation, and which reliably
// have access to a context value. Only the most recent traces are maintained,
// so long term historical data is not available. And, becase traces are
// maintained in memory, if a process crashes or restarts, all previous data is
// by default lost.
//
// Even with these caveats, in-process request tracing often provides a better
// user experience for operators than traditional logging. The value of
// application telemetry is often highly correlated to its age. A rich interface
// over only the most recent data can be surprisingly powerful.
//
// Most applications should not import this package directly, and should instead
// use [github.com/peterbourgon/trc/eztrc], which provides an easy-to-use API
// for common use cases.
package trc
