package trcweb

import (
	"net/http"

	"github.com/peterbourgon/trc"
)

type TraceServer struct {
	search http.Handler
	stream http.Handler
}

func NewTraceServer(searcher trc.Searcher, streamer Streamer) *TraceServer {
	return &TraceServer{
		search: NewSearchServer(searcher),
		stream: NewStreamServer(streamer),
	}
}

func TraceServerCategory(r *http.Request) string {
	switch {
	case requestExplicitlyAccepts(r, "text/event-stream"):
		return "stream"
	default:
		return "traces"
	}
}

func (s *TraceServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case requestExplicitlyAccepts(r, "text/event-stream"):
		s.stream.ServeHTTP(w, r)
	default:
		s.search.ServeHTTP(w, r)
	}
}
