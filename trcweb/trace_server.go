package trcweb

import (
	"context"
	"net/http"

	"github.com/peterbourgon/trc"
)

// HTTPClient models an http.Client.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

var _ HTTPClient = (*http.Client)(nil)

// Searcher is just a trc.Searcher.
type Searcher trc.Searcher

// Streamer models the subscriber methods of a trc.Collector.
type Streamer interface {
	Stream(ctx context.Context, f trc.Filter, ch chan<- trc.Trace) (trc.StreamStats, error)
	StreamStats(ctx context.Context, ch chan<- trc.Trace) (trc.StreamStats, error)
}

// Categorize the request for a [Middleware].
func Categorize(r *http.Request) string {
	if requestExplicitlyAccepts(r, "text/event-stream") {
		return "stream"
	}
	return "traces"
}

// TraceServer provides an HTTP interface to trace data.
type TraceServer struct {
	// Collector is the default implementation for Searcher and Streamer.
	Collector *trc.Collector

	// Searcher is used to serve requests which Accept: text/html and/or
	// application/json. If not provided, the Collector will be used.
	Searcher Searcher

	// Streamer is used to serve requests which Accept: text/event-stream. If
	// not provided, the Collector will be used.
	Streamer Streamer

	searchServer SearchServer
	streamServer StreamServer
}

// NewTraceServer returns a standard trace server wrapping the collector.
func NewTraceServer(c *trc.Collector) *TraceServer {
	s := &TraceServer{
		Collector: c,
	}
	s.initialize()
	return s
}

func (s *TraceServer) initialize() {
	if s.Searcher == nil {
		s.Searcher = s.Collector
	}
	if s.Streamer == nil {
		s.Streamer = s.Collector
	}

	s.searchServer.Searcher = s.Searcher
	s.streamServer.Streamer = s.Streamer
}

// ServeHTTP implements http.Handler.
func (s *TraceServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.initialize()
	switch Categorize(r) {
	case "stream":
		s.streamServer.ServeHTTP(w, r)
	default:
		s.searchServer.ServeHTTP(w, r)
	}
}
