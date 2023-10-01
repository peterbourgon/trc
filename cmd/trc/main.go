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
	case err == nil, errors.Is(err, ff.ErrHelp), errors.Is(err, context.Canceled), errors.As(err, &(run.SignalError{})):
		os.Exit(0)
	case err != nil:
		fmt.Fprintf(stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func exec(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) (err error) {
	// Config for `trc`.
	rootConfig := &rootConfig{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}

	baseFlags := ff.NewFlagSet("base")
	rootConfig.registerBaseFlags(baseFlags)

	filterFlags := ff.NewFlagSet("filter").SetParent(baseFlags)
	rootConfig.registerFilterFlags(filterFlags)

	trcFlags := filterFlags

	trcCommand := &ff.Command{
		Name:      "trc",
		ShortHelp: "query trace data from one or more instances",
		Flags:     trcFlags,
		Exec:      func(ctx context.Context, args []string) error { return ff.ErrHelp },
	}

	// Config for `trc search`.
	searchConfig := &searchConfig{rootConfig: rootConfig}
	searchFlags := ff.NewFlagSet("search").SetParent(trcFlags)
	searchConfig.register(searchFlags)
	searchCommand := &ff.Command{
		Name:      "search",
		ShortHelp: "search for trace data",
		LongHelp:  "Fetch traces that match the filter.",
		Flags:     searchFlags,
		Exec:      searchConfig.Exec,
	}
	trcCommand.Subcommands = append(trcCommand.Subcommands, searchCommand)

	// Config for `trc stream`.
	streamConfig := &streamConfig{rootConfig: rootConfig}
	streamFlags := ff.NewFlagSet("stream").SetParent(trcFlags)
	streamConfig.register(streamFlags)
	streamCommand := &ff.Command{
		Name:      "stream",
		ShortHelp: "stream trace data to the terminal",
		LongHelp:  "Stream traces, or trace events, that match the filter.",
		Flags:     streamFlags,
		Exec:      streamConfig.Exec,
	}
	trcCommand.Subcommands = append(trcCommand.Subcommands, streamCommand)

	// Print help when appropriate.
	showHelp := true
	defer func() {
		if showHelp || errors.Is(err, ff.ErrHelp) {
			fmt.Fprintf(stderr, "\n%s\n", ffhelp.Command(trcCommand))
		}
	}()

	// Initial parsing.
	if err := trcCommand.Parse(args, ff.WithEnvVarPrefix("TRC")); err != nil {
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
	return trcCommand.Run(ctx)
}
