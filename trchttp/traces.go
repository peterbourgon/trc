package trchttp

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/peterbourgon/trc"
)

func TraceCollectorHandler(c *trc.TraceCollector) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var (
			ctx       = r.Context()
			tr        = trc.FromContext(ctx)
			query     = r.URL.Query()
			n         = parseDefault(query.Get("n"), strconv.Atoi, 10)
			min       = parseDefault(query.Get("min"), time.ParseDuration, 0)
			bucketing = parseBucketing(query["b"])
			q         = query.Get("q")
			problems  = []string{}
		)

		var re *regexp.Regexp
		if q != "" {
			rr, err := regexp.Compile(q)
			switch {
			case err == nil:
				re = rr
			case err != nil:
				problems = append(problems, fmt.Sprintf("bad query: %v", err))
			}
		}

		req := &trc.TraceQueryRequest{
			Bucketing:   bucketing,
			Limit:       n,
			IDs:         query["id"],
			Category:    query.Get("category"),
			IsActive:    query.Has("active"),
			IsFinished:  query.Has("finished"),
			IsSucceeded: query.Has("succeeded"),
			IsErrored:   query.Has("errored"),
			MinDuration: ifThenElse(query.Has("min"), &min, nil),
			Search:      re,
		}

		tr.Tracef("querying")

		res, err := c.TraceQuery(ctx, req)
		if err != nil {
			tr.Errorf("TraceQuery: %v", err)
			problems = append(problems, err.Error())
		}

		tr.Tracef("matched %d, selected %d", res.Matched, len(res.Selected))

		res.Problems = append(problems, res.Problems...)

		switch getBestContentType(r) {
		case "text/html":
			renderHTML(ctx, w, "traces2.html", res)
		default:
			renderJSON(ctx, w, res)
		}
	})
}
