package trchttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/peterbourgon/trc"
)

type DistributedQueryer struct {
	client HTTPClient
	uris   []string
}

var _ TraceQueryer = (*DistributedQueryer)(nil)

// TODO: origin/remote type with both URI and name?
func NewDistributedQueryer(c HTTPClient, uris ...string) *DistributedQueryer {
	return &DistributedQueryer{
		client: c,
		uris:   uris,
	}
}

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

func (tc *DistributedQueryer) QueryTraces(ctx context.Context, tqr *trc.QueryTracesRequest) (*trc.QueryTracesResponse, error) {
	tr := trc.PrefixTracef(trc.FromContext(ctx), "[dist]")

	type tuple struct {
		uri string
		res *trc.QueryTracesResponse
		err error
	}

	// Scatter a query request to each URI.
	begin := time.Now()
	tuplec := make(chan tuple, len(tc.uris))
	for _, uri := range tc.uris {
		go func(uri string) {
			body, err := json.Marshal(tqr)
			if err != nil {
				tuplec <- tuple{uri, nil, fmt.Errorf("marshal query: %w", err)}
				return
			}

			req, err := http.NewRequestWithContext(ctx, "GET", uri, bytes.NewReader(body))
			if err != nil {
				tuplec <- tuple{uri, nil, fmt.Errorf("create request: %w", err)}
				return
			}
			req.Header.Set("content-type", "application/json")

			resp, err := tc.client.Do(req)
			if err != nil {
				tuplec <- tuple{uri, nil, fmt.Errorf("execute request: %w", err)}
				return
			}
			defer resp.Body.Close()

			var res trc.QueryTracesResponse
			if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
				tuplec <- tuple{uri, nil, fmt.Errorf("decode response: %w", err)}
				return
			}

			// TODO: different types for QueryResponse, aggregate responses, template data?
			res.Origins = []string{uri}
			for _, tr := range res.Selected {
				tr.Origin = uri
			}
			res.Duration = time.Since(begin)

			tuplec <- tuple{uri, &res, nil}
		}(uri)
	}

	// We'll merge responses into a single aggregate response.
	aggregate := trc.NewQueryTracesResponse(tqr, nil)
	for i := 0; i < cap(tuplec); i++ {
		t := <-tuplec

		if t.err != nil {
			tr.Tracef("%s: query error: %v", t.uri, t.err)
			aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("%s: %v", t.uri, t.err))
			continue
		}

		if err := aggregate.Merge(t.res); err != nil {
			tr.Tracef("%s: merge error: matched %d, selected %d, error %v", t.uri, t.res.Matched, len(t.res.Selected), err)
			aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("%s: %v", t.uri, t.err))
			continue
		}

		tr.Tracef("%s: OK: matched %d, selected %d", t.uri, t.res.Matched, len(t.res.Selected))
	}

	// The selected traces aren't sorted, and may be too many.
	sort.Slice(aggregate.Selected, func(i, j int) bool {
		return aggregate.Selected[i].Start().After(aggregate.Selected[j].Start())
	})

	if len(aggregate.Selected) > tqr.Limit {
		aggregate.Selected = aggregate.Selected[:tqr.Limit]
	}

	return aggregate, nil
}

func (tc *DistributedQueryer) Subscribe(ctx context.Context, ch chan<- trc.Trace) error {
	return fmt.Errorf("not implemented")
}

func (tc *DistributedQueryer) Unsubscribe(ctx context.Context, ch chan<- trc.Trace) (uint64, uint64, error) {
	return 0, 0, fmt.Errorf("not implemented")
}

func (tc *DistributedQueryer) Subscription(ctx context.Context, ch chan<- trc.Trace) (uint64, uint64, error) {
	return 0, 0, fmt.Errorf("not implemented")
}
