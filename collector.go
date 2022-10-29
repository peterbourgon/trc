package trc

import (
	"fmt"
	"strings"
)

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

func (c *collector[T]) debug() string {
	var total int
	var output []string
	for name, g := range c.groups.getAll() {
		var group int
		g.walk(func(val T) error { group++; return nil })
		output = append(output, fmt.Sprintf("[%q:%d]", name, group))
		total += group
	}
	output = append(output, fmt.Sprintf("[total:%d]", total))
	return strings.Join(output, " ")
}

func (c *collector[T]) add(cat string, val T) {
	c.groups.getOrCreate(cat).add(val)
}

func (c *collector[T]) subscribe(ch chan<- T) error {
	return c.stream.subscribe(ch)
}

func (c *collector[T]) unsubscribe(ch chan<- T) (sends, drops uint64, _ error) {
	return c.stream.unsubscribe(ch)
}
