package trctracehttp

import "github.com/peterbourgon/trc/trctrace"

type Target struct {
	Name     string
	Searcher trctrace.Searcher
}
