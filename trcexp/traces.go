package trcexp

import (
	"fmt"
	"log"
	"strings"

	"github.com/peterbourgon/trc"
)

type LoggerTrace struct {
	trc.Trace

	Prefix func(tr trc.Trace, isError bool) string
	Logger *log.Logger
}

func LoggerTracePrefixDefault(tr trc.Trace, isError bool) string {
	te := "t"
	if isError {
		te = "E"
	}
	return fmt.Sprintf("%s [%s] %s", tr.ID(), tr.Category(), te)
}

func (ltr *LoggerTrace) Tracef(format string, args ...any) {
	ltr.Logger.Printf(ltr.Prefix(ltr.Trace, false)+" "+strings.TrimSpace(format), args...)
	ltr.Trace.Tracef(format, args...)
}

func (ltr *LoggerTrace) LazyTracef(format string, args ...any) {
	ltr.Logger.Printf(ltr.Prefix(ltr.Trace, false)+" "+strings.TrimSpace(format), args...)
	ltr.Trace.LazyTracef(format, args...)
}

func (ltr *LoggerTrace) Errorf(format string, args ...any) {
	ltr.Logger.Printf(ltr.Prefix(ltr.Trace, true)+" "+strings.TrimSpace(format), args...)
	ltr.Trace.Errorf(format, args...)
}

func (ltr *LoggerTrace) LazyErrorf(format string, args ...any) {
	ltr.Logger.Printf(ltr.Prefix(ltr.Trace, true)+" "+strings.TrimSpace(format), args...)
	ltr.Trace.LazyErrorf(format, args...)
}
