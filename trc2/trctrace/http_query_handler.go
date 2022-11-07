package trctrace

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"time"

	trc "github.com/peterbourgon/trc/trc2"
	"github.com/peterbourgon/trc/trc2/trchttp"
)

//go:embed assets/*
var assetsRoot embed.FS

var assets = func() fs.FS {
	assets, err := fs.Sub(assetsRoot, "assets")
	if err != nil {
		panic(err)
	}
	return assets
}()

const defaultQueryTarget = "default"

func NewHTTPQueryHandler(q Queryer) http.Handler {
	return NewHTTPQueryHandlerFor(q, nil)
}

func NewHTTPQueryHandlerFor(primary Queryer, alternative map[string]Queryer) http.Handler {
	var targets []string
	for name := range alternative {
		targets = append(targets, name)
	}
	sort.Strings(targets)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, tr, finish := trc.Region(r.Context(), "QueryHandler")
		defer finish()

		var (
			begin    = time.Now()
			target   = r.URL.Query().Get("target")
			problems = []string{}
		)

		q := primary
		if target != "" {
			altq, ok := alternative[target]
			if !ok {
				target = ""
				err := fmt.Errorf("invalid query target %q, using default", target)
				problems = append(problems, err.Error())
				tr.Errorf(err.Error())
			} else {
				tr.Tracef("using query target %s", target)
				q = altq
			}
		}

		req, err := ParseQueryRequest(r)
		if err != nil {
			problems = append(problems, err.Error())
			tr.Errorf("parse query request: %v", err)
			req = &QueryRequest{} // default
		}

		// TODO: content length, transfer encoding?
		if strings.Contains(r.Header.Get("content-type"), "application/json") {
			err := json.NewDecoder(r.Body).Decode(req)
			if err != nil {
				err = fmt.Errorf("parse query from request body: %w", err)
				problems = append(problems, err.Error())
				tr.Errorf("parse query from request body: %v", err.Error())
			} else {
				tr.Tracef("parsed query from request body")
			}
		}

		if err := req.Sanitize(); err != nil {
			err = fmt.Errorf("sanitize request: %w", err)
			problems = append(problems, err.Error())
			tr.Errorf(err.Error())
		}

		tr.Tracef("query start (%s)", req)

		res, err := q.Query(ctx, req)
		if err != nil {
			tr.Errorf("query failed, error %v", err)
			res = NewQueryResponse(req, nil)
			problems = append(problems, err.Error())
		}
		res.Duration = time.Since(begin)
		res.Problems = append(problems, res.Problems...)

		tr.Tracef("query complete, matched %d, selected %d, duration %s", res.Matched, len(res.Selected), res.Duration)

		trchttp.Render(ctx, w, r, assets, "traces.html", HTTPQueryResponse{
			Targets:  targets,
			Target:   target,
			Request:  req,
			Response: res,
		})
	})
}

type HTTPQueryResponse struct {
	Targets  []string       `json:"targets"`
	Target   string         `json:"target,omitempty"`
	Request  *QueryRequest  `json:"request"`
	Response *QueryResponse `json:"response"`
}
