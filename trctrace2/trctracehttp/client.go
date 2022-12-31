package trctracehttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/peterbourgon/trc"
	trctrace "github.com/peterbourgon/trc/trctrace2"
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	client  HTTPClient
	baseurl string
}

var _ trctrace.Searcher = (*Client)(nil)

func NewClient(client HTTPClient, baseurl string) *Client {
	return &Client{
		client:  client,
		baseurl: baseurl,
	}
}

func (c *Client) Search(ctx context.Context, req *trctrace.SearchRequest) (*trctrace.SearchResponse, error) {
	ctx, tr, finish := trc.Region(ctx, "<%s>", c.baseurl)
	defer finish()

	httpReq, err := req.MakeHTTPRequest(ctx, c.baseurl)
	if err != nil {
		return nil, fmt.Errorf("make HTTP request: %w", err)
	}

	tr.Tracef("using remote URL %s", httpReq.URL.String())

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute HTTP request: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, httpResp.Body)
		httpResp.Body.Close()
	}()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote status code %d", httpResp.StatusCode)
	}

	//{
	//	var debugBuf bytes.Buffer
	//	io.Copy(&debugBuf, httpResp.Body)
	//	bodyStr := debugBuf.String()
	//	log.Printf("### %s", bodyStr)
	//	httpResp.Body = io.NopCloser(strings.NewReader(bodyStr))
	//}

	var searchResp trctrace.SearchResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	tr.Tracef("search response: origins %v, total %d, matched %d, selected %d", searchResp.Origins, searchResp.Total, searchResp.Matched, len(searchResp.Selected))

	return &searchResp, nil
}
