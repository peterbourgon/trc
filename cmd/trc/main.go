// trc is a CLI tool for interacting with trc web servers.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/oklog/run"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/unixtransport"
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
	case err == nil, errors.Is(err, context.Canceled), errors.As(err, &(run.SignalError{})):
		os.Exit(0)
	case err != nil:
		fmt.Fprintf(stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func exec(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) (err error) {
	unixtransport.RegisterDefault()

	// Config for `trc` root command.
	rootConfig := &rootConfig{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
	rootFlags := ff.NewFlagSet("root")
	rootConfig.registerBaseFlags(rootFlags)
	rootCommand := &ff.Command{
		Name:      "trc",
		ShortHelp: "access trace data from one or more trc instances",
		Flags:     rootFlags,
	}

	// Shared flags for `trc search` and `trc stream`.
	filterConfig := &filterConfig{}
	filterFlags := ff.NewFlagSet("filter").SetParent(rootFlags)
	filterConfig.registerFilterFlags(filterFlags)

	// Config for `trc search`.
	searchConfig := &searchConfig{rootConfig: rootConfig, filterConfig: filterConfig}
	searchFlags := ff.NewFlagSet("search").SetParent(filterFlags)
	searchConfig.register(searchFlags)
	searchCommand := &ff.Command{
		Name:      "search",
		ShortHelp: "run a single search request",
		LongHelp:  "Fetch traces that match the provided filter flags.",
		Flags:     searchFlags,
		Exec:      searchConfig.Exec,
	}
	rootCommand.Subcommands = append(rootCommand.Subcommands, searchCommand)

	// Config for `trc stream`.
	streamConfig := &streamConfig{rootConfig: rootConfig, filterConfig: filterConfig}
	streamFlags := ff.NewFlagSet("stream").SetParent(filterFlags)
	streamConfig.register(streamFlags)
	streamCommand := &ff.Command{
		Name:      "stream",
		ShortHelp: "continuously stream trace data to the terminal",
		LongHelp:  "Stream traces (or trace events) that match the provided filter flags.",
		Flags:     streamFlags,
		Exec:      streamConfig.Exec,
	}
	rootCommand.Subcommands = append(rootCommand.Subcommands, streamCommand)

	// Config for `trc serve`.
	serveConfig := &serveConfig{rootConfig: rootConfig}
	serveFlags := ff.NewFlagSet("serve").SetParent(rootFlags)
	serveConfig.register(serveFlags)
	serveCommand := &ff.Command{
		Name:      "serve",
		ShortHelp: "run a local traces UI",
		LongHelp:  "Serve a local web UI with trace data from all provided trc instances.",
		Flags:     serveFlags,
		Exec:      serveConfig.Exec,
	}
	rootCommand.Subcommands = append(rootCommand.Subcommands, serveCommand)

	// Print help when appropriate.
	showHelp := true
	defer func() {
		errHelp := errors.Is(err, ff.ErrHelp) || errors.Is(err, ff.ErrNoExec)
		if showHelp || errHelp {
			fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(rootCommand))
		}
		if errHelp {
			err = nil
		}
	}()

	// Initial parsing.
	if err := rootCommand.Parse(args, ff.WithEnvVarPrefix("TRC")); err != nil {
		return err
	}

	// Validation and set-up.
	{
		var infodst, debugdst, tracedst io.Writer
		switch rootConfig.LogLevel {
		case "n", "none":
			infodst, debugdst, tracedst = io.Discard, io.Discard, io.Discard
		case "i", "info":
			infodst, debugdst, tracedst = stderr, io.Discard, io.Discard
		case "d", "debug":
			infodst, debugdst, tracedst = stderr, stderr, io.Discard
		case "t", "trace":
			infodst, debugdst, tracedst = stderr, stderr, stderr
		default:
			return fmt.Errorf("invalid log level %q", rootConfig.LogLevel)
		}
		rootConfig.info = log.New(infodst, "", 0)
		rootConfig.debug = log.New(debugdst, "[DEBUG] ", log.Lmsgprefix)
		rootConfig.trace = log.New(tracedst, "[TRACE] ", log.Lmsgprefix)
	}

	if len(rootConfig.URIs) <= 0 {
		return fmt.Errorf("at least one -u, --uri is required")
	}

	for i, uri := range rootConfig.URIs {
		uri = strings.TrimSpace(uri)
		if uri == "" {
			continue
		}

		if !strings.HasPrefix(uri, "http") {
			uri = "http://" + uri
		}

		u, err := url.ParseRequestURI(uri)
		if err != nil {
			return fmt.Errorf("%s: invalid: %w", uri, err)
		}

		if rootConfig.URIPath != "" {
			u.Path = rootConfig.URIPath
		}

		uri = u.String()
		rootConfig.URIs[i] = uri

		rootConfig.debug.Printf("URI: %s", uri)
	}

	{
		var minDuration *time.Duration
		if f, ok := filterFlags.GetFlag("duration"); ok && f.IsSet() {
			rootConfig.debug.Printf("using --duration %s", filterConfig.MinDuration)
			minDuration = &filterConfig.MinDuration
		}

		filterConfig.filter = trc.Filter{
			Sources:     filterConfig.Sources,
			IDs:         filterConfig.IDs,
			Category:    filterConfig.Category,
			IsActive:    filterConfig.IsActive,
			IsFinished:  filterConfig.IsFinished,
			MinDuration: minDuration,
			IsSuccess:   filterConfig.IsSuccess,
			IsErrored:   filterConfig.IsErrored,
			Query:       filterConfig.Query,
		}
	}

	// Run errors shouldn't show help by default.
	showHelp = false

	// Run the selected command.
	return rootCommand.Run(ctx)
}
