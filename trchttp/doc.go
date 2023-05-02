// Package trchttp provides HTTP functionality for traces.
//
// Specifically, it provides an HTTP server for searching traces, an HTTP client
// that acts a remote searcher by communicating with an instance of that server,
// and an HTTP middleware that will automatically create a trace for each
// incoming request.
package trchttp
