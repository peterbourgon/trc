package trc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	type tuple struct {
		uri string
		res *TraceQueryResponse
		err error
	}

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

			tuplec <- tuple{uri, &res, nil}
		}(uri)
	}

	aggregate := &TraceQueryResponse{
		Request: tqr,
		Stats:   newTraceQueryStats(tqr, nil),
	}

	for i := 0; i < cap(tuplec); i++ {
		t := <-tuplec

		if t.err == nil {
			log.Printf("### %+v", t.res)
			log.Printf("### stats: %+v", t.res.Stats)
			t.err = mergeTraceQueryResponse(aggregate, t.res)
		}

		if t.err != nil {
			aggregate.Problems = append(aggregate.Problems, fmt.Sprintf("%s: %v", t.uri, t.err))
		}
	}

	return aggregate, nil
}
