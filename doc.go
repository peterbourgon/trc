// Package trc provides in-process request tracing, an efficient alternative to
// logging. The package is inspired by https://golang.org/x/net/trace, much
// gratitude to those authors.
//
// The basic idea is that applications should log not by sending events to a
// destination like stdout or a file on disk, but instead by adding events to a
// value retrieved from the context, known as a [Trace].
//
// Traces are created for each operation processed by your application, e.g.
// every incoming HTTP request. Each trace is given a semantically meaningful
// category, and injected into the downstream context so applications can add
// events over the course of the operation. The trace is marked as finished when
// the operation completes.
//
// Traces are collected into per-category ring buffers, which are exposed via an
// HTTP interface that operators can query. That interface is fairly rich,
// allowing traces to be selected by category, minimum duration, successful vs.
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
// user experience than traditional logging. The value of application telemetry
// tends to be highly correlated to age. A rich interface over just the most
// recent data can be surprisingly powerful.
//
// Most applications should not import this package directly, and should instead
// use [github.com/peterbourgon/trc/eztrc], which provides an API specifically
// designed for common use cases.
package trc
