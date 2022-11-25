package trctrace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/peterbourgon/trc"
)

type HTTPQueryClient struct {
	client   *http.Client
	origin   string
	endpoint string
}

func NewHTTPQueryClient(client *http.Client, origin string, endpoint string) *HTTPQueryClient {
	return &HTTPQueryClient{
		client:   client,
		origin:   origin,
		endpoint: endpoint,
	}
}

func (c *HTTPQueryClient) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	tr := trc.FromContext(trc.WithPrefixContext(ctx, "<%s>", c.endpoint))

	httpReq, err := req.MakeHTTPRequest(ctx, c.endpoint)
	if err != nil {
		return nil, fmt.Errorf("make HTTP request: %w", err)
	}

	tr.Tracef("URL %s", httpReq.URL.String())

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

	var httpQueryResp HTTPQueryData
	if err := json.NewDecoder(httpResp.Body).Decode(&httpQueryResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	tr.Tracef("total=%d matched=%d selected=%d", httpQueryResp.Response.Total, httpQueryResp.Response.Matched, len(httpQueryResp.Response.Selected))

	res := httpQueryResp.Response
	res.Request = req
	res.Origins = []string{c.origin}
	for _, tr := range res.Selected {
		tr.Origin = c.origin
	}

	return res, nil
}
