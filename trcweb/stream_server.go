package trcweb

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/bernerdschaefer/eventsource"
	"github.com/peterbourgon/trc"
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

	if !requestExplicitlyAccepts(r, "text/event-stream") {
		http.Error(w, "request must Accept: text/event-stream", http.StatusPreconditionRequired)
		return
	}

	var (
		ctx = r.Context()
		tr  = trc.Get(ctx)
		buf = parseDefault(r.URL.Query().Get("buf"), strconv.Atoi, 10)
		c   = make(chan trc.Trace, buf)
		f   = trc.Filter{} // TODO
	)

	tr.Tracef("subscribing, buf %d", buf)

	if err := s.b.Subscribe(ctx, c, f); err != nil {
		err = fmt.Errorf("subscribe: %w", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	defer func() {
		skips, sends, drops, err := s.b.Unsubscribe(ctx, c)
		switch {
		case err == nil:
			tr.Tracef("unsubscribe OK: skips %d, sends %d, drops %d", skips, sends, drops)
		case err != nil:
			tr.Errorf("unsubscribe error: %v", err)
		}
	}()

	tr.Tracef("starting event source handler...")

	eventsource.Handler(func(lastId string, encoder *eventsource.Encoder, stop <-chan bool) {
		var seq uint64
		for {
			select {
			case recv := <-c:
				tr.Tracef("recv trace ID %s, events count %d, finished %v", recv.ID(), len(recv.Events()), recv.Finished())
				seq++
				str := trc.NewSelectedTrace(recv).TrimStacks(-1)
				data, err := json.Marshal(str)
				if err != nil {
					tr.Errorf("JSON marshal trace: %v", err)
					continue
				}
				if err := encoder.Encode(eventsource.Event{
					Type: "trace",
					ID:   strconv.FormatUint(seq, 10),
					Data: data,
				}); err != nil {
					tr.Errorf("encode trace: %v", err)
					continue
				}

			case <-ctx.Done():
				tr.Tracef("stopping: context done: %v", ctx.Err())
				return

			case <-stop:
				tr.Tracef("stopping: stop signal")
				return
			}
		}
	}).ServeHTTP(w, r)
}
