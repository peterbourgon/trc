package trchttp

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/peterbourgon/trc"
)

type logData struct {
	// Overview
	Stats trc.LogStats

	// Detail
	Filter   trc.LogFilter
	Limit    int
	Matched  int
	Selected trc.Logs
	Problems []string
}

// HandleLogs returns an HTTP handler that renders a basic representation of the
// logs in the given collector. It defaults to JSON but also supports HTML.
func HandleLogs(c *trc.Collector) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var (
			ctx      = r.Context()
			tr       = trc.FromContext(ctx)
			query    = r.URL.Query()
			category = query.Get("category")
			q        = query.Get("q")
			n        = parseDefault(query.Get("n"), strconv.Atoi, 10)
			problems = []string{}
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

		stats := c.LogStats()

		f := trc.LogFilter{
			Category: category,
			Query:    q,
			Regexp:   re,
		}

		selected, matched := c.LogQuery(f, n)

		tr.Tracef("matched count %d", matched)
		tr.Tracef("selected count %d", len(selected))

		data := logData{
			Stats:    stats,
			Filter:   f,
			Limit:    n,
			Matched:  matched,
			Selected: selected,
			Problems: problems,
		}

		switch getBestContentType(r) {
		case "text/html":
			renderHTML(ctx, w, "logs.html", data)
		default:
			renderJSON(ctx, w, data)
		}
	})
}
