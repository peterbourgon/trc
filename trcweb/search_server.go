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
	"github.com/peterbourgon/trc/trcweb/assets"
)

// SearchData is returned by normal trace search requests.
type SearchData struct {
	Request  trc.SearchRequest  `json:"request"`
	Response trc.SearchResponse `json:"response"`
	Problems []error            `json:"-"` // for rendering, not transmitting
}

type SearchServer struct {
	Searcher
}

func (s *SearchServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		ctx    = r.Context()
		tr     = trc.Get(ctx)
		isJSON = strings.Contains(r.Header.Get("content-type"), "application/json")
		data   = SearchData{}
	)

	switch {
	case isJSON:
		body := http.MaxBytesReader(w, r.Body, maxRequestBodySizeBytes)
		var req trc.SearchRequest
		if err := json.NewDecoder(body).Decode(&req); err != nil {
			//tr.Errorf("decode JSON request failed, using defaults (%v)", err)
			//data.Problems = append(data.Problems, fmt.Errorf("decode JSON request: %w", err))
			tr.Errorf("decode JSON request failed (%v) -- returning error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data.Request = req

	default:
		urlquery := r.URL.Query()
		data.Request = trc.SearchRequest{
			Bucketing:  parseBucketing(urlquery["b"]), // nil is OK
			Filter:     parseFilter(r),
			Limit:      parseRange(urlquery.Get("n"), strconv.Atoi, trc.SearchLimitMin, trc.SearchLimitDefault, trc.SearchLimitMax),
			StackDepth: parseDefault(urlquery.Get("stack"), strconv.Atoi, 0),
		}
	}

	data.Problems = append(data.Problems, data.Request.Normalize()...)

	tr.LazyTracef("search request %s", data.Request)

	res, err := s.Searcher.Search(ctx, &data.Request)
	if err != nil {
		data.Problems = append(data.Problems, fmt.Errorf("execute select request: %w", err))
	} else {
		data.Response = *res
	}

	for _, problem := range data.Response.Problems {
		data.Problems = append(data.Problems, fmt.Errorf("response: %s", problem))
	}

	if n := len(data.Response.Stats.Categories); n >= 100 {
		data.Problems = append(data.Problems, fmt.Errorf("way too many categories (%d)", n))
	}

	renderResponse(ctx, w, r, assets.FS, "traces.html", nil, data)
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
