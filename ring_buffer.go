package trc

import (
	"sync"
)

type ringBuffer[T any] struct {
	mtx sync.Mutex
	buf []T // fully allocated at construction
	cur int // index for next write, walk backwards to read
	len int // count of actual values
}

func newRingBuffer[T any](cap int) *ringBuffer[T] {
	return &ringBuffer[T]{
		buf: make([]T, cap),
	}
}

func (rb *ringBuffer[T]) add(v T) {
	rb.mtx.Lock()
	defer rb.mtx.Unlock()

	// Safety first.
	if cap(rb.buf) <= 0 {
		return
	}

	// Write the value at the write cursor.
	rb.buf[rb.cur] = v

	// Update the ring buffer size.
	if rb.len < len(rb.buf) {
		rb.len += 1
	}

	// Advance the write cursor.
	rb.cur += 1
	if rb.cur >= len(rb.buf) {
		rb.cur -= len(rb.buf)
	}
}

func (rb *ringBuffer[T]) walk(f func(T) error) error {
	rb.mtx.Lock()
	defer rb.mtx.Unlock()

	// Read up to rb.len values.
	for i := 0; i < rb.len; i++ {
		// Reads go backwards from one before the write cursor.
		cur := rb.cur - 1 - i

		// Wrap around when necessary.
		if cur < 0 {
			cur += rb.len
		}

		// If the function returns an error, we're done.
		if err := f(rb.buf[cur]); err != nil {
			return err
		}
	}

	return nil
}

func (rb *ringBuffer[T]) stats() (newest, oldest T, count int) {
	rb.mtx.Lock()
	defer rb.mtx.Unlock()

	// The cursor math assumes a non-empty buffer.
	if rb.len == 0 {
		var zero T
		return zero, zero, 0
	}

	// The read head is the value just before the write cursor.
	headidx := rb.cur - 1
	if headidx < 0 {
		headidx += rb.len
	}

	// The read tail is len+1 values back from the read head.
	// If the buffer is full, this is the write cursor.
	tailidx := headidx - rb.len + 1
	if tailidx < 0 {
		tailidx += rb.len
	}

	return rb.buf[headidx], rb.buf[tailidx], rb.len
}

//
//
//

type ringBuffers[T any] struct {
	mtx sync.Mutex
	max int
	set map[string]*ringBuffer[T]
}

func newRingBuffers[T any](max int) *ringBuffers[T] {
	return &ringBuffers[T]{
		max: max,
		set: map[string]*ringBuffer[T]{},
	}
}

func (rbs *ringBuffers[T]) getOrCreate(name string) *ringBuffer[T] {
	rbs.mtx.Lock()
	defer rbs.mtx.Unlock()

	rb, ok := rbs.set[name]
	if !ok {
		rb = newRingBuffer[T](rbs.max)
		rbs.set[name] = rb
	}

	return rb
}

func (rbs *ringBuffers[T]) getAll() map[string]*ringBuffer[T] {
	rbs.mtx.Lock()
	defer rbs.mtx.Unlock()

	all := make(map[string]*ringBuffer[T], len(rbs.set))
	for name, rb := range rbs.set {
		all[name] = rb
	}

	return all
}
