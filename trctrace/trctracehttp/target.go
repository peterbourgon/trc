package trctracehttp

import "github.com/peterbourgon/trc/trctrace"

// Target is a named searcher which users can query.
type Target struct {
	name     string
	searcher trctrace.Searcher
}

func NewTarget(name string, searcher trctrace.Searcher) *Target {
	return &Target{
		name:     name,
		searcher: searcher,
	}
}
