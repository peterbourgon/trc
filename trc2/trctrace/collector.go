package trctrace

import (
	"context"
	"fmt"
	"sort"
	"time"

	trc "github.com/peterbourgon/trc/trc2"
	trcds "github.com/peterbourgon/trc/trc2/internal/trcds"
)

type Collector struct {
	categories *trcds.RingBuffers[trc.Trace]
}

func NewCollector(maxPerCategory int) *Collector {
	return &Collector{
		categories: trcds.NewRingBuffers[trc.Trace](maxPerCategory),
	}
}

func (c *Collector) NewTrace(ctx context.Context, category string) (context.Context, trc.Trace) {
	if tr, ok := trc.MaybeFromContext(ctx); ok {
		tr.Tracef("(+ %s)", category)
		return ctx, tr
	}

	ctx, tr := trc.NewTrace(ctx, category)
	c.categories.GetOrCreate(category).Add(tr)
	return ctx, tr
}

func (c *Collector) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	_, tr, finish := trc.Region(ctx, "Collector Query")
	defer finish()

	if err := req.Sanitize(); err != nil {
		return nil, fmt.Errorf("sanitize request: %w", err)
	}

	begin := time.Now()

	var overall trc.Traces
	for cat, rb := range c.categories.GetAll() {
		if err := rb.Walk(func(tr trc.Trace) error {
			overall = append(overall, tr)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("gathering traces (%s): %w", cat, err)
		}
	}

	var allowed trc.Traces
	for _, tr := range overall {
		if req.Allow(tr) {
			allowed = append(allowed, tr)
		}
	}

	var (
		matched  = len(allowed)
		took     = time.Since(begin)
		perTrace = time.Duration(float64(took) / float64(len(overall)))
	)

	tr.Tracef("evaluated %d, matched %d, took %s, %s/trace", len(overall), matched, took, perTrace)

	stats := newQueryStats(req, overall)

	tr.Tracef("computed stats")

	sort.Sort(allowed)
	if len(allowed) > req.Limit {
		allowed = allowed[:req.Limit]
	}

	selected := make([]*trc.StaticTrace, len(allowed))
	for i := range allowed {
		selected[i] = trc.NewStaticTrace(allowed[i])
	}

	tr.Tracef("selected %d", len(selected))

	return &QueryResponse{
		Request:  req,
		Stats:    stats,
		Total:    len(overall),
		Matched:  matched,
		Selected: selected,
		Problems: nil,
	}, nil
}
