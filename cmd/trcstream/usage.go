package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/peterbourgon/ff/v4"
)

func usageFunc(fs ff.Flags) string {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "COMMAND\n")
	fmt.Fprintf(&buf, "%s%s\n", defaultUsageIndent, fs.GetName())
	fmt.Fprintf(&buf, "\n")

	writeFlagGroups(fs, &buf)

	return strings.TrimSpace(buf.String())
}

const defaultUsageIndent = "  "

func writeFlagGroups(fs ff.Flags, w io.Writer) {
	for i, g := range makeFlagGroups(fs) {
		switch {
		case i == 0, g.name == "":
			fmt.Fprintf(w, "FLAGS\n")
		default:
			fmt.Fprintf(w, "FLAGS (%s)\n", g.name)
		}
		for _, line := range g.help {
			fmt.Fprintf(w, "%s%s\n", defaultUsageIndent, line)
		}
		fmt.Fprintf(w, "\n")
	}
}

type flagGroup struct {
	name string
	help []string
}

func makeFlagGroups(fs ff.Flags) []flagGroup {
	var (
		order = []string{}
		index = map[string][]ff.Flag{}
	)
	fs.WalkFlags(func(f ff.Flag) error {
		name := f.GetFlagsName()
		if _, ok := index[name]; !ok {
			order = append(order, name)
		}
		index[name] = append(index[name], f)
		return nil
	})

	groups := make([]flagGroup, 0, len(order))
	for _, name := range order {
		flags := index[name]
		help := getFlagsHelp(flags)
		groups = append(groups, flagGroup{
			name: name,
			help: help,
		})
	}

	return groups
}

func getFlagsHelp(flags []ff.Flag) []string {
	var haveShortFlags bool
	for _, f := range flags {
		if _, ok := f.GetShortName(); ok {
			haveShortFlags = true
			break
		}
	}

	var buf bytes.Buffer

	tw := newTabWriter(&buf)
	for _, f := range flags {
		var (
			short, hasShort = f.GetShortName()
			long, hasLong   = f.GetLongName()
			flagNames       string
		)
		switch {
		case hasShort && hasLong:
			flagNames = fmt.Sprintf("-%s, --%s", string(short), long)
		case hasShort && !hasLong:
			flagNames = fmt.Sprintf("-%s", string(short))
		case !hasShort && hasLong && haveShortFlags:
			flagNames = fmt.Sprintf("    --%s", long)
		case !hasShort && hasLong && !haveShortFlags:
			flagNames = fmt.Sprintf("--%s", long)
		}

		if p := f.GetPlaceholder(); p != "" {
			flagNames = fmt.Sprintf("%s %s", flagNames, p)
		}

		flagUsage := f.GetUsage()

		switch d := f.GetDefault(); d {
		case "0s", "false", "":
			//
		default:
			flagUsage = fmt.Sprintf("%s (default: %s)", flagUsage, d)
		}

		fmt.Fprintf(tw, "%s\t%s\n", flagNames, flagUsage)
	}
	tw.Flush()

	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
}
