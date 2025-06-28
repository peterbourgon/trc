package main

import (
	"context"
	"io"
	"log"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffval"
	"github.com/peterbourgon/trc"
)

type rootConfig struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	URIs     []string `ff:" short: u | long: uri       | placeholder: URI    | usage: server instance URI e.g. 'localhost:1234/traces' (repeatable) "`
	URIPath  string   `ff:"          | long: uri-path  | placeholder: PATH   | usage: if set, override every server instance URI path with this one "`
	LogLevel string   `ff:" short: l | long: log-level | placeholder: LEVEL  | usage: log level: i/info, d/debug, t/trace, n/none "`
	Output   string   `ff:" short: o | long: output    | placeholder: FORMAT | usage: output format: ndjson, prettyjson "`

	info, debug, trace *log.Logger
}

func (cfg *rootConfig) registerBaseFlags(fs *ff.FlagSet) {
	fs.AddFlag(ff.FlagConfig{
		ShortName:   'u',
		LongName:    "uri",
		Value:       ffval.NewUniqueList(&cfg.URIs),
		Usage:       "server instance URI e.g. 'localhost:1234/traces' (repeatable)",
		Placeholder: "URI",
	})
	fs.AddFlag(ff.FlagConfig{
		LongName:    "uri-path",
		Value:       ffval.NewValue(&cfg.URIPath),
		Usage:       "if set, override every server instance URI path with this one",
		Placeholder: "PATH",
	})
	fs.AddFlag(ff.FlagConfig{
		ShortName:   'l',
		LongName:    "log",
		Value:       ffval.NewEnum(&cfg.LogLevel, "info", "i", "debug", "d", "trace", "t", "none", "n"),
		Usage:       "log level: i/info, d/debug, t/trace, n/none",
		Placeholder: "LEVEL",
	})
	fs.AddFlag(ff.FlagConfig{
		ShortName:   'o',
		LongName:    "output",
		Value:       ffval.NewEnum(&cfg.Output, "ndjson", "prettyjson"),
		Usage:       "output format: ndjson, prettyjson",
		Placeholder: "FORMAT",
	})
}

func (cfg *rootConfig) newTrace(ctx context.Context, category string) (context.Context, trc.Trace) {
	ctx, tr := trc.New(ctx, "trc", category)
	tr = trc.LogDecorator(&logWriter{Logger: cfg.trace})(tr)
	ctx, tr = trc.Put(ctx, tr)
	return ctx, tr
}
