package trctrace

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
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
			begin    = time.Now()
			origin   = r.URL.Query().Get("origin")
			problems = []string{}
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

		tr.Tracef("query complete, considered %d, matched %d, selected %d, duration %s", res.Considered, res.Matched, len(res.Selected), res.Duration)

		trchttp.Render(ctx, w, r, assets, "traces.html", templateFuncs, HTTPQueryResponse{
			AvailableOrigins: origins,
			ResponseOrigin:   origin,
			Request:          req,
			Response:         res,
		})
	})
}

type HTTPQueryResponse struct {
	AvailableOrigins []string       `json:"available_origins"`
	ResponseOrigin   string         `json:"response_origin,omitempty"`
	Request          *QueryRequest  `json:"request"`
	Response         *QueryResponse `json:"response"`
}

var templateFuncs = template.FuncMap{
	"category2class":   category2class,
	"highlightclasses": highlightclasses,
}

func category2class(name string) string {
	return "category-" + sha256hex(name)
}

func highlightclasses(req *QueryRequest) []string {
	var classes []string
	if len(req.IDs) > 0 {
		return nil
	}
	if req.Category != "" {
		classes = append(classes, "category-"+sha256hex(req.Category))
	}
	if req.IsActive {
		classes = append(classes, "active")
	}
	if req.IsErrored {
		classes = append(classes, "errored")
	}
	if req.IsFinished {
		classes = append(classes, "finished")
	}
	if req.IsSucceeded {
		classes = append(classes, "succeeded")
	}
	if req.MinDuration != nil {
		classes = append(classes, "min-"+req.MinDuration.String())
	}
	return classes
}

func sha256hex(input string) string {
	h := sha256.Sum256([]byte(input))
	s := hex.EncodeToString(h[:])
	return s
}
