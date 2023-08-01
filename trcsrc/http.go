package trcsrc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/peterbourgon/trc"
)

type SelecterServer struct {
	sel Selecter
}

func NewSelecterServer(sel Selecter) *SelecterServer {
	return &SelecterServer{
		sel: sel,
	}
}

const maxSelectRequestSizeBytes = 1 * 1024 * 1024 // 1MB

func (h *SelecterServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		ctx  = r.Context()
		tr   = trc.Get(ctx)
		body = http.MaxBytesReader(w, r.Body, maxSelectRequestSizeBytes)
	)

	var req SelectRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		tr.Errorf("decode select request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	res, err := h.sel.Select(ctx, &req)
	if err != nil {
		tr.Errorf("execute select request: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("content-type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		tr.Errorf("encode select response: %v", err)
		return
	}
}

//
//
//

type SelecterClient struct {
	client HTTPClient
	uri    string
}

var _ Selecter = (*SelecterClient)(nil)

func NewSelecterClientx(client HTTPClient, remoteURI string) *SelecterClient {
	return &SelecterClient{
		client: client,
		uri:    remoteURI,
	}
}

func (c *SelecterClient) Select(ctx context.Context, req *SelectRequest) (_ *SelectResponse, err error) {
	ctx, tr, finish := trc.Region(ctx, c.uri)
	defer finish()

	defer func() {
		if err != nil {
			trc.Get(ctx).Errorf("remote select (%s): error: %v", c.uri, err)
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

	var res SelectResponse
	if err := json.NewDecoder(httpRes.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode select response: %w", err)
	}

	tr.Tracef("TotalCount %d, MatchCount %d, Problems %v", res.TotalCount, res.MatchCount, res.Problems)

	return &res, nil
}

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

var _ HTTPClient = (*http.Client)(nil)
