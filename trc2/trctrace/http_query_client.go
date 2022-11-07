package trctrace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type HTTPQueryClient struct {
	client   *http.Client
	endpoint string
}

func NewHTTPQueryClient(client *http.Client, endpoint string) *HTTPQueryClient {
	return &HTTPQueryClient{
		client:   client,
		endpoint: endpoint,
	}
}

func (c *HTTPQueryClient) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	httpReq, err := req.MakeHTTPRequest(ctx, c.endpoint)
	if err != nil {
		return nil, fmt.Errorf("make HTTP request: %w", err)
	}

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

	var httpQueryResp HTTPQueryResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&httpQueryResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	res := httpQueryResp.Response
	res.Request = req
	res.Origins = append(res.Origins, c.endpoint)
	for _, tr := range res.Selected {
		q := url.Values{"id": []string{tr.ID()}}
		httpReq.URL.RawQuery = q.Encode()
		tr.OriginURI = httpReq.URL.String()
	}
	return res, nil
}
