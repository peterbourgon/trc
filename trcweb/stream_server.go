package trcweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bernerdschaefer/eventsource"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcutil"
	"github.com/peterbourgon/trc/trcstream"
)

type StreamServer struct {
	b *trcstream.Broker
}

func NewStreamServer(b *trcstream.Broker) *StreamServer {
	return &StreamServer{
		b: b,
	}
}

func (s *StreamServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "only GET is supported", http.StatusMethodNotAllowed)
		return
	}
	switch {
	case requestExplicitlyAccepts(r, "text/event-stream"):
		s.handleEvents(w, r)
	case requestExplicitlyAccepts(r, "text/html"):
		s.handleHTML(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusNotAcceptable), http.StatusNotAcceptable)
	}
}

func (s *StreamServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "only GET is supported", http.StatusMethodNotAllowed)
		return
	}

	if !requestExplicitlyAccepts(r, "text/event-stream") {
		http.Error(w, "request must Accept: text/event-stream", http.StatusNotAcceptable)
		return
	}

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

	var (
		buf    = parseRange(r.URL.Query().Get("buf"), strconv.Atoi, 0, 100, 1000)
		report = parseRange(r.URL.Query().Get("report"), time.ParseDuration, time.Second, 10*time.Second, time.Minute)
	)

	tr.Tracef("filter %s", f)
	tr.Tracef("buf %d", buf)
	tr.Tracef("report %s", report)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		tracec = make(chan trc.Trace, buf)
		donec  = make(chan struct{})
	)
	go func() {
		stats, err := s.b.Stream(ctx, f, tracec)
		tr.Tracef("Stream finished (%v), skips %d, sends %d, drops %d (%.1f%%)", err, stats.Skips, stats.Sends, stats.Drops, 100*stats.DropRate())
		close(donec)
	}()
	defer func() {
		<-donec
	}()

	eventsource.Handler(func(lastId string, encoder *eventsource.Encoder, stop <-chan bool) {
		tr.Tracef("event source handler started")

		stats := time.NewTicker(report)
		defer stats.Stop()

		initc := make(chan struct{}, 1)
		initc <- struct{}{}

		for {
			select {
			case <-initc:
				data, err := json.Marshal(map[string]any{
					"filter": f,
					"buffer": cap(tracec),
					"stats":  report.String(),
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
				stats, err := s.b.Stats(ctx, tracec)
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
				tr.Tracef("stopping: context done (%v)", ctx.Err())
				return

			case <-stop:
				tr.Tracef("stopping: stop signal")
				return
			}
		}
	}).ServeHTTP(w, r)
}

func (s *StreamServer) handleHTML(w http.ResponseWriter, r *http.Request) {
	renderHTML(r.Context(), w, assets, "stream.html", nil, nil)
}

//
//
//

type StreamClient struct {
	HTTPClient    HTTPClient
	URI           string
	RemoteBuffer  int
	OnRead        func(eventType string, eventData []byte)
	RetryInterval time.Duration
	StatsInterval time.Duration
}

func (c *StreamClient) initialize() {
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}

	if c.URI != "" && !strings.HasPrefix(c.URI, "http") {
		c.URI = "http://" + c.URI
	}

	if c.OnRead == nil {
		c.OnRead = func(eventType string, eventData []byte) {}
	}

	if c.RetryInterval == 0 {
		c.RetryInterval = time.Second
	}

	if c.StatsInterval == 0 {
		c.StatsInterval = 10 * time.Second
	}
}

func NewStreamClient(client HTTPClient, uri string) *StreamClient {
	return NewStreamClientCallback(client, uri, nil)
}

func NewStreamClientCallback(client HTTPClient, uri string, onRead func(eventType string, eventData []byte)) *StreamClient {
	if !strings.HasPrefix(uri, "http") {
		uri = "http://" + uri
	}
	return &StreamClient{
		HTTPClient: client,
		URI:        uri,
		OnRead:     onRead,
	}
}

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
		if c.RemoteBuffer > 0 {
			query.Set("buf", strconv.Itoa(c.RemoteBuffer))
		}
		if c.StatsInterval > 0 {
			query.Set("report", c.StatsInterval.String())
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

		c.OnRead(ev.Type, ev.Data)

		switch ev.Type {
		case "init":
			continue

		case "trace":
			var str trcstream.StreamTrace
			if err := json.Unmarshal(ev.Data, &str); err != nil {
				return fmt.Errorf("decode trace event: %w", err)
			}
			select {
			case <-ctx.Done():
			case ch <- &str:
			}

		case "stats":
			var stats trcstream.Stats
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
