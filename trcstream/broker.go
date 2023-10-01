package trcstream

import (
	"context"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcpubsub"
)

// Broker provides publish and subscribe semantics for [trc.Trace] values.
type Broker struct {
	broker *trcpubsub.Broker[trc.Trace]
}

// NewBroker returns a broker that transforms traces via [trc.NewStreamTrace].
func NewBroker() *Broker {
	return &Broker{
		broker: trcpubsub.NewBroker(func(tr trc.Trace) trc.Trace { return trc.NewStreamTrace(tr) }),
	}
}

// Streamer is analogous to [trc.Searcher] and describes the broker.
type Streamer interface {
	Stream(ctx context.Context, f trc.Filter, ch chan<- trc.Trace) (Stats, error)
	StreamStats(ctx context.Context, ch chan<- trc.Trace) (Stats, error)
}

var _ Streamer = (*Broker)(nil)

// Publish the trace to any active subscribers.
func (b *Broker) Publish(tr trc.Trace) {
	b.broker.Publish(tr)
}

// Stats for active subscribers.
type Stats = trcpubsub.Stats

// Stream traces matching the filter to the provided channel. The method blocks
// until ctx is canceled.
func (b *Broker) Stream(ctx context.Context, f trc.Filter, ch chan<- trc.Trace) (Stats, error) {
	return b.broker.Subscribe(ctx, f.Allow, ch)
}

// StreamStats for the active stream represented by the given channel.
func (b *Broker) StreamStats(ctx context.Context, ch chan<- trc.Trace) (Stats, error) {
	return b.broker.Stats(ctx, ch)
}

//

// PublishTracesDecorator returns a decorator that will publish complete traces
// when they are finished.
func (b *Broker) PublishTracesDecorator() trc.DecoratorFunc {
	return b.publishDecorator(false)
}

// PublishEventsDecorator returns a decorator that will publish each individual
// trace event as they occur, and also complete traces when they are finished.
// Be careful: this can result in an enormous amount of data, which can
// significantly impact the performance of your application.
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
