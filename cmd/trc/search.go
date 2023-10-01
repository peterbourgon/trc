package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
)

type searchConfig struct {
	*rootConfig

	Limit          int  `ff:" short=n | long=limit            | default=10 | usage: maximum number of traces to return                "`
	StackDepth     int  `ff:"         | long=stack-depth      | default=0  | usage: number of stack frames to include with each event "`
	IncludeRequest bool `ff:"         | long=include-request  | nodefault  | usage: include search request in output                  "`
	IncludeStats   bool `ff:"         | long=include-stats    | nodefault  | usage: include search statistics in output               "`
}

func (cfg *searchConfig) register(fs *ff.FlagSet) {
	if err := fs.AddStruct(cfg); err != nil {
		panic(err)
	}
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
		searcher = append(searcher, trchttp.NewSearchClient(http.DefaultClient, uri))
	}

	if cfg.StackDepth == 0 {
		cfg.StackDepth = -1 // 0 means all available stacks, -1 means no stacks
	}

	req := &trc.SearchRequest{
		Filter:     cfg.filter,
		Limit:      cfg.Limit,
		StackDepth: cfg.StackDepth,
	}

	cfg.debug.Printf("request: filter: %s", cfg.filter)
	cfg.debug.Printf("request: limit: %d", cfg.Limit)
	cfg.debug.Printf("request: stack depth: %d", cfg.StackDepth)

	res, err := searcher.Search(ctx, req)
	if err != nil {
		return fmt.Errorf("execute search: %w", err)
	}

	cfg.debug.Printf("response: sources: %d (%s)", len(res.Sources), strings.Join(res.Sources, " "))
	cfg.debug.Printf("response: total: %d", res.TotalCount)
	cfg.debug.Printf("response: matched: %d", res.MatchCount)
	cfg.debug.Printf("response: returned: %d", len(res.Traces))
	cfg.debug.Printf("response: duration: %s", res.Duration)

	if !cfg.IncludeRequest {
		cfg.debug.Printf("removing request from response")
		res.Request = nil
	}

	if !cfg.IncludeStats {
		cfg.debug.Printf("removing stats from response")
		res.Stats = nil
	}

	if err := cfg.writeResult(ctx, res); err != nil {
		return err
	}

	return nil
}
