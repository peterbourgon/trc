package trc

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
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

// TestLogWriterDecorator is like LogDecorator, but writes to the test output.
func TestLogWriterDecorator(tb testing.TB) DecoratorFunc {
	return LogDecorator(tb.Output())
}

type loggerWriter struct{ logger *log.Logger }

func (lw *loggerWriter) Write(p []byte) (int, error) {
	lw.logger.Print(string(p))
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
	ltr.logEvent(format+"  <ERROR>", args...)
	ltr.Trace.Errorf(format, args...)
}

func (ltr *logTrace) LazyErrorf(format string, args ...any) {
	ltr.logEvent(format+"  <ERROR>", args...)
	ltr.Trace.LazyErrorf(format, args...)
}

func (ltr *logTrace) Finish() {
	if ltr.Trace.Finished() {
		return
	}

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

	ltr.Trace.Finish()
}

func (ltr *logTrace) logEvent(format string, args ...any) {
	if ltr.Trace.Finished() {
		return
	}
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

//
//
//

// WrapDecorator returns a DecoratorFunc that wraps traces using [WrapTrace].
func WrapDecorator(name string) DecoratorFunc {
	return func(tr Trace) Trace {
		return NewWrapTrace(tr, WrapTraceConfig{
			Name:          name,
			EventPrefix:   "·",
			ShowEnterExit: true,
		})
	}
}

// WrapTrace is a trace decorator that adds a name and optional event prefix to
// all events, and optionally logs "enter" and "exit" events when the trace is
// created and finished, respectively.
type WrapTrace struct {
	Trace
	name          string
	eventPrefix   string
	showEnterExit bool
	start         time.Time

	mtx  sync.Mutex
	done bool
}

// WrapTraceConfig contains configuration options for [WrapTrace].
type WrapTraceConfig struct {
	Name          string
	EventPrefix   string
	ShowEnterExit bool
}

// NewWrapTrace wraps tr according to config.
func NewWrapTrace(tr Trace, config WrapTraceConfig) *WrapTrace {
	if config.Name == "" {
		config.Name = "(anonymous)"
	}

	if config.EventPrefix != "" {
		config.EventPrefix = strings.TrimSpace(config.EventPrefix) + " "
	}

	wtr := &WrapTrace{
		Trace:         tr,
		name:          config.Name,
		eventPrefix:   config.EventPrefix,
		showEnterExit: config.ShowEnterExit,
		start:         time.Now().UTC(),
	}

	if wtr.showEnterExit {
		tr.LazyTracef("→ %s", config.Name)
	}
	return wtr
}

// Tracef implements Trace.
func (wtr *WrapTrace) Tracef(format string, args ...any) {
	wtr.mtx.Lock()
	defer wtr.mtx.Unlock()

	if wtr.done {
		return
	}

	wtr.Trace.Tracef(wtr.eventPrefix+format, args...)
}

// LazyTracef implements Trace.
func (wtr *WrapTrace) LazyTracef(format string, args ...any) {
	wtr.mtx.Lock()
	defer wtr.mtx.Unlock()

	if wtr.done {
		return
	}

	wtr.Trace.LazyTracef(wtr.eventPrefix+format, args...)
}

// Errorf implements Trace.
func (wtr *WrapTrace) Errorf(format string, args ...any) {
	wtr.mtx.Lock()
	defer wtr.mtx.Unlock()

	if wtr.done {
		return
	}

	wtr.Trace.Errorf(wtr.eventPrefix+format, args...)
}

// LazyErrorf implements Trace.
func (wtr *WrapTrace) LazyErrorf(format string, args ...any) {
	wtr.mtx.Lock()
	defer wtr.mtx.Unlock()

	if wtr.done {
		return
	}

	wtr.Trace.LazyErrorf(wtr.eventPrefix+format, args...)
}

// Finish the outer (wrap) trace, without finishing the inner (wrapped) trace.
func (wtr *WrapTrace) Finish() {
	wtr.mtx.Lock()
	defer wtr.mtx.Unlock()

	if wtr.done {
		return
	}

	if wtr.showEnterExit {
		wtr.Trace.LazyTracef("← %s [%s]", wtr.name, trcutil.HumanizeDuration(time.Since(wtr.start)))
	}

	wtr.done = true
}
