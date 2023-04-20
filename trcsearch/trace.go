package trcsearch

import (
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcstatic"
)

type SelectedTrace struct {
	// Via records the source(s) of the trace, which is useful when aggregating
	// traces from multiple collectors into a single result.
	Via []string `json:"via,omitempty"`

	*trcstatic.StaticTrace
}

var _ trc.Trace = (*SelectedTrace)(nil)

func NewSelectedTrace(tr trc.Trace) *SelectedTrace {
	return &SelectedTrace{
		StaticTrace: trcstatic.NewStaticTrace(tr),
	}
}
