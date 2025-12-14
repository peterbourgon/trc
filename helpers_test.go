package trc_test

import (
	"context"
	"io"
	"math/rand"
	"strings"
	"testing"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/eztrc"
)

func TestRegion(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

func TestFinish(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	eztrc.Collector().SetDecorators(trc.LogDecorator(t.Output()))
	ctx, tr := eztrc.New(ctx, t.Name())
	defer tr.Finish()

	fna := func(ctx context.Context) (err error) {
		ctx, tr := eztrc.New(ctx, "fna")
		defer trc.Finish(tr, &err)
		tr.Tracef("fna doing work")
		return nil
	}

	fnb := func(ctx context.Context) (err error) {
		ctx, tr := eztrc.New(ctx, "fnb")
		defer trc.Finish(tr, &err)
		tr.Tracef("fna: err=%v", fna(ctx))
		tr.Tracef("fnb doing work")
		if r := rand.Float64(); r < 0.2 {
			return io.EOF
		}
		return nil
	}

	for range 1 {
		func() {
			ctx, tr := eztrc.New(ctx, t.Name())
			defer tr.Finish()
			tr.Tracef("top level: fnb: err=%v", fnb(ctx))
		}()
	}
}
