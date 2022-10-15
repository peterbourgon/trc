package trc

/*
import (
	"errors"
	"sync"
)

type Stream[T any] struct {
	mtx  sync.Mutex
	subs map[chan<- T]*StreamStats
}

type StreamStats struct {
	Sends uint64
	Drops uint64
}

func NewStream[T any]() *Stream[T] {
	return &Stream[T]{
		subs: map[chan<- T]*StreamStats{},
	}
}

func (s *Stream[T]) Subscribe(c chan<- T) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if _, ok := s.subs[c]; ok {
		return ErrAlreadySubscribed
	}

	s.subs[c] = &StreamStats{}
	return nil
}

func (s *Stream[T]) Unsubscribe(c chan<- T) (StreamStats, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	stats, ok := s.subs[c]
	if !ok {
		return StreamStats{}, ErrNotSubscribed
	}

	delete(s.subs, c)
	return *stats, nil
}

var (
	ErrAlreadySubscribed = errors.New("already subscribed")
	ErrNotSubscribed     = errors.New("not subscribed")
)

func (s *Stream[T]) Broadcast(t T) {
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
*/
