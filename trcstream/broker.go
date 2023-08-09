package trcstream

import (
	"context"
	"fmt"
	"sync"

	"github.com/peterbourgon/trc"
)

type Broker struct {
	mtx  sync.Mutex
	subs map[chan<- trc.Trace]*subscriber
}

func NewBroker() *Broker {
	return &Broker{
		subs: map[chan<- trc.Trace]*subscriber{},
	}
}

func (b *Broker) Publish(ctx context.Context, tr trc.Trace) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if len(b.subs) <= 0 {
		return
	}

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

func (b *Broker) Stream(ctx context.Context, f trc.Filter, ch chan<- trc.Trace) (Stats, error) {
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
		return Stats{}, err
	}

	<-ctx.Done()

	sub := func() *subscriber {
		b.mtx.Lock()
		defer b.mtx.Unlock()

		sub := b.subs[ch]
		delete(b.subs, ch)

		return sub
	}()

	return sub.stats, ctx.Err()
}

func (b *Broker) Stats(ctx context.Context, ch chan<- trc.Trace) (Stats, error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	sub, ok := b.subs[ch]
	if !ok {
		return Stats{}, fmt.Errorf("not subscribed")
	}

	return sub.stats, nil
}

type Stats struct {
	Skips int `json:"skips"`
	Sends int `json:"sends"`
	Drops int `json:"drops"`
}

func (s *Stats) DropRate() float64 {
	var (
		n = float64(s.Drops)
		d = float64(s.Sends + s.Drops)
	)
	if d == 0 {
		return 0
	}
	return n / d
}

type subscriber struct {
	traces chan<- trc.Trace
	filter trc.Filter
	stats  Stats
}
