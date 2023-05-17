package trcringbuf

import (
	"sync"
)

// RingBuffer is a fixed-size collection of recent items.
type RingBuffer[T any] struct {
	mtx sync.Mutex
	buf []T // fully allocated at construction
	cur int // index for next write, walk backwards to read
	len int // count of actual values
}

// NewRingBuffer returns an empty ring buffer of items, pre-allocated with the
// given capacity.
func NewRingBuffer[T any](cap int) *RingBuffer[T] {
	return &RingBuffer[T]{
		buf: make([]T, cap),
	}
}

// Resize changes the capacity of the ring buffer to the given value. If the new
// capacity is smaller than the existing capacity, resize will drop the older
// items as necessary, and return those dropped items.
func (rb *RingBuffer[T]) Resize(cap int) (dropped []T) {
	// Safety first.
	if cap <= 0 {
		return
	}

	rb.mtx.Lock()
	defer rb.mtx.Unlock()

	// Calculate how many values to fill from the old buffer to the new one.
	fill := rb.len
	if fill > cap {
		fill = cap
	}

	// Calculate the read cursor for the old buffer.
	rdcur := rb.cur - 1
	if rdcur < 0 {
		rdcur += rb.len
	}

	// Construct the new buffer with the given capacity. As fill is guaranteed
	// to be less than or equal to cap, we calculate the write cursor as simply
	// fill, and will copy values by walking both cursors backwards.
	buf := make([]T, cap)
	wrcur := fill - 1

	// Copy recent values from the old buffer to the new buffer.
	for wrcur >= 0 {
		buf[wrcur] = rb.buf[rdcur]

		rdcur = rdcur - 1
		if rdcur < 0 {
			rdcur += len(rb.buf)
		}

		wrcur = wrcur - 1 // no need to do the wraparound math
	}

	// If we resized smaller, and the old buffer has more values than the new
	// capacity, then capture the values from the old buffer which are dropped.
	for i := cap; i < rb.len; i++ {
		dropped = append(dropped, rb.buf[rdcur])

		rdcur = rdcur - 1
		if rdcur < 0 {
			rdcur += len(rb.buf)
		}
	}

	// Calculate the next write cursor for the new buffer. If we resized
	// smaller, then fill will equal cap, and we need to wrap around.
	cur := fill
	if cur >= cap {
		cur -= cap
	}

	// Modify all of the buffer fields to their new values.
	rb.buf = buf
	rb.cur = cur
	rb.len = fill

	// Done.
	return dropped
}

// Add the value to the ring buffer. If the ring buffer was full and an item was
// overwritten by this add, return that item and true, otherwise return a zero
// value item and false.
func (rb *RingBuffer[T]) Add(val T) (dropped T, ok bool) {
	rb.mtx.Lock()
	defer rb.mtx.Unlock()

	// Safety first.
	if cap(rb.buf) <= 0 {
		var zero T
		return zero, false
	}

	// Capture any overwritten value so it can be returned.
	if rb.len >= len(rb.buf) {
		dropped, ok = rb.buf[rb.cur], true
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

	// Done.
	return dropped, ok
}

// Walk calls the given function for each value in the ring buffer, starting
// with the most recent value, and ending with the oldest value. Walk takes an
// exclusive lock on the ring buffer, which blocks other calls like Add.
func (rb *RingBuffer[T]) Walk(fn func(T) error) error {
	rb.mtx.Lock()
	defer rb.mtx.Unlock()

	// Read up to rb.len values.
	for i := 0; i < rb.len; i++ {
		// Reads go backwards from one before the write cursor.
		cur := rb.cur - 1 - i

		// Wrap around when necessary.
		if cur < 0 {
			cur += len(rb.buf)
		}

		// If the function returns an error, we're done.
		if err := fn(rb.buf[cur]); err != nil {
			return err
		}
	}

	return nil
}

// Stats returns the newest and oldest values in the ring buffer, as well as the
// total number of values stored in the ring buffer.
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
		headidx += len(rb.buf)
	}

	// The read tail is len+1 values back from the read head.
	// If the buffer is full, this is the write cursor.
	tailidx := headidx - rb.len + 1
	if tailidx < 0 {
		tailidx += len(rb.buf)
	}

	return rb.buf[headidx], rb.buf[tailidx], rb.len
}

//
//
//

// RingBuffers collects individual ring buffers by string key.
type RingBuffers[T any] struct {
	mtx  sync.Mutex
	cap  int
	bufs map[string]*RingBuffer[T]
}

// NewRingBuffers returns an empty set of ring buffers, each of which will have
// a maximum capacity of the given cap.
func NewRingBuffers[T any](cap int) *RingBuffers[T] {
	return &RingBuffers[T]{
		cap:  cap,
		bufs: map[string]*RingBuffer[T]{},
	}
}

// GetOrCreate returns a ring buffer corresponding to the given category string.
// Once a ring buffer is created in this way, it will always exist.
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

// GetAll returns all of the ring buffers in the set, grouped by category.
func (rbs *RingBuffers[T]) GetAll() map[string]*RingBuffer[T] {
	rbs.mtx.Lock()
	defer rbs.mtx.Unlock()

	all := make(map[string]*RingBuffer[T], len(rbs.bufs))
	for name, rb := range rbs.bufs {
		all[name] = rb
	}

	return all
}

// Resize all of the ring buffers in the set to the new capacity.
func (rbs *RingBuffers[T]) Resize(cap int) (dropped []T) {
	if cap <= 0 {
		return
	}

	rbs.mtx.Lock()
	defer rbs.mtx.Unlock()

	rbs.cap = cap

	for _, rb := range rbs.bufs {
		dropped = append(dropped, rb.Resize(cap)...)
	}

	return dropped
}
