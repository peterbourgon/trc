package trc

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// Event represents a trace statement in user code.
//
// Events may be retained for an indeterminate length of time, and accessed
// concurrently by multiple goroutines. Once created, an event is expected to be
// immutable. In particular, the fmt.Stringer implementation of the What field
// must be safe for concurrent use, including any values it may capture by
// reference.
type Event interface {
	When() time.Time
	What() string
	Stack() []Call
	IsError() bool
}

type Call interface {
	Function() string
	FileLine() string
}

//
//
//

type coreEvent struct {
	when    time.Time    // ideally UTC
	what    fmt.Stringer // must be safe for concurrent use
	stack   []Call       //
	isError bool         //
}

func (cev *coreEvent) When() time.Time { return cev.when }
func (cev *coreEvent) What() string    { return cev.what.String() }
func (cev *coreEvent) Stack() []Call   { return cev.stack }
func (cev *coreEvent) IsError() bool   { return cev.isError }

func newEvent(format string, args ...any) Event {
	return &coreEvent{
		when:    time.Now().UTC(),
		what:    stringer(fmt.Sprintf(format, args...)),
		stack:   getLazyCallStack(3),
		isError: false,
	}
}

func newLazyEvent(format string, args ...any) Event {
	return &coreEvent{
		when:    time.Now().UTC(),
		what:    &lazyStringer{fmt: format, args: args},
		stack:   getLazyCallStack(3),
		isError: false,
	}
}

func newErrorEvent(format string, args ...any) Event {
	return &coreEvent{
		when:    time.Now().UTC(),
		what:    stringer(fmt.Sprintf(format, args...)),
		stack:   getLazyCallStack(3),
		isError: true,
	}
}

func newLazyErrorEvent(format string, args ...any) Event {
	return &coreEvent{
		when:    time.Now().UTC(),
		what:    &lazyStringer{fmt: format, args: args},
		stack:   getLazyCallStack(3),
		isError: true,
	}
}

//
//
//

type lazyCall struct {
	pc uintptr
}

func getLazyCallStack(skip int) []Call {
	pcs := [512]uintptr{}
	n := runtime.Callers(skip+1, pcs[:])
	cs := make([]Call, n)
	for i := range cs {
		cs[i] = lazyCall{pcs[i]}
	}
	return cs
}

func (c lazyCall) Function() string {
	return runtime.FuncForPC(c.pc).Name()
}

func (c lazyCall) FileLine() string {
	file, line := runtime.FuncForPC(c.pc).FileLine(c.pc)
	{
		pre := pkgPrefix(c.Function())
		post := pathSuffix(file)
		if pre == "" {
			file = post
		} else {
			file = pre + "/" + post
		}
	}
	return fmt.Sprintf("%s:%d", file, line)
}

//
//
//

type stringer string

func (z stringer) String() string {
	return string(z)
}

type lazyStringer struct {
	fmt  string
	args []any
}

func (z *lazyStringer) String() string {
	return fmt.Sprintf(z.fmt, z.args...)
}

//
//
//

func pkgPrefix(funcName string) string {
	const pathSep = "/"
	end := strings.LastIndex(funcName, pathSep)
	if end == -1 {
		return ""
	}
	return funcName[:end]
}

func pathSuffix(path string) string {
	const pathSep = "/"
	lastSep := strings.LastIndex(path, pathSep)
	if lastSep == -1 {
		return path
	}
	return path[strings.LastIndex(path[:lastSep], pathSep)+1:]
}
