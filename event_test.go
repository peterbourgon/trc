package trc

import (
	"context"
	"testing"
)

func TestEventStacks(t *testing.T) {
	ctx := context.Background()
	ctx, tr := NewTrace(ctx, "my category")
	testCallStackFoo(t, ctx)
	tr.Finish()
	events := tr.Events()
	for i, ev := range events {
		t.Logf("%d/%d: %s", i+1, len(events), ev.What())
		frames := ev.Stack()
		for j, fr := range frames {
			t.Logf(" - %d/%d: %s (%s)", j+1, len(frames), fr.Function(), fr.FileLine())
		}
	}
}
