package trc_test

import (
	"context"
	"testing"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcstream"
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

func BenchmarkCollectorStream(b *testing.B) {
	ctx := context.Background()
	category := "category"

	b.Run("zero subscribers baseline test", func(b *testing.B) {
		collector := trc.NewDefaultCollector()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, tr := collector.NewTrace(ctx, category)
			tr.Tracef("trace event")
			tr.Finish()
		}
	})

	b.Run("one subscriber bad category", func(b *testing.B) {
		broker := trcstream.NewBroker()
		collector := trc.NewCollector(trc.CollectorConfig{Decorators: []trc.DecoratorFunc{broker.PublishTracesDecorator()}})

		ctx, cancel := context.WithCancel(ctx)
		ch := make(chan trc.Trace)
		errc := make(chan error, 1)
		go func() { _, err := broker.Stream(ctx, trc.Filter{Category: "xxx"}, ch); errc <- err }()
		defer func() { cancel(); <-errc }()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, tr := collector.NewTrace(ctx, category)
			tr.Tracef("trace event")
			tr.Finish()
		}
	})

	b.Run("one subscriber bad IsErrored", func(b *testing.B) {
		broker := trcstream.NewBroker()
		collector := trc.NewCollector(trc.CollectorConfig{Decorators: []trc.DecoratorFunc{broker.PublishTracesDecorator()}})

		ctx, cancel := context.WithCancel(ctx)
		ch := make(chan trc.Trace)
		errc := make(chan error, 1)
		go func() { _, err := broker.Stream(ctx, trc.Filter{IsErrored: true}, ch); errc <- err }()
		defer func() { cancel(); <-errc }()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, tr := collector.NewTrace(ctx, category)
			tr.Tracef("trace event")
			tr.Finish()
		}
	})

	b.Run("one subscriber always drop", func(b *testing.B) {
		broker := trcstream.NewBroker()
		collector := trc.NewCollector(trc.CollectorConfig{Decorators: []trc.DecoratorFunc{broker.PublishTracesDecorator()}})

		ctx, cancel := context.WithCancel(ctx)
		ch := make(chan trc.Trace)
		errc := make(chan error, 1)
		go func() { _, err := broker.Stream(ctx, trc.Filter{}, ch); errc <- err }()
		defer func() { cancel(); <-errc }()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, tr := collector.NewTrace(ctx, category)
			tr.Tracef("trace event")
			tr.Finish()
		}
	})

	b.Run("one subscriber should recv", func(b *testing.B) {
		broker := trcstream.NewBroker()
		collector := trc.NewCollector(trc.CollectorConfig{Decorators: []trc.DecoratorFunc{broker.PublishTracesDecorator()}})

		ctx, cancel := context.WithCancel(ctx)

		ch := make(chan trc.Trace)
		defer close(ch)

		var received int
		go func() {
			for range ch {
				received++
			}
		}()

		errc := make(chan error, 1)
		go func() {
			_, err := broker.Stream(ctx, trc.Filter{}, ch)
			errc <- err
		}()
		defer func() {
			cancel()
			<-errc
		}()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, tr := collector.NewTrace(ctx, category)
			tr.Tracef("trace event")
			tr.Finish()
		}
	})
}
