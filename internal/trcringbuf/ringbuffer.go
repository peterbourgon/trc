package trcringbuf

import (
	"sync"
)

type RingBuffer[T any] struct {
	mtx sync.Mutex
	buf []T // fully allocated at construction
	cur int // index for next write, walk backwards to read
	len int // count of actual values
}

func NewRingBuffer[T any](cap int) *RingBuffer[T] {
	return &RingBuffer[T]{
		buf: make([]T, cap),
	}
}

func (rb *RingBuffer[T]) Resize(cap int) {
	if cap <= 0 {
		return
	}

	rb.mtx.Lock()
	defer rb.mtx.Unlock()

	fill := rb.len
	if fill > cap {
		fill = cap
	}

	rcur := rb.cur - 1
	if rcur < 0 {
		rcur += rb.len
	}

	wcur := fill - 1

	buf := make([]T, cap)

	for wcur >= 0 {
		buf[wcur] = rb.buf[rcur]

		rcur = rcur - 1
		if rcur < 0 {
			rcur += rb.len
		}

		wcur = wcur - 1
	}

	cur := fill
	if cur >= cap {
		cur -= cap
	}

	rb.buf = buf
	rb.cur = cur
	rb.len = fill
}

func (rb *RingBuffer[T]) Add(val T) {
	rb.mtx.Lock()
	defer rb.mtx.Unlock()

	// Safety first.
	if cap(rb.buf) <= 0 {
		return
	}

	// Write the value at the write cursor.
	rb.buf[rb.cur] = val

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

func (rb *RingBuffer[T]) Get() []T {
	var values []T
	rb.Walk(func(t T) error {
		values = append(values, t)
		return nil
	})
	return values
}

func (rb *RingBuffer[T]) Walk(fn func(T) error) error {
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
		if err := fn(rb.buf[cur]); err != nil {
			return err
		}
	}

	return nil
}

func (rb *RingBuffer[T]) Stats() (newest, oldest T, count int) {
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

type RingBuffers[T any] struct {
	mtx  sync.Mutex
	cap  int
	bufs map[string]*RingBuffer[T]
}

func NewRingBuffers[T any](cap int) *RingBuffers[T] {
	return &RingBuffers[T]{
		cap:  cap,
		bufs: map[string]*RingBuffer[T]{},
	}
}

func (rbs *RingBuffers[T]) GetOrCreate(category string) *RingBuffer[T] {
	rbs.mtx.Lock()
	defer rbs.mtx.Unlock()

	rb, ok := rbs.bufs[category]
	if !ok {
		rb = NewRingBuffer[T](rbs.cap)
		rbs.bufs[category] = rb
	}

	return rb
}

func (rbs *RingBuffers[T]) GetAll() map[string]*RingBuffer[T] {
	rbs.mtx.Lock()
	defer rbs.mtx.Unlock()

	all := make(map[string]*RingBuffer[T], len(rbs.bufs))
	for name, rb := range rbs.bufs {
		all[name] = rb
	}

	return all
}

func (rbs *RingBuffers[T]) Resize(cap int) {
	if cap <= 0 {
		return
	}

	rbs.mtx.Lock()
	defer rbs.mtx.Unlock()

	rbs.cap = cap

	for _, rb := range rbs.bufs {
		rb.Resize(cap)
	}
}
