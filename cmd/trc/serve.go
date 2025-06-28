package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcweb"
	"github.com/peterbourgon/unixtransport/unixproxy"
)

type serveConfig struct {
	*rootConfig

	ListenAddr string `ff:"short: a | long: listen-addr | default: localhost:8001 | placeholder: ADDR | usage: HTTP server listen address"`
}

func (cfg *serveConfig) register(fs *ff.FlagSet) {
	if err := fs.AddStruct(cfg); err != nil {
		panic(fmt.Errorf("invalid struct config: %w", err))
	}
}

func (cfg *serveConfig) Exec(ctx context.Context, args []string) error {
	var ms trc.MultiSearcher
	for _, uri := range cfg.rootConfig.URIs {
		ms = append(ms, trcweb.NewSearchClient(http.DefaultClient, uri))
	}

	cfg.info.Printf("serving instance count %d", len(ms))

	ln, err := unixproxy.ListenURI(ctx, cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	cfg.info.Printf("listening on %s", cfg.ListenAddr)

	traceServer := &trcweb.TraceServer{
		Searcher: ms,
	}

	httpServer := &http.Server{
		Handler: traceServer,
	}

	return httpServer.Serve(ln)
}
