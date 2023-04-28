// Package trchttp provides HTTP helpers for traces.
//
// Specifically, it provides an HTTP server for searching traces, an HTTP client
// that can communicate with that server, and an HTTP middleware that will
// automatically create a trace for each incoming request.
package trchttp
