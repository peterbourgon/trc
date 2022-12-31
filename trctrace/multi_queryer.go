package trctrace

import (
	"context"
	"fmt"
)

type MultiQueryer []OriginQueryer

func (m MultiQueryer) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	type tuple struct {
		res *QueryResponse
		err error
	}

	tuplec := make(chan tuple, len(m))

	for _, oq := range m {
		go func(oq OriginQueryer) {
			res, err := oq.Query(ctx, req)
			res.Origins = []string{oq.Origin()}
			tuplec <- tuple{res, err}
		}(oq)
	}

	res := NewQueryResponse(req, nil)
	for i := 0; i < cap(tuplec); i++ {
		t := <-tuplec
		if t.err != nil {
			return nil, fmt.Errorf("query error: %w", t.err) // TODO: fail fast OK?
		}
		if err := res.Merge(t.res); err != nil {
			return nil, fmt.Errorf("merge response: %w", err)
		}
	}

	res.Finalize()

	return res, nil
}
