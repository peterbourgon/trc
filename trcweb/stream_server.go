package trcweb

/*
type StreamServer struct {
	streamer Streamer
}

type Streamer interface {
	Stream(ctx context.Context, f trc.Filter, ch chan<- trc.Trace) (trc.Stats, error)
	StreamStats(ctx context.Context, ch chan<- trc.Trace) (trc.Stats, error)
}

func NewStreamServer(s Streamer) *StreamServer {
	return &StreamServer{
		streamer: s,
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

	var (
		statsInterval = parseDefault(r.URL.Query().Get("stats"), time.ParseDuration, 10*time.Second)
	)

	tr.Tracef("stats %s", statsInterval)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		stats, err := s.streamer.Stream(ctx, f, tracec)
		tr.Tracef("Stream finished (%v), skips %d, sends %d, drops %d", err, stats.Skips, stats.Sends, stats.Drops)
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
				stats, err := s.streamer.StreamStats(ctx, tracec)
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

/*
// StreamClient streams trace data from a [StreamServer].
type StreamClient struct {
	// HTTPClient used to make the stream request. If not provided,
	// [http.DefaultClient] is used.
	HTTPClient HTTPClient

	// URI of the remote stream server. Required.
	URI string

	// SendBuffer sent to the remote stream server. Optional.
	SendBuffer int

	// OnRead is called for every stream event received by the client.
	// Implementations must not block.
	OnRead func(ctx context.Context, eventType string, eventData []byte)

	// RetryInterval is the delay between stream reconnection attempts. The
	// default value is 1s.
	RetryInterval time.Duration

	// StatsInterval is how often stream stats are sent from the server to the
	// client. The default value is 10s.
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
		c.OnRead = func(ctx context.Context, eventType string, eventData []byte) {}
	}

	if c.RetryInterval == 0 {
		c.RetryInterval = time.Second
	}

	if c.StatsInterval == 0 {
		c.StatsInterval = 10 * time.Second
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
			query.Set("buf", strconv.Itoa(c.SendBuffer))
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
*/
