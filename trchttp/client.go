package trchttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/peterbourgon/trc"
)

// HTTPClient models a concrete http.Client.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Client implements the searcher interface by making an HTTP request to a URL
// assumed to be handled by an instance of the server also defined in this
// package.
type Client struct {
	client  HTTPClient
	baseurl string
}

var _ trc.Searcher = (*Client)(nil)

// NewClient returns a client calling the provided URL, which is assumed to be
// an instance of the server also defined in this package.
func NewClient(client HTTPClient, baseurl string) *Client {
	if !strings.HasPrefix(baseurl, "http") {
		baseurl = "http://" + baseurl
	}
	return &Client{
		client:  client,
		baseurl: baseurl,
	}
}

// Search implements the searcher interface.
func (c *Client) Search(ctx context.Context, req *trc.SearchRequest) (*trc.SearchResponse, error) {
	tr := trc.FromContext(ctx)

	httpReq, err := req.HTTPRequest(ctx, c.baseurl)
	if err != nil {
		return nil, fmt.Errorf("make HTTP request: %w", err)
	}

	tr.Tracef("⇒ %s", httpReq.URL.String())

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute HTTP request: %w", redactURL(err))
	}
	defer func() {
		io.Copy(io.Discard, httpResp.Body)
		httpResp.Body.Close()
	}()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote status code %d", httpResp.StatusCode)
	}

	var d ResponseData
	if err := json.NewDecoder(httpResp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	for _, tr := range d.Response.Selected {
		tr.Via = append(tr.Via, c.baseurl)
	}

	tr.Tracef("⇐ total=%d matched=%d selected=%d", d.Response.Total, d.Response.Matched, len(d.Response.Selected))

	return d.Response, nil
}

func redactURL(err error) error {
	if urlErr := (&url.Error{}); errors.As(err, &urlErr) {
		err = fmt.Errorf("%s: %w", urlErr.Op, urlErr.Err)
	}
	return err
}