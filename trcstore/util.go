package trcstore

import (
	"time"
)

var DefaultBucketing = []time.Duration{
	0 * time.Millisecond,
	1 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	1000 * time.Millisecond,
}

const (
	queryLimitMin = 1
	queryLimitDef = 10
	queryLimitMax = 1000
)

func olderOf(a, b time.Time) time.Time {
	switch {
	case !a.IsZero() && !b.IsZero():
		return iff(a.Before(b), a, b)
	case !a.IsZero() && b.IsZero():
		return a
	case a.IsZero() && !b.IsZero():
		return b
	case a.IsZero() && b.IsZero():
		return time.Time{}
	default:
		panic("unreachable")
	}
}

func newerOf(a, b time.Time) time.Time {
	switch {
	case !a.IsZero() && !b.IsZero():
		return iff(a.After(b), a, b)
	case !a.IsZero() && b.IsZero():
		return a
	case a.IsZero() && !b.IsZero():
		return b
	case a.IsZero() && b.IsZero():
		return time.Time{}
	default:
		panic("unreachable")
	}
}

func iff[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}
