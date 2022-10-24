package trchttp

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
)

type TraceCollector interface {
	TraceQueryer
	TraceStreamer
}

type TraceQueryer interface {
	QueryTraces(ctx context.Context, req *trc.QueryTracesRequest) (*trc.QueryTracesResponse, error)
}

type TraceStreamer interface {
	Subscribe(ctx context.Context, c chan<- trc.Trace) error
	Unsubscribe(ctx context.Context, c chan<- trc.Trace) (uint64, uint64, error)
}

func TracesHandler(c TraceCollector) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var (
			ctx         = r.Context()
			tr          = trc.FromContext(ctx)
			begin       = time.Now()
			urlquery    = r.URL.Query()
			limit       = parseDefault(urlquery.Get("n"), strconv.Atoi, 10)
			minDuration = parseDefault(urlquery.Get("min"), parseDurationPointer, nil)
			bucketing   = parseBucketing(urlquery["b"])
			search      = urlquery.Get("q")
			remotes     = urlquery["r"]
			problems    = []string{}
		)

		req := &trc.QueryTracesRequest{
			Bucketing:   bucketing,
			Limit:       limit,
			IDs:         urlquery["id"],
			Category:    urlquery.Get("category"),
			IsActive:    urlquery.Has("active"),
			IsFinished:  urlquery.Has("finished"),
			IsSucceeded: urlquery.Has("succeeded"),
			IsErrored:   urlquery.Has("errored"),
			MinDuration: minDuration,
			Search:      search,
		}

		if ct := r.Header.Get("content-type"); strings.Contains(ct, "application/json") {
			tr.Tracef("parsing request body as JSON")
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				err = fmt.Errorf("parse JSON request from body: %w", err)
				problems = append(problems, err.Error())
				tr.Errorf(err.Error())
			}
		}

		if err := req.Sanitize(); err != nil {
			err = fmt.Errorf("sanitize request: %w", err)
			problems = append(problems, err.Error())
			tr.Errorf(err.Error())
		}

		if requestExplicitlyAccepts(r, "text/event-stream") {
			tr.Tracef("text/event-stream")
			streamTraces(c, req, r, w) // TODO: distributed streamer
			return
		}

		var queryer TraceQueryer = c // default queryer
		if len(remotes) > 0 {
			tr.Tracef("remotes count %d, using explicit distributed trace collector")
			queryer = NewDistributedQueryer(http.DefaultClient, remotes...)
		}

		tr.Tracef("query starting: %s", req)

		res, err := queryer.QueryTraces(ctx, req)
		if err != nil {
			tr.Errorf("query errored: %v", err)
			res = trc.NewQueryTracesResponse(req, nil)
			problems = append(problems, err.Error())
		}
		res.Duration = time.Since(begin)
		res.Problems = append(problems, res.Problems...)

		tr.Tracef("query finished: matched=%d selected=%d duration=%s", res.Matched, len(res.Selected), res.Duration)

		switch {
		case requestExplicitlyAccepts(r, "text/html"):
			renderHTML(ctx, w, "traces.html", res)
		default:
			renderJSON(ctx, w, res)
		}
	})
}

func streamTraces(s TraceStreamer, req *trc.QueryTracesRequest, r *http.Request, w http.ResponseWriter) {
	eventsource.Handler(func(lastID string, enc *eventsource.Encoder, stop <-chan bool) {
		var (
			ctx = r.Context()
			tr  = trc.FromContext(ctx)
			in  = make(chan trc.Trace, 1000)
		)

		if err := s.Subscribe(ctx, in); err != nil {
			http.Error(w, fmt.Sprintf("subscribe: %v", err), http.StatusInternalServerError)
			return
		}

		defer func(begin time.Time) {
			sends, drops, err := s.Unsubscribe(ctx, in)
			tr.Tracef("unsubscribe: sends=%d drops=%d err=%v", sends, drops, err)
		}(time.Now())

		var (
			heartbeats uint64
			filtered   uint64
			emitted    uint64
			// sends      uint64 // TODO
			// drops      uint64 // TODO
		)

		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case tr := <-in:
				sent, err := maybeSendTrace(tr, req, enc)
				if err != nil {
					tr.Tracef("send trace error: %v", err)
					return
				}
				switch {
				case sent:
					emitted++
				case !sent:
					filtered++
				}

			case <-ticker.C:
				if err := sendHeartbeat(enc); err != nil {
					tr.Tracef("send heartbeat error: %v", err)
					return
				}
				heartbeats++

			case <-stop:
				return
			}
		}
	}).ServeHTTP(w, r)
}

const (
	eventTypeTrace     = "trace.1"
	eventTypeHeartbeat = "heartbeat.1"
)

func maybeSendTrace(tr trc.Trace, req *trc.QueryTracesRequest, enc *eventsource.Encoder) (bool, error) {
	if !req.Allow(tr) {
		return false, nil
	}

	trs := trc.NewTraceStatic(tr)
	data, err := json.Marshal(trs)
	if err != nil {
		return false, fmt.Errorf("marshal trace: %w", err)
	}

	if err := enc.Encode(eventsource.Event{
		Type: eventTypeTrace,
		Data: data,
	}); err != nil {
		return false, fmt.Errorf("encode event: %w", err)
	}

	return true, nil
}

func sendHeartbeat(enc *eventsource.Encoder) error {
	return enc.Encode(eventsource.Event{
		Type: eventTypeHeartbeat,
		Data: []byte(`{}`),
	})
}

/*
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestExplicitlyAccepts(r, "text/event-stream") {
			tracesHandlerStream(w, r)
			return
		}

		var (
			ctx         = r.Context()
			tr          = trc.FromContext(ctx)
			begin       = time.Now()
			urlquery    = r.URL.Query()
			limit       = parseDefault(urlquery.Get("n"), strconv.Atoi, 10)
			minDuration = parseDefault(urlquery.Get("min"), parseDurationPointer, nil)
			bucketing   = parseBucketing(urlquery["b"])
			search      = urlquery.Get("q")
			remotes     = urlquery["r"]
			problems    = []string{}
		)

		req := &trc.QueryRequest{
			Bucketing:   bucketing,
			Limit:       limit,
			IDs:         urlquery["id"],
			Category:    urlquery.Get("category"),
			IsActive:    urlquery.Has("active"),
			IsFinished:  urlquery.Has("finished"),
			IsSucceeded: urlquery.Has("succeeded"),
			IsErrored:   urlquery.Has("errored"),
			MinDuration: minDuration,
			Search:      search,
		}

		if ct := r.Header.Get("content-type"); strings.Contains(ct, "application/json") {
			tr.Tracef("parsing request body as JSON")
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				err = fmt.Errorf("parse JSON request from body: %w", err)
				problems = append(problems, err.Error())
				tr.Errorf(err.Error())
			}
		}

		if err := req.Sanitize(); err != nil {
			err = fmt.Errorf("sanitize request: %w", err)
			problems = append(problems, err.Error())
			tr.Errorf(err.Error())
		}

		var queryer TraceQueryer = c // default queryer
		if len(remotes) > 0 {
			tr.Tracef("remotes count %d, using explicit distributed trace collector")
			queryer = NewDistributedQueryer(http.DefaultClient, remotes...)
		}

		tr.Tracef("query starting: %s", req)

		res, err := queryer.Query(ctx, req)
		if err != nil {
			tr.Errorf("query errored: %v", err)
			res = trc.NewQueryResponse(req, nil)
			problems = append(problems, err.Error())
		}
		res.Duration = time.Since(begin)
		res.Problems = append(problems, res.Problems...)

		tr.Tracef("query finished: matched=%d selected=%d duration=%s", res.Matched, len(res.Selected), res.Duration)

		switch getBestContentType(r) {
		case "text/html":
			renderHTML(ctx, w, "traces.html", res)
		default:
			renderJSON(ctx, w, res)
		}
	})
}

func tracesHandlerStream(w http.ResponseWriter, r *http.Request) {
}
*/
