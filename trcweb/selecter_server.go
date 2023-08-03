package trcweb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/peterbourgon/trc"
)

type SelecterServer struct {
	sel trc.Selecter
}

func NewSelecterServer(sel trc.Selecter) *SelecterServer {
	return &SelecterServer{
		sel: sel,
	}
}

type SelectData struct {
	Request  trc.SelectRequest  `json:"request"`
	Response trc.SelectResponse `json:"response"`
	Problems []error            `json:"problems,omitempty"`
}

func (s *SelecterServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		ctx    = r.Context()
		tr     = trc.Get(ctx)
		isJSON = strings.Contains(r.Header.Get("content-type"), "application/json")
		data   = SelectData{}
	)

	switch {
	case isJSON:
		body := http.MaxBytesReader(w, r.Body, maxRequestBodySizeBytes)
		if err := json.NewDecoder(body).Decode(&data.Request); err != nil {
			tr.Errorf("decode JSON request failed, using defaults (%v)", err)
			data.Problems = append(data.Problems, fmt.Errorf("decode JSON request: %w", err))
		}

	default:
		urlquery := r.URL.Query()
		data.Request = trc.SelectRequest{
			Bucketing: parseBucketing(urlquery["b"]), // nil is OK
			Filter: trc.Filter{
				Sources:     urlquery["source"],
				IDs:         urlquery["id"],
				Category:    urlquery.Get("category"),
				IsActive:    urlquery.Has("active"),
				IsFinished:  urlquery.Has("finished"),
				MinDuration: parseDefault(urlquery.Get("min"), parseDurationPointer, nil),
				IsSuccess:   urlquery.Has("success"),
				IsErrored:   urlquery.Has("errored"),
				Query:       urlquery.Get("q"),
			},
			Limit:      parseRange(urlquery.Get("n"), strconv.Atoi, trc.SelectRequestLimitMin, trc.SelectRequestLimitDefault, trc.SelectRequestLimitMax),
			StackDepth: parseDefault(urlquery.Get("stack"), strconv.Atoi, 0),
		}
	}

	data.Problems = append(data.Problems, data.Request.Normalize()...)

	res, err := s.sel.Select(ctx, &data.Request)
	if err != nil {
		data.Problems = append(data.Problems, fmt.Errorf("execute select request: %w", err))
	} else {
		data.Response = *res
	}

	for _, problem := range data.Response.Problems {
		data.Problems = append(data.Problems, fmt.Errorf("response: %s", problem))
	}

	renderResponse(ctx, w, r, assets, "traces.html", nil, data)
}

//
//
//

type SelecterClient struct {
	client HTTPClient
	uri    string
}

var _ trc.Selecter = (*SelecterClient)(nil)

func NewSelecterClient(client HTTPClient, remoteURI string) *SelecterClient {
	return &SelecterClient{
		client: client,
		uri:    remoteURI,
	}
}

func (c *SelecterClient) Select(ctx context.Context, req *trc.SelectRequest) (_ *trc.SelectResponse, err error) {
	tr := trc.Get(ctx)

	defer func() {
		if err != nil {
			tr.Errorf("error: %v", err)
		}
	}()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode select request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.uri, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}

	httpReq.Header.Set("content-type", "application/json; charset=utf-8")

	httpRes, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute HTTP request: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, httpRes.Body)
		httpRes.Body.Close()
	}()

	if httpRes.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP response %d %s", httpRes.StatusCode, http.StatusText(httpRes.StatusCode))
	}

	var res SelectData
	if err := json.NewDecoder(httpRes.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode select response: %w", err)
	}

	tr.Tracef("%s -> total count %d, match count %d, trace count %d", c.uri, res.Response.TotalCount, res.Response.MatchCount, len(res.Response.Traces))

	return &res.Response, nil
}

//
//
//
