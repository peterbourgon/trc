package trcweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

	tr.Tracef("filter %s", f)

	var (
		buf    = parseRange(r.URL.Query().Get("buf"), strconv.Atoi, 0, 100, 1000)
		tracec = make(chan trc.Trace, buf)
		donec  = make(chan struct{})
	)

	tr.Tracef("buffer %d", buf)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		stats, err := s.b.Stream(ctx, f, tracec)
		tr.Tracef("Stream finished (%v), skips %d, sends %d, drops %d (%.1f%%)", err, stats.Skips, stats.Sends, stats.Drops, 100*stats.DropRate())
		close(donec)
	}()
	defer func() {
		<-donec
	}()

	eventsource.Handler(func(lastId string, encoder *eventsource.Encoder, stop <-chan bool) {
		tr.Tracef("event source handler started, last ID %q", lastId)

		stats := time.NewTicker(10 * time.Second)
		defer stats.Stop()

		initc := make(chan struct{}, 1)
		initc <- struct{}{}

		for {
			select {
			case <-initc:
				data, err := json.Marshal(map[string]any{
					"filter": f,
					"buffer": cap(tracec),
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
	client HTTPClient
	uri    string
}

func NewStreamClient(client HTTPClient, uri string) *StreamClient {
	if !strings.HasPrefix(uri, "http") {
		uri = "http://" + uri
	}
	return &StreamClient{
		client: client,
		uri:    uri,
	}
}

func (c *StreamClient) Stream(ctx context.Context, f trc.Filter, ch chan<- trc.Trace) (err error) {
	tr := trc.Get(ctx)

	defer func() {
		if err != nil {
			tr.Errorf("error: %v", err)
		}
	}()

	{
		req, err := http.NewRequestWithContext(ctx, "GET", c.uri, nil)
		if err != nil {
			return err
		}
		res, err := c.client.Do(req)
		if err != nil {
			return err
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}

	body, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("encode filter: %w", err)
	}

	// Explicitly don't provide the context to the request, because EventSource
	// (incorrectly) treats context cancelation as a recoverable error, which
	// can cause Read to block for a single retry duration before returning
	// ErrClosed.
	req, err := http.NewRequest("GET", c.uri, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("content-type", "application/json; charset=utf-8")

	es := eventsource.New(req, 1*time.Second)
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
			if err := json.Unmarshal(ev.Data, &stats); err != nil {
				return fmt.Errorf("decode stats event: %w", err)
			}
			tr.LazyTracef("stream: skips %d, sends %d, drops %d (%.1f%%)", stats.Skips, stats.Sends, stats.Drops, 100*stats.DropRate())

		default:
			tr.LazyTracef("unknown event type %q", ev.Type)
		}
	}
}
