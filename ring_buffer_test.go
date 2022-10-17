package trc

import (
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func assertEqual[T any](t *testing.T, have, want T) {
	t.Helper()
	if !cmp.Equal(have, want) {
		t.Fatal(cmp.Diff(have, want))
	}
}

func TestRingBuffer(t *testing.T) {
	rb := newRingBuffer[int](3)

	top := func(k int) []int {
		res := []int{}
		rb.walk(func(i int) error {
			if k >= 0 && len(res) >= k {
				return errors.New("done")
			}
			res = append(res, i)
			return nil
		})
		return res
	}

	assertEqual(t, top(-1), []int{})
	assertEqual(t, top(0), []int{})
	assertEqual(t, top(99), []int{})

	rb.add(1)

	assertEqual(t, top(-1), []int{1})
	assertEqual(t, top(0), []int{})
	assertEqual(t, top(1), []int{1})
	assertEqual(t, top(2), []int{1})
	assertEqual(t, top(3), []int{1})
	assertEqual(t, top(4), []int{1})

	rb.add(2)

	assertEqual(t, top(-1), []int{2, 1})
	assertEqual(t, top(0), []int{})
	assertEqual(t, top(1), []int{2})
	assertEqual(t, top(2), []int{2, 1})
	assertEqual(t, top(3), []int{2, 1})
	assertEqual(t, top(4), []int{2, 1})

	rb.add(3)

	assertEqual(t, top(-1), []int{3, 2, 1})
	assertEqual(t, top(0), []int{})
	assertEqual(t, top(1), []int{3})
	assertEqual(t, top(2), []int{3, 2})
	assertEqual(t, top(3), []int{3, 2, 1})
	assertEqual(t, top(4), []int{3, 2, 1})

	rb.add(4)

	assertEqual(t, top(-1), []int{4, 3, 2})
	assertEqual(t, top(0), []int{})
	assertEqual(t, top(1), []int{4})
	assertEqual(t, top(2), []int{4, 3})
	assertEqual(t, top(3), []int{4, 3, 2})
	assertEqual(t, top(4), []int{4, 3, 2})

	rb.add(5)
	rb.add(6)

	assertEqual(t, top(-1), []int{6, 5, 4})
	assertEqual(t, top(99), []int{6, 5, 4})
}

func TestRingBufferStats(t *testing.T) {
	firstLast := func(rb *ringBuffer[int]) (int, int) {
		var count, first, last int
		rb.walk(func(i int) error {
			if count == 0 {
				first = i
			}
			last = i
			count++
			return nil
		})
		return first, last
	}

	{
		rb := newRingBuffer[int](0)
		var zeroint int

		newest, oldest, n := rb.stats()
		assertEqual(t, newest, zeroint)
		assertEqual(t, oldest, zeroint)
		assertEqual(t, n, 0)

		rb.add(1)
		rb.add(2)

		newest, oldest, n = rb.stats()
		first, last := firstLast(rb)
		assertEqual(t, newest, first)
		assertEqual(t, oldest, last)
		assertEqual(t, n, 0)
	}

	{
		rb := newRingBuffer[int](10)

		rb.add(1)
		rb.add(2)
		rb.add(3)

		newest, oldest, n := rb.stats()
		assertEqual(t, newest, 3)
		assertEqual(t, oldest, 1)
		assertEqual(t, n, 3)

		first, last := firstLast(rb)
		assertEqual(t, newest, first)
		assertEqual(t, oldest, last)
	}

	{
		rb := newRingBuffer[int](123)

		for i := 42; i < 951; i++ {
			rb.add(i)
		}

		newest, oldest, n := rb.stats()
		first, last := firstLast(rb)
		assertEqual(t, newest, first)
		assertEqual(t, oldest, last)
		assertEqual(t, n, 123)
	}
}

func BenchmarkRingBuffer(b *testing.B) {
	for _, cap := range []int{100, 1000, 10000, 100000} {

		b.Run(strconv.Itoa(cap), func(b *testing.B) {
			rb := newRingBuffer[int](cap)
			for i := 0; i < cap; i++ {
				rb.add(i)
			}

			var captured int
			_ = captured

			walkOnlyFn := func(int) error {
				return nil
			}

			walkReadFn := func(i int) error {
				captured = i
				return nil
			}

			b.Run("Add", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					rb.add(i)
				}
			})

			b.Run("Walk", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					rb.walk(walkOnlyFn)
				}
			})

			b.Run("Walk+Read", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					rb.walk(walkReadFn)
				}
			})

			b.Run("Add+Walk", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					rb.add(i)
					rb.walk(walkOnlyFn)
				}
			})

			b.Run("Add+Walk+Read", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					rb.add(i)
					rb.walk(walkReadFn)
				}
			})
		})
	}
}

func BenchmarkRingBufferParallel(b *testing.B) {
	walkFn := func(int) error { return nil }
	_ = walkFn

	for _, cap := range []int{10000} {
		for _, par := range []int{10, 100, 1000} {
			b.Run(fmt.Sprintf("cap=%d/par=%d", cap, par), func(b *testing.B) {
				rb := newRingBuffer[int](cap)
				b.SetParallelism(par)
				b.RunParallel(func(p *testing.PB) {
					for p.Next() {
						rb.add(123)
						//rb.walk(walkFn)
					}
				})
			})
		}
	}
}