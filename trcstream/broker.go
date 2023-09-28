package trcstream

import (
	"context"

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

func (b *Broker) Publish(tr trc.Trace) {
	b.broker.Publish(tr)
}

type Stats = trcpubsub.Stats

func (b *Broker) Stream(ctx context.Context, f trc.Filter, ch chan<- trc.Trace) (Stats, error) {
	return b.broker.Subscribe(ctx, f.Allow, ch)
}

func (b *Broker) StreamStats(ctx context.Context, ch chan<- trc.Trace) (Stats, error) {
	return b.broker.Stats(ctx, ch)
}

//

func (b *Broker) PublishTracesDecorator() trc.DecoratorFunc {
	return b.publishDecorator(false)
}

func (b *Broker) PublishEventsDecorator() trc.DecoratorFunc {
	return b.publishDecorator(true)
}

func (b *Broker) publishDecorator(publishEvents bool) trc.DecoratorFunc {
	return func(tr trc.Trace) trc.Trace {
		if !b.broker.IsActive() { // TODO: maybe not a good optimization?
			return tr
		}
		ptr := &publishTrace{
			Trace:         tr,
			broker:        b,
			publishEvents: publishEvents,
		}
		if ptr.publishEvents {
			b.Publish(ptr.Trace)
		}
		return ptr
	}
}

//
//
//

type publishTrace struct {
	trc.Trace
	broker        *Broker
	publishEvents bool
}

var _ trc.Freer = (*publishTrace)(nil)

func (ptr *publishTrace) Tracef(format string, args ...any) {
	ptr.Trace.Tracef(format, args...)
	if ptr.publishEvents {
		ptr.broker.Publish(ptr.Trace)
	}
}

func (ptr *publishTrace) LazyTracef(format string, args ...any) {
	ptr.Trace.LazyTracef(format, args...)
	if ptr.publishEvents {
		ptr.broker.Publish(ptr.Trace)
	}
}

func (ptr *publishTrace) Errorf(format string, args ...any) {
	ptr.Trace.Errorf(format, args...)
	if ptr.publishEvents {
		ptr.broker.Publish(ptr.Trace)
	}
}

func (ptr *publishTrace) LazyErrorf(format string, args ...any) {
	ptr.Trace.LazyErrorf(format, args...)
	if ptr.publishEvents {
		ptr.broker.Publish(ptr.Trace)
	}
}

func (ptr *publishTrace) Finish() {
	ptr.Trace.Finish()
	ptr.broker.Publish(ptr.Trace)
}

func (ptr *publishTrace) Free() {
	if f, ok := ptr.Trace.(trc.Freer); ok {
		f.Free()
	}
}

func (ptr *publishTrace) StreamEvents() ([]trc.Event, bool) {
	if s, ok := ptr.Trace.(interface{ StreamEvents() ([]trc.Event, bool) }); ok {
		return s.StreamEvents()
	}
	return nil, false
}
