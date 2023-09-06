package trc

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/peterbourgon/trc/internal/trcutil"
)

// DecoratorFunc is a function that decorates a trace in some way. It's similar
// to an HTTP middleware.
type DecoratorFunc func(Trace) Trace

//
//
//

// PublishDecorator returns a decorator that publishes the trace to the
// publisher when it's created, on every event, and when the trace is finished.
// The published trace is a reduced form of the full trace, containing only the
// core metadata and, in the case of trace events, the single event that
// triggered the publish.
func PublishDecorator(p Publisher) DecoratorFunc {
	return func(tr Trace) Trace {
		ptr := &publishTrace{
			Trace: tr,
			p:     p,
		}
		p.Publish(context.Background(), ptr.Trace)
		return ptr
	}
}

// Publisher is a consumer contract for the [PublishDecorator] which describes
// anything that can publish a trace. It models the [trcstream.Broker].
type Publisher interface {
	Publish(ctx context.Context, tr Trace)
}

type publishTrace struct {
	Trace
	p Publisher
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

//
//
//

// LogDecorator returns a decorator that logs a simple string to the provided
// destination when the trace is created, on every event, and when the trace is
// finished. The logged string is a reduced form of the full trace, containing
// only the trace ID, the event type, and the single event that triggered the
// log.
func LogDecorator(dst io.Writer) DecoratorFunc {
	return func(tr Trace) Trace {
		ltr := &logTrace{
			Trace:    tr,
			id:       tr.ID(),
			category: tr.Category(),
			dst:      dst,
		}
		ltr.logEvent("BEGIN", "source '%s' category '%s'", ltr.Trace.Source(), ltr.Trace.Category())
		return ltr
	}
}

type logTrace struct {
	Trace
	id       string
	category string
	dst      io.Writer
}

var _ interface{ Free() } = (*logTrace)(nil)

func (ltr *logTrace) Tracef(format string, args ...any) {
	ltr.logEvent("TRACE", format, args...)
	ltr.Trace.Tracef(format, args...)
}

func (ltr *logTrace) LazyTracef(format string, args ...any) {
	ltr.logEvent("TRACE", format, args...)
	ltr.Trace.LazyTracef(format, args...)
}

func (ltr *logTrace) Errorf(format string, args ...any) {
	ltr.logEvent("ERROR", format, args...)
	ltr.Trace.Errorf(format, args...)
}

func (ltr *logTrace) LazyErrorf(format string, args ...any) {
	ltr.logEvent("ERROR", format, args...)
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
	ltr.logEvent("FINIS", "%s %s", outcome, duration)
}

func (ltr *logTrace) logEvent(what, format string, args ...any) {
	format = "[" + ltr.category + "] " + ltr.id + " " + what + " " + strings.TrimSuffix(format, "\n") + "\n"
	fmt.Fprintf(ltr.dst, format, args...)
}

func (ltr *logTrace) Free() {
	if f, ok := ltr.Trace.(interface{ Free() }); ok {
		f.Free()
	}
}