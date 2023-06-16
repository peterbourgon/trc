package trcdebug

import "sync/atomic"

var (
	// CoreTraceNewCount tracks new core traces.
	CoreTraceNewCount atomic.Uint64

	// CoreTraceAllocCount tracks when the trace pool allocates a new value.
	CoreTraceAllocCount atomic.Uint64

	// CoreTraceFreeCount tracks when a core trace is successfully free'd.
	CoreTraceFreeCount atomic.Uint64

	// CoreTraceLostCount tracks when a core trace which is still active
	// is requested to be free'd, which will result in a no-op and the trace
	// (eventually) being GC'd.
	CoreTraceLostCount atomic.Uint64

	// CoreEventNewCount tracks new core events.
	CoreEventNewCount atomic.Uint64

	// CoreEventAllocCount tracks when the event pool allocates a new value.
	CoreEventAllocCount atomic.Uint64

	// CoreEventFreeCount tracks when a core event is free'd.
	CoreEventFreeCount atomic.Uint64

	// CoreEventLostCount tracks when a core event is lost (see above).
	CoreEventLostCount atomic.Uint64

	// StringerNewCount tracks new stringers, both lazy and normal.
	StringerNewCount atomic.Uint64

	// StringerAllocCount tracks when the stringer pool allocates a new value.
	StringerAllocCount atomic.Uint64

	// StringerFreeCount tracks when a stringer is free'd.
	StringerFreeCount atomic.Uint64

	// StringerLostCount tracks when a core event is lost (see above).
	StringerLostCount atomic.Uint64
)
