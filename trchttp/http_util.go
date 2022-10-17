package trchttp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
)

func parseParam[T any](r *http.Request, key string, parse func(string) (T, error)) (T, bool) {
	var zero T

	if !r.URL.Query().Has(key) {
		return zero, false
	}

	str := r.URL.Query().Get(key)
	val, err := parse(str)
	if err != nil {
		return zero, false
	}

	return val, false
}

func parseDefault[T any](s string, parse func(string) (T, error), def T) T {
	if v, err := parse(s); err == nil {
		return v
	}
	return def
}

func ifThenElse[T any](condition bool, affirmative, negative T) T {
	if condition {
		return affirmative
	}
	return negative
}

//go:embed *.html *.css
var fs embed.FS

func renderJSON(ctx context.Context, w http.ResponseWriter, data interface{}) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "    ")
	enc.Encode(data)
}

func renderHTML(ctx context.Context, w http.ResponseWriter, filename string, data any) {
	tr := trc.FromContext(ctx)

	body, err := func() (_ []byte, err error) {
		defer func() {
			if x := recover(); x != nil {
				err = fmt.Errorf("PANIC: %v", x)
			}
		}()

		// List everything in the base dir of the embedded fs.
		entries, err := fs.ReadDir(".")
		if err != nil {
			return nil, fmt.Errorf("read embedded filesystem: %w", err)
		}

		// Get the names of all of those assets.
		var assets []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			assets = append(assets, entry.Name())
		}

		// Use the embedded assets as the defaults in the template.
		t, err := template.New("").Funcs(templateFuncs).ParseFS(fs, assets...)
		if err != nil {
			return nil, fmt.Errorf("parse assets: %w", err)
		}

		// As a convenience for development, if any asset exists on the local
		// filesystem, parse and use it instead.
		for _, asset := range assets {
			if _, err := os.Stat(asset); err == nil {
				if tt, err := t.ParseFiles(asset); err == nil {
					t = tt
					log.Printf("### asset %s: using from local disk", asset)
				} else {
					log.Printf("### asset %s: using asset, Parse err %v", asset, err)
				}
			} else {
				log.Printf("### asset %s: using asset, Stat err %v", asset, err)
			}
		}

		// Get the requested asset.
		tt := t.Lookup(filename)
		if tt == nil {
			return nil, fmt.Errorf("template (%s) not found", filename)
		}

		// Execute the template.
		var buf bytes.Buffer
		if err := tt.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("execute template: %w", err)
		}

		return buf.Bytes(), nil
	}()

	tr.Tracef("rendered body (%dB)", len(body))

	code := http.StatusOK
	if err != nil {
		body = []byte(fmt.Sprintf(`<html><body><h1>Error</h1><p>%v</p>`, err))
		code = http.StatusInternalServerError
	}

	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	w.Write(body)
}

//
//
//

const timeFormat = "15:04:05.000000"

var templateFuncs = template.FuncMap{
	"intadd":         func(i, j int) int { return i + j },
	"floatadd":       func(i, j float64) float64 { return i + j },
	"category2class": func(name string) string { return "category-" + sha256hex(name) },
	"timenow":        func() time.Time { return time.Now().UTC() },
	"timesince":      func(t time.Time) time.Duration { return time.Since(t) },
	"timediff":       func(a, b time.Time) time.Duration { return a.Sub(b) },
	"timeadd":        func(t time.Time, d time.Duration) time.Time { return t.Add(d) },
	"timetrunc":      func(t time.Time) string { return t.Format(timeFormat) },
	"timepercent":    func(n, d time.Duration) int { return int(100 * float64(n) / float64(d)) },
	"intpercent":     func(n, d int) int { return int(100 * float64(n) / float64(d)) },
	"queryescape":    func(s string) string { return url.QueryEscape(s) },
	"insertbreaks":   func(s string) template.HTML { return template.HTML(breaksReplacer.Replace(s)) },
	"ratecalc": func(n int, d time.Duration) float64 {
		if d == 0 {
			return 0.0
		}
		return float64(n) / float64(d.Seconds())
	},
	"humanizefloat": humanizefloat,
	"humanize":      humanize,
	"highlightclasses": func(res *trc.TraceQueryResponse) []string {
		var classes []string

		if len(res.Stats.Request.IDs) > 0 {
			return nil
		}

		if res.Stats.Request.Category != "" {
			classes = append(classes, "category-"+sha256hex(res.Stats.Request.Category))
		}

		if res.Stats.Request.IsActive {
			classes = append(classes, "active")
		}

		if res.Stats.Request.IsErrored {
			classes = append(classes, "errored")
		}

		if res.Stats.Request.IsFinished {
			classes = append(classes, "finished")
		}

		if res.Stats.Request.IsSucceeded {
			classes = append(classes, "succeeded")
		}

		if res.Stats.Request.MinDuration != nil {
			classes = append(classes, "min-"+res.Stats.Request.MinDuration.String())
		}

		return classes
	},
}

var breaksReplacer = strings.NewReplacer(
	`,`, `,<wbr>`,
	`;`, `;<wbr>`,
)

var defaultBucketing = []time.Duration{
	0 * time.Second,
	1 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	1000 * time.Millisecond,
}

func parseBucketing(strs []string) []time.Duration {
	if len(strs) <= 0 {
		return defaultBucketing
	}

	var ds []time.Duration
	for _, s := range strs {
		if d, err := time.ParseDuration(s); err == nil {
			ds = append(ds, d)
		}
	}

	sort.Slice(ds, func(i, j int) bool {
		return ds[i] < ds[j]
	})

	if ds[0] != 0 {
		ds = append([]time.Duration{0}, ds...)
	}

	return ds
}

func sha256hex(name string) string {
	h := sha256.Sum256([]byte(name))
	s := hex.EncodeToString(h[:])
	return s
}

func humanize(d time.Duration) time.Duration {
	switch {
	case d > 10*24*time.Hour:
		return d.Truncate(24 * time.Hour)
	case d > 24*time.Hour:
		return d.Truncate(time.Hour)
	case d > time.Hour:
		return d.Truncate(time.Minute)
	case d > time.Minute:
		return d.Truncate(time.Second)
	case d > time.Second:
		return d.Truncate(100 * time.Millisecond)
	case d > 10*time.Millisecond:
		return d.Truncate(1 * time.Millisecond)
	case d > 1*time.Millisecond:
		return d.Truncate(100 * time.Microsecond)
	case d > 1*time.Microsecond:
		return d.Truncate(1 * time.Microsecond)
	default:
		return d
	}
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

//
//
//

func getBestContentType(r *http.Request) string {
	if r.URL.Query().Has("json") {
		return "application/json"
	}

	accept := r.Header.Get("accept")
	mediaRanges := strings.Split(accept, ",")
	accepts := make([]acceptValue, 0, len(mediaRanges))

	for _, mediaRange := range mediaRanges {
		rangeParams, typeSubtype, err := parseMediaRange(mediaRange)
		if err != nil {
			continue
		}

		accept := acceptValue{
			Type:       typeSubtype[0],
			Subtype:    typeSubtype[1],
			Q:          1.0,
			Extensions: make(map[string]string),
		}

		// If there is only one rangeParams, we can stop here.
		if len(rangeParams) == 1 {
			accepts = append(accepts, accept)
			continue
		}

		// Validate the rangeParams.
		validParams := true
		for _, v := range rangeParams[1:] {
			nameVal := strings.SplitN(v, "=", 2)
			if len(nameVal) != 2 {
				validParams = false
				break
			}
			nameVal[1] = strings.TrimSpace(nameVal[1])
			if name := strings.TrimSpace(nameVal[0]); name == "q" {
				qval, err := strconv.ParseFloat(nameVal[1], 64)
				if err != nil || qval < 0 {
					validParams = false
					break
				}
				if qval > 1.0 {
					qval = 1.0
				}
				accept.Q = qval
			} else {
				accept.Extensions[name] = nameVal[1]
			}
		}

		if validParams {
			accepts = append(accepts, accept)
		}
	}

	sort.Slice(accepts, func(i, j int) bool {
		// Higher qvalues come first.
		if accepts[i].Q > accepts[j].Q {
			return true
		} else if accepts[i].Q < accepts[j].Q {
			return false
		}

		// Specific types come before wildcard types.
		if accepts[i].Type != "*" && accepts[j].Type == "*" {
			return true
		} else if accepts[i].Type == "*" && accepts[j].Type != "*" {
			return false
		}

		// Specific subtypes come before wildcard subtypes.
		if accepts[i].Subtype != "*" && accepts[j].Subtype == "*" {
			return true
		} else if accepts[i].Subtype == "*" && accepts[j].Subtype != "*" {
			return false
		}

		// A lot of extensions comes before not a lot of extensions.
		if len(accepts[i].Extensions) > len(accepts[j].Extensions) {
			return true
		}

		return false
	})

	if len(accepts) <= 0 {
		return ""
	}

	return accepts[0].Type + "/" + accepts[0].Subtype
}

type acceptValue struct {
	Type, Subtype string
	Q             float64
	Extensions    map[string]string
}

func parseMediaRange(mediaRange string) ([]string, []string, error) {
	rangeParams := strings.Split(mediaRange, ";")
	typeSubtype := strings.Split(rangeParams[0], "/")
	if len(typeSubtype) > 2 {
		return nil, nil, fmt.Errorf("accept: invalid type %q", rangeParams[0])
	}

	typeSubtype = append(typeSubtype, "*")

	typeSubtype[0] = strings.TrimSpace(typeSubtype[0])
	if typeSubtype[0] == "" {
		typeSubtype[0] = "*"
	}

	typeSubtype[1] = strings.TrimSpace(typeSubtype[1])
	if typeSubtype[1] == "" {
		typeSubtype[1] = "*"
	}

	return rangeParams, typeSubtype, nil
}