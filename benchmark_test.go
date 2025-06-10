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

	b.Run("normal const string", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.Tracef("format string")
			tr.Finish()
		}
	})

	b.Run("lazy const string", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.LazyTracef("format string")
			tr.Finish()
		}
	})

	b.Run("normal single int", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.Tracef("format string %d", i)
			tr.Finish()
		}
	})

	b.Run("lazy single int", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.LazyTracef("format string %d", i)
			tr.Finish()
		}
	})

	b.Run("normal five args", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.Tracef("format string %d %d %d %d %d", i, i, i, i, i)
			tr.Finish()
		}
	})

	b.Run("lazy five args", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, tr := trc.New(ctx, "source", "category")
			tr.LazyTracef("format string %d %d %d %d %d", i, i, i, i, i)
			tr.Finish()
		}
	})
}

func BenchmarkCollector(b *testing.B) {
	ctx := context.Background()
	category := "category"

	b.Run("baseline", func(b *testing.B) {
		collector := trc.NewDefaultCollector()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, tr := collector.NewTrace(ctx, category)
			tr.Tracef("trace event")
			tr.Finish()
		}
	})

	b.Run("publish no subscribers", func(b *testing.B) {
		collector := trc.NewDefaultCollector()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, tr := collector.NewTrace(ctx, category)
			tr.Tracef("trace event")
			tr.Finish()
		}
	})

	b.Run("publish one skip subscriber", func(b *testing.B) {
		collector := trc.NewDefaultCollector()

		ctx, cancel := context.WithCancel(ctx)
		ch := make(chan trc.Trace)
		errc := make(chan error, 1)
		go func() { _, err := collector.Stream(ctx, trc.Filter{IsErrored: true}, ch); errc <- err }()
		defer func() { cancel(); <-errc }()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, tr := collector.NewTrace(ctx, category)
			tr.Tracef("trace event")
			tr.Finish()
		}
	})

	b.Run("publish one drop subscriber", func(b *testing.B) {
		collector := trc.NewDefaultCollector()

		ctx, cancel := context.WithCancel(ctx)
		ch := make(chan trc.Trace)
		errc := make(chan error, 1)
		go func() { _, err := collector.Stream(ctx, trc.Filter{}, ch); errc <- err }()
		defer func() { cancel(); <-errc }()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, tr := collector.NewTrace(ctx, category)
			tr.Tracef("trace event")
			tr.Finish()
		}
	})
}

func BenchmarkParallelCollector(b *testing.B) {
	ctx := context.Background()
	category := "category"

	b.Run("publish no subscribers", func(b *testing.B) {
		collector := trc.NewDefaultCollector()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, tr := collector.NewTrace(ctx, category)
				tr.Tracef("trace event")
				tr.Finish()
			}
		})
	})

	b.Run("publish one skip subscriber", func(b *testing.B) {
		collector := trc.NewDefaultCollector()
		ctx, cancel := context.WithCancel(ctx)
		ch := make(chan trc.Trace)
		errc := make(chan error, 1)
		go func() {
			_, err := collector.Stream(ctx, trc.Filter{IsErrored: true}, ch)
			errc <- err
		}()
		defer func() {
			cancel()
			<-errc
		}()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, tr := collector.NewTrace(ctx, category)
				tr.LazyTracef("trace event")
				tr.Finish()
			}
		})
	})

	b.Run("publish one drop subscriber", func(b *testing.B) {
		collector := trc.NewDefaultCollector()
		ctx, cancel := context.WithCancel(ctx)
		ch := make(chan trc.Trace)
		errc := make(chan error, 1)
		go func() {
			_, err := collector.Stream(ctx, trc.Filter{}, ch)
			errc <- err
		}()
		defer func() {
			cancel()
			<-errc
		}()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, tr := collector.NewTrace(ctx, category)
				tr.LazyTracef("trace event")
				tr.Finish()
			}
		})
	})

	b.Run("publish one pass subscriber", func(b *testing.B) {
		collector := trc.NewDefaultCollector()
		ctx, cancel := context.WithCancel(ctx)
		ch := make(chan trc.Trace, 1000)
		go func() {
			for range ch {
				//
			}
		}()
		errc := make(chan error, 1)
		go func() {
			defer close(ch)
			_, err := collector.Stream(ctx, trc.Filter{}, ch)
			errc <- err
		}()
		defer func() {
			cancel()
			<-errc
		}()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, tr := collector.NewTrace(ctx, category)
				tr.LazyTracef("trace event")
				tr.Finish()
			}
		})
	})
}
