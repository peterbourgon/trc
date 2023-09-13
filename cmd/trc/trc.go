package main

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffval"
	"github.com/peterbourgon/trc"
)

type rootConfig struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	uris     []string
	uriPath  string
	logLevel string
	output   string

	info, debug, trace *log.Logger

	sources     []string
	ids         []string
	category    string
	query       string
	isActive    bool
	isFinished  bool
	minDuration time.Duration
	isSuccess   bool
	isErrored   bool

	filter trc.Filter
}

func (cfg *rootConfig) registerBaseFlags(fs *ff.FlagSet) {
	fs.AddFlag(ff.FlagConfig{ShortName: 'u', LongName: "uri" /*      */, Value: ffval.NewUniqueList(&cfg.uris) /*                                                     */, Usage: "trace server URI (repeatable, required)" /*     */, Placeholder: "URI"})
	fs.AddFlag(ff.FlagConfig{ShortName: 0x0, LongName: "uri-path" /* */, Value: ffval.NewValue(&cfg.uriPath) /*                                                       */, Usage: "path that will be applied to every URI" /*      */, Placeholder: "PATH"})
	fs.AddFlag(ff.FlagConfig{ShortName: 'l', LongName: "log" /*      */, Value: ffval.NewEnum(&cfg.logLevel, "info", "i", "debug", "d", "trace", "t", "none", "n") /* */, Usage: "log level: i/info, d/debug, t/trace, n/none" /* */, Placeholder: "LEVEL"})
	fs.AddFlag(ff.FlagConfig{ShortName: 'o', LongName: "output" /*   */, Value: ffval.NewEnum(&cfg.output, "ndjson", "prettyjson") /*                                 */, Usage: "output format: ndjson, prettyjson" /*           */, Placeholder: "FORMAT"})
}

func (cfg *rootConfig) registerFilterFlags(fs *ff.FlagSet) {
	fs.AddFlag(ff.FlagConfig{ShortName: 0x0, LongName: "source" /*   */, Value: ffval.NewUniqueList(&cfg.sources) /* */, NoDefault: true, Usage: "trace source (repeatable)"})
	fs.AddFlag(ff.FlagConfig{ShortName: 'i', LongName: "id" /*       */, Value: ffval.NewUniqueList(&cfg.ids) /*     */, NoDefault: true, Usage: "trace ID (repeatable)"})
	fs.AddFlag(ff.FlagConfig{ShortName: 'c', LongName: "category" /* */, Value: ffval.NewValue(&cfg.category) /*     */, NoDefault: true, Usage: "trace category"})
	fs.AddFlag(ff.FlagConfig{ShortName: 'q', LongName: "query" /*    */, Value: ffval.NewValue(&cfg.query) /*        */, NoDefault: true, Usage: "query expression", Placeholder: "REGEX"})
	fs.AddFlag(ff.FlagConfig{ShortName: 'a', LongName: "active" /*   */, Value: ffval.NewValue(&cfg.isActive) /*     */, NoDefault: true, Usage: "only active traces"})
	fs.AddFlag(ff.FlagConfig{ShortName: 'f', LongName: "finished" /* */, Value: ffval.NewValue(&cfg.isFinished) /*   */, NoDefault: true, Usage: "only finished traces"})
	fs.AddFlag(ff.FlagConfig{ShortName: 'd', LongName: "duration" /* */, Value: ffval.NewValue(&cfg.minDuration) /*  */, NoDefault: true, Usage: "only finished traces of at least this duration"})
	fs.AddFlag(ff.FlagConfig{ShortName: 0x0, LongName: "success" /*  */, Value: ffval.NewValue(&cfg.isSuccess) /*    */, NoDefault: true, Usage: "only successful (non-errored) traces"})
	fs.AddFlag(ff.FlagConfig{ShortName: 0x0, LongName: "errored" /*  */, Value: ffval.NewValue(&cfg.isErrored) /*    */, NoDefault: true, Usage: "only errored traces"})
}

func (cfg *rootConfig) newTrace(ctx context.Context, category string) (context.Context, trc.Trace) {
	ctx, tr := trc.New(ctx, "trc", category)
	tr = trc.LogDecorator(&logWriter{Logger: cfg.trace})(tr)
	ctx, tr = trc.Put(ctx, tr)
	return ctx, tr
}
