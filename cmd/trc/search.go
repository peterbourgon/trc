package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffval"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcweb"
)

type searchConfig struct {
	*rootConfig

	limit          int
	stackDepth     int
	includeRequest bool
	includeStats   bool
}

func (cfg *searchConfig) register(fs *ff.FlagSet) {
	fs.AddFlag(ff.FlagConfig{ShortName: 'n', LongName: "limit" /*            */, Value: ffval.NewValueDefault(&cfg.limit, 10) /*  */, Usage: "maximum number of traces to return"})
	fs.AddFlag(ff.FlagConfig{ShortName: 0x0, LongName: "stack-depth" /*      */, Value: ffval.NewValue(&cfg.stackDepth) /*        */, Usage: "number of stack frames to include with each event"})
	fs.AddFlag(ff.FlagConfig{ShortName: 0x0, LongName: "include-request" /*  */, Value: ffval.NewValue(&cfg.includeRequest) /*    */, Usage: "include search request in output", NoDefault: true})
	fs.AddFlag(ff.FlagConfig{ShortName: 0x0, LongName: "include-stats" /*    */, Value: ffval.NewValue(&cfg.includeStats) /*      */, Usage: "include search statistics in output", NoDefault: true})
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
