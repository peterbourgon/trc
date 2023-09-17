package trc_test

import (
	"context"
	"testing"

	"github.com/peterbourgon/trc"
)

func BenchmarkBrokerPublish(b *testing.B) {
	ctxbg := context.Background()

	fn := func(name string, fs ...trc.Filter) {
		b.Run(name, func(b *testing.B) {
			var (
				ctx, cancel = context.WithCancel(ctxbg)
				broker      = trc.NewBroker()
			)
			for _, f := range fs {
				tracec := make(chan trc.Trace)
				defer func() { <-tracec }()
				go func(f trc.Filter) {
					broker.Stream(ctx, f, tracec)
					close(tracec)
				}(f)
			}

			_, tr := trc.New(ctxbg, "source", "category")
			defer tr.Finish()

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				broker.Publish(ctxbg, tr)
			}

			cancel()
		})
	}

	var (
		isErrored = trc.Filter{IsErrored: true}
		isActive  = trc.Filter{IsActive: true}
	)

	fn("no subscribers")
	fn("1 skip subscriber", isErrored)
	fn("10 skip subscribers", isErrored, isErrored, isErrored, isErrored, isErrored, isErrored, isErrored, isErrored, isErrored, isErrored)
	fn("1 send subscriber", isActive)
	fn("10 send subscribers", isActive, isActive, isActive, isActive, isActive, isActive, isActive, isActive, isActive, isActive)
	fn("9 skip, 1 send", isErrored, isErrored, isErrored, isErrored, isErrored, isErrored, isErrored, isErrored, isErrored, isActive)
	fn("1 skip, 9 send", isActive, isErrored, isErrored, isErrored, isErrored, isErrored, isErrored, isErrored, isErrored, isErrored)
}
