package trcweb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime/trace"
	"strconv"
	"strings"

	"github.com/peterbourgon/trc"
)

// SearchData is returned by the search server.
type SearchData struct {
	Request  trc.SearchRequest  `json:"request"`
	Response trc.SearchResponse `json:"response"`
	Problems []string           `json:"problems,omitempty"`
}

//
//
//

type SearchServer struct {
	trc.Searcher
}

func NewSearchServer(s trc.Searcher) *SearchServer {
	return &SearchServer{
		Searcher: s,
	}
}

func (s *SearchServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, task := trace.NewTask(r.Context(), "SearchServer.ServeHTTP")
	defer task.End()

	var (
		tr     = trc.Get(ctx)
		isJSON = RequestHasContentType(r, "application/json")
		data   = SearchData{}
	)

	switch {
	case isJSON:
		body := http.MaxBytesReader(w, r.Body, maxRequestBodySizeBytes)
		if err := json.NewDecoder(body).Decode(&data.Request); err != nil {
			tr.Errorf("decode JSON request failed (%v) -- returning error", err)
			respondError(w, r, err, http.StatusBadRequest)
			return
		}

	default:
		urlquery := r.URL.Query()
		data.Request = trc.SearchRequest{
			Bucketing:  parseBucketing(urlquery["b"]), // nil is OK
			Filter:     parseFilter(r),
			Limit:      parseRange(urlquery.Get("n"), strconv.Atoi, trc.SearchLimitMin, trc.SearchLimitDefault, trc.SearchLimitMax),
			StackDepth: parseDefault(urlquery.Get("stack"), strconv.Atoi, 0),
		}
	}

	data.Problems = append(data.Problems, makeErrorStrings(data.Request.Normalize()...)...)

	tr.LazyTracef("search request %s", data.Request)

	res, err := s.Searcher.Search(ctx, &data.Request)
	if err != nil {
		data.Problems = append(data.Problems, fmt.Errorf("execute search request: %w", err).Error())
	} else {
		data.Response = *res
	}

	for _, problem := range data.Response.Problems {
		data.Problems = append(data.Problems, fmt.Errorf("response: %s", problem).Error())
	}

	if n := len(data.Response.Stats.Categories); n >= 100 {
		data.Problems = append(data.Problems, fmt.Errorf("way too many categories (%d)", n).Error())
	}

	respondData(ctx, w, r, 200, "traces.html", data)
}

//
//
//

// SearchClient implements [trc.Searcher] by querying a search server.
type SearchClient struct {
	client HTTPClient
	uri    string
}

var _ trc.Searcher = (*SearchClient)(nil)

// NewSearchClient returns a search client using the given HTTP client to query
// the given search server URI.
func NewSearchClient(client HTTPClient, uri string) *SearchClient {
	if !strings.HasPrefix(uri, "http") {
		uri = "http://" + uri
	}
	return &SearchClient{
		client: client,
		uri:    uri,
	}
}

// Search implements [trc.Searcher].
func (c *SearchClient) Search(ctx context.Context, req *trc.SearchRequest) (_ *trc.SearchResponse, err error) {
	ctx, task := trace.NewTask(ctx, "SearchClient.Search")
	defer task.End()

	tr := trc.Get(ctx)

	defer func() {
		if err != nil {
			tr.Errorf("error: %v", err)
		}
	}()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode search request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.uri, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}

	httpReq.Header.Set("content-type", "application/json; charset=utf-8")
	httpReq.Header.Set("accept", "application/json")
	httpReq.Header.Set("x-trc-id", tr.ID())

	httpRes, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute HTTP request: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, httpRes.Body)
		httpRes.Body.Close()
	}()

	if httpRes.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read HTTP response: server gave HTTP %d (%s)", httpRes.StatusCode, http.StatusText(httpRes.StatusCode))
	}

	var res SearchData
	if err := json.NewDecoder(httpRes.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	tr.LazyTracef("%s -> total %d, matched %d, returned %d", c.uri, res.Response.TotalCount, res.Response.MatchCount, len(res.Response.Traces))

	return &res.Response, nil
}
