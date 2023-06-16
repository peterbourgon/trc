package trcdebug

import "sync/atomic"

var (
	// CoreTraceNewCount tracks when a new core trace is requested.
	CoreTraceNewCount atomic.Uint64

	// CoreTraceAllocCount tracks when the core trace pool allocs a new value.
	CoreTraceAllocCount atomic.Uint64

	// CoreTraceFreeCount tracks when a core trace returns to the pool.
	CoreTraceFreeCount atomic.Uint64

	// CoreTraceLostCount tracks when a core trace which is still active
	// is requested to be free'd, which will result in a no-op and the trace
	// (eventually) being GC'd.
	CoreTraceLostCount atomic.Uint64

	// CoreEventNewCount tracks when a new core event is requested.
	CoreEventNewCount atomic.Uint64

	// CoreEventAllocCount tracks when the core event pool allocs a new value.
	CoreEventAllocCount atomic.Uint64

	// CoreEventFreeCount tracks when a core event returns to the pool.
	CoreEventFreeCount atomic.Uint64

	// CoreEventLostCount tracks when a core event is lost (see above).
	CoreEventLostCount atomic.Uint64

	// StringerNewCount tracks when a new stringer is requested.
	StringerNewCount atomic.Uint64

	// StringerAllocCount tracks when the stringer pool allocs a new value.
	StringerAllocCount atomic.Uint64

	// StringerFreeCount tracks when a stringer returns to the pool.
	StringerFreeCount atomic.Uint64

	// StringerLostCount tracks when a stringer is lost (see above).
	StringerLostCount atomic.Uint64
)
