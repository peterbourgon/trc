package trc

import (
	"fmt"
	"runtime"
	"time"
)

type Event2 interface {
	When() time.Time
	What() string
	Stack() []Call2
	IsError() bool
}

type CallStack []Call2

type Call2 interface {
	Function() string
	FileLine() string
}

type lazyCall struct {
	pc uintptr
}

func (c lazyCall) Function() string {
	return runtime.FuncForPC(c.pc).Name()
}

func (c lazyCall) FileLine() string {
	file, line := runtime.FuncForPC(c.pc).FileLine(c.pc)
	return fmt.Sprintf("%s:%d", file, line)
}

func getStack2() CallStack {
	pcs := make([]uintptr, 1024)
	n := runtime.Callers(1, pcs)
	pcs = pcs[:n]
	var lazyCalls []Call2
	for _, pc := range pcs {
		lazyCalls = append(lazyCalls, lazyCall{pc})
	}
	return CallStack(lazyCalls)
}
