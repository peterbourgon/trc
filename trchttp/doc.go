// Package trchttp provides HTTP functionality for traces.
//
// Specifically, it provides an HTTP middleware that creates (and finishes) a
// trace for each incoming HTTP request. It also provides an HTTP server for
// searching traces, and an HTTP client for that server.
package trchttp
