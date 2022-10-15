package trc

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-stack/stack"
)

// Event represents the information captured as part of a trace or log
// statement. It includes metadata like a timestamp and call stack.
//
// Events may be retained for an indeterminate length of time, and accessed
// concurrently by multiple goroutines. Once created, an event is expected to be
// immutable. In particular, the fmt.Stringer implementation used in the "what"
// field must be safe for concurrent use, including any values it may capture by
// reference.
type Event struct {
	Seq   uint64       // should be unique for each event
	When  time.Time    // ideally UTC
	What  fmt.Stringer // must be safe for concurrent use
	Stack CallStack    // optional but recommented
}

type CallStack []Call

type Call struct {
	Function string
	FileLine string
}

var eventSeq uint64

// MakeEvent creates a new event with the provided format string and args.
// Arguments are evaluated immediately.
func MakeEvent(format string, args ...interface{}) Event {
	return Event{
		Seq:   atomic.AddUint64(&eventSeq, 1),
		When:  time.Now().UTC(),
		What:  stringer(fmt.Sprintf(format, args...)),
		Stack: getStack(),
	}
}

// MakeLazyEvent creates a new event with the provided format string and args.
// Arguments are evaluated lazily upon read. Reads can happen at any point in
// the future, and from any number of concurrent goroutines, so arguments must
// be safe for concurrent access.
func MakeLazyEvent(format string, args ...interface{}) Event {
	return Event{
		Seq:   atomic.AddUint64(&eventSeq, 1),
		When:  time.Now().UTC(),
		What:  &lazyStringer{fmt: format, args: args},
		Stack: getStack(),
	}
}

// MatchRegexp returns true if the regexp matches relevant event metadata.
func (ev *Event) MatchRegexp(r *regexp.Regexp) bool {
	if r.MatchString(ev.What.String()) {
		return true
	}

	for _, c := range ev.Stack {
		if r.MatchString(c.Function) || r.MatchString(c.FileLine) {
			return true
		}
	}

	return false
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
	Seq   uint64        `json:"seq"`
	When  time.Time     `json:"when"`
	What  string        `json:"what"`
	Stack jsonCallStack `json:"stack"`
}

func jsonEventFrom(ev *Event) jsonEvent {
	return jsonEvent{
		Seq:   ev.Seq,
		When:  ev.When,
		What:  ev.What.String(),
		Stack: jsonCallStackFrom(ev.Stack),
	}
}

func (jev *jsonEvent) writeTo(ev *Event) {
	ev.Seq = jev.Seq
	ev.When = jev.When
	ev.What = stringer(jev.What)
	ev.Stack = jev.Stack.toCallStack()
}

type jsonCallStack []*jsonCall

type jsonCall struct {
	Function string `json:"function"`
	FileLine string `json:"fileline"`
}

func jsonCallStackFrom(cs CallStack) jsonCallStack {
	jcs := make(jsonCallStack, len(cs))
	for i := range cs {
		jcs[i] = (*jsonCall)(&cs[i]) // pointer avoids copy, and equivalent structs are assignable
	}
	return jcs
}

func (jcs *jsonCallStack) toCallStack() CallStack {
	cs := make(CallStack, len(*jcs))
	for i, jc := range *jcs {
		cs[i] = Call(*jc)
	}
	return cs
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
	args []interface{}
}

func (z *lazyStringer) String() string {
	return fmt.Sprintf(z.fmt, z.args...)
}

//
//
//

var myPackagePath = fmt.Sprintf("%+k", stack.Caller(0)) // also captures subpackages e.g. eztrc

func getStack() CallStack {
	var cs CallStack
	for _, c := range stack.Trace().TrimRuntime() {
		if strings.Contains(fmt.Sprintf("%+v", c), myPackagePath) {
			continue
		}
		cs = append(cs, Call{
			Function: fmt.Sprintf("%n", c),
			FileLine: fmt.Sprintf("%+v", c),
		})
	}
	return cs
}
