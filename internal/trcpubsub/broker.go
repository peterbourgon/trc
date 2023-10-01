package trcpubsub

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// Broker provides publish and subscribe semantics for values of any type T.
type Broker[T any] struct {
	mtx         sync.Mutex
	transform   func(T) T
	subscribers map[chan<- T]*subscriber[T]
	active      atomic.Bool
}

type subscriber[T any] struct {
	allow func(T) bool
	ch    chan<- T
	stats Stats
}

// NewBroker returns a new broker for type T. If the transform function is
// non-nil, it will be applied to every published value before that value is
// emitted to subscribers.
func NewBroker[T any](transform func(T) T) *Broker[T] {
	return &Broker[T]{
		transform:   transform,
		subscribers: map[chan<- T]*subscriber[T]{},
	}
}

// IsActive returns true if there are any active subscribers.
func (b *Broker[T]) IsActive() bool {
	return b.active.Load()
}

// Publish the value to all active subscribers that allow it. Publishing takes
// an exclusive lock on the broker, but doesn't block when sending the value to
// matching subscribers.
func (b *Broker[T]) Publish(val T) {
	if !b.active.Load() { // optimization
		return
	}

	if b.transform != nil {
		val = b.transform(val)
	}

	b.mtx.Lock()
	defer b.mtx.Unlock()

	if len(b.subscribers) <= 0 { // re-check, might have changed
		return
	}

	for _, sub := range b.subscribers {
		if !sub.allow(val) {
			sub.stats.Skips++
			continue
		}
		select {
		case sub.ch <- val:
			sub.stats.Sends++
		default:
			sub.stats.Drops++
		}
	}
}

// Subscribe forwards all published values which pass the allow function to the
// provided channel ch. If the channel is not able to receive a published value,
// the value will be dropped. Subscribe returns when the context is canceled.
func (b *Broker[T]) Subscribe(ctx context.Context, allow func(T) bool, ch chan<- T) (Stats, error) {
	if err := func() error {
		b.mtx.Lock()
		defer b.mtx.Unlock()

		if _, ok := b.subscribers[ch]; ok {
			return fmt.Errorf("already subscribed")
		}

		b.subscribers[ch] = &subscriber[T]{
			allow: allow,
			ch:    ch,
		}

		b.active.Store(len(b.subscribers) > 0)

		return nil
	}(); err != nil {
		return Stats{}, err
	}

	<-ctx.Done()

	sub := func() *subscriber[T] {
		b.mtx.Lock()
		defer b.mtx.Unlock()

		sub := b.subscribers[ch]
		delete(b.subscribers, ch)

		b.active.Store(len(b.subscribers) > 0)

		return sub
	}()
	if sub == nil {
		return Stats{}, fmt.Errorf("not subscribed (programmer error)")
	}

	return sub.stats, ctx.Err()
}

// Stats returns statistics for the active subscription represented by ch.
func (b *Broker[T]) Stats(ctx context.Context, ch chan<- T) (Stats, error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	sub, ok := b.subscribers[ch]
	if !ok {
		return Stats{}, fmt.Errorf("not subscribed")
	}

	return sub.stats, nil
}

// Stats represents details about published values for a specific subscription.
type Stats struct {
	Skips uint64 `json:"skips"`
	Sends uint64 `json:"sends"`
	Drops uint64 `json:"drops"`
}

// String implements [fmt.Stringer].
func (s Stats) String() string {
	return fmt.Sprintf("skips=%d sends=%d drops=%d", s.Skips, s.Sends, s.Drops)
}
