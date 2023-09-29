package trchttp

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

// StreamServer provides an HTTP interface to a [trcstream.Streamer].
type StreamServer struct {
	trcstream.Streamer
}

// NewStreamServer returns a stream server wrapping the provided streamer.
func NewStreamServer(s trcstream.Streamer) *StreamServer {
	return &StreamServer{
		Streamer: s,
	}
}

// ServeHTTP implements [http.Handler]. Requests must Accept: text/event-stream.
func (s *StreamServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		ctx = r.Context()
		tr  = trc.Get(ctx)
	)

	if !RequestExplicitlyAccepts(r, "text/event-stream") {
		err := fmt.Errorf("invalid request Accept header (%s)", r.Header.Get("accept"))
		tr.Errorf("%v", err)
		respondError(w, r, err, http.StatusBadRequest)
		return
	}

	var f trc.Filter
	switch {
	case RequestHasContentType(r, "application/json"):
		body := http.MaxBytesReader(w, r.Body, maxRequestBodySizeBytes)
		if err := json.NewDecoder(body).Decode(&f); err != nil {
			tr.Errorf("decode filter error (%v), using default", err)
		}
	default:
		f = parseFilter(r)
	}

	if normalizeErrs := f.Normalize(); len(normalizeErrs) > 0 {
		err := fmt.Errorf("bad request: %s", strings.Join(trcutil.FlattenErrors(normalizeErrs...), "; "))
		respondError(w, r, err, http.StatusBadRequest)
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
		tr.LazyTracef("stream done, %s, error=%v", stats, err)
		close(donec)
	}()
	defer func() {
		<-donec
		cancel()
	}()

	eventsource.Handler(func(lastId string, encoder *eventsource.Encoder, stop <-chan bool) {
		tr.LazyTracef("event source handler started")
		trID := tr.ID()

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

			case recv := <-tracec:
				if recv.ID() == trID {
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

			case <-donec:
				tr.LazyTracef("stopping: stream done")
				cancel()
				return

			case <-stop:
				tr.LazyTracef("stopping: stop signal (canceling context)")
				cancel()
				return

			case <-ctx.Done():
				tr.LazyTracef("stopping: context done (%v)", ctx.Err())
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
	// Implementations must not block and must not modify event data.
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
			tr.LazyTracef("read server-sent event: connection closed (%v)", err)
			return nil
		}
		if err != nil {
			tr.LazyTracef("read server-sent event: error (%v)", err)
			return fmt.Errorf("read server-sent event: %w", err)
		}

		c.OnRead(ctx, ev.Type, ev.Data)

		switch ev.Type {
		case "init":
			tr.LazyTracef("init: %s", string(ev.Data))

		case "trace":
			var st trc.StaticTrace
			if err := json.Unmarshal(ev.Data, &st); err != nil {
				return fmt.Errorf("decode trace event: %w", err)
			}
			select {
			case <-ctx.Done():
				tr.LazyTracef("emit event: context done")
			case ch <- &st:
				// OK
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
