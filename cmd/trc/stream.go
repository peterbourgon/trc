package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/oklog/run"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffval"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
	"github.com/peterbourgon/trc/trcstream"
)

type streamConfig struct {
	*rootConfig

	streamEvents  bool
	sendBuf       int
	recvBuf       int
	statsInterval time.Duration
	retryInterval time.Duration

	traces chan trc.Trace
}

func (cfg *streamConfig) register(fs *ff.FlagSet) {
	fs.AddFlag(ff.FlagConfig{ShortName: 'e', LongName: "events" /*         */, Value: ffval.NewValue(&cfg.streamEvents) /*                         */, Usage: "stream individual events rather than complete traces", NoDefault: true})
	fs.AddFlag(ff.FlagConfig{ShortName: 0x0, LongName: "send-buffer" /*    */, Value: ffval.NewValueDefault(&cfg.sendBuf, 100) /*                  */, Usage: "remote send buffer size"})
	fs.AddFlag(ff.FlagConfig{ShortName: 0x0, LongName: "recv-buffer" /*    */, Value: ffval.NewValueDefault(&cfg.recvBuf, 100) /*                  */, Usage: "local receive buffer size"})
	fs.AddFlag(ff.FlagConfig{ShortName: 0x0, LongName: "stats-interval" /* */, Value: ffval.NewValueDefault(&cfg.statsInterval, 10*time.Second) /* */, Usage: "stats reporting interval"})
	fs.AddFlag(ff.FlagConfig{ShortName: 0x0, LongName: "retry-interval" /* */, Value: ffval.NewValueDefault(&cfg.retryInterval, 1*time.Second) /*  */, Usage: "connection retry interval"})
}

func (cfg *streamConfig) Exec(ctx context.Context, args []string) error {
	ctx, tr := cfg.newTrace(ctx, "stream")
	defer tr.Finish()

	cfg.traces = make(chan trc.Trace, cfg.recvBuf)

	var streaming string
	{
		// IsActive rejects the final trace, which we always want. IsFinished
		// rejects every trace except the last one, which is what we want to
		// control by the streamEvents flag.
		cfg.filter.IsActive = false
		if cfg.streamEvents {
			streaming = "events"
			cfg.filter.IsFinished = false
		} else {
			streaming = "traces"
			cfg.filter.IsFinished = true
		}
	}
	{
		cfg.info.Printf("streaming: %s", streaming)
		cfg.info.Printf("filter: %s", cfg.filter)
		cfg.debug.Printf("send buffer: %d", cfg.sendBuf)
		cfg.debug.Printf("recv buffer: %d", cfg.recvBuf)
		cfg.debug.Printf("stats interval: %s", cfg.statsInterval)
		cfg.debug.Printf("retry interval: %s", cfg.retryInterval)
	}

	cfg.debug.Printf("starting streams")

	var g run.Group

	{
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			return cfg.runStreams(ctx)
		}, func(error) {
			cancel()
		})
	}

	{
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			return cfg.writeTraces(ctx)
		}, func(error) {
			cancel()
		})
	}

	// {
	// ctx, cancel := context.WithCancel(ctx)
	// g.Add(func() error {
	// sig := make(chan os.Signal, 1)
	// signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	// select {
	// case <-ctx.Done():
	// return ctx.Err()
	// case s := <-sig:
	// return fmt.Errorf("received signalz %s", s)
	// }
	// }, func(err error) {
	// cancel()
	// })
	// }

	{
		g.Add(run.SignalHandler(ctx, syscall.SIGINT, syscall.SIGTERM))
	}

	return g.Run()
}

func (cfg *streamConfig) runStreams(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup
	for _, uri := range cfg.uris {
		wg.Add(1)
		go func(uri string) {
			defer wg.Done()
			cfg.runStream(ctx, uri)
		}(uri)
	}

	cfg.debug.Printf("started streams")
	<-ctx.Done()
	cfg.debug.Printf("stopping streams...")
	cancel()
	wg.Wait()
	cfg.debug.Printf("streams finished")
	return nil
}

func (cfg *streamConfig) runStream(ctx context.Context, uri string) {
	ctx, _ = trc.Prefix(ctx, "<%s>", uri)

	var (
		lastDataTime atomic.Value
		initCount    int
	)

	// This function is called on every received event.
	onRead := func(ctx context.Context, eventType string, eventData []byte) {
		lastDataTime.Store(time.Now())

		switch eventType {
		case "init":
			if initCount == 0 {
				cfg.debug.Printf("%s: stream connected", uri)
			} else {
				cfg.debug.Printf("%s: stream reconnected", uri)
			}
			initCount++

		case "stats":
			var stats trcstream.Stats
			if err := json.Unmarshal(eventData, &stats); err != nil {
				cfg.debug.Printf("%s: stats error: %v", uri, err)
			} else {
				cfg.debug.Printf("%s: %s", uri, stats)
			}
		}
	}

	// This goroutine reports if it's been too long without any data.
	reporterDone := make(chan struct{})
	go func() {
		defer close(reporterDone)

		ticker := time.NewTicker(cfg.statsInterval)
		defer ticker.Stop()

		for {
			select {
			case ts := <-ticker.C:
				last, ok := lastDataTime.Load().(time.Time)
				delta := ts.Sub(last)
				switch {
				case !ok:
					cfg.debug.Printf("%s: no data", uri)
				case delta > 2*cfg.statsInterval:
					cfg.debug.Printf("%s: last data %s ago", uri, delta.Truncate(100*time.Millisecond))
				}

			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() {
		<-reporterDone
	}()

	cfg.debug.Printf("%s: starting", uri)
	defer cfg.debug.Printf("%s: stopped", uri)

	sc := &trchttp.StreamClient{
		HTTPClient:    http.DefaultClient,
		URI:           uri,
		SendBuffer:    cfg.sendBuf,
		OnRead:        onRead,
		RetryInterval: cfg.retryInterval,
		StatsInterval: cfg.statsInterval,
	}

	for ctx.Err() == nil {
		subctx, cancel := context.WithCancel(ctx)                         // per-iteration sub-context
		errc := make(chan error, 1)                                       // per-iteration stream result
		go func() { errc <- sc.Stream(subctx, cfg.filter, cfg.traces) }() // returns only on terminal errors

		select {
		case <-subctx.Done():
			cfg.debug.Printf("%s: stream done", uri) // parent context was canceled, so we should stop
			cancel()                                 // signal the Stream goroutine to stop
			<-errc                                   // wait for it to stop
			return                                   // we're done

		case err := <-errc:
			cfg.debug.Printf("%s: stream error, will retry (%v)", uri, err) // our stream failed (usually) independently, so we try again
			cancel()                                                        // just to be safe, but note this means contextSleep needs ctx, not subctx
			contextSleep(ctx, cfg.retryInterval)                            // can be interrupted by parent context
			continue                                                        // try again
		}
	}
}

func (cfg *streamConfig) writeTraces(ctx context.Context) error {
	var encode func(tr trc.Trace)
	switch cfg.output {
	case "ndjson":
		enc := json.NewEncoder(cfg.stdout)
		encode = func(tr trc.Trace) { enc.Encode(tr) }
	case "prettyjson":
		enc := json.NewEncoder(cfg.stdout)
		enc.SetIndent("", "    ")
		encode = func(tr trc.Trace) { enc.Encode(tr) }
	default:
		encode = func(tr trc.Trace) {}
	}

	var count uint64
	for {
		select {
		case tr := <-cfg.traces:
			count++
			encode(tr)
		case <-ctx.Done():
			cfg.debug.Printf("emitted trace count %d", count)
			return ctx.Err()
		}
	}
}
