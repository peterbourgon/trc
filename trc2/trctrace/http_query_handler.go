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

func NewHTTPQueryHandler(q Queryer) http.Handler {
	return NewHTTPQueryHandlerFor(q, nil)
}

func NewHTTPQueryHandlerFor(defaultOrigin Queryer, availableOrigins map[string]Queryer) http.Handler {
	var origins []string
	for name := range availableOrigins {
		origins = append(origins, name)
	}
	sort.Strings(origins)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, tr, finish := trc.Region(r.Context(), "QueryHandler")
		defer finish()

		var (
			begin     = time.Now()
			origin    = r.URL.Query().Get("origin")
			pageTitle = "trc"
			problems  = []string{}
		)

		var q Queryer
		{
			queryerForOrigin, validOrigin := availableOrigins[origin]
			switch {
			case origin == "":
				tr.Tracef("no explicit origin given, using default queryer")
				q = defaultOrigin
			case origin != "" && validOrigin:
				tr.Tracef("valid origin %q, querying that one", origin)
				q = queryerForOrigin
				pageTitle = fmt.Sprintf("trc - %s", origin)
			case origin != "" && !validOrigin:
				err := fmt.Errorf("invalid origin %q, using default queryer", origin)
				problems = append(problems, err.Error())
				tr.Errorf(err.Error())
				q = defaultOrigin
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

		trchttp.Render(ctx, w, r, assets, "traces.html", templateFuncs, &HTTPQueryData{
			PageTitle:        pageTitle,
			AvailableOrigins: origins,
			ResponseOrigin:   origin,
			Request:          req,
			Response:         res,
		})
	})
}
