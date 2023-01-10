package trctracehttp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"

	"github.com/peterbourgon/trc/trctrace"
)

const timeFormat = "15:04:05.000000"

var breaksReplacer = strings.NewReplacer(
	`,`, `,<wbr>`,
	`;`, `;<wbr>`,
)

var templateFuncs = template.FuncMap{
	"intadd":             func(i, j int) int { return i + j },
	"floatadd":           func(i, j float64) float64 { return i + j },
	"timenow":            func() time.Time { return time.Now().UTC() },
	"timesince":          func(t time.Time) time.Duration { return time.Since(t) },
	"timediff":           func(a, b time.Time) time.Duration { return a.Sub(b) },
	"timeadd":            func(t time.Time, d time.Duration) time.Time { return t.Add(d) },
	"timetrunc":          func(t time.Time) string { return t.Format(timeFormat) },
	"timerfc3339":        func(t time.Time) string { return t.Format(time.RFC3339) },
	"durationpercent":    func(n, d time.Duration) int { return int(100 * float64(n) / float64(d)) },
	"uint64percent":      func(n, d uint64) int { return int(100 * float64(n) / float64(d)) },
	"intpercent":         func(n, d int) int { return int(100 * float64(n) / float64(d)) },
	"queryescape":        func(s string) string { return url.QueryEscape(s) },
	"pathescape":         func(s string) string { return url.PathEscape(s) },
	"htmlescape":         func(s string) string { return template.HTMLEscapeString(s) },
	"insertbreaks":       func(s string) template.HTML { return template.HTML(breaksReplacer.Replace(s)) },
	"safeurl":            func(s string) template.URL { return template.URL(s) },
	"stringsjoinnewline": func(a []string) string { return strings.Join(a, string([]byte{0xa})) },
	"truncateduration":   truncateduration,
	"humanizeduration":   humanizeduration,
	"humanizefloat":      humanizefloat,
	"ratecalc":           ratecalc,
	"category2class":     category2class,
	"highlightclasses":   highlightclasses,
}

func sha256hex(input string) string {
	h := sha256.Sum256([]byte(input))
	s := hex.EncodeToString(h[:])
	return s
}

func category2class(name string) string {
	return "category-" + sha256hex(name)
}

func highlightclasses(req *trctrace.SearchRequest) []string {
	var classes []string
	if len(req.IDs) > 0 {
		return nil
	}
	if req.Category != "" {
		classes = append(classes, "category-"+sha256hex(req.Category))
	}
	if req.IsActive {
		classes = append(classes, "active")
	}
	if req.MinDuration != nil {
		classes = append(classes, "min-"+req.MinDuration.String())
	}
	if req.IsFailed {
		classes = append(classes, "failed")
	}
	return classes
}

func truncateduration(d time.Duration) time.Duration {
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

func humanizeduration(d time.Duration) string {
	dd := truncateduration(d)
	ds := dd.String()

	if dd > time.Hour && strings.HasSuffix(ds, "0s") {
		ds = strings.TrimSuffix(ds, "0s")
	}

	return ds
}

func humanizefloat(f float64) string {
	// try to enforce max width of 3-4
	switch {
	case f > 1_000_000:
		return "1M+"
	case f > 10_000:
		return fmt.Sprintf("%.0fK", f/1000) // 32756 -> 32K
	case f > 1_000:
		return fmt.Sprintf("%.1fK", f/1000) // 5142 -> 5.1K
	case f > 1:
		return fmt.Sprintf("%.0f", f) // 812.3 -> 821
	default:
		return fmt.Sprintf("%0.01f", f) // 0.15845 -> 0.1
	}
}

func ratecalc(n int, d time.Duration) float64 {
	if d == 0 {
		return 0.0
	}
	return float64(n) / float64(d.Seconds())
}
