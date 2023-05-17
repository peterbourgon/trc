// Package trc provides in-process request tracing, an efficient alternative to
// logging. The package is inspired by https://golang.org/x/net/trace, much
// gratitude to those authors.
//
// The basic idea is that your application should "log" not by writing to a
// destination like stdout or a file on disk, but instead by adding events to a
// value it retrieves from the context, known as a [Trace].
//
// Traces are created for each operation performed by your application, e.g.
// every incoming HTTP request. Each trace is assigned a semantically meaningful
// category, and injected into the downstream context. Traces are also typicall
// written to a per-category ring buffer, which, in aggregate, represent the
// most recent "log" data from the application.
//
// The traces collected in those ring buffers are exposed via an HTTP interface,
// which operators can query. That interface is fairly rich, allowing traces to
// be selected by category, minimum duration, successful vs. errored, and so on.
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
