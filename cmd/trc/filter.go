package main

import (
	"time"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffval"
	"github.com/peterbourgon/trc"
)

type filterConfig struct {
	Sources     []string
	IDs         []string
	Category    string
	Query       string
	IsActive    bool
	IsFinished  bool
	MinDuration time.Duration
	IsSuccess   bool
	IsErrored   bool

	filter trc.Filter
}

func (cfg *filterConfig) registerFilterFlags(fs *ff.FlagSet) {
	fs.AddFlag(ff.FlagConfig{
		LongName:  "source",
		Value:     ffval.NewUniqueList(&cfg.Sources),
		NoDefault: true,
		Usage:     "trace source (repeatable)",
	})
	fs.AddFlag(ff.FlagConfig{
		ShortName: 'i',
		LongName:  "id",
		Value:     ffval.NewUniqueList(&cfg.IDs),
		NoDefault: true,
		Usage:     "trace ID (repeatable)",
	})
	fs.AddFlag(ff.FlagConfig{
		ShortName: 'c',
		LongName:  "category",
		Value:     ffval.NewValue(&cfg.Category),
		NoDefault: true,
		Usage:     "trace category",
	})
	fs.AddFlag(ff.FlagConfig{
		ShortName:   'q',
		LongName:    "query",
		Value:       ffval.NewValue(&cfg.Query),
		NoDefault:   true,
		Usage:       "query expression",
		Placeholder: "REGEX",
	})
	fs.AddFlag(ff.FlagConfig{
		ShortName: 'a',
		LongName:  "active",
		Value:     ffval.NewValue(&cfg.IsActive),
		NoDefault: true,
		Usage:     "only active traces",
	})
	fs.AddFlag(ff.FlagConfig{
		ShortName: 'f',
		LongName:  "finished",
		Value:     ffval.NewValue(&cfg.IsFinished),
		NoDefault: true,
		Usage:     "only finished traces",
	})
	fs.AddFlag(ff.FlagConfig{
		ShortName: 'd',
		LongName:  "duration",
		Value:     ffval.NewValue(&cfg.MinDuration),
		NoDefault: true,
		Usage:     "only finished traces of at least this duration",
	})
	fs.AddFlag(ff.FlagConfig{
		LongName:  "success",
		Value:     ffval.NewValue(&cfg.IsSuccess),
		NoDefault: true,
		Usage:     "only successful (non-errored) traces",
	})
	fs.AddFlag(ff.FlagConfig{
		LongName:  "errored",
		Value:     ffval.NewValue(&cfg.IsErrored),
		NoDefault: true,
		Usage:     "only errored traces",
	})
}
