package trc

import (
	"context"
	"fmt"
	"sync"
)

// Broker allows traces to be published to a set of subscribers.
type Broker struct {
	mtx  sync.Mutex
	subs map[chan<- Trace]*subscriber
}

// NewBroker returns a new, empty broker.
func NewBroker() *Broker {
	return &Broker{
		subs: map[chan<- Trace]*subscriber{},
	}
}

// Publish the trace, transformed via [NewStreamTrace], to any active and
// matching subscribers. Sends to subscribers don't block and will drop.
func (b *Broker) Publish(ctx context.Context, tr Trace) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	// Fast path exit if there are no subscribers.
	if len(b.subs) <= 0 {
		return
	}

	// Need the reduced form so that filter works correctly.
	str := NewStreamTrace(tr)

	for _, sub := range b.subs {
		if !sub.filter.Allow(str) {
			sub.stats.Skips++
			continue
		}

		select {
		case sub.traces <- str:
			sub.stats.Sends++
		default:
			sub.stats.Drops++
		}
	}
}

// Stream will forward a copy of every trace created in the collector matching
// the filter to the provided channel. If the channel is full, traces will be
// dropped. For reasons of efficiency, streamed trace events don't have stacks.
// Stream blocks until the context is canceled.
//
// Note that if the filter has IsActive true, the caller will receive not only
// complete matching traces as they are finished, but also a single-event trace
// for each individual matching event as they are created. This can be an
// enormous volume of data, please be careful.
func (b *Broker) Stream(ctx context.Context, f Filter, ch chan<- Trace) (StreamStats, error) {
	if err := func() error {
		b.mtx.Lock()
		defer b.mtx.Unlock()

		if _, ok := b.subs[ch]; ok {
			return fmt.Errorf("already subscribed")
		}

		b.subs[ch] = &subscriber{
			filter: f,
			traces: ch,
		}

		return nil
	}(); err != nil {
		return StreamStats{}, err
	}

	<-ctx.Done()

	sub := func() *subscriber {
		b.mtx.Lock()
		defer b.mtx.Unlock()

		sub := b.subs[ch]
		delete(b.subs, ch)

		return sub
	}()

	if sub == nil {
		return StreamStats{}, fmt.Errorf("not subscribed (programmer error)")
	}

	return sub.stats, ctx.Err()
}

// StreamStats returns statistics about a currently active subscription.
func (b *Broker) StreamStats(ctx context.Context, ch chan<- Trace) (StreamStats, error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	sub, ok := b.subs[ch]
	if !ok {
		return StreamStats{}, fmt.Errorf("not subscribed")
	}

	return sub.stats, nil
}

// StreamStats is metadata about a currently active subscription.
type StreamStats struct {
	// Skips is how many traces were considered but didn't pass the filter.
	Skips int `json:"skips"`

	// Sends is how many traces were successfully sent to the subscriber.
	Sends int `json:"sends"`

	// Drops is how many traces were dropped due to lack of capacity.
	Drops int `json:"drops"`
}

// String implements fmt.Stringer.
func (s StreamStats) String() string {
	return fmt.Sprintf("skips=%d sends=%d drops=%d", s.Skips, s.Sends, s.Drops)
}

type subscriber struct {
	traces chan<- Trace
	filter Filter
	stats  StreamStats
}
