package trcpubsub

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

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

func NewBroker[T any](transform func(T) T) *Broker[T] {
	return &Broker[T]{
		transform:   transform,
		subscribers: map[chan<- T]*subscriber[T]{},
	}
}

func (b *Broker[T]) Publish(ctx context.Context, val T) {
	if !b.active.Load() { // optimization
		return
	}

	b.mtx.Lock()
	defer b.mtx.Unlock()

	if len(b.subscribers) <= 0 { // re-check, might have changed
		return
	}

	if b.transform != nil {
		val = b.transform(val)
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

func (b *Broker[T]) Stats(ctx context.Context, ch chan<- T) (Stats, error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	sub, ok := b.subscribers[ch]
	if !ok {
		return Stats{}, fmt.Errorf("not subscribed")
	}

	return sub.stats, nil
}

type Stats struct {
	Skips uint64 `json:"skips"`
	Sends uint64 `json:"sends"`
	Drops uint64 `json:"drops"`
}

func (s Stats) String() string {
	return fmt.Sprintf("skips=%d sends=%d drops=%d", s.Skips, s.Sends, s.Drops)
}
