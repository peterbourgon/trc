package trcweb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcsrc"
)

type Client struct {
	client HTTPClient
	uri    string
}

var _ trcsrc.Selecter = (*Client)(nil)

func NewClient(client HTTPClient, remoteURI string) *Client {
	return &Client{
		client: client,
		uri:    remoteURI,
	}
}

func (c *Client) Select(ctx context.Context, req *trcsrc.SelectRequest) (_ *trcsrc.SelectResponse, err error) {
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

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

var _ HTTPClient = (*http.Client)(nil)
