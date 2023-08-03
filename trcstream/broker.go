package trcstream

import (
	"context"
	"fmt"
	"sync"

	"github.com/peterbourgon/trc"
)

type Broker struct {
	mtx  sync.Mutex
	subs map[chan<- trc.Trace]*Subscriber
}

func NewBroker() *Broker {
	return &Broker{
		subs: map[chan<- trc.Trace]*Subscriber{},
	}
}

func (b *Broker) Publish(ctx context.Context, tr trc.Trace) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	for _, sub := range b.subs {
		if !sub.filter.Allow(tr) {
			sub.stats.skips++
			continue
		}
		select {
		case sub.c <- tr:
			sub.stats.sends++
		default:
			sub.stats.drops++
		}
	}
}

func (b *Broker) Subscribe(ctx context.Context, c chan<- trc.Trace, f trc.Filter) error {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if _, ok := b.subs[c]; ok {
		return fmt.Errorf("already subscribed")
	}

	b.subs[c] = &Subscriber{
		c:      c,
		filter: f,
	}
	return nil
}

func (b *Broker) Unsubscribe(ctx context.Context, c chan<- trc.Trace) (skips, sends, drops int, err error) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	sub, ok := b.subs[c]
	if !ok {
		return 0, 0, 0, fmt.Errorf("not subscribed")
	}

	delete(b.subs, c)

	return sub.stats.skips, sub.stats.sends, sub.stats.drops, nil
}

type Subscriber struct {
	c      chan<- trc.Trace
	filter trc.Filter
	stats  stats
}

type stats struct {
	skips int
	sends int
	drops int
}
