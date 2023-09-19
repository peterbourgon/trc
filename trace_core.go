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

//
//
//

const (
	traceMaxEventsMin     = 10
	traceMaxEventsDefault = 1000
	traceMaxEventsMax     = 10000
)

var traceMaxEvents = func() *atomic.Int32 {
	var v atomic.Int32
	v.Store(traceMaxEventsDefault)
	return &v
}()

// SetTraceMaxEvents sets the max number of events that will be stored in a core
// trace. Once a core trace has the maximum number of events, additional events
// increment a "truncated" counter, which is represented as a single final
// event. The default is 1000, the minimum is 10, and the maximum is 10000.
//
// Changing this value does not affect traces that have already been created.
func SetTraceMaxEvents(n int) {
	if n < traceMaxEventsMin {
		n = traceMaxEventsMin
	}
	if n > traceMaxEventsMax {
		n = traceMaxEventsMax
	}
	traceMaxEvents.Store(int32(n))
}

var traceNoStacks atomic.Bool

// SetTraceStacks sets a boolean that determines whether trace events include
// stack traces. By default, trace event stacks are enabled, because they're
// generally very useful. However, computing stack traces can be the single most
// computationally heavy part of using package trc, so disabling them altogether
// can be a significant performance optimization.
//
// Changing this value does not affect traces that have already been created.
func SetTraceStacks(enable bool) {
	traceNoStacks.Store(!enable)
}

//
//
//

var traceIDEntropy = ulid.DefaultEntropy()

// coreTrace is the default, mutable implementation of a trace. Trace IDs are
// ULIDs, using a default monotonic source of entropy. The maximum number of
// events that can be stored in a trace is set when the trace is created, based
// on the current value of TraceMaxEvents.
type coreTrace struct {
	mtx         sync.Mutex
	source      string
	id          ulid.ULID
	category    string
	start       time.Time
	errored     bool
	finished    bool
	duration    time.Duration
	nostackflag uint8
	events      []*coreEvent
	eventsmax   int
	truncated   int
}

var _ Trace = (*coreTrace)(nil)

// New creates a new core trace with the given source and category, and injects
// it into the given context. It returns a new context containing that trace,
// and the trace itself.
func New(ctx context.Context, source, category string, decorators ...DecoratorFunc) (context.Context, Trace) {
	tr := Trace(newCoreTrace(source, category))
	for _, d := range decorators {
		tr = d(tr)
	}
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

// newCoreTrace starts a new trace with the given source and category.
func newCoreTrace(source, category string) *coreTrace {
	trcdebug.CoreTraceNewCount.Add(1)
	now := time.Now().UTC()
	tr := coreTracePool.Get().(*coreTrace)
	tr.id = ulid.MustNew(ulid.Timestamp(now), traceIDEntropy) // defer String computation
	tr.source = source
	tr.category = category
	tr.start = now
	tr.errored = false
	tr.finished = false
	tr.duration = 0
	tr.nostackflag = iff(traceNoStacks.Load(), flagNoStack, uint8(0))
	tr.events = tr.events[:0]
	tr.eventsmax = int(traceMaxEvents.Load())
	tr.truncated = 0
	return tr
}

func iff[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}

func (tr *coreTrace) ID() string {
	return tr.id.String() // immutable
}

func (tr *coreTrace) Source() string {
	return tr.source // immutable
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
		tr.events = append(tr.events, newCoreEvent(flagNormal|tr.nostackflag, format, args...))
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
		tr.events = append(tr.events, newCoreEvent(flagLazy|tr.nostackflag, format, args...))
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
		tr.events = append(tr.events, newCoreEvent(flagError|tr.nostackflag, format, args...))
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
		tr.events = append(tr.events, newCoreEvent(flagLazy|flagError|tr.nostackflag, format, args...))
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
	return tr.EventsDetail(-1, true)
}

func (tr *coreTrace) EventsDetail(n int, stacks bool) []Event {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	if n <= 0 || n > len(tr.events) {
		n = len(tr.events)
	}

	latest := tr.events[len(tr.events)-n:]
	events := snapshotEvents(latest, stacks)

	if tr.truncated > 0 {
		events = append(events, Event{
			When:    time.Now().UTC(),
			What:    fmt.Sprintf("(truncated event count %d)", tr.truncated),
			Stack:   nil,
			IsError: false,
		})
		events = events[1:]
	}

	return events
}

func (tr *coreTrace) EventCount() int {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	return len(tr.events)
}

func (tr *coreTrace) ObserveStats(cs *CategoryStats, bucketing []time.Duration) bool {
	tr.mtx.Lock()
	defer tr.mtx.Unlock()

	cs.EventCount += len(tr.events)

	var (
		traceStarted  = tr.start
		traceFinished = tr.finished
		traceErrored  = tr.errored
		isActive      = !traceFinished
		isBucket      = traceFinished && !traceErrored
		isErrored     = traceFinished && traceErrored
	)
	switch {
	case isActive:
		cs.ActiveCount++
	case isBucket:
		duration := tr.duration
		for i, bucket := range bucketing {
			if bucket > duration {
				break
			}
			cs.BucketCounts[i]++
		}
	case isErrored:
		cs.ErroredCount++
	}

	cs.Oldest = olderOf(cs.Oldest, traceStarted)
	cs.Newest = newerOf(cs.Newest, traceStarted)

	return true
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

	if !tr.finished { // presumably still in use by caller(s)
		trcdebug.CoreTraceLostCount.Add(1)
		return // can't recycle, will be GC'd
	}

	for _, ev := range tr.events {
		ev.free() // TODO: these individual frees can show up in profiles, maybe pre-allocate?
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

// coreEvent must exist in the context of a single parent core trace, and must
// not be retained beyond the lifetime of that parent trace, especially after
// the parent trace is free'd. It is not safe for concurrent use.
type coreEvent struct {
	when  time.Time
	what  *stringer
	pc    [8]uintptr
	pcn   int
	stack []Frame
	iserr bool
}

const (
	flagNormal  = 0b0000_0000
	flagLazy    = 0b0000_0001
	flagError   = 0b0000_0010
	flagNoStack = 0b0000_0100
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

	cev.stack = cev.stack[:0] // be safe

	if flags&flagNoStack != 0 {
		cev.pcn = 0 // be safe
	} else {
		cev.pcn = runtime.Callers(3, cev.pc[:])
	}

	cev.iserr = flags&flagError != 0

	return cev
}

func (cev *coreEvent) getStack() []Frame {
	if cev.pcn <= 0 {
		return nil
	}

	if len(cev.stack) > 0 {
		return cev.stack
	}

	stdframes := runtime.CallersFrames(cev.pc[:cev.pcn])
	fr, more := stdframes.Next()
	for more {
		if !ignoreStackFrameFunction(fr.Function) {
			cev.stack = append(cev.stack, Frame{
				Function: fr.Function,
				FileLine: fr.File + ":" + strconv.Itoa(fr.Line),
			})
		}
		fr, more = stdframes.Next()
	}

	return cev.stack
}

func (cev *coreEvent) free() {
	cev.what.free()
	cev.what = nil
	cev.pcn = 0
	cev.stack = cev.stack[:0]
	trcdebug.CoreEventFreeCount.Add(1)
	coreEventPool.Put(cev)
}

func snapshotEvents(cevs []*coreEvent, stacks bool) []Event {
	res := make([]Event, len(cevs))
	for i, cev := range cevs {
		var stack []Frame
		if stacks {
			stack = cev.getStack()
		}
		res[i] = Event{
			When:    cev.when,
			What:    cev.what.String(),
			Stack:   stack,
			IsError: cev.iserr,
		}
	}
	return res
}

func ignoreStackFrameFunction(function string) bool {
	if !strings.HasPrefix(function, "github.com/peterbourgon/trc") {
		return false // fast path
	}
	if strings.HasSuffix(function, "Tracef") || strings.HasSuffix(function, "Errorf") {
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

func (z *stringer) free() {
	trcdebug.StringerFreeCount.Add(1)
	stringerPool.Put(z)
}
