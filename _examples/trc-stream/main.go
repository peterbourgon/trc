package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/oklog/run"
	"github.com/peterbourgon/ff/v3"
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
		return
	case err != nil:
		log.Fatal(err)
	}
}

func exec(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("trc-stream", flag.ContinueOnError)
	var (
		streamURI = fs.String("stream-uri", "http://localhost:8080/stream", "stream URI")
		category  = fs.String("category", "", "trace category")
		query     = fs.String("query", "", "query regexp")
	)
	if err := ff.Parse(fs, os.Args[1:]); err != nil {
		return err
	}

	var (
		httpClient   = http.DefaultClient
		streamClient = trcweb.NewStreamClient(httpClient, *streamURI)
		streamTraces = make(chan *trcstream.StreamTrace)
		streamFilter = trc.Filter{
			Category: *category,
			Query:    *query,
		}
	)

	var g run.Group

	{
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			ctx, tr := trc.NewLogTrace(ctx, "source", "stream", stderr)
			defer tr.Finish()
			return streamClient.Stream(ctx, streamFilter, streamTraces)
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
				case str := <-streamTraces:
					enc.Encode(str)
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
