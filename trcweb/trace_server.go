package trcweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bernerdschaefer/eventsource"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcutil"
	"github.com/peterbourgon/trc/trcweb/assets"
)

// HTTPClient models an http.Client.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

var _ HTTPClient = (*http.Client)(nil)

// Searcher is just a trc.Searcher.
type Searcher trc.Searcher

// Streamer models the subscriber methods of a trc.Collector.
type Streamer interface {
	Stream(ctx context.Context, f trc.Filter, ch chan<- trc.Trace) (trc.StreamStats, error)
	StreamStats(ctx context.Context, ch chan<- trc.Trace) (trc.StreamStats, error)
}

//
//
//

// TraceServer provides an HTTP interface to a trace collector.
type TraceServer struct {
	// Collector is the default implementation for Searcher and Streamer.
	Collector *trc.Collector

	// Searcher is used to serve requests which Accept: text/html and/or
	// application/json. If not provided, the Collector will be used.
	Searcher Searcher

	// Streamer is used to serve requests which Accept: text/event-stream. If
	// not provided, the Collector will be used.
	Streamer Streamer
}

// NewTraceServer returns a standard trace server wrapping the collector.
func NewTraceServer(c *trc.Collector) *TraceServer {
	s := &TraceServer{
		Collector: c,
	}
	s.initialize()
	return s
}

func (s *TraceServer) initialize() {
	if s.Searcher == nil {
		s.Searcher = s.Collector
	}
	if s.Streamer == nil {
		s.Streamer = s.Collector
	}
}

// ServeHTTP implements http.Handler.
func (s *TraceServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.initialize()

	switch Categorize(r) {
	case "stream":
		s.handleStream(w, r)
	default:
		s.handleSearch(w, r)
	}
}

// Categorize the request for a [Middleware].
func Categorize(r *http.Request) string {
	if requestExplicitlyAccepts(r, "text/event-stream") {
		return "stream"
	}
	return "traces"
}

//
//
//

// SearchData is returned by normal trace search requests.
type SearchData struct {
	Request  trc.SearchRequest  `json:"request"`
	Response trc.SearchResponse `json:"response"`
	Problems []error            `json:"-"` // for rendering, not transmitting
}

func (s *TraceServer) handleSearch(w http.ResponseWriter, r *http.Request) {
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

//
//
//

func (s *TraceServer) handleStream(w http.ResponseWriter, r *http.Request) {
	var (
		ctx = r.Context()
		tr  = trc.Get(ctx)
	)

	var f trc.Filter
	switch {
	case strings.Contains(r.Header.Get("content-type"), "application/json"):
		body := http.MaxBytesReader(w, r.Body, maxRequestBodySizeBytes)
		if err := json.NewDecoder(body).Decode(&f); err != nil {
			tr.Errorf("decode filter error (%v), using default", err)
		}
	default:
		f = parseFilter(r)
	}

	if normalizeErrs := f.Normalize(); len(normalizeErrs) > 0 {
		err := fmt.Errorf("bad request: %s", strings.Join(trcutil.FlattenErrors(normalizeErrs...), "; "))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tr.LazyTracef("stream filter %s", f)

	if f.IsFinished {
		tr.LazyTracef("streaming complete traces")
	} else {
		tr.LazyTracef("streaming individual events")
	}

	var (
		stats   = parseDefault(r.URL.Query().Get("stats"), time.ParseDuration, 10*time.Second)
		sendbuf = parseRange(r.URL.Query().Get("sendbuf"), strconv.Atoi, 0, 100, 100000)
		tracec  = make(chan trc.Trace, sendbuf)
		donec   = make(chan struct{})
	)

	tr.LazyTracef("stats interval %s", stats)
	tr.LazyTracef("send buffer %d", sendbuf)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		stats, err := s.Streamer.Stream(ctx, f, tracec)
		tr.LazyTracef("%s (error: %v)", stats, err)
		close(donec)
	}()
	defer func() {
		<-donec
	}()

	eventsource.Handler(func(lastId string, encoder *eventsource.Encoder, stop <-chan bool) {
		tr.LazyTracef("event source handler started")

		stats := time.NewTicker(stats)
		defer stats.Stop()

		initc := make(chan struct{}, 1)
		initc <- struct{}{}

		for {
			select {
			case <-initc:
				data, err := json.Marshal(map[string]any{
					"filter":  f,
					"sendbuf": cap(tracec),
				})
				if err != nil {
					tr.Errorf("JSON marshal init: %v", err)
					continue
				}

				if err := encoder.Encode(eventsource.Event{
					Type: "init",
					Data: data,
				}); err != nil {
					tr.Errorf("encode init: %v", err)
					continue
				}

			case <-stats.C:
				stats, err := s.Streamer.StreamStats(ctx, tracec)
				if err != nil {
					tr.Errorf("get stats: %v", err)
					continue
				}

				data, err := json.Marshal(stats)
				if err != nil {
					tr.Errorf("JSON marshal stats: %v", err)
					continue
				}

				if err := encoder.Encode(eventsource.Event{
					Type: "stats",
					Data: data,
				}); err != nil {
					tr.Errorf("encode stats: %v", err)
					continue
				}

				tr.Tracef("stats: %s", stats.String())

			case recv := <-tracec:
				if recv.ID() == tr.ID() {
					continue // don't publish our own trace events
				}

				data, err := json.Marshal(recv)
				if err != nil {
					tr.Errorf("JSON marshal trace: %v", err)
					continue
				}

				if err := encoder.Encode(eventsource.Event{
					Type: "trace",
					Data: data,
				}); err != nil {
					tr.Errorf("encode trace: %v", err)
					continue
				}

			case <-ctx.Done():
				tr.LazyTracef("stopping: context done (%v)", ctx.Err())
				return

			case <-stop:
				tr.LazyTracef("stopping: stop signal (canceling context)")
				cancel()
				return
			}
		}
	}).ServeHTTP(w, r)
}

//

// StreamClient streams trace data from a server.
type StreamClient struct {
	// HTTPClient used to make the stream request. Optional.
	HTTPClient HTTPClient

	// URI of the remote stream server. Required.
	URI string

	// SendBuffer used by the remote stream server. Min 0, max 100k.
	SendBuffer int

	// OnRead is called for every stream event received by the client.
	// Implementations must not block.
	OnRead func(ctx context.Context, eventType string, eventData []byte)

	// RetryInterval between reconnect attempts. Default 3s, min 1s, max 60s.
	RetryInterval time.Duration

	// StatsInterval for stream stats updates. Default 10s, min 1s, max 60s.
	StatsInterval time.Duration
}

func (c *StreamClient) initialize() {
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}

	if c.URI != "" && !strings.HasPrefix(c.URI, "http") {
		c.URI = "http://" + c.URI
	}

	if min, max := 0, 100000; c.SendBuffer < min {
		c.SendBuffer = min
	} else if c.SendBuffer > max {
		c.SendBuffer = max
	}

	if c.OnRead == nil {
		c.OnRead = func(ctx context.Context, eventType string, eventData []byte) {}
	}

	if def, min, max := 3*time.Second, 1*time.Second, 60*time.Second; c.RetryInterval == 0 {
		c.RetryInterval = def
	} else if c.RetryInterval < min {
		c.RetryInterval = min
	} else if c.RetryInterval > max {
		c.RetryInterval = max
	}

	if def, min, max := 10*time.Second, 1*time.Second, 60*time.Second; c.StatsInterval == 0 {
		c.StatsInterval = def
	} else if c.StatsInterval < min {
		c.StatsInterval = min
	} else if c.StatsInterval > max {
		c.StatsInterval = max
	}
}

// NewStreamClient constructs a stream client connecting to the provided URI.
func NewStreamClient(uri string) *StreamClient {
	c := &StreamClient{
		URI: uri,
	}
	c.initialize()
	return c
}

// Stream trace data from the remote server, filtered by the provided filter, to
// the provided channel. The stream stops when the context is canceled, or a
// non-recoverable error occurs.
func (c *StreamClient) Stream(ctx context.Context, f trc.Filter, ch chan<- trc.Trace) (err error) {
	c.initialize()

	tr := trc.Get(ctx)

	defer func() {
		if err != nil {
			tr.Errorf("error: %v", err)
		}
	}()

	// Explicitly don't provide the context to the request, because EventSource
	// (incorrectly) treats context cancelation as a recoverable error, in which
	// case Read can block for a single retry duration before returning.
	//
	// Also, EventSource directly re-uses this request over reconnect attempts,
	// which prevents the use of a body, and means we have to encode the filter
	// in the URL.
	var req *http.Request
	{
		uri, err := url.Parse(c.URI)
		if err != nil {
			return err
		}

		query := uri.Query()
		if c.SendBuffer > 0 {
			query.Set("sendbuf", strconv.Itoa(c.SendBuffer))
		}
		if c.StatsInterval > 0 {
			query.Set("stats", c.StatsInterval.String())
		}
		uri.RawQuery = query.Encode()

		r, err := http.NewRequest("GET", uri.String(), nil)
		if err != nil {
			return err
		}

		encodeFilter(f, r)

		req = r
	}

	es := eventsource.New(req, c.RetryInterval)
	go func() {
		<-ctx.Done()
		es.Close()
	}()

	for {
		ev, err := es.Read()
		if errors.Is(err, eventsource.ErrClosed) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read server-sent event: %w", err)
		}

		c.OnRead(ctx, ev.Type, ev.Data)

		switch ev.Type {
		case "init":
			tr.LazyTracef("init: %s", string(ev.Data))

		case "trace":
			var str trc.StaticTrace
			if err := json.Unmarshal(ev.Data, &str); err != nil {
				return fmt.Errorf("decode trace event: %w", err)
			}
			select {
			case <-ctx.Done():
			case ch <- &str:
			}

		case "stats":
			var stats trc.StreamStats
			if err := json.Unmarshal(ev.Data, &stats); err == nil {
				tr.LazyTracef("%s", stats)
			} else {
				return fmt.Errorf("invalid stats event: %w", err)
			}

		default:
			tr.LazyTracef("unknown event type %q", ev.Type)
		}
	}
}
