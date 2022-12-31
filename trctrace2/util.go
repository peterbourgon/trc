package trctrace

import (
	"fmt"
	"time"
)

var ErrBadMerge = fmt.Errorf("bad merge")

func badMerge(what string, dst, src any) error {
	return fmt.Errorf("%w: %s: %v ↯ %v", ErrBadMerge, what, dst, src)
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
