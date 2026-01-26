package eztrc_test

import (
	"context"
	"runtime"
	"testing"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/eztrc"
)

func TestGet(t *testing.T) {
	t.Parallel()

	eztrc.Collector().SetDecorators(trc.TestLogWriterDecorator(t))

	ctx := t.Context()

	streamTraceCh := make(chan trc.Trace, 1000)
	streamContext, streamCancel := context.WithCancel(ctx)
	t.Cleanup(streamCancel)
	go func() {
		defer close(streamTraceCh)
		stats, err := eztrc.Collector().Stream(streamContext, trc.Filter{}, streamTraceCh)
		t.Logf("Stream done, err=%v, stats=%+v", err, stats)
	}()

	runtime.Gosched()

	{
		ctx, tr := eztrc.New(ctx, "category", trc.WithTraceWrapEnterExit)
		t.Logf("%T", tr)
		tr.LazyTracef("1")
		{
			tr := trc.Get(ctx)
			t.Logf("%T", tr)
			tr.LazyTracef("2")
			{
				_, tr := eztrc.New(ctx, "inner-category", trc.WithTraceWrapEnterExit)
				t.Logf("%T", tr)
				tr.Tracef("inner")
				tr.Finish()
			}
			tr.Tracef("2b")
		}
		tr.Tracef("3")
		t.Logf("IsFinished? %v", tr.Finished())
		tr.Finish()
	}

	for tr := range streamTraceCh {
		t.Logf("streamed trace ID=%s Events=%02d Finished=%v Duration=%v", tr.ID(), len(tr.Events()), tr.Finished(), tr.Duration())
		for _, ev := range tr.Events() {
			t.Logf("  %v", ev.What)
		}
		if tr.Finished() {
			streamCancel()
		}
	}
}
