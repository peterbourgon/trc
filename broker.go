package trc

import (
	"context"
	"fmt"
	"sync"
)

type Broker struct {
	mtx  sync.Mutex
	subs map[chan<- Trace]*subscriber
}

func NewBroker() *Broker {
	return &Broker{
		subs: map[chan<- Trace]*subscriber{},
	}
}

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
