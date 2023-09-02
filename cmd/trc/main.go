package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/run"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"github.com/peterbourgon/ff/v4/ffval"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcweb"
)

func main() {
	var (
		ctx    = context.Background()
		stdin  = os.Stdin
		stdout = os.Stdout
		stderr = os.Stderr
		args   = os.Args[1:]
	)
	err := exec(ctx, stdin, stdout, stderr, args)
	switch {
	case err == nil:
		os.Exit(0)
	case errors.As(err, &(run.SignalError{})):
		os.Exit(0)
	case err != nil:
		fmt.Fprintf(stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func exec(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) (err error) {
	// Definitions.

	rootConfig := &rootConfig{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
	rootFlags := ff.NewFlags("trc")
	rootConfig.register(rootFlags)
	rootCommand := &ff.Command{
		Name:      "trc",
		ShortHelp: "query one or more trace endpoints for trace data",
		Usage:     "trc --uri=U [--uri=U ...] [<FLAGS>] <SUBCOMMAND> ...",
		Flags:     rootFlags,
	}

	searchConfig := &searchConfig{rootConfig: rootConfig}
	searchFlags := ff.NewFlags("search").SetParent(rootFlags)
	searchConfig.register(searchFlags)
	searchCommand := &ff.Command{
		Name:      "search",
		ShortHelp: "search for trace data",
		Usage:     "trc search [<FLAGS>]",
		Flags:     searchFlags,
		Exec:      searchConfig.Exec,
	}
	rootCommand.Subcommands = append(rootCommand.Subcommands, searchCommand)

	streamConfig := &streamConfig{rootConfig: rootConfig}
	streamFlags := ff.NewFlags("stream").SetParent(rootFlags)
	streamConfig.register(streamFlags)
	streamCommand := &ff.Command{
		Name:      "stream",
		ShortHelp: "stream trace data to the terminal",
		Usage:     "trc stream [<FLAGS>]",
		Flags:     streamFlags,
		Exec:      streamConfig.Exec,
	}
	rootCommand.Subcommands = append(rootCommand.Subcommands, streamCommand)

	showHelp := true
	defer func() {
		if showHelp {
			fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(rootCommand))
		}
		if errors.Is(err, ff.ErrHelp) || errors.Is(err, ff.ErrNoExec) {
			err = nil
		}
	}()

	// Root command parsing.

	if err := rootCommand.Parse(args, ff.WithEnvVarPrefix("TRC")); err != nil {
		return err
	}

	if len(rootConfig.uris) <= 0 {
		return fmt.Errorf("at least one URI is required")
	}

	{
		var infodst, debugdst, tracedst io.Writer
		switch rootConfig.logging {
		case "none":
			infodst, debugdst, tracedst = io.Discard, io.Discard, io.Discard
		case "info":
			infodst, debugdst, tracedst = stderr, io.Discard, io.Discard
		case "debug":
			infodst, debugdst, tracedst = stderr, stderr, io.Discard
		case "trace":
			infodst, debugdst, tracedst = stderr, stderr, stderr
		default:
			return fmt.Errorf("invalid logging value %q", rootConfig.logging)
		}
		rootConfig.info = log.New(infodst, "", 0)
		rootConfig.debug = log.New(debugdst, "[DEBUG] ", log.Lmsgprefix)
		rootConfig.trace = log.New(tracedst, "[TRACE] ", log.Lmsgprefix)

	}

	{
		var minDuration *time.Duration
		if f, ok := rootFlags.GetFlag("duration"); ok && f.IsSet() {
			rootConfig.debug.Printf("using --duration %s", rootConfig.minDuration)
			minDuration = &rootConfig.minDuration
		}

		rootConfig.filter = trc.Filter{
			Sources:     rootConfig.sources,
			IDs:         rootConfig.ids,
			Category:    rootConfig.category,
			IsActive:    rootConfig.isActive,
			IsFinished:  rootConfig.isFinished,
			MinDuration: minDuration,
			IsSuccess:   rootConfig.isSuccess,
			IsErrored:   rootConfig.isErrored,
			Query:       rootConfig.query,
		}
	}

	// Selected command parsing.

	// Running.

	showHelp = false

	if err := rootCommand.Run(ctx); err != nil {
		return err
	}

	return nil
}

type rootConfig struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	uris        []string
	sources     []string
	ids         []string
	category    string
	query       string
	isActive    bool
	isFinished  bool
	minDuration time.Duration
	isSuccess   bool
	isErrored   bool
	logging     string
	output      string

	filter trc.Filter

	info, debug, trace *log.Logger
}

func (cfg *rootConfig) register(fs *ff.CoreFlags) {
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'u', LongName: "uri" /*        */, Value: ffval.NewUniqueList(&cfg.uris) /*                                */, Usage: "trace server URI (repeatable, required)"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "source" /*     */, Value: ffval.NewUniqueList(&cfg.sources) /*                             */, Usage: "filter for this source (repeatable)"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'i', LongName: "id" /*         */, Value: ffval.NewUniqueList(&cfg.ids) /*                                 */, Usage: "filter for this ID (repeatable)"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'c', LongName: "category" /*   */, Value: ffval.NewValue(&cfg.category) /*                                 */, Usage: "filter for this category"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'q', LongName: "query" /*      */, Value: ffval.NewValue(&cfg.query) /*                                    */, Usage: "filter for this query regexp"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'a', LongName: "active" /*     */, Value: ffval.NewValue(&cfg.isActive) /*                                 */, Usage: "filter for active traces", NoDefault: true})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'f', LongName: "finished" /*   */, Value: ffval.NewValue(&cfg.isFinished) /*                               */, Usage: "filter for finished traces", NoDefault: true})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'd', LongName: "duration" /*   */, Value: ffval.NewValue(&cfg.minDuration) /*                              */, Usage: "filter for finished traces of at least this duration", NoDefault: true})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "success" /*    */, Value: ffval.NewValue(&cfg.isSuccess) /*                                */, Usage: "filter for successful (non-errored) traces", NoDefault: true})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "errored" /*    */, Value: ffval.NewValue(&cfg.isErrored) /*                                */, Usage: "filter for errored traces", NoDefault: true})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'l', LongName: "log" /*        */, Value: ffval.NewEnum(&cfg.logging, "info", "debug", "trace", "none") /* */, Usage: "log level: info, debug, trace, none"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'o', LongName: "output" /*     */, Value: ffval.NewEnum(&cfg.output, "ndjson", "prettyjson") /*            */, Usage: "output format: ndjson, prettyjson"})
}

type searchConfig struct {
	*rootConfig

	//
}

func (cfg *searchConfig) register(fs *ff.CoreFlags) {
	//
}

func (cfg *searchConfig) Exec(ctx context.Context, args []string) error {
	return fmt.Errorf("not implemented")
}

type streamConfig struct {
	*rootConfig

	recvBuf       int
	sendBuf       int
	statsInterval time.Duration
	retryInterval time.Duration
	streamEvents  bool

	traces chan trc.Trace
}

func (cfg *streamConfig) register(fs *ff.CoreFlags) {
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "recvbuf" /* */, Value: ffval.NewValueDefault(&cfg.recvBuf, 100) /*                  */, Usage: "local receive buffer size"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "sendbuf" /* */, Value: ffval.NewValueDefault(&cfg.sendBuf, 100) /*                  */, Usage: "remote send buffer size"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "stats" /*   */, Value: ffval.NewValueDefault(&cfg.statsInterval, 10*time.Second) /* */, Usage: "stream stats reporting interval"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "retry" /*   */, Value: ffval.NewValueDefault(&cfg.retryInterval, 1*time.Second) /*  */, Usage: "stream connection retry interval"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'e', LongName: "events" /*  */, Value: ffval.NewValue(&cfg.streamEvents) /*                         */, Usage: "stream events instead of traces (overrides --active and --finished)", NoDefault: true})
}

func (cfg *streamConfig) Exec(ctx context.Context, args []string) error {
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
		cfg.info.Printf("streaming %s with %s", streaming, cfg.filter)
		cfg.debug.Printf("send buffer: %d", cfg.sendBuf)
		cfg.debug.Printf("recv buffer: %d", cfg.recvBuf)
		cfg.debug.Printf("stats interval: %s", cfg.statsInterval)
		cfg.debug.Printf("retry interval: %s", cfg.retryInterval)
		cfg.debug.Printf("output format: %s", cfg.output)
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
	{
		g.Add(run.SignalHandler(ctx, os.Interrupt, os.Kill))
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
	ctx, tr := trc.New(ctx, "stream", uri, trc.LogDecorator(&logWriter{cfg.trace}))
	defer tr.Finish()

	var lastData atomic.Value
	onRead := func(ctx context.Context, eventType string, eventData []byte) {
		lastData.Store(time.Now())
	}

	reporterDone := make(chan struct{})
	go func() {
		defer close(reporterDone)
		ticker := time.NewTicker(cfg.statsInterval)
		defer ticker.Stop()
		for {
			select {
			case ts := <-ticker.C:
				last, ok := lastData.Load().(time.Time)
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

	cfg.info.Printf("%s: starting", uri)
	defer cfg.info.Printf("%s: stopped", uri)

	c := &trcweb.StreamClient{
		HTTPClient:    http.DefaultClient,
		URI:           uri,
		SendBuffer:    cfg.sendBuf,
		OnRead:        onRead,
		RetryInterval: cfg.retryInterval,
		StatsInterval: cfg.statsInterval,
	}

	for ctx.Err() == nil {
		subctx, cancel := context.WithCancel(ctx)                        // per-iteration sub-context
		errc := make(chan error, 1)                                      // per-iteration stream result
		go func() { errc <- c.Stream(subctx, cfg.filter, cfg.traces) }() // returns only on terminal errors

		select {
		case <-subctx.Done():
			cfg.debug.Printf("%s: done", uri) // parent context was canceled, so we should stop
			cancel()                          // signal the Stream goroutine to stop
			<-errc                            // wait for it to stop
			return                            // we're done

		case err := <-errc:
			cfg.debug.Printf("%s: retry (%v)", uri, err) // our stream failed (usually) independently, so we try again
			cancel()                                     // just to be safe
			contextSleep(subctx, cfg.retryInterval)      // can be interrupted by parent context
			continue                                     // try again
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

	for {
		select {
		case tr := <-cfg.traces:
			encode(tr)
		case <-ctx.Done():
			return ctx.Err()
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
	w.Logger.Print(string(p))
	return len(p), nil
}
