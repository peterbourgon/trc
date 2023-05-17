package trc_test

import (
	"context"
	"testing"

	"github.com/peterbourgon/trc"
)

func BenchmarkTraceEvents(b *testing.B) {
	ctx := context.Background()

	b.Run("baseline", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.Finish()
		}
	})

	b.Run("Tracef const string", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.Tracef("format string")
			tr.Finish()
		}
	})

	b.Run("LazyTracef const string", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.LazyTracef("format string")
			tr.Finish()
		}
	})

	b.Run("Tracef single int", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.Tracef("format string %d", i)
			tr.Finish()
		}
	})

	b.Run("LazyTracef single int", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.LazyTracef("format string %d", i)
			tr.Finish()
		}
	})

	b.Run("Tracef five args", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.Tracef("format string %d %d %d %d %d", i, i, i, i, i)
			tr.Finish()
		}
	})

	b.Run("LazyTracef five args", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.LazyTracef("format string %d %d %d %d %d", i, i, i, i, i)
			tr.Finish()
		}
	})
}
