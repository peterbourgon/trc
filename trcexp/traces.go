package trcexp

import (
	"fmt"
	"log"
	"strings"

	"github.com/peterbourgon/trc"
)

type LoggerTrace struct {
	trc.Trace
	*log.Logger
}

func (ltr *LoggerTrace) Tracef(format string, args ...any) {
	ltr.Logger.Printf(fmt.Sprintf("%s [%s] T", ltr.Trace.ID(), ltr.Trace.Category())+" "+strings.TrimSpace(format), args...)
	ltr.Trace.Tracef(format, args...)
}

func (ltr *LoggerTrace) LazyTracef(format string, args ...any) {
	ltr.Logger.Printf(fmt.Sprintf("%s [%s] T", ltr.Trace.ID(), ltr.Trace.Category())+" "+strings.TrimSpace(format), args...)
	ltr.Trace.LazyTracef(format, args...)
}

func (ltr *LoggerTrace) Errorf(format string, args ...any) {
	ltr.Logger.Printf(fmt.Sprintf("%s [%s] E", ltr.Trace.ID(), ltr.Trace.Category())+" "+strings.TrimSpace(format), args...)
	ltr.Trace.Errorf(format, args...)
}

func (ltr *LoggerTrace) LazyErrorf(format string, args ...any) {
	ltr.Logger.Printf(fmt.Sprintf("%s [%s] E", ltr.Trace.ID(), ltr.Trace.Category())+" "+strings.TrimSpace(format), args...)
	ltr.Trace.LazyErrorf(format, args...)
}
