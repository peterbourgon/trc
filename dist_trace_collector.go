package trc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type DistributedTraceCollector struct {
	client HTTPClient
	uris   []string
}

func NewDistributedTraceCollector(c HTTPClient, uris ...string) *DistributedTraceCollector {
	return &DistributedTraceCollector{
		client: c,
		uris:   uris,
	}
}

func (tc *DistributedTraceCollector) TraceQuery(ctx context.Context, tqr *TraceQueryRequest) (*TraceQueryResponse, error) {
	tr := PrefixTracef(FromContext(ctx), "[dist]")

	type tuple struct {
		uri string
		res *TraceQueryResponse
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

			var res TraceQueryResponse
			if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
				tuplec <- tuple{uri, nil, fmt.Errorf("decode response: %w", err)}
				return
			}

			res.Origins = []string{uri}
			for _, tr := range res.Selected {
				tr.Origin = uri
			}
			res.Duration = time.Since(begin)

			tuplec <- tuple{uri, &res, nil}
		}(uri)
	}

	// We'll merge responses into a single aggregate response.
	aggregate := &TraceQueryResponse{
		Request: tqr,
		Stats:   newTraceQueryStats(tqr, nil),
	}

	for i := 0; i < cap(tuplec); i++ {
		t := <-tuplec

		if t.err != nil {
			tr.Tracef("%s: query error: %v", t.uri, t.err)
			aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("%s: %v", t.uri, t.err))
			continue
		}

		if err := mergeTraceQueryResponse(aggregate, t.res); err != nil {
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
