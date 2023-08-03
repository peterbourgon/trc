package trc

import (
	"context"
	"fmt"
	"io"
	"strings"
)

type DecoratorFunc func(Trace) Trace

//
//
//

func DecorateNewTrace(newTrace NewTraceFunc, decorators ...DecoratorFunc) NewTraceFunc {
	return func(ctx context.Context, source, category string) (context.Context, Trace) {
		ctx, tr := newTrace(ctx, source, category)

		if len(decorators) > 0 {
			for _, decorator := range decorators {
				tr = decorator(tr)
			}
			ctx, tr = Put(ctx, tr)
		}

		return ctx, tr
	}
}

//
//
//

func LogDecorator(dst io.Writer) DecoratorFunc {
	return func(tr Trace) Trace {
		return &logTrace{
			Trace: tr,
			id:    tr.ID(),
			dst:   dst,
		}
	}
}

type logTrace struct {
	Trace
	id  string
	dst io.Writer
}

func (ltr *logTrace) Tracef(format string, args ...any) {
	ltr.logEvent("TRC", format, args...)
	ltr.Trace.Tracef(format, args...)
}

func (ltr *logTrace) LazyTracef(format string, args ...any) {
	ltr.logEvent("TRC", format, args...)
	ltr.Trace.LazyTracef(format, args...)
}

func (ltr *logTrace) Errorf(format string, args ...any) {
	ltr.logEvent("ERR", format, args...)
	ltr.Trace.Errorf(format, args...)
}

func (ltr *logTrace) LazyErrorf(format string, args ...any) {
	ltr.logEvent("ERR", format, args...)
	ltr.Trace.LazyErrorf(format, args...)
}

func (ltr *logTrace) logEvent(what, format string, args ...any) {
	format = ltr.id + " " + what + " " + strings.TrimSuffix(format, "\n") + "\n"
	fmt.Fprintf(ltr.dst, format, args...)
}

//
//
//

func PublishDecorator(p Publisher) DecoratorFunc {
	return func(tr Trace) Trace {
		return &publishTrace{
			Trace: tr,
			p:     p,
		}
	}
}

type Publisher interface {
	Publish(ctx context.Context, tr Trace)
}

type publishTrace struct {
	Trace
	p Publisher
}

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
