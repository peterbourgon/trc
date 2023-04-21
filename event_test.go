package trc

import (
	"testing"
)

func TestLazyCallStack(t *testing.T) {
	var stacks [][]Call

	foo := func() {
		stacks = append(stacks, getLazyCallStack(1))
	}

	bar := func() {
		stacks = append(stacks, getLazyCallStack(1))
		foo()
	}

	baz := func() {
		bar()
		stacks = append(stacks, getLazyCallStack(1))
	}

	foo()
	bar()
	baz()

	for i, s := range stacks {
		for j, c := range s {
			t.Logf("stack %d/%d: call %d/%d: %s: %s", i+1, len(stacks), j+1, len(s), c.FileLine(), c.Function())
		}
	}
}
