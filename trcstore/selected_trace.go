package trcstore

import (
	"github.com/peterbourgon/trc"
)

type SelectedTrace struct {
	// Via records the source(s) of the trace, which is useful when aggregating
	// traces from multiple collectors into a single result.
	Via []string `json:"via,omitempty"`

	*StaticTrace
}

var _ trc.Trace = (*SelectedTrace)(nil)

func NewSelectedTrace(tr trc.Trace) *SelectedTrace {
	return &SelectedTrace{
		StaticTrace: NewStaticTrace(tr),
	}
}
