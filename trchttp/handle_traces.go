package trchttp

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/peterbourgon/trc"
)

type traceData struct {
	// Overview
	Stats trc.TraceStats

	// Detail
	Filter   trc.TraceFilter
	Limit    int
	Matched  int
	Selected trc.Traces
	Problems []string
}

// HandleTraces returns an HTTP handler that renders a basic representation of the
// traces in the given collector. It defaults to JSON but also supports HTML.
func HandleTraces(c *trc.Collector) http.Handler {
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

		stats := c.TraceStats(bucketing)

		f := trc.TraceFilter{
			IDs:         query["id"],
			Category:    query.Get("category"),
			Active:      query.Has("active"),
			Finished:    query.Has("finished"),
			Succeeded:   query.Has("succeeded"),
			Errored:     query.Has("errored"),
			MinDuration: ifThenElse(query.Has("min"), &min, nil),
			Query:       q,
			Regexp:      re,
		}

		f.Category, _ = url.QueryUnescape(f.Category)
		f.Category, _ = url.PathUnescape(f.Category)

		selected, matched, err := c.TraceQuery(f, n)
		if err != nil {
			tr.Errorf("select traces: %v", err)
			problems = append(problems, err.Error())
		}

		tr.Tracef("matched count %d", matched)
		tr.Tracef("selected count %d", len(selected))

		data := &traceData{
			Stats:    stats,
			Filter:   f,
			Limit:    n,
			Matched:  matched,
			Selected: selected,
			Problems: problems,
		}

		switch getBestContentType(r) {
		case "text/html":
			renderHTML(ctx, w, "traces.html", data)
		default:
			renderJSON(ctx, w, data)
		}
	})
}
