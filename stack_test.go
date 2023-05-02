package trc

import (
	"context"
	"testing"
)

func testCallStackFoo(t *testing.T, ctx context.Context) {
	t.Helper()
	tr := FromContext(ctx)
	tr.Tracef("foo 1")
	testCallStackBar(t, ctx)
	tr.Tracef("foo 2")
}

func testCallStackBar(t *testing.T, ctx context.Context) {
	t.Helper()
	tr := FromContext(ctx)
	tr.Tracef("bar 1")
	testCallStackBaz(t, ctx)
	tr.Tracef("bar 2")
}

func testCallStackBaz(t *testing.T, ctx context.Context) {
	t.Helper()
	tr := FromContext(ctx)
	tr.Tracef("baz 1")
	func() {
		tr.Tracef("quux")
	}()
	tr.Tracef("bar 2")
}
