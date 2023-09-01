package trcweb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bernerdschaefer/eventsource"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcutil"
	"github.com/peterbourgon/trc/trcstream"
)

type TraceServer struct {
	searcher trc.Searcher
	streamer Streamer
}

type Streamer interface {
	Stream(ctx context.Context, f trc.Filter, ch chan<- trc.Trace) (trcstream.Stats, error)
	Stats(ctx context.Context, ch chan<- trc.Trace) (trcstream.Stats, error)
}

func NewTraceServer(searcher trc.Searcher, streamer Streamer) *TraceServer {
	return &TraceServer{
		searcher: searcher,
		streamer: streamer,
	}
}

func TraceServerCategory(r *http.Request) string {
	switch {
	case requestExplicitlyAccepts(r, "text/event-stream"):
		return "stream"
	default:
		return "traces"
	}
}

func (s *TraceServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case requestExplicitlyAccepts(r, "text/event-stream"):
		s.handleStream(w, r)
	default:
		s.handleTraces(w, r)
	}
}

func (s *TraceServer) handleTraces(w http.ResponseWriter, r *http.Request) {
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

	res, err := s.searcher.Search(ctx, &data.Request)
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

	tr.Tracef("filter %s", f)

	var (
		buf    = parseRange(r.URL.Query().Get("buf"), strconv.Atoi, 0, 100, 1000)
		tracec = make(chan trc.Trace, buf)
		donec  = make(chan struct{})
	)

	tr.Tracef("buffer %d", buf)

	var (
		statsInterval = parseDefault(r.URL.Query().Get("stats"), time.ParseDuration, 10*time.Second)
	)

	tr.Tracef("stats %s", statsInterval)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		stats, err := s.streamer.Stream(ctx, f, tracec)
		tr.Tracef("Stream finished (%v), skips %d, sends %d, drops %d (%.1f%%)", err, stats.Skips, stats.Sends, stats.Drops, 100*stats.DropRate())
		close(donec)
	}()
	defer func() {
		<-donec
	}()

	eventsource.Handler(func(lastId string, encoder *eventsource.Encoder, stop <-chan bool) {
		tr.Tracef("event source handler started")

		stats := time.NewTicker(statsInterval)
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
				stats, err := s.streamer.Stats(ctx, tracec)
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
