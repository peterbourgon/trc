package trchttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
)

type TraceQueryer interface {
	TraceQuery(ctx context.Context, req *trc.TraceQueryRequest) (*trc.TraceQueryResponse, error)
}

func TraceCollectorHandler(tq TraceQueryer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var (
			ctx       = r.Context()
			tr        = trc.FromContext(ctx)
			query     = r.URL.Query()
			n         = parseDefault(query.Get("n"), strconv.Atoi, 10)
			min       = parseDefault(query.Get("min"), time.ParseDuration, 0)
			bucketing = parseBucketing(query["b"])
			q         = query.Get("q")
			remotes   = query["r"]
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

		if ct := r.Header.Get("content-type"); strings.Contains(ct, "application/json") {
			tr.Tracef("parsing request body as JSON")
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				err = fmt.Errorf("parse JSON request from body: %w", err)
				problems = append(problems, err.Error())
				tr.Errorf(err.Error())
			}
		}

		queryer := tq
		if len(remotes) > 0 {
			queryer = trc.NewDistributedTraceCollector(http.DefaultClient, remotes...)
		}

		tr.Tracef("querying")

		res, err := queryer.TraceQuery(ctx, req)
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

type TraceCollectorClient struct {
	Transport http.RoundTripper
	Source    string
}

func (c TraceCollectorClient) TraceQuery(ctx context.Context, req *trc.TraceQueryRequest) (*trc.TraceQueryResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", "irrelevant?json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")

	httpRes, err := c.Transport.RoundTrip(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		io.Copy(io.Discard, httpRes.Body)
		httpRes.Body.Close()
	}()

	var res trc.TraceQueryResponse
	if err := json.NewDecoder(httpRes.Body).Decode(&res); err != nil {
		return nil, err
	}

	for _, tr := range res.Selected {
		tr.StaticID = c.Source + "-" + tr.StaticID
	}

	return &res, nil
}
