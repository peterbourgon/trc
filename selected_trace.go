package trc

import (
	"encoding/json"
	"time"
)

type SelectedTrace struct {
	Source   string        `json:"source"`
	ID       string        `json:"id"`
	Category string        `json:"category"`
	Started  time.Time     `json:"started"`
	Finished bool          `json:"finished"`
	Errored  bool          `json:"errored"`
	Duration time.Duration `json:"duration"`
	Events   []Event       `json:"events"`
}

func NewSelectedTrace(tr Trace) *SelectedTrace {
	return &SelectedTrace{
		Source:   tr.Source(),
		ID:       tr.ID(),
		Category: tr.Category(),
		Started:  tr.Started(),
		Finished: tr.Finished(),
		Errored:  tr.Errored(),
		Duration: tr.Duration(),
		Events:   tr.Events(),
	}
}

func (st *SelectedTrace) TrimStacks(depth int) *SelectedTrace {
	if depth == 0 {
		return st // zero value (0) means don't do anything
	}
	if depth < 0 {
		depth = 0 // negative value means remove all stacks
	}
	for i, ev := range st.Events {
		if len(ev.Stack) > depth {
			st.Events[i].Stack = st.Events[i].Stack[:depth]
		}
	}
	return st
}

func (st *SelectedTrace) Dump() string {
	buf, _ := json.MarshalIndent(st, "", "    ")
	return string(buf)
}
