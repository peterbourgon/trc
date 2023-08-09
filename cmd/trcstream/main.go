package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/oklog/run"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcstream"
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

func exec(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) error {
	fs := ff.NewFlags("trc-stream")
	flagURIs := stringset{}
	flagSources := stringset{}
	flagIDs := stringset{}
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'u', LongName: "uri", Value: &flagURIs, Usage: "trace server stream URI (repeatable)"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 's', LongName: "source", Value: &flagSources, Usage: "filter for this source (repeatable)"})
	fs.AddFlag(ff.CoreFlagConfig{ShortName: 'i', LongName: "id", Value: &flagIDs, Usage: "filter for this ID (repeatable)"})
	flagCategory := fs.String('c', "category", "", "filter for this category")
	flagQuery := fs.String('q', "query", "", "filter for this query regexp")
	flagMinDuration := fs.Duration('d', "duration", 0, "filter for finished traces of at least this duration")
	flagIsSuccess := fs.Bool('y', "success", false, "filter for successful traces")
	flagIsErrored := fs.Bool('n', "errored", false, "filter for errored traces")
	flagBuffer := fs.IntLong("buffer", 100, "receive buffer size")
	flagDebug := fs.BoolLong("debug", false, "log debug information")
	flagEvents := fs.BoolLong("events", false, "stream individual events instead of complete traces")

	err := ff.Parse(fs, args)
	if errors.Is(err, ff.ErrHelp) {
		fmt.Fprintf(stderr, "\n%s\n\n", usageFunc(fs))
		return nil
	}
	if err != nil {
		return err
	}

	if len(flagURIs) <= 0 {
		return fmt.Errorf("-u, --uri is required")
	}

	var debug *log.Logger
	{
		debugWriter := io.Discard
		if *flagDebug {
			debugWriter = stderr
		}
		debug = log.New(debugWriter, "[DEBUG] ", 0)
	}

	var minDuration *time.Duration
	{
		if f, ok := fs.GetFlag("duration"); ok && f.IsSet() {
			debug.Printf("using --duration %s", *flagMinDuration)
			minDuration = flagMinDuration
		}
	}

	var (
		httpClient   = &http.Client{}
		streamTraces = make(chan trc.Trace, *flagBuffer)
		streamFilter = trc.Filter{
			Sources:     flagSources,
			IDs:         flagIDs,
			Category:    *flagCategory,
			IsActive:    false,
			IsFinished:  !*flagEvents,
			MinDuration: minDuration,
			IsSuccess:   *flagIsSuccess,
			IsErrored:   *flagIsErrored,
			Query:       *flagQuery,
		}
	)

	debug.Printf("filter %s", streamFilter)

	var g run.Group

	{
		for _, uri := range flagURIs {
			debug.Printf("URI %s", uri)
			streamClient := trcweb.NewStreamClient(httpClient, uri)
			streamManager := &streamManager{client: streamClient, filter: streamFilter, traces: streamTraces}
			streamCategory := uri
			ctx, cancel := context.WithCancel(ctx)
			g.Add(func() error {
				ctx, tr := newLoggerTrace(ctx, streamCategory, debug)
				defer tr.Finish()
				return streamManager.run(ctx)
			}, func(error) {
				cancel()
			})
		}
	}

	{
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			enc := json.NewEncoder(stdout)
			for {
				select {
				case tr := <-streamTraces:
					// Optimization for --events.
					if *flagEvents && tr.Finished() {
						if str, ok := tr.(*trcstream.StreamTrace); ok {
							str.TraceEvents = nil
						}
					}
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

//
//
//

type streamManager struct {
	client *trcweb.StreamClient
	filter trc.Filter
	traces chan trc.Trace
}

func (sm *streamManager) run(ctx context.Context) error {
	tr := trc.Get(ctx)
	tr.Tracef("starting stream...")

	for {
		var (
			subctx, cancel = context.WithCancel(ctx)
			errc           = make(chan error, 1)
		)
		go func() {
			errc <- sm.client.Stream(subctx, sm.filter, sm.traces)
		}()

		select {
		case <-ctx.Done():
			cancel()
			err := <-errc
			tr.Tracef("client stream stopped (%v) -- returning", err)
			return ctx.Err()

		case err := <-errc:
			tr.Errorf("client stream error (%v) -- giving up", err)
			cancel()
			<-ctx.Done()
			return ctx.Err()
		}
	}
}

//
//
//

type stringset []string

var _ flag.Value = (*stringset)(nil)

func (ss *stringset) Set(val string) error {
	for _, s := range *ss {
		if s == val {
			return nil
		}
	}
	(*ss) = append(*ss, val)
	return nil
}

func (ss *stringset) String() string {
	return strings.Join(*ss, ", ")
}

func (ss *stringset) Placeholder() string {
	return "STRING"
}

//
//
//

func newLoggerTrace(ctx context.Context, category string, logger *log.Logger) (context.Context, trc.Trace) {
	ctx, tr := trc.New(ctx, "source", category)
	tr = &loggerTrace{Trace: tr, category: category, logger: logger}
	return trc.Put(ctx, tr)
}

type loggerTrace struct {
	trc.Trace
	category string
	logger   *log.Logger
}

func (ltr *loggerTrace) Tracef(format string, args ...any) {
	ltr.logger.Printf("TRACE ["+ltr.category+"] "+format, args...)
	ltr.Trace.Tracef(format, args...)
}

func (ltr *loggerTrace) LazyTracef(format string, args ...any) {
	ltr.logger.Printf("TRACE ["+ltr.category+"] "+format, args...)
	ltr.Trace.LazyTracef(format, args...)
}

func (ltr *loggerTrace) Errorf(format string, args ...any) {
	ltr.logger.Printf("ERROR ["+ltr.category+"] "+format, args...)
	ltr.Trace.Errorf(format, args...)
}

func (ltr *loggerTrace) LazyErrorf(format string, args ...any) {
	ltr.logger.Printf("ERROR ["+ltr.category+"] "+format, args...)
	ltr.Trace.LazyErrorf(format, args...)
}
