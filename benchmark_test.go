package trc_test

import (
	"context"
	"testing"

	"github.com/peterbourgon/trc"
)

func BenchmarkTraceCollector(b *testing.B) {
	b.Run("Tracef", func(b *testing.B) {
		ctx := context.Background()
		collector := trc.NewDefaultCollector()
		category := "some category"
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, tr := collector.NewTrace(ctx, category)
			tr.Tracef("trace")
			tr.Finish()
		}
	})

	b.Run("LazyTracef", func(b *testing.B) {
		ctx := context.Background()
		collector := trc.NewDefaultCollector()
		category := "some category"
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, tr := collector.NewTrace(ctx, category)
			tr.LazyTracef("trace")
			tr.Finish()
		}
	})

}
