package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
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
	case err == nil, errors.Is(err, context.Canceled), errors.As(err, &(run.SignalError{})):
		os.Exit(0)
	case err != nil:
		fmt.Fprintf(stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func exec(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) (err error) {
	// Config for `trc`.
	rootConfig := &rootConfig{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}

	baseFlags := ff.NewFlags("base")
	rootConfig.registerBaseFlags(baseFlags)

	queryFlags := ff.NewFlags("query").SetParent(baseFlags)
	rootConfig.registerQueryFlags(queryFlags)

	trcFlags := queryFlags
	trcCommand := &ff.Command{
		Name:      "trc",
		ShortHelp: "query trace data from one or more instances",
		Flags:     trcFlags,
	}

	// Config for `trc search`.
	searchConfig := &searchConfig{rootConfig: rootConfig}
	searchFlags := ff.NewFlags("search").SetParent(trcFlags)
	searchConfig.register(searchFlags)
	searchCommand := &ff.Command{
		Name:      "search",
		ShortHelp: "search for trace data",
		LongHelp:  "Fetch traces that match the provided query flags.",
		Flags:     searchFlags,
		Exec:      searchConfig.Exec,
	}
	trcCommand.Subcommands = append(trcCommand.Subcommands, searchCommand)

	// Config for `trc stream`.
	streamConfig := &streamConfig{rootConfig: rootConfig}
	streamFlags := ff.NewFlags("stream").SetParent(trcFlags)
	streamConfig.register(streamFlags)
	streamCommand := &ff.Command{
		Name:      "stream",
		ShortHelp: "stream trace data to the terminal",
		LongHelp:  "Stream traces, or trace events, that match the provided query flags.",
		Flags:     streamFlags,
		Exec:      streamConfig.Exec,
	}
	trcCommand.Subcommands = append(trcCommand.Subcommands, streamCommand)

	// Errors should show help only in some cases.
	showHelp := true
	defer func() {
		if showHelp {
			fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(trcCommand))
		}
		if errors.Is(err, ff.ErrHelp) || errors.Is(err, ff.ErrNoExec) {
			err = nil
		}
	}()

	// Initial parsing.
	if err := trcCommand.Parse(args, ff.WithEnvVarPrefix("TRC")); err != nil {
		return err
	}

	// Validation and set-up.
	{
		var infodst, debugdst, tracedst io.Writer
		switch rootConfig.logLevel {
		case "n", "none":
			infodst, debugdst, tracedst = io.Discard, io.Discard, io.Discard
		case "i", "info":
			infodst, debugdst, tracedst = stderr, io.Discard, io.Discard
		case "d", "debug":
			infodst, debugdst, tracedst = stderr, stderr, io.Discard
		case "t", "trace":
			infodst, debugdst, tracedst = stderr, stderr, stderr
		default:
			return fmt.Errorf("invalid log level %q", rootConfig.logLevel)
		}
		rootConfig.info = log.New(infodst, "", 0)
		rootConfig.debug = log.New(debugdst, "[DEBUG] ", log.Lmsgprefix)
		rootConfig.trace = log.New(tracedst, "[TRACE] ", log.Lmsgprefix)
	}

	if len(rootConfig.uris) <= 0 {
		return fmt.Errorf("at least one URI is required")
	}

	for i, uri := range rootConfig.uris {
		uri = strings.TrimSpace(uri)
		if uri == "" {
			continue
		}

		if !strings.HasPrefix(uri, "http") {
			uri = "http://" + uri
		}

		u, err := url.ParseRequestURI(uri)
		if err != nil {
			return fmt.Errorf("%s: invalid: %w", uri, err)
		}

		if rootConfig.uriPath != "" {
			u.Path = rootConfig.uriPath
		}

		uri = u.String()
		rootConfig.uris[i] = uri

		rootConfig.debug.Printf("URI: %s", uri)
	}

	{
		var minDuration *time.Duration
		if f, ok := queryFlags.GetFlag("duration"); ok && f.IsSet() {
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

	// Past this point, errors are from the command, and shouldn't show help.
	showHelp = false

	// Run the selected command.
	return trcCommand.Run(ctx)
}

type rootConfig struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	uris     []string
	uriPath  string
	logLevel string
	output   string

	info, debug, trace *log.Logger

	sources     []string
	ids         []string
	category    string
	query       string
	isActive    bool
	isFinished  bool
	minDuration time.Duration
	isSuccess   bool
	isErrored   bool

	filter trc.Filter
}

func (cfg *rootConfig) registerBaseFlags(fs *ff.CoreFlags) {
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'u', LongName: "uri" /*      */, Value: ffval.NewUniqueList(&cfg.uris) /*                                                     */, Usage: "trace server URI (repeatable, required)" /*     */, Placeholder: "URI"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "uri-path" /* */, Value: ffval.NewValue(&cfg.uriPath) /*                                                       */, Usage: "path that will be applied to every URI" /*      */, Placeholder: "PATH"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'l', LongName: "log" /*      */, Value: ffval.NewEnum(&cfg.logLevel, "info", "i", "debug", "d", "trace", "t", "none", "n") /* */, Usage: "log level: i/info, d/debug, t/trace, n/none" /* */, Placeholder: "LEVEL"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'o', LongName: "output" /*   */, Value: ffval.NewEnum(&cfg.output, "ndjson", "prettyjson") /*                                 */, Usage: "output format: ndjson, prettyjson" /*           */, Placeholder: "FORMAT"})
}

func (cfg *rootConfig) registerQueryFlags(fs *ff.CoreFlags) {
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "source" /*   */, Value: ffval.NewUniqueList(&cfg.sources) /* */, NoDefault: true, Usage: "source (repeatable)"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'i', LongName: "id" /*       */, Value: ffval.NewUniqueList(&cfg.ids) /*     */, NoDefault: true, Usage: "trace ID (repeatable)"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'c', LongName: "category" /* */, Value: ffval.NewValue(&cfg.category) /*     */, NoDefault: true, Usage: "trace category"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'q', LongName: "query" /*    */, Value: ffval.NewValue(&cfg.query) /*        */, NoDefault: true, Usage: "query expression", Placeholder: "REGEX"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'a', LongName: "active" /*   */, Value: ffval.NewValue(&cfg.isActive) /*     */, NoDefault: true, Usage: "only active traces"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'f', LongName: "finished" /* */, Value: ffval.NewValue(&cfg.isFinished) /*   */, NoDefault: true, Usage: "only finished traces"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'd', LongName: "duration" /* */, Value: ffval.NewValue(&cfg.minDuration) /*  */, NoDefault: true, Usage: "only finished traces of at least this duration"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "success" /*  */, Value: ffval.NewValue(&cfg.isSuccess) /*    */, NoDefault: true, Usage: "only successful (non-errored) traces"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "errored" /*  */, Value: ffval.NewValue(&cfg.isErrored) /*    */, NoDefault: true, Usage: "only errored traces"})
}

func (cfg *rootConfig) newTrace(ctx context.Context, category string) (context.Context, trc.Trace) {
	ctx, tr := trc.New(ctx, "trc", category)
	tr = trc.LogDecorator(&logWriter{Logger: cfg.trace})(tr)
	ctx, tr = trc.Put(ctx, tr)
	return ctx, tr
}

//
//
//

type searchConfig struct {
	*rootConfig

	limit          int
	stackDepth     int
	includeRequest bool
	includeStats   bool
}

func (cfg *searchConfig) register(fs *ff.CoreFlags) {
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'n', LongName: "limit" /*            */, Value: ffval.NewValueDefault(&cfg.limit, 10) /*  */, Usage: "maximum number of traces to return"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "stack-depth" /*      */, Value: ffval.NewValue(&cfg.stackDepth) /*        */, Usage: "number of stack frames to include with each event"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "include-request" /*  */, Value: ffval.NewValue(&cfg.includeRequest) /*    */, Usage: "include search request in output", NoDefault: true})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "include-stats" /*    */, Value: ffval.NewValue(&cfg.includeStats) /*      */, Usage: "include search statistics in output", NoDefault: true})
}

func (cfg *searchConfig) writeResult(ctx context.Context, res *trc.SearchResponse) error {
	enc := json.NewEncoder(cfg.stdout)
	switch cfg.output {
	case "prettyjson":
		enc.SetIndent("", "    ")
	case "ndjson":
		//
	default:
		//
	}
	if err := enc.Encode(res); err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	return nil
}

func (cfg *searchConfig) Exec(ctx context.Context, args []string) error {
	ctx, tr := cfg.newTrace(ctx, "search")
	defer tr.Finish()

	var searcher trc.MultiSearcher
	for _, uri := range cfg.uris {
		searcher = append(searcher, trcweb.NewSearchClient(http.DefaultClient, uri))
	}

	if cfg.stackDepth == 0 {
		cfg.stackDepth = -1 // 0 means all available stacks, -1 means no stacks
	}

	req := &trc.SearchRequest{
		Filter:     cfg.filter,
		Limit:      cfg.limit,
		StackDepth: cfg.stackDepth,
	}

	cfg.debug.Printf("request: filter: %s", cfg.filter)
	cfg.debug.Printf("request: limit: %d", cfg.limit)
	cfg.debug.Printf("request: stack depth: %d", cfg.stackDepth)

	res, err := searcher.Search(ctx, req)
	if err != nil {
		return fmt.Errorf("execute search: %w", err)
	}

	cfg.debug.Printf("response: sources: %d (%s)", len(res.Sources), strings.Join(res.Sources, " "))
	cfg.debug.Printf("response: total: %d", res.TotalCount)
	cfg.debug.Printf("response: matched: %d", res.MatchCount)
	cfg.debug.Printf("response: returned: %d", len(res.Traces))
	cfg.debug.Printf("response: duration: %s", res.Duration)

	if !cfg.includeRequest {
		cfg.debug.Printf("removing request from response")
		res.Request = nil
	}

	if !cfg.includeStats {
		cfg.debug.Printf("removing stats from response")
		res.Stats = nil
	}

	if err := cfg.writeResult(ctx, res); err != nil {
		return err
	}

	return nil
}

//
//
//

type streamConfig struct {
	*rootConfig

	streamEvents  bool
	sendBuf       int
	recvBuf       int
	statsInterval time.Duration
	retryInterval time.Duration

	traces chan trc.Trace
}

func (cfg *streamConfig) register(fs *ff.CoreFlags) {
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'e', LongName: "events" /*         */, Value: ffval.NewValue(&cfg.streamEvents) /*                         */, Usage: "stream individual events rather than complete traces", NoDefault: true})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "send-buffer" /*    */, Value: ffval.NewValueDefault(&cfg.sendBuf, 100) /*                  */, Usage: "remote send buffer size"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "recv-buffer" /*    */, Value: ffval.NewValueDefault(&cfg.recvBuf, 100) /*                  */, Usage: "local receive buffer size"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "stats-interval" /* */, Value: ffval.NewValueDefault(&cfg.statsInterval, 10*time.Second) /* */, Usage: "stats reporting interval"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "retry-interval" /* */, Value: ffval.NewValueDefault(&cfg.retryInterval, 1*time.Second) /*  */, Usage: "connection retry interval"})
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
	ctx, _ = trc.Prefix(ctx, "<%s>", uri)

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

	cfg.debug.Printf("%s: starting", uri)
	defer cfg.debug.Printf("%s: stopped", uri)

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
			cfg.debug.Printf("emitted trace count: %d", count)
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
