package trc

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/peterbourgon/trc/internal/trcutil"
)

// DecoratorFunc is a function that decorates a trace in some way. It's similar
// to an HTTP middleware. Decorators can be provided to a [Collector] and will
// be applied to every trace created in that collector.
type DecoratorFunc func(Trace) Trace

//
//
//

// LogDecorator logs a simple string to the provided destination when the trace
// is created, on every event, and when the trace is finished. The logged string
// is a reduced form of the full trace, containing only the trace ID and the
// single event that triggered the log.
func LogDecorator(dst io.Writer) DecoratorFunc {
	return func(tr Trace) Trace {
		ltr := &logTrace{
			Trace: tr,
			id:    tr.ID(),
			dst:   dst,
		}
		ltr.logEvent("started, source '%s', category '%s'", tr.Source(), tr.Category())
		return ltr
	}
}

// LoggerDecorator is like LogDecorator, but uses a log.Logger.
func LoggerDecorator(logger *log.Logger) DecoratorFunc {
	return LogDecorator(&loggerWriter{logger})
}

type loggerWriter struct{ logger *log.Logger }

func (lw *loggerWriter) Write(p []byte) (int, error) {
	lw.logger.Printf(string(p))
	return len(p), nil
}

type logTrace struct {
	Trace
	id  string
	dst io.Writer
}

var _ interface{ Free() } = (*logTrace)(nil)

func (ltr *logTrace) Tracef(format string, args ...any) {
	ltr.logEvent(format, args...)
	ltr.Trace.Tracef(format, args...)
}

func (ltr *logTrace) LazyTracef(format string, args ...any) {
	ltr.logEvent(format, args...)
	ltr.Trace.LazyTracef(format, args...)
}

func (ltr *logTrace) Errorf(format string, args ...any) {
	ltr.logEvent("ERROR: "+format, args...)
	ltr.Trace.Errorf(format, args...)
}

func (ltr *logTrace) LazyErrorf(format string, args ...any) {
	ltr.logEvent("ERROR: "+format, args...)
	ltr.Trace.LazyErrorf(format, args...)
}

func (ltr *logTrace) Finish() {
	ltr.Trace.Finish()
	var (
		outcome  = "unknown"
		duration = trcutil.HumanizeDuration(ltr.Trace.Duration())
	)
	switch {
	case ltr.Errored():
		outcome = "errored"
	default:
		outcome = "success"
	}
	ltr.logEvent("done, %s, %s", outcome, duration)
}

func (ltr *logTrace) logEvent(format string, args ...any) {
	format = ltr.id + " " + strings.TrimSuffix(format, "\n") + "\n"
	fmt.Fprintf(ltr.dst, format, args...)
}

func (ltr *logTrace) Free() {
	if f, ok := ltr.Trace.(interface{ Free() }); ok {
		f.Free()
	}
}

//
//
//

func publishDecorator(p publisher) DecoratorFunc {
	return func(tr Trace) Trace {
		ptr := &publishTrace{
			Trace: tr,
			p:     p,
		}
		p.Publish(context.Background(), ptr.Trace)
		return ptr
	}
}

type publisher interface {
	Publish(ctx context.Context, tr Trace)
}

type publishTrace struct {
	Trace
	p publisher
}

var _ interface{ Free() } = (*publishTrace)(nil)

func (ptr *publishTrace) Tracef(format string, args ...any) {
	ptr.Trace.Tracef(format, args...)
	ptr.p.Publish(context.Background(), ptr.Trace)
}

func (ptr *publishTrace) LazyTracef(format string, args ...any) {
	ptr.Trace.LazyTracef(format, args...)
	ptr.p.Publish(context.Background(), ptr.Trace)
}

func (ptr *publishTrace) Errorf(format string, args ...any) {
	ptr.Trace.Errorf(format, args...)
	ptr.p.Publish(context.Background(), ptr.Trace)
}

func (ptr *publishTrace) LazyErrorf(format string, args ...any) {
	ptr.Trace.LazyErrorf(format, args...)
	ptr.p.Publish(context.Background(), ptr.Trace)
}

func (ptr *publishTrace) Finish() {
	ptr.Trace.Finish()
	ptr.p.Publish(context.Background(), ptr.Trace)
}

func (ptr *publishTrace) Free() {
	if f, ok := ptr.Trace.(interface{ Free() }); ok {
		f.Free()
	}
}

func (ptr *publishTrace) EventCount() int {
	if ec, ok := ptr.Trace.(interface{ EventCount() int }); ok {
		return ec.EventCount()
	}
	return len(ptr.Trace.Events())
}

func (ptr *publishTrace) ObserveStats(cs *CategoryStats, bucketing []time.Duration) bool {
	if os, ok := ptr.Trace.(interface {
		ObserveStats(cs *CategoryStats, bucketing []time.Duration) bool
	}); ok {
		return os.ObserveStats(cs, bucketing)
	}
	return false
}
