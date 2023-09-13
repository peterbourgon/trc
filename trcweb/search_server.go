package trcweb

/*

type SearchServer struct {
	src trc.Searcher
}

func NewSearchServer(src trc.Searcher) *SearchServer {
	return &SearchServer{
		src: src,
	}
}

type SearchData struct {
	Request  trc.SearchRequest  `json:"request"`
	Response trc.SearchResponse `json:"response"`
	Problems []error            `json:"-"`
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
		if err := json.NewDecoder(body).Decode(&data.Request); err != nil {
			tr.Errorf("decode JSON request failed, using defaults (%v)", err)
			data.Problems = append(data.Problems, fmt.Errorf("decode JSON request: %w", err))
		}

	default:
		urlquery := r.URL.Query()
		data.Request = trc.SearchRequest{
			Bucketing:  parseBucketing(urlquery["b"]), // nil is OK
			Filter:     parseFilter(r),
			Limit:      parseRange(urlquery.Get("n"), strconv.Atoi, trc.SelectRequestLimitMin, trc.SelectRequestLimitDefault, trc.SelectRequestLimitMax),
			StackDepth: parseDefault(urlquery.Get("stack"), strconv.Atoi, 0),
		}
	}

	data.Problems = append(data.Problems, data.Request.Normalize()...)

	tr.LazyTracef("search request %s", data.Request)

	res, err := s.src.Search(ctx, &data.Request)
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

type SearchClient struct {
	client HTTPClient
	uri    string
}

var _ trc.Searcher = (*SearchClient)(nil)

func NewSearchClient(client HTTPClient, uri string) *SearchClient {
	if !strings.HasPrefix(uri, "http") {
		uri = "http://" + uri
	}
	return &SearchClient{
		client: client,
		uri:    uri,
	}
}

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

	var res SearchData
	if err := json.NewDecoder(httpRes.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	tr.Tracef("%s -> total %d, matched %d, returned %d", c.uri, res.Response.TotalCount, res.Response.MatchCount, len(res.Response.Traces))

	return &res.Response, nil
}
*/
