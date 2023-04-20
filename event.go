package trc

import (
	"encoding/json"
	"fmt"
	"time"
)

// Event represents a trace statement in user code.
//
// Events may be retained for an indeterminate length of time, and accessed
// concurrently by multiple goroutines. Once created, an event is expected to be
// immutable. In particular, the fmt.Stringer implementation of the What field
// must be safe for concurrent use, including any values it may capture by
// reference.
type Event struct {
	When    time.Time    // ideally UTC
	What    fmt.Stringer // must be safe for concurrent use
	Stack   CallStack    // optional but recommented
	IsError bool         //
}

type CallStackx []Call

type Call struct {
	Function string
	FileLine string
}

// NewEvent creates a new event with the provided format string and args.
// Arguments are evaluated immediately.
func NewEvent(format string, args ...any) *Event {
	return &Event{
		When:    time.Now().UTC(),
		What:    stringer(fmt.Sprintf(format, args...)),
		Stack:   getStack2(),
		IsError: false,
	}
}

// NewLazyEvent creates a new event with the provided format string and args.
// Arguments are evaluated lazily on read. Reads can happen at any point in the
// future, and from any number of concurrent goroutines, so arguments must be
// safe for concurrent access.
func NewLazyEvent(format string, args ...any) *Event {
	return &Event{
		When:    time.Now().UTC(),
		What:    &lazyStringer{fmt: format, args: args},
		Stack:   getStack2(),
		IsError: false,
	}
}

// MakeErrorEvent is equivalent to MakeEvent, and sets IsError.
func NewErrorEvent(format string, args ...any) *Event {
	return &Event{
		When:    time.Now().UTC(),
		What:    stringer(fmt.Sprintf(format, args...)),
		Stack:   getStack2(),
		IsError: true,
	}
}

// MakeLazyErrorEvent is equivalent to NewLazyEvent, and sets IsError.
func NewLazyErrorEvent(format string, args ...any) *Event {
	return &Event{
		When:    time.Now().UTC(),
		What:    &lazyStringer{fmt: format, args: args},
		Stack:   getStack2(),
		IsError: true,
	}
}

// MarshalJSON implements json.Marshaler for the event.
func (ev *Event) MarshalJSON() ([]byte, error) {
	return json.Marshal(jsonEventFrom(ev))
}

// UnmarshalJSON implements json.Marshaler for the event.
func (ev *Event) UnmarshalJSON(data []byte) error {
	var jev jsonEvent
	if err := json.Unmarshal(data, &jev); err != nil {
		return err
	}
	jev.writeTo(ev)
	return nil
}

//
//
//

type jsonEvent struct {
	When    time.Time     `json:"when"`
	What    string        `json:"what"`
	Stack   jsonCallStack `json:"stack"`
	IsError bool          `json:"iserr,omitempty"`
}

func jsonEventFrom(ev *Event) jsonEvent {
	return jsonEvent{
		When:    ev.When,
		What:    ev.What.String(),
		Stack:   jsonCallStackFrom(ev.Stack),
		IsError: ev.IsError,
	}
}

func (jev *jsonEvent) writeTo(ev *Event) {
	ev.When = jev.When
	ev.What = stringer(jev.What)
	ev.Stack = jev.Stack.toCallStack()
	ev.IsError = jev.IsError
}

type jsonCallStack []*jsonCall

type jsonCall struct {
	Fn string `json:"function"`
	FL string `json:"fileline"`
}

func jsonCallStackFrom(cs CallStack) jsonCallStack {
	jcs := make(jsonCallStack, len(cs))
	for i := range cs {
		jcs[i] = &jsonCall{Fn: cs[i].Function(), FL: cs[i].FileLine()}
		// jcs[i] = (*jsonCall)(&cs[i]) // pointer avoids copy, and equivalent structs are assignable
	}
	return jcs
}

func (jcs *jsonCallStack) toCallStack() CallStack {
	cs := make(CallStack, len(*jcs))
	for i, jc := range *jcs {
		// cs[i] = Call(*jc)
		cs[i] = *jc
	}
	return cs
}

func (jc jsonCall) Function() string { return jc.Fn }
func (jc jsonCall) FileLine() string { return jc.FL }

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
/*
func getStack() CallStack {
	var cs CallStack
	for _, c := range stack.Trace().TrimRuntime().TrimBelow(stack.Caller(3)) { // TODO: trim package trc
		fr := c.Frame()
		cs = append(cs, Call{
			Function: funcNameOnly(fr.Function),
			FileLine: pkgFilePath(&fr) + ":" + strconv.Itoa(fr.Line),
		})
	}
	return cs
}

func pkgFilePath(frame *runtime.Frame) string {
	pre := pkgPrefix(frame.Function)
	post := pathSuffix(frame.File)
	if pre == "" {
		return post
	}
	return pre + "/" + post
}

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

func funcNameOnly(name string) string {
	const pathSep = "/"
	if i := strings.LastIndex(name, pathSep); i != -1 {
		name = name[i+len(pathSep):]
	}
	const pkgSep = "."
	if i := strings.Index(name, pkgSep); i != -1 {
		name = name[i+len(pkgSep):]
	}
	return name
}
*/
