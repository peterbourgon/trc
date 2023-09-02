package trc

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Filter is a set of rules that can be applied to an individual trace, which
// will either be allowed (pass) or rejected (fail).
type Filter struct {
	Sources     []string       `json:"sources,omitempty"`
	IDs         []string       `json:"ids,omitempty"`
	Category    string         `json:"category,omitempty"`
	IsActive    bool           `json:"is_active,omitempty"`
	IsFinished  bool           `json:"is_finished,omitempty"`
	MinDuration *time.Duration `json:"min_duration,omitempty"`
	IsSuccess   bool           `json:"is_success,omitempty"`
	IsErrored   bool           `json:"is_errored,omitempty"`
	Query       string         `json:"query,omitempty"`
	regexp      *regexp.Regexp
}

// Normalize must be called before the filter can be used.
func (f *Filter) Normalize() []error {
	var errs []error

	if err := f.initializeQueryRegexp(); err != nil {
		errs = append(errs, fmt.Errorf("query: %w", err))
	}

	return errs
}

// String returns an operator-readable representation of the filter.
func (f Filter) String() string {
	var elems []string

	if len(f.Sources) > 0 {
		elems = append(elems, fmt.Sprintf("Sources=%v", f.Sources))
	}

	if len(f.IDs) > 0 {
		elems = append(elems, fmt.Sprintf("IDs=%v", f.Sources))
	}

	if f.Category != "" {
		elems = append(elems, fmt.Sprintf("Category='%s'", f.Category))
	}

	if f.IsActive {
		elems = append(elems, "IsActive")
	}

	if f.IsFinished {
		elems = append(elems, "IsFinished")
	}

	if f.MinDuration != nil {
		elems = append(elems, fmt.Sprintf("MinDuration=%s", f.MinDuration.String()))
	}

	if f.IsSuccess {
		elems = append(elems, "IsSuccess")
	}

	if f.IsErrored {
		elems = append(elems, "IsErrored")
	}

	if f.Query != "" {
		elems = append(elems, fmt.Sprintf("Query='%s'", f.Query))
	}

	if len(elems) <= 0 {
		return "(allow all)"
	}

	return strings.Join(elems, " ")
}

// Allow returns true if the provided trace satisfies all of the conditions in
// the filter.
func (f *Filter) Allow(tr Trace) bool {
	if len(f.Sources) > 0 {
		var found bool
		for _, source := range f.Sources {
			if source == tr.Source() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(f.IDs) > 0 {
		var found bool
		for _, id := range f.IDs {
			if id == tr.ID() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if f.Category != "" {
		if tr.Category() != f.Category {
			return false
		}
	}

	if f.IsActive {
		if tr.Finished() {
			return false
		}
	}

	if f.IsFinished {
		if !tr.Finished() {
			return false
		}
	}

	if f.MinDuration != nil {
		if !tr.Finished() {
			return false // we assert MinDuration requires the trace to be finished
		}
		if tr.Duration() < *f.MinDuration {
			return false
		}
	}

	if f.IsSuccess {
		if tr.Errored() {
			return false
		}
	}

	if f.IsErrored {
		if !tr.Errored() {
			return false
		}
	}

	f.initializeQueryRegexp()
	if f.regexp != nil {
		for _, ev := range tr.Events() {
			if f.regexp.MatchString(ev.What) {
				return true
			}
			for _, c := range ev.Stack {
				if f.regexp.MatchString(c.Function) {
					return true
				}
				if f.regexp.MatchString(c.CompactFileLine()) {
					return true
				}
			}
		}
		return false
	}

	return true
}

func (f *Filter) initializeQueryRegexp() error {
	if f.regexp != nil {
		return nil
	}

	if f.Query == "" {
		return nil
	}

	re, err := regexp.Compile(f.Query)
	if err != nil {
		f.Query = ""
		return fmt.Errorf("invalid, ignoring (%w)", err)

	}

	f.regexp = re
	return nil
}
