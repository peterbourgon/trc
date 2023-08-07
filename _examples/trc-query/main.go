package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/oklog/run"
	"github.com/peterbourgon/ff/v3"
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
		return
	case err != nil:
		log.Fatal(err)
	}
}

func exec(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) error {
	fs := flag.NewFlagSet("trc-query", flag.ContinueOnError)
	var (
		searchURI = fs.String("search-uri", "http://localhost:8080/traces", "search URI")
		category  = fs.String("category", "", "trace category")
		query     = fs.String("query", "", "query regexp")
		interval  = fs.Duration("interval", 1*time.Second, "update interval")
	)
	if err := ff.Parse(fs, os.Args[1:]); err != nil {
		return err
	}

	var (
		httpClient    = http.DefaultClient
		searchClient  = trcweb.NewSearchClient(httpClient, *searchURI)
		searchRequest = &trc.SearchRequest{
			Limit: 3,
			Filter: trc.Filter{
				Sources:    []string{"global"},
				Category:   *category,
				Query:      *query,
				IsFinished: true,
			},
			StackDepth: -1,
		}
	)

	var g run.Group

	{
		ctx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			ticker := time.NewTicker(*interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					res, err := searchClient.Search(ctx, searchRequest)
					if err != nil {
						return fmt.Errorf("search error: %w", err)
					}

					data, err := json.Marshal(res.Traces)
					if err != nil {
						return fmt.Errorf("marshal response: %w", err)
					}

					fmt.Fprintln(stdout, string(data))

				case <-ctx.Done():
					return nil
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
