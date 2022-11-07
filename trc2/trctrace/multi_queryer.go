package trctrace

import (
	"context"
	"fmt"
)

type MultiQueryer struct {
	set []Queryer
}

var _ Queryer = (*MultiQueryer)(nil)

func (m *MultiQueryer) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	type tuple struct {
		res *QueryResponse
		err error
	}

	tuplec := make(chan tuple, len(m.set))

	for _, q := range m.set {
		go func(q Queryer) {
			res, err := q.Query(ctx, req)
			tuplec <- tuple{res, err}
		}(q)
	}

	var res QueryResponse
	for i := 0; i < cap(tuplec); i++ {
		t := <-tuplec
		if t.err != nil {
			return nil, fmt.Errorf("query error: %w", t.err) // TODO: fail fast OK?
		}
		if err := res.Merge(t.res); err != nil {
			return nil, fmt.Errorf("merge response: %w", err)
		}
	}

	return &res, nil
}
