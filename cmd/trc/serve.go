package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffval"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcweb"
	"github.com/peterbourgon/unixtransport"
	"github.com/peterbourgon/unixtransport/unixproxy"
)

type serveConfig struct {
	*rootConfig

	listenAddr string
}

func (cfg *serveConfig) register(fs *ff.FlagSet) {
	fs.AddFlag(ff.FlagConfig{
		LongName: "listen-addr",
		Value:    ffval.NewValueDefault(&cfg.listenAddr, "localhost:8001"),
		Usage:    "HTTP listen address",
	})
}

func (cfg *serveConfig) Exec(ctx context.Context, args []string) error {
	transport := &http.Transport{
		//
	}

	unixtransport.Register(transport)

	client := &http.Client{
		Transport: transport,
	}

	var ms trc.MultiSearcher
	for _, uri := range cfg.rootConfig.uris {
		ms = append(ms, trcweb.NewSearchClient(client, uri))
	}

	cfg.info.Printf("serving instance count %d", len(ms))

	ln, err := unixproxy.ListenURI(ctx, cfg.listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	cfg.info.Printf("listening on %s", cfg.listenAddr)

	traceServer := &trcweb.TraceServer{
		Searcher: ms,
	}

	httpServer := &http.Server{
		Handler: traceServer,
	}

	return httpServer.Serve(ln)
}
