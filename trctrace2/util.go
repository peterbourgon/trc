package trctrace

import (
	"fmt"
	"sort"
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

func parseBucketing(bs []string) []time.Duration {
	var ds []time.Duration
	for _, s := range bs {
		if d, err := time.ParseDuration(s); err == nil {
			ds = append(ds, d)
		}
	}

	sort.Slice(ds, func(i, j int) bool {
		return ds[i] < ds[j]
	})

	if len(ds) <= 0 {
		return DefaultBucketing
	}

	if ds[0] != 0 {
		ds = append([]time.Duration{0}, ds...)
	}

	return ds
}

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
