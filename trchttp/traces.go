package trchttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
)

type Queryer interface {
	Query(ctx context.Context, req *trc.QueryRequest) (*trc.QueryResponse, error)
}

func TracesHandler(q Queryer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var (
			ctx         = r.Context()
			tr          = trc.FromContext(ctx)
			begin       = time.Now()
			urlquery    = r.URL.Query()
			limit       = parseDefault(urlquery.Get("n"), strconv.Atoi, 10)
			minDuration = parseDefault(urlquery.Get("min"), parseDurationPointer, nil)
			bucketing   = parseBucketing(urlquery["b"])
			search      = urlquery.Get("q")
			remotes     = urlquery["r"]
			problems    = []string{}
		)

		req := &trc.QueryRequest{
			Bucketing:   bucketing,
			Limit:       limit,
			IDs:         urlquery["id"],
			Category:    urlquery.Get("category"),
			IsActive:    urlquery.Has("active"),
			IsFinished:  urlquery.Has("finished"),
			IsSucceeded: urlquery.Has("succeeded"),
			IsErrored:   urlquery.Has("errored"),
			MinDuration: minDuration,
			Search:      search,
		}

		if ct := r.Header.Get("content-type"); strings.Contains(ct, "application/json") {
			tr.Tracef("parsing request body as JSON")
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				err = fmt.Errorf("parse JSON request from body: %w", err)
				problems = append(problems, err.Error())
				tr.Errorf(err.Error())
			}
		}

		if err := req.Sanitize(); err != nil {
			err = fmt.Errorf("sanitize request: %w", err)
			problems = append(problems, err.Error())
			tr.Errorf(err.Error())
		}

		queryer := q // default queryer
		if len(remotes) > 0 {
			tr.Tracef("remotes count %d, using explicit distributed trace collector")
			queryer = NewDistributedQueryer(http.DefaultClient, remotes...)
		}

		tr.Tracef("query starting: %s", req)

		res, err := queryer.Query(ctx, req)
		if err != nil {
			tr.Errorf("query errored: %v", err)
			res = trc.NewQueryResponse(req, nil)
			problems = append(problems, err.Error())
		}
		res.Duration = time.Since(begin)
		res.Problems = append(problems, res.Problems...)

		tr.Tracef("query finished: matched=%d selected=%d duration=%s", res.Matched, len(res.Selected), res.Duration)

		switch getBestContentType(r) {
		case "text/html":
			renderHTML(ctx, w, "traces.html", res)
		default:
			renderJSON(ctx, w, res)
		}
	})
}
