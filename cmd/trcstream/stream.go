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
	RemoteBuffer  int
	StatsInterval time.Duration
	RetryInterval time.Duration
	Info          *log.Logger
	Debug         *log.Logger
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
				RemoteBuffer:  cfg.RemoteBuffer,
				StatsInterval: cfg.StatsInterval,
				RetryInterval: cfg.RetryInterval,
				Info:          cfg.Info,
				Debug:         cfg.Debug,
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
	RemoteBuffer  int
	StatsInterval time.Duration
	RetryInterval time.Duration
	Info          *log.Logger
	Debug         *log.Logger
}

func runStream(ctx context.Context, cfg streamConfig) {
	var lastData atomic.Value
	onRead := func(eventType string, eventData []byte) { lastData.Store(time.Now()) }

	reporterDone := make(chan struct{})
	go func() {
		defer close(reporterDone)
		ticker := time.NewTicker(cfg.StatsInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if ts, ok := lastData.Load().(time.Time); ok {
					cfg.Debug.Printf("%s: last data %s ago", cfg.URI, time.Since(ts).Truncate(100*time.Millisecond))
				} else {
					cfg.Debug.Printf("%s: no data received yet", cfg.URI)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() { <-reporterDone }()

	cfg.Info.Printf("stream %s starting", cfg.URI)
	defer cfg.Info.Printf("stream %s stopped", cfg.URI)

	c := &trcweb.StreamClient{
		HTTPClient:    http.DefaultClient,
		URI:           cfg.URI,
		RemoteBuffer:  cfg.RemoteBuffer,
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
			contextSleep(subctx, 5*time.Second)              // can be interrupted by parent context
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
