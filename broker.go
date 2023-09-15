package trc

import (
	"context"
	"fmt"
	"sync"
)

type Broker struct {
	mtx   sync.Mutex
	subs  map[chan<- Trace]*subscriber
	async chan Trace
}

func NewBroker() *Broker {
	return &Broker{
		subs: map[chan<- Trace]*subscriber{},
	}
}

func (b *Broker) Run(ctx context.Context, bufsz int) error {
	async := make(chan Trace, bufsz)

	if running := func() bool {
		b.mtx.Lock()
		defer b.mtx.Unlock()
		if b.async != nil {
			return false
		}
		b.async = async
		return true
	}(); running {
		return fmt.Errorf("already running")
	}

	defer func() {
		b.mtx.Lock()
		defer b.mtx.Unlock()
		b.async = nil
	}()

	defer func() {
		close(async)
		for range async {
			//
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case tr := <-async:
				b.publish(ctx, tr)
			}
		}
	}()

	<-done

	return ctx.Err()
}

func (b *Broker) Publish(ctx context.Context, tr Trace) {
	var async chan Trace

	b.mtx.Lock()
	async = b.async
	b.mtx.Unlock()

	switch {
	case async == nil:
		b.publish(ctx, tr)

	case async != nil:
		select {
		case async <- tr:
			// ok
		default:
			// drop
		}
	}
}

func (b *Broker) publish(ctx context.Context, tr Trace) {
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

func (b *Broker) StreamStats(ctx context.Context, ch chan<- Trace) (StreamStats, error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	sub, ok := b.subs[ch]
	if !ok {
		return StreamStats{}, fmt.Errorf("not subscribed")
	}

	return sub.stats, nil
}

type StreamStats struct {
	Skips int `json:"skips"`
	Sends int `json:"sends"`
	Drops int `json:"drops"`
}

func (s StreamStats) String() string {
	return fmt.Sprintf("skips=%d sends=%d drops=%d", s.Skips, s.Sends, s.Drops)
}

type subscriber struct {
	traces chan<- Trace
	filter Filter
	stats  StreamStats
}
