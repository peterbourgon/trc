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

func exec(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) error {
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
		debug         bool
		events        bool
	}

	fs := ff.NewFlags("trcstream")
	{
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'u', LongName: "uri", Value: ffval.NewUniqueList(&flags.uris), Usage: "trace server stream URI (repeatable, required)"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 's', LongName: "source", Value: ffval.NewUniqueList(&flags.sources), Usage: "filter for this source (repeatable)"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'i', LongName: "id", Value: ffval.NewUniqueList(&flags.ids), Usage: "filter for this ID (repeatable)"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'c', LongName: "category", Value: ffval.NewValue(&flags.category), Usage: "filter for this category"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'q', LongName: "query", Value: ffval.NewValue(&flags.query), Usage: "filter for this query regexp"})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'd', LongName: "duration", Value: ffval.NewValue(&flags.minDuration), Usage: "filter for finished traces of at least this duration", NoDefault: true})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'y', LongName: "success", Value: ffval.NewValue(&flags.isSuccess), Usage: "filter for finished and successful traces", NoDefault: true})
		fs.AddFlag(ff.CoreFlagConfig{ShortName: 'n', LongName: "errored", Value: ffval.NewValue(&flags.isErrored), Usage: "filter for finished and errored traces", NoDefault: true})
		fs.AddFlag(ff.CoreFlagConfig{ /*          */ LongName: "recvbuf", Value: ffval.NewValueDefault(&flags.recvBuf, 100), Usage: "local receive buffer size"})
		fs.AddFlag(ff.CoreFlagConfig{ /*          */ LongName: "sendbuf", Value: ffval.NewValueDefault(&flags.sendBuf, 100), Usage: "remote send buffer size"})
		fs.AddFlag(ff.CoreFlagConfig{ /*          */ LongName: "stats", Value: ffval.NewValueDefault(&flags.statsInterval, 10*time.Second), Usage: "debug stats reporting interval"})
		fs.AddFlag(ff.CoreFlagConfig{ /*          */ LongName: "retry", Value: ffval.NewValueDefault(&flags.retryInterval, 1*time.Second), Usage: "stream connection retry interval"})
		fs.AddFlag(ff.CoreFlagConfig{ /*          */ LongName: "events", Value: ffval.NewValue(&flags.events), Usage: "stream individual events for matching traces", NoDefault: true})
		fs.AddFlag(ff.CoreFlagConfig{ /*          */ LongName: "debug", Value: ffval.NewValue(&flags.debug), Usage: "log debug information", NoDefault: true})
	}

	if err := ff.Parse(fs, args); err != nil {
		fmt.Fprintf(stderr, "%s\n", ffhelp.Flags(fs, usage))
		if errors.Is(err, ff.ErrHelp) {
			err = nil
		}
		return err
	}

	if len(flags.uris) <= 0 {
		fmt.Fprintf(stderr, "%s\n", ffhelp.Flags(fs, usage))
		return fmt.Errorf("-u, --uri is required")
	}

	var info, debug *log.Logger
	{
		info = log.New(stderr, "", log.LstdFlags)
		if flags.debug {
			debug = log.New(stderr, "[DEBUG] ", log.LstdFlags|log.Lmsgprefix)
		} else {
			debug = log.New(io.Discard, "", 0)
		}
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

	debug.Printf("filter %s", streamFilter)
	debug.Printf("recvbuf %d", flags.recvBuf)
	debug.Printf("sendbuf %d", flags.sendBuf)
	debug.Printf("stats %s", flags.statsInterval)
	debug.Printf("retry %s", flags.retryInterval)

	var g run.Group

	{
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			return runStreams(ctx, streamsConfig{
				URIs:          flags.uris,
				Filter:        streamFilter,
				Traces:        streamTraces,
				RemoteBuffer:  flags.sendBuf,
				StatsInterval: flags.statsInterval,
				RetryInterval: flags.retryInterval,
				Info:          info,
				Debug:         debug,
			})
		}, func(error) {
			cancel()
		})
	}

	{
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			enc := json.NewEncoder(stdout)
			for {
				select {
				case tr := <-streamTraces:
					enc.Encode(tr)
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
