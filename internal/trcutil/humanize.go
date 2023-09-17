package trcutil

import (
	"fmt"
	"strings"
	"time"
)

// TruncateDuration truncates the provided duration to a more human-friendly
// form, depending on its magnitude. For example, a duration over 1s is
// truncated at 100ms, a duration over 1m is truncated at 1s, and so on.
func TruncateDuration(d time.Duration) time.Duration {
	switch {
	case d >= 10*24*time.Hour:
		return d.Truncate(24 * time.Hour)
	case d >= 24*time.Hour:
		return d.Truncate(time.Hour)
	case d >= time.Hour:
		return d.Truncate(time.Minute)
	case d >= time.Minute:
		return d.Truncate(time.Second)
	case d >= time.Second:
		return d.Truncate(100 * time.Millisecond)
	case d >= 10*time.Millisecond:
		return d.Truncate(1000 * time.Microsecond)
	case d >= 1*time.Millisecond:
		return d.Truncate(100 * time.Microsecond)
	case d >= 1*time.Microsecond:
		return d.Truncate(1 * time.Microsecond)
	default:
		return d
	}
}

// HumanizeDuration truncates the duration and returns a human-friendly string
// representation.
func HumanizeDuration(d time.Duration) string {
	dd := TruncateDuration(d)
	ds := dd.String()

	if dd >= time.Hour && strings.HasSuffix(ds, "0s") {
		ds = strings.TrimSuffix(ds, "0s")
	}

	return ds
}

// HumanizeFloat returns a human-friendly string representation of the float,
// trying to ensure a max width of 3-4 characters, and using K to represent
// 1000, e.g. "32K". Values are expected to range between 0 and 1 million.
// Values larger than 1 million become "1M+".
func HumanizeFloat(f float64) (s string) {
	defer func() {
		if s == "0.0" {
			s = "0"
		}
	}()
	switch {
	case f > 1_000_000:
		return "1M+"
	case f > 10_000:
		return fmt.Sprintf("%.0fK", f/1000) // 32756 -> 32K
	case f > 1_000:
		return fmt.Sprintf("%.1fK", f/1000) // 5142 -> 5.1K
	case f >= 1:
		return fmt.Sprintf("%.0f", f) // 812.3 -> 821
	case f == 0:
		return "0"
	default:
		return fmt.Sprintf("%0.01f", f) // 0.15845 -> 0.1
	}
}

// HumanizeBytes returns a human-friendly string representation of n, which is
// assumed to be bytes. KB is used to represent 1024 bytes, and MB is used to
// represent 1048576 bytes. Larger units like GB are not used.
func HumanizeBytes[T interface {
	~int | ~uint | ~int64 | ~uint64
}](n T) string {
	var (
		kib = float64(1024)
		mib = float64(1024 * kib)
		fn  = float64(n)
	)
	switch {
	case fn < 1*kib:
		return fmt.Sprintf("%0.1fB", fn)
	case fn < 100*kib:
		return fmt.Sprintf("%.1fKB", fn/kib)
	case fn < 1*mib:
		return fmt.Sprintf("%.0fKB", fn/kib)
	case fn < 100*mib:
		return fmt.Sprintf("%.1fMB", fn/mib)
	default:
		return fmt.Sprintf("%.0fMB", fn/mib)
	}
}
