package trcstream

import (
	"context"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcpubsub"
)

type Broker struct {
	broker *trcpubsub.Broker[trc.Trace]
}

func NewBroker() *Broker {
	return &Broker{
		broker: trcpubsub.NewBroker(func(tr trc.Trace) trc.Trace { return trc.NewStreamTrace(tr) }),
	}
}

type Streamer interface {
	Stream(ctx context.Context, f trc.Filter, ch chan<- trc.Trace) (Stats, error)
	StreamStats(ctx context.Context, ch chan<- trc.Trace) (Stats, error)
}

var _ Streamer = (*Broker)(nil)

func (b *Broker) Publish(ctx context.Context, tr trc.Trace) {
	b.broker.Publish(ctx, tr)
}

type Stats = trcpubsub.Stats

func (b *Broker) Stream(ctx context.Context, f trc.Filter, ch chan<- trc.Trace) (Stats, error) {
	return b.broker.Subscribe(ctx, f.Allow, ch)
}

func (b *Broker) StreamStats(ctx context.Context, ch chan<- trc.Trace) (Stats, error) {
	return b.broker.Stats(ctx, ch)
}

func (b *Broker) PublishDecorator() trc.DecoratorFunc {
	return func(tr trc.Trace) trc.Trace {
		ptr := &publishTrace{
			Trace:  tr,
			broker: b,
		}
		b.Publish(context.Background(), ptr.Trace)
		return ptr
	}
}

type publishTrace struct {
	trc.Trace
	broker *Broker
}

var _ interface{ Free() } = (*publishTrace)(nil)

func (ptr *publishTrace) Tracef(format string, args ...any) {
	ptr.Trace.Tracef(format, args...)
	ptr.broker.Publish(context.Background(), ptr.Trace)
}

func (ptr *publishTrace) LazyTracef(format string, args ...any) {
	ptr.Trace.LazyTracef(format, args...)
	ptr.broker.Publish(context.Background(), ptr.Trace)
}

func (ptr *publishTrace) Errorf(format string, args ...any) {
	ptr.Trace.Errorf(format, args...)
	ptr.broker.Publish(context.Background(), ptr.Trace)
}

func (ptr *publishTrace) LazyErrorf(format string, args ...any) {
	ptr.Trace.LazyErrorf(format, args...)
	ptr.broker.Publish(context.Background(), ptr.Trace)
}

func (ptr *publishTrace) Finish() {
	ptr.Trace.Finish()
	ptr.broker.Publish(context.Background(), ptr.Trace)
}

func (ptr *publishTrace) Free() {
	if f, ok := ptr.Trace.(interface{ Free() }); ok {
		f.Free()
	}
}

func (ptr *publishTrace) ObserveStats(cs *trc.CategoryStats, bucketing []time.Duration) bool {
	if os, ok := ptr.Trace.(interface {
		ObserveStats(cs *trc.CategoryStats, bucketing []time.Duration) bool
	}); ok {
		return os.ObserveStats(cs, bucketing)
	}
	return false
}
