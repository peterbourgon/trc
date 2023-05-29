package trc_test

import (
	"context"
	"strings"
	"testing"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/eztrc"
)

func testCallStackFoo(t *testing.T, ctx context.Context) {
	t.Helper()
	tr := trc.Get(ctx)
	tr.Tracef("foo 1")
	testCallStackBar(t, ctx)
	tr.Tracef("foo 2")
}

func testCallStackBar(t *testing.T, ctx context.Context) {
	t.Helper()
	tr := trc.Get(ctx)
	tr.LazyTracef("bar 1")
	testCallStackBaz(t, ctx)
	tr.LazyTracef("bar 2")
}

func testCallStackBaz(t *testing.T, ctx context.Context) {
	t.Helper()
	eztrc.Tracef(ctx, "baz 1")
	func() {
		eztrc.LazyTracef(ctx, "quux")
	}()
	eztrc.Tracef(ctx, "baz 2")
}

func TestEventStacks(t *testing.T) {
	ctx, tr := trc.New(context.Background(), "src", "cat")
	testCallStackFoo(t, ctx)
	tr.Finish()
	events := tr.Events()
	AssertEqual(t, 7, len(events))
	for i, want := range []struct {
		function string
		fileline string
		what     string
	}{
		{
			function: "github.com/peterbourgon/trc_test.testCallStackFoo",
			fileline: "stack_test.go:15",
			what:     "foo 1",
		},
		{
			function: "github.com/peterbourgon/trc_test.testCallStackBar",
			fileline: "stack_test.go:23",
			what:     "bar 1",
		},
		{
			function: "github.com/peterbourgon/trc_test.testCallStackBaz",
			fileline: "stack_test.go:30",
			what:     "baz 1",
		},
		{
			function: "github.com/peterbourgon/trc_test.testCallStackBaz.func1",
			fileline: "stack_test.go:32",
			what:     "quux",
		},
		{
			function: "github.com/peterbourgon/trc_test.testCallStackBaz",
			fileline: "stack_test.go:34",
			what:     "baz 2",
		},
		{
			function: "github.com/peterbourgon/trc_test.testCallStackBar",
			fileline: "stack_test.go:25",
			what:     "bar 2",
		},
		{
			function: "github.com/peterbourgon/trc_test.testCallStackFoo",
			fileline: "stack_test.go:17",
			what:     "foo 2",
		},
	} {
		AssertEqual(t, want.function, events[i].Stack[0].Function)
		if have := events[i].Stack[0].FileLine; !strings.HasSuffix(have, want.fileline) {
			t.Errorf("%s: want %s", have, want.fileline)
		}
		AssertEqual(t, want.what, events[i].What)
	}
}
