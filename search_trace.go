package trc

import (
	"encoding/json"
	"time"
)

type SearchTrace struct {
	Source   string        `json:"source"`
	ID       string        `json:"id"`
	Category string        `json:"category"`
	Started  time.Time     `json:"started"`
	Duration time.Duration `json:"duration"`
	Finished bool          `json:"finished"`
	Errored  bool          `json:"errored"`
	Events   []Event       `json:"events"`
}

func NewSearchTrace(tr Trace) *SearchTrace {
	return &SearchTrace{
		Source:   tr.Source(),
		ID:       tr.ID(),
		Category: tr.Category(),
		Started:  tr.Started(),
		Duration: tr.Duration(),
		Finished: tr.Finished(),
		Errored:  tr.Errored(),
		Events:   tr.Events(),
	}
}

func (st *SearchTrace) TrimStacks(depth int) *SearchTrace {
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

func (st *SearchTrace) Dump() string {
	buf, _ := json.MarshalIndent(st, "", "    ")
	return string(buf)
}
