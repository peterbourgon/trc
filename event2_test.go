package trc

import "testing"

func TestEvent2(t *testing.T) {
	var stacks []CallStack

	foo := func() {
		stacks = append(stacks, getStack2())
	}

	bar := func() {
		stacks = append(stacks, getStack2())
		foo()
	}

	baz := func() {
		bar()
		stacks = append(stacks, getStack2())
	}

	foo()
	bar()
	baz()

	for i, s := range stacks {
		t.Logf("stack %d/%d", i+1, len(stacks))
		for j, c := range s {
			t.Logf("call %d/%d: %s: %s", j+1, len(s), c.Function(), c.FileLine())
		}
	}
}
