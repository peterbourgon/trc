// Package trc provides in-process request tracing, an efficient alternative to
// logging. The package is inspired by https://golang.org/x/net/trace, much
// gratitude to those authors.
//
// The basic idea is to "log" to a value in the context -- known as a [Trace] --
// instead of a destination like stdout or a file. Each e.g. request creates a
// new trace with a specific category. That trace is injected to the request
// context, making it available to application code. It's also saved to a
// per-category ring buffer, which are all exposed over an HTTP interface. You
// then read your "logs" by querying those ring buffers by various dimensions,
// like category or duration.
//
// There are a few caveats. This approach works only for applications that
// reliably have access to a context value. Only recent traces are maintained,
// so historical data is not available. And traces are maintained in memory, so
// if a process crashes or restarts, historical data is by default lost.
//
// Even with these caveats, in-process request tracing often provides a much
// better user experience than typical logging. The value of application
// telemetry is often highly correlated to its age. A rich interface over only
// the most recent data can be surprisingly powerful.
//
// Most applications should not need to import this package directly, and should
// instead use [github.com/peterbourgon/trc/eztrc], which provides an
// easy-to-use API for common use cases.
package trc
