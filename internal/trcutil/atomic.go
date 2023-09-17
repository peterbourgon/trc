package trcutil

import "sync"

// Atomic set and get operations for any type.
type Atomic[T any] struct {
	mtx sync.Mutex
	val T
}

// NewAtomic returns a new atomic wrapper around val.
func NewAtomic[T any](val T) *Atomic[T] {
	return &Atomic[T]{val: val}
}

// Set the value to val.
func (a *Atomic[T]) Set(val T) { a.mtx.Lock(); defer a.mtx.Unlock(); a.val = val }

// Get the current value.
func (a *Atomic[T]) Get() T { a.mtx.Lock(); defer a.mtx.Unlock(); return a.val }
