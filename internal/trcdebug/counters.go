package trcdebug

import "sync/atomic"

// PoolCounters track operations on a sync.Pool for a specific type.
type PoolCounters struct {
	Get   atomic.Uint64
	Alloc atomic.Uint64
	Put   atomic.Uint64
	Lost  atomic.Uint64
}

// ReusePercent returns the percent (0..100) reuse of the pool type.
func (pc *PoolCounters) ReusePercent() float64 {
	var (
		get   = pc.Get.Load()
		alloc = pc.Alloc.Load()
		reuse = get - alloc
	)
	if get <= 0 {
		return 0.0
	}
	return 100 * float64(reuse) / float64(get)
}

// Values returns the current values of the counters.
func (pc *PoolCounters) Values() (get, alloc, put, lost uint64, reuse float64) {
	var (
		g = pc.Get.Load()
		a = pc.Alloc.Load()
		p = pc.Put.Load()
		l = pc.Lost.Load()
		r = pc.ReusePercent()
	)
	return g, a, p, l, r
}

var (
	// CoreTraceCounters tracks the core trace pool.
	CoreTraceCounters PoolCounters

	// CoreEventCounters tracks the core event pool.
	CoreEventCounters PoolCounters

	// StringerCounters tracks the stringer pool.
	StringerCounters PoolCounters

	// StaticTraceCounters tracks the StaticTrace pool.
	StaticTraceCounters PoolCounters
)
