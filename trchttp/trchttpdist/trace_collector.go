package trchttpdist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type RemoteCollector struct {
	URI  string
	Name string // optional
}

func (rc *RemoteCollector) String() string {
	switch {
	case rc.Name == "":
		return rc.URI
	default:
		return rc.Name
	}
}

//
//
//

type TraceCollector struct {
	client  HTTPClient
	remotes []*RemoteCollector
}

var _ trchttp.TraceQueryer = (*TraceCollector)(nil)

func NewTraceCollector(client HTTPClient, remotes ...*RemoteCollector) *TraceCollector {
	return &TraceCollector{
		client:  client,
		remotes: remotes,
	}
}

func (c *TraceCollector) QueryTraces(ctx context.Context, qtreq *trc.QueryTracesRequest) (*trc.QueryTracesResponse, error) {
	tr := trc.PrefixTracef(trc.FromContext(ctx), "[dist]")

	type tuple struct {
		rc    *RemoteCollector
		qtres *trc.QueryTracesResponse
		err   error
	}

	// Scatter a query request to each remote.
	begin := time.Now()
	tuplec := make(chan tuple, len(c.remotes))
	for _, rc := range c.remotes {
		go func(rc *RemoteCollector) {
			body, err := json.Marshal(qtreq)
			if err != nil {
				tuplec <- tuple{rc, nil, fmt.Errorf("marshal query: %w", err)}
				return
			}

			req, err := http.NewRequestWithContext(ctx, "GET", rc.URI, bytes.NewReader(body))
			if err != nil {
				tuplec <- tuple{rc, nil, fmt.Errorf("create request: %w", err)}
				return
			}
			req.Header.Set("content-type", "application/json")

			resp, err := c.client.Do(req)
			if err != nil {
				tuplec <- tuple{rc, nil, fmt.Errorf("execute request: %w", err)}
				return
			}
			defer resp.Body.Close()

			var res trc.QueryTracesResponse
			if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
				tuplec <- tuple{rc, nil, fmt.Errorf("decode response: %w", err)}
				return
			}

			// TODO: different types for QueryResponse, aggregate responses, template data?
			res.Origins = []string{rc.String()}
			for _, tr := range res.Selected {
				tr.StaticURI = rc.URI
			}

			res.Duration = time.Since(begin)

			tuplec <- tuple{rc, &res, nil}
		}(rc)
	}

	// We'll merge responses into a single aggregate response.
	aggregate := trc.NewQueryTracesResponse(qtreq, nil)
	for i := 0; i < cap(tuplec); i++ {
		t := <-tuplec

		if t.err != nil {
			tr.Tracef("%s: query error: %v", t.rc, t.err)
			aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("%s: %v", t.rc, t.err))
			continue
		}

		if err := aggregate.Merge(t.qtres); err != nil {
			tr.Tracef("%s: merge error: matched %d, selected %d, error %v", t.rc, t.qtres.Matched, len(t.qtres.Selected), err)
			aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("%s: %v", t.rc, t.err))
			continue
		}

		tr.Tracef("%s: OK: matched %d, selected %d", t.rc, t.qtres.Matched, len(t.qtres.Selected))
	}

	// The selected traces aren't sorted, and may be too many.
	sort.Slice(aggregate.Selected, func(i, j int) bool {
		return aggregate.Selected[i].Start().After(aggregate.Selected[j].Start())
	})

	if len(aggregate.Selected) > qtreq.Limit {
		aggregate.Selected = aggregate.Selected[:qtreq.Limit]
	}

	return aggregate, nil
}

func (dc *TraceCollector) Subscribe(ctx context.Context, ch chan<- trc.Trace) error {
	return fmt.Errorf("not implemented")
}

func (dc *TraceCollector) Unsubscribe(ctx context.Context, ch chan<- trc.Trace) (uint64, uint64, error) {
	return 0, 0, fmt.Errorf("not implemented")
}

func (dc *TraceCollector) Subscription(ctx context.Context, ch chan<- trc.Trace) (uint64, uint64, error) {
	return 0, 0, fmt.Errorf("not implemented")
}
