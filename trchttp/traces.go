package trchttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	QueryTraces(ctx context.Context, qtreq *trc.QueryTracesRequest) (*trc.QueryTracesResponse, error)
}

type TraceStreamer interface {
	Subscribe(ctx context.Context, ch chan<- trc.Trace) error
	Unsubscribe(ctx context.Context, ch chan<- trc.Trace) (sends, drops uint64, _ error)
	Subscription(ctx context.Context, ch chan<- trc.Trace) (sends, drops uint64, _ error)
}

func TracesHandler(c TraceCollector) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tr := trc.FromContext(ctx)
		defer tr.Finish()

		var (
			begin       = time.Now()
			urlquery    = r.URL.Query()
			limit       = parseDefault(urlquery.Get("n"), strconv.Atoi, 10)
			minDuration = parseDefault(urlquery.Get("min"), parseDurationPointer, nil)
			bucketing   = parseBucketing(urlquery["b"])
			search      = urlquery.Get("q")
			remotes     = urlquery["r"]
			problems    = []string{}
		)

		qtreq := &trc.QueryTracesRequest{
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
			if err := json.NewDecoder(r.Body).Decode(&qtreq); err != nil {
				err = fmt.Errorf("parse JSON request from body: %w", err)
				problems = append(problems, err.Error())
				tr.Errorf(err.Error())
			}
		}

		if err := qtreq.Sanitize(); err != nil {
			err = fmt.Errorf("sanitize request: %w", err)
			problems = append(problems, err.Error())
			tr.Errorf(err.Error())
		}

		var collector TraceCollector = c // default collector
		if len(remotes) > 0 {
			tr.Tracef("remotes count %d, using explicit distributed trace collector")
			collector = NewDistributedCollector(http.DefaultClient, remotes...)
		}

		if requestExplicitlyAccepts(r, "text/event-stream") {
			tr.Tracef("text/event-stream request, using SSE handler")
			streamTraces(r, qtreq, collector, w) // TODO: distributed streamer
			return                               // TODO: superfluous WriteHeader call from trchttp.interceptor.WriteHeader
		}

		tr.Tracef("query starting: %s", qtreq)

		qtres, err := collector.QueryTraces(ctx, qtreq)
		if err != nil {
			tr.Errorf("query errored: %v", err)
			qtres = trc.NewQueryTracesResponse(qtreq, nil)
			problems = append(problems, err.Error())
		}
		qtres.Duration = time.Since(begin)
		qtres.Problems = append(problems, qtres.Problems...)

		tr.Tracef("query finished: matched=%d selected=%d duration=%s", qtres.Matched, len(qtres.Selected), qtres.Duration)

		switch {
		case requestExplicitlyAccepts(r, "text/html"):
			renderHTML(ctx, w, "traces.html", qtres)
		default:
			renderJSON(ctx, w, qtres)
		}
	})
}

func streamTraces(r *http.Request, qtreq *trc.QueryTracesRequest, s TraceStreamer, w http.ResponseWriter) {
	eventsource.Handler(func(lastID string, enc *eventsource.Encoder, stop <-chan bool) {
		ctx := r.Context()
		tr := trc.PrefixTracef(trc.FromContext(ctx), "[stream]")
		defer tr.Finish()

		tr.Tracef("%s %s", r.Method, r.URL.String())

		ch := make(chan trc.Trace, 1000)

		if err := s.Subscribe(ctx, ch); err != nil {
			tr.Errorf("subscribe: err=%v", err)
			http.Error(w, fmt.Sprintf("subscribe: %v", err), http.StatusInternalServerError)
			return
		}

		defer func(begin time.Time) {
			sends, drops, err := s.Unsubscribe(ctx, ch)
			tr.Tracef("unsubscribe: sends=%d drops=%d err=%v", sends, drops, err)
		}(time.Now())

		var (
			beats    uint64
			filtered uint64
			emitted  uint64
			sends    uint64
			drops    uint64
		)

		defer func() {
			tr.Tracef("stream end: beats=%d filtered=%d emitted=%v", beats, filtered, emitted)
		}()

		heartbeatTicker := time.NewTicker(time.Second)
		defer heartbeatTicker.Stop()

		traceTicker := time.NewTicker(time.Second)
		defer traceTicker.Stop()

		tr.Tracef("starting trace stream")

		for {
			select {
			case tr := <-ch:
				sent, err := maybeSendTrace(ctx, enc, r, qtreq, tr)
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

			case <-traceTicker.C:
				tr.Tracef("beats=%d filtered=%d emitted=%d", beats, filtered, emitted)

			case ts := <-heartbeatTicker.C:
				s, d, err := s.Subscription(ctx, ch)
				if err != nil {
					tr.Tracef("subscription stats: %v", err)
					return
				}
				beats, sends, drops = beats+1, s, d
				if err := sendHeartbeat(ctx, enc, ts, beats, sends, drops, filtered, emitted); err != nil {
					tr.Tracef("send heartbeat error: %v", err)
					return
				}

			case <-stop:
				tr.Tracef("stopped")
				return

			case <-ctx.Done():
				tr.Tracef("context done")
				return
			}
		}
	}).ServeHTTP(w, r)
}

const (
	eventTypeTraceJSON = "trace.json.v1"
	eventTypeTraceHTML = "trace.html.v1"
	eventTypeHeartbeat = "heartbeat.v1"
)

func maybeSendTrace(ctx context.Context, enc *eventsource.Encoder, r *http.Request, qtreq *trc.QueryTracesRequest, tr trc.Trace) (bool, error) {
	ctxtr := trc.FromContext(ctx)

	if !qtreq.Allow(tr) {
		return false, nil
	}

	staticTrace := trc.NewTraceStatic(tr)
	staticTrace.IsStreamed = true

	var (
		eventType string
		eventData []byte
		err       error
	)
	switch {
	case r.URL.Query().Has("json"):
		ctxtr.Tracef("building JSON trace")
		eventType = eventTypeTraceJSON
		eventData, err = json.Marshal(staticTrace)
	default:
		ctxtr.Tracef("building HTML trace")
		eventType = eventTypeTraceHTML
		eventData, err = renderTemplate("traces.trace.html", staticTrace)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "### render err %v\n", err)
		ctxtr.Errorf("render trace error: %v", err)
		return false, fmt.Errorf("render trace: %w", err)
	}

	if err := enc.Encode(eventsource.Event{
		Type: eventType,
		Data: eventData,
	}); err != nil {
		ctxtr.Errorf("encode trace error: %v", err)
		return false, fmt.Errorf("encode event: %w", err)
	}

	return true, nil
}

func sendHeartbeat(ctx context.Context, enc *eventsource.Encoder, ts time.Time, beats, sends, drops, filtered, emitted uint64) error {
	data, err := json.Marshal(struct {
		Timestamp time.Time `json:"ts"`
		Beats     uint64    `json:"beats"`
		Sends     uint64    `json:"sends"`
		Drops     uint64    `json:"drops"`
		Filtered  uint64    `json:"filtered"`
		Emitted   uint64    `json:"emitted"`
	}{
		Timestamp: ts,
		Beats:     beats,
		Sends:     sends,
		Drops:     drops,
		Filtered:  filtered,
		Emitted:   emitted,
	})
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}

	if err := enc.Encode(eventsource.Event{
		Type: eventTypeHeartbeat,
		Data: data,
	}); err != nil {
		return fmt.Errorf("encode heartbeat: %w", err)
	}

	return nil
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
