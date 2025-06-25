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
	rootConfig := &rootConfig{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}

	rootFlags := ff.NewFlagSet("base")
	rootConfig.registerBaseFlags(rootFlags)

	filterFlags := ff.NewFlagSet("filter").SetParent(rootFlags)
	rootConfig.registerFilterFlags(filterFlags)

	rootCommand := &ff.Command{
		Name:      "trc",
		ShortHelp: "access trace data from one or more trc server instances",
		Flags:     rootFlags,
	}

	// Config for `trc search`.
	searchConfig := &searchConfig{rootConfig: rootConfig}
	searchFlags := ff.NewFlagSet("search").SetParent(filterFlags)
	searchConfig.register(searchFlags)
	searchCommand := &ff.Command{
		Name:      "search",
		ShortHelp: "run a single search request",
		LongHelp:  "Fetch traces that match the provided query flags.",
		Flags:     searchFlags,
		Exec:      searchConfig.Exec,
	}
	rootCommand.Subcommands = append(rootCommand.Subcommands, searchCommand)

	// Config for `trc stream`.
	streamConfig := &streamConfig{rootConfig: rootConfig}
	streamFlags := ff.NewFlagSet("stream").SetParent(filterFlags)
	streamConfig.register(streamFlags)
	streamCommand := &ff.Command{
		Name:      "stream",
		ShortHelp: "continuously stream trace data to the terminal",
		LongHelp:  "Stream traces, or trace events, that match the provided query flags.",
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
		ShortHelp: "run a local web UI over all provided instances",
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
		switch rootConfig.logLevel {
		case "n", "none":
			infodst, debugdst, tracedst = io.Discard, io.Discard, io.Discard
		case "i", "info":
			infodst, debugdst, tracedst = stderr, io.Discard, io.Discard
		case "d", "debug":
			infodst, debugdst, tracedst = stderr, stderr, io.Discard
		case "t", "trace":
			infodst, debugdst, tracedst = stderr, stderr, stderr
		default:
			return fmt.Errorf("invalid log level %q", rootConfig.logLevel)
		}
		rootConfig.info = log.New(infodst, "", 0)
		rootConfig.debug = log.New(debugdst, "[DEBUG] ", log.Lmsgprefix)
		rootConfig.trace = log.New(tracedst, "[TRACE] ", log.Lmsgprefix)
	}

	if len(rootConfig.uris) <= 0 {
		return fmt.Errorf("at least one URI is required")
	}

	for i, uri := range rootConfig.uris {
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

		if rootConfig.uriPath != "" {
			u.Path = rootConfig.uriPath
		}

		uri = u.String()
		rootConfig.uris[i] = uri

		rootConfig.debug.Printf("URI: %s", uri)
	}

	{
		var minDuration *time.Duration
		if f, ok := filterFlags.GetFlag("duration"); ok && f.IsSet() {
			rootConfig.debug.Printf("using --duration %s", rootConfig.minDuration)
			minDuration = &rootConfig.minDuration
		}

		rootConfig.filter = trc.Filter{
			Sources:     rootConfig.sources,
			IDs:         rootConfig.ids,
			Category:    rootConfig.category,
			IsActive:    rootConfig.isActive,
			IsFinished:  rootConfig.isFinished,
			MinDuration: minDuration,
			IsSuccess:   rootConfig.isSuccess,
			IsErrored:   rootConfig.isErrored,
			Query:       rootConfig.query,
		}
	}

	// Run errors shouldn't show help by default.
	showHelp = false

	// Run the selected command.
	return rootCommand.Run(ctx)
}
