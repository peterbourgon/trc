package trctrace

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, tr, finish := trc.Region(r.Context(), "QueryHandler")
		defer finish()

		var (
			begin       = time.Now()
			urlquery    = r.URL.Query()
			limit       = trchttp.ParseDefault(urlquery.Get("n"), strconv.Atoi, 10)
			minDuration = trchttp.ParseDefault(urlquery.Get("min"), trchttp.ParseDurationPointer, nil)
			bucketing   = ParseBucketing(urlquery["b"])
			search      = urlquery.Get("q")
			problems    = []string{}
		)

		req := &QueryRequest{
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

		var (
			contentType  = r.Header.Get("content-type")
			tryParseBody = strings.Contains(contentType, "application/json") // TODO: content length, transfer encoding?
		)
		if tryParseBody {
			tr.Tracef("parsing query from request body")
			if err := json.NewDecoder(r.Body).Decode(req); err != nil {
				err = fmt.Errorf("parse query from request body: %w", err)
				problems = append(problems, err.Error())
				tr.Errorf(err.Error())
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

		trchttp.Render(ctx, w, r, assets, "traces.html", res)
	})
}
