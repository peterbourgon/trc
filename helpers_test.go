package trc_test

import (
	"context"
	"strings"
	"testing"

	"github.com/peterbourgon/trc"
)

func TestRegion(t *testing.T) {
	ctx := context.Background()
	ctx, tr := trc.New(ctx, "source", "category")
	tr.Tracef("before x1")
	{
		_, tr, finish := trc.Region(ctx, "region")
		tr.Tracef("within x2")
		finish()
	}
	tr.Tracef("after x3")
	tr.Finish()

	want := []string{
		"before x1",
		"region",
		"within x2",
		"region",
		"after x3",
	}

	if want, have := len(want), len(tr.Events()); want != have {
		t.Fatalf("events: want %d, have %d", want, have)
	}

	for i, ev := range tr.Events() {
		havestr := ev.What
		wantstr := want[i]
		if !strings.Contains(havestr, wantstr) {
			t.Errorf("event %d/%d: want %q, have %q", i+1, len(tr.Events()), wantstr, havestr)
		}
	}
}

func TestPrefix(t *testing.T) {
	ctx := context.Background()
	ctx, tr := trc.New(ctx, "source", "category")
	tr.Tracef("before x1")
	{
		_, tr := trc.Prefix(ctx, "prefix")
		tr.Tracef("one")
		tr.LazyTracef("two")
		tr.Errorf("three")
	}
	tr.Tracef("after x2")
	tr.Finish()

	want := []string{
		"before x1",
		"prefix one",
		"prefix two",
		"prefix three",
		"after x2",
	}

	if want, have := len(want), len(tr.Events()); want != have {
		t.Fatalf("events: want %d, have %d", want, have)
	}

	for i, ev := range tr.Events() {
		havestr := ev.What
		wantstr := want[i]
		if !strings.Contains(havestr, wantstr) {
			t.Errorf("event %d/%d: want %q, have %q", i+1, len(tr.Events()), wantstr, havestr)
		}
	}
}
