package trc

type collector[T any] struct {
	groups *ringBuffers[T]
	stream *stream[T]
}

func newCollector[T any](maxPerBuf int) *collector[T] {
	return &collector[T]{
		groups: newRingBuffers[T](maxPerBuf),
		stream: newStream[T](),
	}
}

func (c *collector[T]) add(cat string, val T) {
	c.groups.getOrCreate(cat).add(val)
	c.stream.broadcast(val)
}

func (c *collector[T]) subscribe(ch chan<- T) error {
	return c.stream.subscribe(ch)
}

func (c *collector[T]) unsubscribe(ch chan<- T) (sends, drops uint64, _ error) {
	return c.stream.unsubscribe(ch)
}
