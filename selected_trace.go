package trc

type SelectedTrace struct {
	// Via records the source(s) of the trace, which is useful when aggregating
	// traces from multiple collectors into a single result.
	Via []string `json:"via,omitempty"`

	*StaticTrace
}

var _ Trace = (*SelectedTrace)(nil)

func NewSelectedTrace(tr Trace) *SelectedTrace {
	return &SelectedTrace{
		StaticTrace: NewStaticTrace(tr),
	}
}
