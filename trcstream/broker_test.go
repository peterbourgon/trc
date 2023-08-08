package trcstream_test

import (
	"context"
	"testing"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcstream"
)

func BenchmarkBrokerPublish(b *testing.B) {
	ctx := context.Background()

	b.Run("no subscribers", func(b *testing.B) {
		broker := trcstream.NewBroker()

		_, tr := trc.New(ctx, "source", "category")
		defer tr.Finish()

		for i := 0; i < b.N; i++ {
			broker.Publish(ctx, tr)
		}
	})

	b.Run("one skip subscriber", func(b *testing.B) {
		var (
			broker      = trcstream.NewBroker()
			ctx, cancel = context.WithCancel(ctx)
			tracec      = make(chan trc.Trace)
		)

		go func() {
			broker.Stream(ctx, trc.Filter{IsErrored: true}, tracec)
			close(tracec)
		}()

		_, tr := trc.New(ctx, "source", "category")
		defer tr.Finish()

		for i := 0; i < b.N; i++ {
			broker.Publish(ctx, tr)
		}

		cancel()
		<-tracec
	})

	b.Run("one send subscriber", func(b *testing.B) {
		var (
			broker      = trcstream.NewBroker()
			ctx, cancel = context.WithCancel(ctx)
			tracec      = make(chan trc.Trace)
		)

		go func() {
			broker.Stream(ctx, trc.Filter{IsActive: true}, tracec)
			close(tracec)
		}()

		_, tr := trc.New(ctx, "source", "category")
		defer tr.Finish()

		for i := 0; i < b.N; i++ {
			broker.Publish(ctx, tr)
		}

		cancel()
		<-tracec
	})
}
