package trcsrc

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
)

type Selecter interface {
	Select(context.Context, *SelectRequest) (*SelectResponse, error)
}

//
//
//

type SelectRequest struct {
	Bucketing []time.Duration `json:"bucketing"`
	Filter    Filter          `json:"filter"`
	Limit     int             `json:"limit"`
}

func (req *SelectRequest) Normalize() []error {
	var errs []error

	if len(req.Bucketing) <= 0 {
		req.Bucketing = DefaultBucketing
	}
	sort.Slice(req.Bucketing, func(i, j int) bool {
		return req.Bucketing[i] < req.Bucketing[j]
	})
	if req.Bucketing[0] != 0 {
		req.Bucketing = append([]time.Duration{0}, req.Bucketing...)
	}

	for _, err := range req.Filter.Normalize() {
		errs = append(errs, fmt.Errorf("filter: %w", err))
	}

	switch {
	case req.Limit <= 0:
		req.Limit = SelectRequestLimitDefault
	case req.Limit < SelectRequestLimitMin:
		req.Limit = SelectRequestLimitMin
	case req.Limit > SelectRequestLimitMax:
		req.Limit = SelectRequestLimitMax
	}

	return errs
}

func (req *SelectRequest) String() string {
	var elems []string

	if !reflect.DeepEqual(req.Bucketing, DefaultBucketing) {
		buckets := make([]string, len(req.Bucketing))
		for i := range req.Bucketing {
			buckets[i] = req.Bucketing[i].String()
		}
		elems = append(elems, fmt.Sprintf("Bucketing=[%s]", strings.Join(buckets, " ")))
	}

	elems = append(elems, fmt.Sprintf("Filter=%s", req.Filter.String()))

	elems = append(elems, fmt.Sprintf("Limit=%d", req.Limit))

	return fmt.Sprintf("[%s]", strings.Join(elems, " "))
}

const (
	SelectRequestLimitMin     = 1
	SelectRequestLimitDefault = 10
	SelectRequestLimitMax     = 250
)

var DefaultBucketing = []time.Duration{
	0 * time.Millisecond,
	1 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	1000 * time.Millisecond,
}

//
//
//

type SelectResponse struct {
	Request    *SelectRequest   `json:"request"`
	Sources    []string         `json:"sources"`
	TotalCount int              `json:"total_count"`
	MatchCount int              `json:"match_count"`
	Traces     []*SelectedTrace `json:"traces"`
	Stats      *SelectStats     `json:"stats"`
	Problems   []string         `json:"problems,omitempty"`
	Duration   time.Duration    `json:"duration"`
}

//
//
//

type SelectedTrace struct {
	Source   string        `json:"source"`
	ID       string        `json:"id"`
	Category string        `json:"category"`
	Started  time.Time     `json:"started"`
	Finished bool          `json:"finished"`
	Errored  bool          `json:"errored"`
	Duration time.Duration `json:"duration"`
	Events   []trc.Event   `json:"events"`
}

func NewSelectedTrace(tr trc.Trace) *SelectedTrace {
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

//
//
//

type MultiSelecter []Selecter

func (ms MultiSelecter) Select(ctx context.Context, req *SelectRequest) (*SelectResponse, error) {
	var (
		begin         = time.Now()
		tr            = trc.Get(ctx)
		normalizeErrs = req.Normalize()
	)

	type tuple struct {
		id  string
		res *SelectResponse
		err error
	}

	// Scatter.
	tuplec := make(chan tuple, len(ms))
	for i, s := range ms {
		go func(id string, s Selecter) {
			ctx, _ := trc.Prefix(ctx, "<%s>", id)
			res, err := s.Select(ctx, req)
			tuplec <- tuple{id, res, err}
		}(strconv.Itoa(i+1), s)
	}
	tr.Tracef("scattered request count %d", len(ms))

	// We'll collect responses into this aggregate value.
	aggregate := &SelectResponse{
		Request:  req,
		Stats:    NewSelectStats(req.Bucketing),
		Problems: flattenErrors(normalizeErrs...),
	}

	// Gather.
	for i := 0; i < cap(tuplec); i++ {
		t := <-tuplec
		switch {
		case t.res == nil && t.err == nil: // weird
			tr.Tracef("%s: weird: no result, no error", t.id)
			aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("%s: weird: empty response", t.id))
		case t.res == nil && t.err != nil: // error case
			tr.Tracef("%s: error: %v", t.id, t.err)
			aggregate.Problems = append(aggregate.Problems, t.err.Error())
		case t.res != nil && t.err == nil: // success case
			aggregate.Stats.Merge(t.res.Stats)
			aggregate.Sources = append(aggregate.Sources, t.res.Sources...)
			aggregate.TotalCount += t.res.TotalCount
			aggregate.MatchCount += t.res.MatchCount
			aggregate.Traces = append(aggregate.Traces, t.res.Traces...) // needs sort+limit
			aggregate.Problems = append(aggregate.Problems, t.res.Problems...)
		case t.res != nil && t.err != nil: // weird
			tr.Tracef("%s: weird: valid result (accepting it) with error: %v", t.id, t.err)
			aggregate.Stats.Merge(t.res.Stats)
			aggregate.Sources = append(aggregate.Sources, t.res.Sources...)
			aggregate.TotalCount += t.res.TotalCount
			aggregate.MatchCount += t.res.MatchCount
			aggregate.Traces = append(aggregate.Traces, t.res.Traces...) // needs sort+limit
			aggregate.Problems = append(aggregate.Problems, t.res.Problems...)
			aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("got valid search response with error (%v) -- weird", t.err))
		}
	}
	tr.Tracef("gathered responses")

	// At this point, the aggregate response has all of the raw data it's ever
	// gonna get. We need to do a little bit of post-processing. First, we need
	// to sort all of the selected traces by start time, and then limit them by
	// the request limit.
	sort.Slice(aggregate.Traces, func(i, j int) bool {
		return aggregate.Traces[i].Started.After(aggregate.Traces[j].Started)
	})
	if len(aggregate.Traces) > req.Limit {
		aggregate.Traces = aggregate.Traces[:req.Limit]
	}

	// Fix up the sources.
	sort.Strings(aggregate.Sources)

	// Duration is defined across all individual requests.
	aggregate.Duration = time.Since(begin)

	// That should be it.
	return aggregate, nil
}