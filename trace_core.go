package trc

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/peterbourgon/trc/internal/trcdebug"
)

// TraceMaxEvents establishes the maximum number of events that will be stored
// in a core trace produced via e.g. [New]. The default value is 1000. The
// minimum is 10, and the maximum is 10000.
var TraceMaxEvents atomic.Uint64

// TraceEventCallStacks determines whether core trace events will capture call
// stacks when they're created. The default value is true, as call stacks are
// generally very useful. However, capturing call stacks can be the single most
// expensive part of using traces, and call stacks can be the single biggest
// contributor to the size of search results. Setting this value to false is
// therefore a performance optimization.
var TraceEventCallStacks atomic.Bool

func init() {
	TraceMaxEvents.Store(traceMaxEventsDef)
	TraceEventCallStacks.Store(true)
}

const (
	traceMaxEventsMin = 10
	traceMaxEventsDef = 1000
	traceMaxEventsMax = 10000
)

func getTraceMaxEvents() int {
	val := TraceMaxEvents.Load()
	switch {
	case val < traceMaxEventsMin:
		return traceMaxEventsMin
	case val > traceMaxEventsMax:
		return traceMaxEventsMax
	default:
		return int(val)
	}
}

var traceIDEntropy = ulid.DefaultEntropy()

// coreTrace is the default, mutable implementation of a trace. Trace IDs are
// ULIDs, using a default monotonic source of entropy. The maximum number of
// events that can be stored in a trace is set when the trace is created, based
// on the current value of TraceMaxEvents.
type coreTrace struct {
	mtx       sync.Mutex
	source    string
	id        string
	category  string
	start     time.Time
	errored   bool
	finished  bool
	duration  time.Duration
	events    []*coreEvent
	eventsmax int
	truncated int
}

var _ Trace = (*coreTrace)(nil)

// New creates a new core trace with the given source and category, and injects
// it into the given context. It returns a new context containing that trace,
// and the trace itself.
func New(ctx context.Context, source, category string) (context.Context, Trace) {
	tr := newCoreTrace(source, category)
	return Put(ctx, tr)
}

type traceContextKey struct{}

var traceContextVal traceContextKey

var coreTracePool = sync.Pool{
	New: func() any {
		trcdebug.CoreTraceAllocCount.Add(1)
		return &coreTrace{}
	},
}

// newCoreTrace creates and starts a new trace with the given category.
func newCoreTrace(source, category string) *coreTrace {
	trcdebug.CoreTraceNewCount.Add(1)
	now := time.Now().UTC()
	tr := coreTracePool.Get().(*coreTrace)
	tr.source = source
	tr.id = ulid.MustNew(ulid.Timestamp(now), traceIDEntropy).String()
	tr.category = category
	tr.start = now
	tr.errored = false
	tr.finished = false
	tr.duration = 0
	tr.events = tr.events[:0]
	tr.eventsmax = getTraceMaxEvents()
	tr.truncated = 0
	return tr
}

func (tr *coreTrace) Source() string {
	return tr.source // immutable
}

func (tr *coreTrace) ID() string {
	return tr.id // immutable
}

func (tr *coreTrace) Category() string {
	return tr.category // immutable
}

func (tr *coreTrace) Started() time.Time {
	return tr.start // immutable
}

func (tr *coreTrace) Duration() time.Duration {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if tr.finished {
		return tr.duration
	}

	return time.Since(tr.start)
}

func (tr *coreTrace) Tracef(format string, args ...any) {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if tr.finished {
		return
	}

	switch {
	case len(tr.events) >= tr.eventsmax:
		tr.truncated++
	default:
		tr.events = append(tr.events, newCoreEvent(flagNormal, format, args...))
	}
}

func (tr *coreTrace) LazyTracef(format string, args ...any) {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if tr.finished {
		return
	}

	switch {
	case len(tr.events) >= tr.eventsmax:
		tr.truncated++
	default:
		tr.events = append(tr.events, newCoreEvent(flagLazy, format, args...))
	}
}

func (tr *coreTrace) Errorf(format string, args ...any) {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if tr.finished {
		return
	}

	tr.errored = true

	switch {
	case len(tr.events) >= tr.eventsmax:
		tr.truncated++
	default:
		tr.events = append(tr.events, newCoreEvent(flagError, format, args...))
	}
}

func (tr *coreTrace) LazyErrorf(format string, args ...any) {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if tr.finished {
		return
	}

	tr.errored = true

	switch {
	case len(tr.events) >= tr.eventsmax:
		tr.truncated++
	default:
		tr.events = append(tr.events, newCoreEvent(flagLazy|flagError, format, args...))
	}
}

func (tr *coreTrace) Finish() {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if tr.finished {
		return
	}

	tr.finished = true
	tr.duration = time.Since(tr.start)
}

func (tr *coreTrace) Finished() bool {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	return tr.finished
}

func (tr *coreTrace) Errored() bool {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	return tr.errored
}

func (tr *coreTrace) Events() []Event {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	events := snapshotEvents(tr.events)

	if tr.truncated > 0 {
		events = append(events, Event{
			When:    time.Now().UTC(),
			What:    fmt.Sprintf("(truncated event count %d)", tr.truncated),
			Stack:   nil,
			IsError: false,
		})
	}

	return events
}

//

func (tr *coreTrace) SetMaxEvents(max int) {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	switch {
	case max < traceMaxEventsMin:
		tr.eventsmax = traceMaxEventsMin
	case max > traceMaxEventsMax:
		tr.eventsmax = traceMaxEventsMax
	default:
		tr.eventsmax = max
	}
}

func (tr *coreTrace) Free() {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	for _, ev := range tr.events {
		ev.Free() // TODO: these individual Frees can show up in profiles, maybe pre-allocate?
	}
	tr.events = tr.events[:0]

	trcdebug.CoreTraceFreeCount.Add(1)
	coreTracePool.Put(tr)
}

//
//
//

var coreEventPool = sync.Pool{
	New: func() any {
		trcdebug.CoreEventAllocCount.Add(1)
		return &coreEvent{}
	},
}

type coreEvent struct {
	when  time.Time
	what  *stringer
	pc    [16]uintptr
	pcn   int
	iserr bool
}

const (
	flagNormal = 0b0000_0000
	flagLazy   = 0b0000_0001
	flagError  = 0b0000_0010
)

func newCoreEvent(flags uint8, format string, args ...any) *coreEvent {
	trcdebug.CoreEventNewCount.Add(1)
	cev := coreEventPool.Get().(*coreEvent)
	cev.when = time.Now().UTC()
	if flags&flagLazy != 0 {
		cev.what = newLazyStringer(format, args...)
	} else {
		cev.what = newNormalStringer(format, args...)
	}
	if TraceEventCallStacks.Load() {
		cev.pcn = runtime.Callers(3, cev.pc[:])
	} else {
		cev.pcn = 0
	}
	cev.iserr = flags&flagError != 0
	return cev
}

func (cev *coreEvent) When() time.Time {
	return cev.when
}

func (cev *coreEvent) What() string {
	return cev.what.String()
}

func (cev *coreEvent) Stack() []Frame {
	stdframes := runtime.CallersFrames(cev.pc[:cev.pcn])
	trcframes := make([]Frame, 0, cev.pcn)
	fr, more := stdframes.Next()
	for more {
		if !ignoreStackFrameFunction(fr.Function) {
			trcframes = append(trcframes, Frame{
				Function: fr.Function,
				FileLine: fr.File + ":" + strconv.Itoa(fr.Line),
			})
		}
		fr, more = stdframes.Next()
	}
	return trcframes
}

func (cev *coreEvent) IsError() bool {
	return cev.iserr
}

func (cev *coreEvent) Free() {
	cev.what.Free()
	cev.what = nil
	trcdebug.CoreEventFreeCount.Add(1)
	coreEventPool.Put(cev)
}

func snapshotEvents(evs []*coreEvent) []Event {
	cevs := make([]Event, len(evs))
	for i, ev := range evs {
		cevs[i] = Event{
			When:    ev.When(),
			What:    ev.What(),
			Stack:   ev.Stack(),
			IsError: ev.IsError(),
		}
	}
	return cevs
}

func ignoreStackFrameFunction(function string) bool {
	if strings.HasPrefix(function, "github.com/peterbourgon/trc.(*prefixTrace)") {
		return true
	}
	if strings.HasPrefix(function, "github.com/peterbourgon/trc.Region") {
		return true
	}
	if strings.HasPrefix(function, "github.com/peterbourgon/trc/eztrc.") {
		return true
	}
	return false
}

//
//
//

var stringerPool = sync.Pool{
	New: func() any {
		trcdebug.StringerAllocCount.Add(1)
		return &stringer{}
	},
}

type stringer struct {
	fmt  string
	args []any
	str  atomic.Value
}

type nullString struct {
	valid bool
	value string
}

var zeroNullString nullString // valid false, value empty

func newNormalStringer(format string, args ...any) *stringer {
	trcdebug.StringerNewCount.Add(1)
	z := stringerPool.Get().(*stringer)
	z.fmt = format
	z.args = args
	z.str.Store(nullString{valid: true, value: fmt.Sprintf(z.fmt, z.args...)}) // pre-compute the string
	return z
}

func newLazyStringer(format string, args ...any) *stringer {
	trcdebug.StringerNewCount.Add(1)
	z := stringerPool.Get().(*stringer)
	z.fmt = format
	z.args = args
	z.str.Store(zeroNullString) // don't pre-compute the string
	return z
}

func (z *stringer) String() string {
	// If we already have a valid string, return it.
	ns := z.str.Load().(nullString)
	if ns.valid {
		return ns.value
	}

	// If we don't, do the formatting work and try to swap it in.
	ns.valid = true
	ns.value = fmt.Sprintf(z.fmt, z.args...)
	if z.str.CompareAndSwap(zeroNullString, ns) {
		return ns.value
	}

	// If that didn't work, then take the value that snuck in.
	ns = z.str.Load().(nullString)
	if !ns.valid {
		panic(fmt.Errorf("invalid state in pooled stringer"))
	}
	return ns.value
}

func (z *stringer) Free() {
	trcdebug.StringerFreeCount.Add(1)
	stringerPool.Put(z)
}
