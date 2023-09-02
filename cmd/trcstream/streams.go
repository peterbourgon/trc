package main

import (
	"context"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcweb"
)

type streamsConfig struct {
	URIs          []string
	Filter        trc.Filter
	Traces        chan trc.Trace
	SendBuffer    int
	RetryInterval time.Duration
	StatsInterval time.Duration
	Info          *log.Logger
	Debug         *log.Logger
	Trace         *log.Logger
}

func runStreams(ctx context.Context, cfg streamsConfig) error {
	subctx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup
	for _, uri := range cfg.URIs {
		wg.Add(1)
		go func(uri string) {
			defer wg.Done()
			runStream(subctx, streamConfig{
				URI:           uri,
				Filter:        cfg.Filter,
				Traces:        cfg.Traces,
				SendBuffer:    cfg.SendBuffer,
				RetryInterval: cfg.RetryInterval,
				StatsInterval: cfg.StatsInterval,
				Info:          cfg.Info,
				Debug:         cfg.Debug,
				Trace:         cfg.Trace,
			})
		}(uri)
	}

	cfg.Debug.Printf("started streams")
	<-ctx.Done()
	cfg.Debug.Printf("stopping streams...")
	cancel()
	wg.Wait()
	cfg.Debug.Printf("streams finished")
	return nil
}

//
//
//

type streamConfig struct {
	URI           string
	Filter        trc.Filter
	Traces        chan trc.Trace
	SendBuffer    int
	RetryInterval time.Duration
	StatsInterval time.Duration
	Info          *log.Logger
	Debug         *log.Logger
	Trace         *log.Logger
}

func runStream(ctx context.Context, cfg streamConfig) {
	ctx, tr := trc.New(ctx, "stream", cfg.URI, trc.LogDecorator(&logWriter{cfg.Trace}))
	defer tr.Finish()

	var lastData atomic.Value
	onRead := func(ctx context.Context, eventType string, eventData []byte) {
		tr.Tracef("onRead %s (%dB)", eventType, len(eventData))
		lastData.Store(time.Now())
	}

	reporterDone := make(chan struct{})
	go func() {
		defer close(reporterDone)
		ticker := time.NewTicker(cfg.StatsInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ts, ok := lastData.Load().(time.Time)
				switch {
				case !ok:
					cfg.Debug.Printf("%s: no data", cfg.URI)
				case ts.Before(time.Now().Add(-2 * cfg.StatsInterval)):
					cfg.Debug.Printf("%s: last data %s ago", cfg.URI, time.Since(ts).Truncate(100*time.Millisecond))
				default:
					// ok
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() { <-reporterDone }()

	cfg.Info.Printf("%s: stream starting", cfg.URI)
	defer cfg.Info.Printf("%s: stream stopped", cfg.URI)

	c := &trcweb.StreamClient{
		HTTPClient:    http.DefaultClient,
		URI:           cfg.URI,
		SendBuffer:    cfg.SendBuffer,
		OnRead:        onRead,
		RetryInterval: cfg.RetryInterval,
		StatsInterval: cfg.StatsInterval,
	}

	for ctx.Err() == nil {
		subctx, cancel := context.WithCancel(ctx)                        // per-iteration sub-context
		errc := make(chan error, 1)                                      // per-iteration stream result
		go func() { errc <- c.Stream(subctx, cfg.Filter, cfg.Traces) }() // returns only on terminal errors

		select {
		case <-subctx.Done():
			cfg.Debug.Printf("%s: done", cfg.URI) // parent context was canceled, so we should stop
			cancel()                              // signal the Stream goroutine to stop
			<-errc                                // wait for it to stop
			return                                // we're done

		case err := <-errc:
			cfg.Debug.Printf("%s: retry (%v)", cfg.URI, err) // our stream failed (usually) independently, so we try again
			cancel()                                         // just to be safe
			contextSleep(subctx, cfg.RetryInterval)          // can be interrupted by parent context
			continue                                         // try again
		}
	}
}

func contextSleep(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

type logWriter struct{ *log.Logger }

func (w *logWriter) Write(p []byte) (int, error) {
	w.Logger.Printf("%s", string(p))
	return len(p), nil
}
