package trc

import (
	"encoding/json"
	"regexp"
	"time"
)

// Log is an interface capturing metadata about an observed event. Unlike
// traces, logs are essentially independent from each other, and don't need to
// relate to a specific context.
//
// Implementations are expected to be safe for concurrent access.
type Log interface {
	// Category should return the user-supplied category of the log.
	Category() string

	// Event should return the immutable event represented by the log.
	Event() Event

	// MatchRegexp should return true if the log satisfies the given regexp.
	// Implementations are expected to match against the event What string
	// and stack trace details, at a minimum.
	MatchRegexp(*regexp.Regexp) bool
}

// Logs is an ordered collection of logs.
type Logs []Log

// CoreLog is the base implementation of the Log interface.
type CoreLog struct {
	category string
	event    Event
}

// NewCoreLog returns a CoreLog with the given category and event data.
func NewCoreLog(category string, ev Event) *CoreLog {
	return &CoreLog{
		category: category,
		event:    ev,
	}
}

// Category implements Log.
func (clg *CoreLog) Category() string {
	return clg.category
}

// Event implements Log.
func (clg *CoreLog) Event() Event {
	return clg.event
}

// MatchRegexp implements Log.
func (clg *CoreLog) MatchRegexp(r *regexp.Regexp) bool {
	return clg.event.MatchRegexp(r)
}

// MarshalJSON implements Log.
func (clg *CoreLog) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Category  string    `json:"category"`
		Timestamp time.Time `json:"timestamp"`
		Event     Event     `json:"event"`
	}{
		Category:  clg.category,
		Timestamp: clg.event.When,
		Event:     clg.event,
	})
}
