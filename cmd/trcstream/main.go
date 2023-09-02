package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/oklog/run"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"github.com/peterbourgon/ff/v4/ffval"
	"github.com/peterbourgon/trc"
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

func exec(ctx context.Context, _ io.Reader, stdout, stderr io.Writer, args []string) error {
	var flags struct {
		uris          []string
		sources       []string
		ids           []string
		category      string
		query         string
		minDuration   time.Duration
		isSuccess     bool
		isErrored     bool
		recvBuf       int
		sendBuf       int
		statsInterval time.Duration
		retryInterval time.Duration
		events        bool
		logging       string
		output        string
	}

	fs := ff.NewFlags("trcstream")
	{
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'u', LongName: "uri" /*        */, Value: ffval.NewUniqueList(&flags.uris) /*                                */, Usage: "trace server stream URI (repeatable, required)"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 's', LongName: "source" /*     */, Value: ffval.NewUniqueList(&flags.sources) /*                             */, Usage: "filter for this source (repeatable)"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'i', LongName: "id" /*         */, Value: ffval.NewUniqueList(&flags.ids) /*                                 */, Usage: "filter for this ID (repeatable)"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'c', LongName: "category" /*   */, Value: ffval.NewValue(&flags.category) /*                                 */, Usage: "filter for this category"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'q', LongName: "query" /*      */, Value: ffval.NewValue(&flags.query) /*                                    */, Usage: "filter for this query regexp"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'd', LongName: "duration" /*   */, Value: ffval.NewValue(&flags.minDuration) /*                              */, Usage: "filter for finished traces of at least this duration", NoDefault: true})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'y', LongName: "success" /*    */, Value: ffval.NewValue(&flags.isSuccess) /*                                */, Usage: "filter for finished and successful traces", NoDefault: true})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'n', LongName: "errored" /*    */, Value: ffval.NewValue(&flags.isErrored) /*                                */, Usage: "filter for finished and errored traces", NoDefault: true})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "recvbuf" /*    */, Value: ffval.NewValueDefault(&flags.recvBuf, 100) /*                      */, Usage: "local receive buffer size"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "sendbuf" /*    */, Value: ffval.NewValueDefault(&flags.sendBuf, 100) /*                      */, Usage: "remote send buffer size"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "stats" /*      */, Value: ffval.NewValueDefault(&flags.statsInterval, 10*time.Second) /*     */, Usage: "debug stats reporting interval"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "retry" /*      */, Value: ffval.NewValueDefault(&flags.retryInterval, 1*time.Second) /*      */, Usage: "stream connection retry interval"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 0x0, LongName: "events" /*     */, Value: ffval.NewValue(&flags.events) /*                                   */, Usage: "stream individual events instead of full traces", NoDefault: true})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'l', LongName: "log" /*        */, Value: ffval.NewEnum(&flags.logging, "info", "debug", "trace", "none") /* */, Usage: "log level: info, debug, trace, none"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'o', LongName: "output" /*     */, Value: ffval.NewEnum(&flags.output, "ndjson", "prettyjson") /*            */, Usage: "output format: ndjson, prettyjson"})
	}

	if err := ff.Parse(fs, args,
		ff.WithEnvVarPrefix("TRCSTREAM"),
	); err != nil {
		fmt.Fprintf(stderr, "%s\n", ffhelp.Flags(fs, usage))
		if errors.Is(err, ff.ErrHelp) {
			err = nil
		}
		return err
	}

	if len(flags.uris) <= 0 {
		fmt.Fprintf(stderr, "%s\n", ffhelp.Flags(fs, usage))
		return fmt.Errorf("at least one URI is required")
	}

	var info, debug, trace *log.Logger
	{
		var infodst, debugdst, tracedst io.Writer
		switch flags.logging {
		case "none":
			infodst, debugdst, tracedst = io.Discard, io.Discard, io.Discard
		case "info":
			infodst, debugdst, tracedst = stderr, io.Discard, io.Discard
		case "debug":
			infodst, debugdst, tracedst = stderr, stderr, io.Discard
		case "trace":
			infodst, debugdst, tracedst = stderr, stderr, stderr
		default:
			panic(fmt.Errorf("invalid logging value %q", flags.logging))
		}
		info = log.New(infodst, "", 0)
		debug = log.New(debugdst, "[DEBUG] ", log.Lmsgprefix)
		trace = log.New(tracedst, "[TRACE] ", log.Lmsgprefix)
	}

	var minDuration *time.Duration
	{
		if f, ok := fs.GetFlag("duration"); ok && f.IsSet() {
			debug.Printf("using --duration %s", flags.minDuration)
			minDuration = &flags.minDuration
		}
	}

	var (
		streamTraces = make(chan trc.Trace, flags.recvBuf)
		streamFilter = trc.Filter{
			Sources:     flags.sources,
			IDs:         flags.ids,
			Category:    flags.category,
			IsActive:    false,
			IsFinished:  !flags.events,
			MinDuration: minDuration,
			IsSuccess:   flags.isSuccess,
			IsErrored:   flags.isErrored,
			Query:       flags.query,
		}
	)

	what := "traces"
	if flags.events {
		what = "events"
	}
	info.Printf("streaming %s with %s", what, streamFilter)

	debug.Printf("send buffer: %d", flags.sendBuf)
	debug.Printf("receive buffer: %d", flags.recvBuf)
	debug.Printf("stats interval: %s", flags.statsInterval)
	debug.Printf("retry interval: %s", flags.retryInterval)
	debug.Printf("output format: %s", flags.output)

	var g run.Group

	{
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			return runStreams(ctx, streamsConfig{
				URIs:          flags.uris,
				Filter:        streamFilter,
				Traces:        streamTraces,
				SendBuffer:    flags.sendBuf,
				RetryInterval: flags.retryInterval,
				StatsInterval: flags.statsInterval,
				Info:          info,
				Debug:         debug,
				Trace:         trace,
			})
		}, func(error) {
			cancel()
		})
	}

	{
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			var encode func(tr trc.Trace)
			switch flags.output {
			case "ndjson":
				enc := json.NewEncoder(stdout)
				encode = func(tr trc.Trace) { enc.Encode(tr) }
			case "prettyjson":
				enc := json.NewEncoder(stdout)
				enc.SetIndent("", "    ")
				encode = func(tr trc.Trace) { enc.Encode(tr) }
			default:
				panic(fmt.Errorf("invalid output format %q", flags.output))
			}
			for {
				select {
				case tr := <-streamTraces:
					encode(tr)
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}, func(error) {
			cancel()
		})
	}

	{
		g.Add(run.SignalHandler(ctx, os.Interrupt, os.Kill))
	}

	return g.Run()
}

const usage = "Stream trace events from one or more instances to the terminal."
