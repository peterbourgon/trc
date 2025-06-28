package trc

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/peterbourgon/trc/internal/trcutil"
)

// Searcher models anything that can serve search requests.
type Searcher interface {
	Search(context.Context, *SearchRequest) (*SearchResponse, error)
}

//
//
//

// SearchRequest describes a complete search request.
type SearchRequest struct {
	Bucketing  []time.Duration `json:"bucketing,omitempty"`
	Filter     Filter          `json:"filter,omitempty"`
	Limit      int             `json:"limit,omitempty"`
	StackDepth int             `json:"stack_depth,omitempty"` // 0 is default stacks, -1 for no stacks
}

// Normalize ensures the search request is valid, modifying it if necessary. It
// returns any errors encountered in the process.
func (req *SearchRequest) Normalize() []error {
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
		req.Limit = SearchLimitDefault
	case req.Limit < SearchLimitMin:
		req.Limit = SearchLimitMin
	case req.Limit > SearchLimitMax:
		req.Limit = SearchLimitMax
	}

	return errs
}

// String implements fmt.Stringer.
func (req SearchRequest) String() string {
	var elems []string

	if !reflect.DeepEqual(req.Bucketing, DefaultBucketing) {
		buckets := make([]string, len(req.Bucketing))
		for i := range req.Bucketing {
			buckets[i] = req.Bucketing[i].String()
		}
		elems = append(elems, fmt.Sprintf("Bucketing:[%s]", strings.Join(buckets, " ")))
	}

	elems = append(elems, fmt.Sprintf("Filter:[%s]", req.Filter))

	elems = append(elems, fmt.Sprintf("Limit:%d", req.Limit))

	if req.StackDepth != 0 {
		elems = append(elems, fmt.Sprintf("StackDepth:%d", req.StackDepth))
	}

	return strings.Join(elems, " ")
}

const (
	// SearchLimitMin is the minimum search limit.
	SearchLimitMin = 1

	// SearchLimitDefault is the default search limit.
	SearchLimitDefault = 10

	// SearchLimitMax is the maximum search limit.
	SearchLimitMax = 250
)

// DefaultBucketing is the default set of time buckets used in search stats.
var DefaultBucketing = []time.Duration{
	0,
	100 * time.Microsecond,
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

// SearchResponse returned by a search request.
type SearchResponse struct {
	Request      *SearchRequest `json:"request,omitempty"`
	Sources      []string       `json:"sources"`
	TotalCount   int            `json:"total_count"`
	MatchCount   int            `json:"match_count"`
	MatchSources []string       `json:"match_sources"`
	Traces       []*StaticTrace `json:"traces"`
	Stats        *SearchStats   `json:"stats,omitempty"`
	Problems     []string       `json:"problems,omitempty"`
	Duration     time.Duration  `json:"duration"`
}

//
//
//

// MultiSearcher allows multiple searchers to be searched as one.
type MultiSearcher []Searcher

var _ Searcher = (MultiSearcher)(nil)

// Search scatters the request over the searchers, gathers responses, and merges
// them into a single response returned to the caller.
func (ms MultiSearcher) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	var (
		begin         = time.Now()
		tr            = Get(ctx)
		normalizeErrs = req.Normalize()
	)

	type tuple struct {
		id  string
		res *SearchResponse
		err error
	}

	// Scatter.
	tuplec := make(chan tuple, len(ms))
	for i, s := range ms {
		go func(id string, s Searcher) {
			ctx, _ := Prefix(ctx, "<%s>", id)
			res, err := s.Search(ctx, req)
			tuplec <- tuple{id, res, err}
		}(strconv.Itoa(i+1), s)
	}
	tr.Tracef("scattered request count %d", len(ms))

	// We'll collect responses into this aggregate value.
	aggregate := &SearchResponse{
		Request:  req,
		Stats:    NewSearchStats(req.Bucketing),
		Problems: trcutil.FlattenErrors(normalizeErrs...),
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
			aggregate.MatchSources = append(aggregate.MatchSources, t.res.MatchSources...)
			aggregate.Traces = append(aggregate.Traces, t.res.Traces...) // needs sort+limit
			aggregate.Problems = append(aggregate.Problems, t.res.Problems...)
		case t.res != nil && t.err != nil: // weird
			tr.Tracef("%s: weird: valid result (accepting it) with error: %v", t.id, t.err)
			aggregate.Stats.Merge(t.res.Stats)
			aggregate.Sources = append(aggregate.Sources, t.res.Sources...)
			aggregate.TotalCount += t.res.TotalCount
			aggregate.MatchCount += t.res.MatchCount
			aggregate.MatchSources = append(aggregate.MatchSources, t.res.MatchSources...)
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
	sort.Sort(staticTracesNewestFirst(aggregate.Traces))
	if len(aggregate.Traces) > req.Limit {
		aggregate.Traces = aggregate.Traces[:req.Limit]
	}

	tr.Tracef("total %d, matched %d, returned %d", aggregate.TotalCount, aggregate.MatchCount, len(aggregate.Traces))

	// Fix up the sources.
	{
		sourceIndex := make(map[string]struct{}, len(aggregate.Sources))
		for _, source := range aggregate.Sources {
			sourceIndex[source] = struct{}{}
		}
		sourceList := make([]string, 0, len(sourceIndex))
		for source := range sourceIndex {
			sourceList = append(sourceList, source)
		}
		sort.Strings(sourceList)
		aggregate.Sources = sourceList
	}
	{
		sourceIndex := make(map[string]struct{}, len(aggregate.MatchSources))
		for _, source := range aggregate.MatchSources {
			sourceIndex[source] = struct{}{}
		}
		sourceList := make([]string, 0, len(sourceIndex))
		for source := range sourceIndex {
			sourceList = append(sourceList, source)
		}
		sort.Strings(sourceList)
		aggregate.MatchSources = sourceList
	}

	// Duration is defined across all individual requests.
	aggregate.Duration = time.Since(begin)

	// That should be it.
	return aggregate, nil
}
