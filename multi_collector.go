package trc

import "context"

type TraceQueryer interface {
	TraceQuery(ctx context.Context, req *TraceQueryRequest) (*TraceQueryResponse, error)
}

/*
type MultiCollector []TraceQueryer

func (mc MultiCollector) TraceQuery(ctx context.Context, req *TraceQueryRequest) (*TraceQueryResponse, error) {
	res := &TraceQueryResponse{
		Request: req,
		Stats:   newTraceQueryStats(req, []Trace{}),
	}

	for _, tq := range mc {
		qres, qerr := tq.TraceQuery(ctx, req)
		switch {
		case qerr != nil:
			res.Problems = append(res.Problems, qerr.Error())
		case qerr == nil:
			res.Matched += qres.Matched
			res.Selected = append(res.Selected, qres.Selected...)
			res.Problems = append(res.Problems, qres.Problems...)
			if merr := mergeTraceQueryStats(res.Stats, qres.Stats); merr != nil {
				res.Problems = append(res.Problems, merr.Error())
			}
		}
	}

	return res, nil
}
*/
