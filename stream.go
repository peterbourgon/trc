package trc

import (
	"errors"
	"sync"
)

type stream[T any] struct {
	mtx  sync.Mutex
	subs map[chan<- T]*StreamStats
}

type StreamStats struct {
	Sends uint64
	Drops uint64
}

func newStream[T any]() *stream[T] {
	return &stream[T]{
		subs: map[chan<- T]*StreamStats{},
	}
}

func (s *stream[T]) subscribe(c chan<- T) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if _, ok := s.subs[c]; ok {
		return ErrAlreadySubscribed
	}

	s.subs[c] = &StreamStats{}
	return nil
}

func (s *stream[T]) unsubscribe(c chan<- T) (sends, drops uint64, _ error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	stats, ok := s.subs[c]
	if !ok {
		return 0, 0, ErrNotSubscribed
	}

	delete(s.subs, c)
	return stats.Sends, stats.Drops, nil
}

func (s *stream[T]) stats(c chan<- T) (sends, drops uint64, _ error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	stats, ok := s.subs[c]
	if !ok {
		return 0, 0, ErrNotSubscribed
	}

	return stats.Sends, stats.Drops, nil
}

var (
	ErrAlreadySubscribed = errors.New("already subscribed")
	ErrNotSubscribed     = errors.New("not subscribed")
)

func (s *stream[T]) broadcast(t T) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for c, stats := range s.subs {
		select {
		case c <- t:
			stats.Sends++
		default:
			stats.Drops++
		}
	}
}
