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

type TraceCollector interface {
	TraceQueryer
}

type TraceQueryer interface {
	QueryTraces(ctx context.Context, qtreq *trc.QueryTracesRequest) (*trc.QueryTracesResponse, error)
}

func NewTracesHandler(c TraceCollector) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tr := trc.FromContext(ctx)
		defer tr.Finish()

		var (
			begin       = time.Now()
			urlquery    = r.URL.Query()
			limit       = parseDefault(urlquery.Get("n"), strconv.Atoi, 10)
			minDuration = parseDefault(urlquery.Get("min"), parseDurationPointer, nil)
			bucketing   = parseBucketing(urlquery["b"])
			search      = urlquery.Get("q")
			problems    = []string{}
		)

		qtreq := &trc.QueryTracesRequest{
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
			if err := json.NewDecoder(r.Body).Decode(&qtreq); err != nil {
				err = fmt.Errorf("parse JSON request from body: %w", err)
				problems = append(problems, err.Error())
				tr.Errorf(err.Error())
			}
		}

		if err := qtreq.Sanitize(); err != nil {
			err = fmt.Errorf("sanitize request: %w", err)
			problems = append(problems, err.Error())
			tr.Errorf(err.Error())
		}

		tr.Tracef("query starting: %s", qtreq)

		qtres, err := c.QueryTraces(ctx, qtreq)
		if err != nil {
			tr.Errorf("query errored: %v", err)
			qtres = trc.NewQueryTracesResponse(qtreq, nil)
			problems = append(problems, err.Error())
		}
		qtres.Duration = time.Since(begin)
		qtres.Problems = append(problems, qtres.Problems...)

		tr.Tracef("query finished: matched=%d selected=%d duration=%s", qtres.Matched, len(qtres.Selected), qtres.Duration)

		switch {
		case requestExplicitlyAccepts(r, "text/html"):
			renderHTML(ctx, w, "traces.html", qtres)
		default:
			renderJSON(ctx, w, qtres)
		}
	})
}
