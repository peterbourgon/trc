package trchttp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/internal/trcdebug"
	"github.com/peterbourgon/trc/internal/trcutil"
	"github.com/peterbourgon/trc/trchttp/assets"
)

func respondData(ctx context.Context, w http.ResponseWriter, r *http.Request, code int, templateName string, data any) {
	tr := trc.Get(ctx)

	switch {
	case RequestExplicitlyAccepts(r, "text/html"):
		body, err := renderTemplate(ctx, assets.FS, templateName, data)
		if err != nil {
			tr.LazyErrorf("render template: %v", err)
			code = http.StatusInternalServerError
			body = []byte(fmt.Sprintf(`<html><body><h1>Error</h1><p>%v</p>`, err))
		}
		w.Header().Set("content-type", "text/html; charset=utf-8")
		w.WriteHeader(code)
		w.Write(body)

	default:
		respondJSON(w, r, code, data)
	}
}

func respondError(w http.ResponseWriter, r *http.Request, err error, code int) {
	switch {
	case RequestExplicitlyAccepts(r, "text/html"):
		w.Header().Set("content-type", "text/html")
		w.WriteHeader(code)
		fmt.Fprintf(w, `
			<html>
			<head>
			<title>trc - error</title>
			</head>
			<body>
			<h1>Error</h1>
			<p>HTTP %d (%s) -- %s</p>
			</body>
			</html>
		`, code, http.StatusText(code), err.Error())

	default:
		respondJSON(w, r, code, map[string]any{
			"error":       err.Error(),
			"status_code": code,
			"status_text": http.StatusText(code),
		})
	}
}

func respondJSON(w http.ResponseWriter, r *http.Request, code int, data any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

// AssetsDirEnvKey can be set to a local path for the assets directory, in which
// case those files will be used when rendering assets, instead of the embedded
// assets. This is especially useful when developing.
const AssetsDirEnvKey = "TRC_ASSETS_DIR"

func renderTemplate(ctx context.Context, fs fs.FS, templateName string, data any) (_ []byte, err error) {
	_, tr, finish := trc.Region(ctx, "renderTemplate")
	defer finish()

	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("PANIC: %v", x)
		}
	}()

	templateRoot, err := template.New("root").Funcs(templateFuncs).ParseFS(fs, "*")
	if err != nil {
		return nil, fmt.Errorf("parse assets: %w", err)
	}

	tr.LazyTracef("template.ParseFS OK")

	{
		var (
			localPath  = filepath.Clean(os.Getenv(AssetsDirEnvKey)) // pwd by default
			localFiles []string
		)
		for _, tp := range templateRoot.Templates() {
			templateName := tp.Name()
			if templateName == "" {
				continue
			}
			assetName := filepath.Join(localPath, templateName)
			if _, err := os.Stat(assetName); err != nil {
				continue
			}
			localFiles = append(localFiles, assetName)
		}
		if len(localFiles) > 0 {
			tt, err := templateRoot.ParseFiles(localFiles...)
			if err != nil {
				return nil, fmt.Errorf("parse local files: %w", err)
			}
			templateRoot = tt
			tr.LazyTracef("local files %v", localFiles)
		}
	}

	templateFile := templateRoot.Lookup(templateName)
	if templateFile == nil {
		return nil, fmt.Errorf("template (%s) not found", templateName)
	}

	tr.LazyTracef("template.Lookup(%s) OK", templateName)

	var templateBuf bytes.Buffer
	if err := templateFile.Execute(&templateBuf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	tr.LazyTracef("template.Execute OK, %s", trcutil.HumanizeBytes(templateBuf.Len()))

	return templateBuf.Bytes(), nil
}

//
//
//

const timeFormat = "15:04:05.000000"

var breaksReplacer = strings.NewReplacer(
	`,`, `,<wbr>`,
	`;`, `;<wbr>`,
)

//
//
//

// SourceLinkFunc converts a local source code file and line to a URL that can
// be opened by a browser.
type SourceLinkFunc func(fileline string) template.URL

var sourceLinkFunc = trcutil.NewAtomic(func(string) template.URL { return "" })

// SetSourceLinkFunc sets the function used to produce clickable links to source
// code in stack traces. By default links are not clickable.
func SetSourceLinkFunc(f SourceLinkFunc) { sourceLinkFunc.Set(f) }

// SourceLinkVSCode produces links that open in VS Code.
func SourceLinkVSCode(fileline string) template.URL { return template.URL("vscode://file/" + fileline) }

//
//
//

var templateFuncs = template.FuncMap{
	"SourceLink":           func(fileline string) template.URL { return sourceLinkFunc.Get()(fileline) },
	"AddInt":               func(i, j int) int { return i + j },
	"AddFloat":             func(i, j float64) float64 { return i + j },
	"PercentInt":           func(n, d int) int { return int(100 * float64(n) / float64(d)) },
	"PercentUint64":        func(n, d uint64) int { return int(100 * float64(n) / float64(d)) },
	"PercentDuration":      func(n, d time.Duration) int { return int(100 * float64(n) / float64(d)) },
	"PercentDurationFloat": func(n, d time.Duration) float64 { return 100 * float64(n) / float64(d) },
	"TimeNow":              func() time.Time { return time.Now().UTC() },
	"TimeSince":            func(t time.Time) time.Duration { return time.Since(t) },
	"TimeDiff":             func(a, b time.Time) time.Duration { return a.Sub(b) },
	"TimeAdd":              func(t time.Time, d time.Duration) time.Time { return t.Add(d) },
	"TimeTrunc":            func(t time.Time) string { return t.Format(timeFormat) },
	"TimeRFC3339":          func(t time.Time) string { return t.Format(time.RFC3339) },
	"QueryEscape":          func(s string) string { return url.QueryEscape(s) },
	"PathEscape":           func(s string) string { return url.PathEscape(s) },
	"HTMLEscape":           func(s string) string { return template.HTMLEscapeString(s) },
	"InsertBreaks":         func(s string) template.HTML { return template.HTML(breaksReplacer.Replace(s)) },
	"URLEncode":            func(s string) template.URL { return template.URL(url.QueryEscape(s)) },
	"SafeURL":              func(s string) template.URL { return template.URL(s) },
	"DefaultBucketing":     func() []time.Duration { return trc.DefaultBucketing },
	"StringsJoinNewline":   func(a []string) string { return strings.Join(a, string([]byte{0xa})) },
	"ReflectDeepEqual":     func(a, b any) bool { return reflect.DeepEqual(a, b) },
	"PositiveDuration":     func(d time.Duration) time.Duration { return iff(d > 0, d, 0) },
	"RateCalc":             func(n int, d time.Duration) float64 { return iff(d > 0, float64(n)/float64(d.Seconds()), 0) },
	"StringSliceContains":  func(ss []string, s string) bool { return contains(ss, s) },
	"TruncateDuration":     trcutil.TruncateDuration,
	"HumanizeDuration":     trcutil.HumanizeDuration,
	"HumanizeFloat":        trcutil.HumanizeFloat,
	"HumanizeBytes":        trcutil.HumanizeBytes[int],
	"HumanizeFunction":     humanizeFunction,
	"CategoryClass":        categoryClass,
	"HighlightClasses":     highlightClasses,
	"DebugInfo":            debugInfo,
	"FlexGrowPercent":      flexGrowPercent,
	"RenderEvents":         renderEvents,
}

func humanizeFunction(s string) string {
	if index := strings.LastIndex(s, "/"); index > 0 && index < len(s) {
		s = s[index+1:]
	}
	return s
}

func categoryClass(category string) string {
	return "category-" + sha256hex(category)
}

func highlightClasses(f trc.Filter) []string {
	var classes []string
	if len(f.IDs) > 0 {
		return nil
	}
	if f.Category != "" {
		classes = append(classes, categoryClass(f.Category))
	}
	if f.IsActive {
		classes = append(classes, "active")
	}
	if f.MinDuration != nil {
		classes = append(classes, "min-"+f.MinDuration.String())
	}
	if f.IsErrored {
		classes = append(classes, "errored")
	}
	return classes
}

func debugInfo() string {
	buf := &bytes.Buffer{}

	{
		tw := tabwriter.NewWriter(buf, 0, 2, 2, ' ', 0)
		fmt.Fprintf(tw, "POOL\tGET\tALLOC\tPUT\tACTIVE\tLOST\tREUSE\n")
		for _, pair := range []struct {
			name     string
			counters *trcdebug.PoolCounters
		}{
			{"coreTrace", &trcdebug.CoreTraceCounters},
			{"coreEvent", &trcdebug.CoreEventCounters},
			{"stringer", &trcdebug.StringerCounters},
			{"StaticTrace", &trcdebug.StaticTraceCounters},
		} {
			get, alloc, put, lost, reuse := pair.counters.Values()
			fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%.2f%%\n", pair.name, get, alloc, put, int64(get)-int64(put), lost, reuse)
		}
		tw.Flush()
	}

	return buf.String()
}

func sha256hex(input string) string {
	h := sha256.Sum256([]byte(input))
	s := hex.EncodeToString(h[:])
	return s
}

func flexGrowPercent(f float64) int {
	if f < 1 {
		return 1
	}
	if f > 100 {
		return 100
	}
	return int(f)
}

func renderEvents(st *trc.StaticTrace) []renderEvent {
	var events []renderEvent

	// Synthetic "start" event.
	events = append(events, renderEvent{
		IsStart: true,
		Index:   -1,
		When:    st.TraceStarted,
		What:    "start",
	})

	// Actual trace events.
	prev := st.TraceStarted
	for i, ev := range st.TraceEvents {
		delta := ev.When.Sub(prev)
		events = append(events, renderEvent{
			Index:        i,
			When:         ev.When,
			Delta:        delta,
			DeltaPercent: 100 * float64(delta) / float64(st.TraceDuration),
			Cumulative:   ev.When.Sub(st.TraceStarted),
			What:         ev.What,
			IsError:      ev.IsError,
			Stack:        ev.Stack,
		})
		prev = ev.When
	}

	// Synthetic "end" event.
	when := st.TraceStarted.Add(st.TraceDuration)
	delta := when.Sub(prev)
	what := iff(st.TraceFinished, "finished", "active...")
	events = append(events, renderEvent{
		IsEnd:        true,
		Index:        len(st.TraceEvents),
		When:         when,
		Delta:        delta,
		DeltaPercent: 100 * float64(delta) / float64(st.TraceDuration),
		Cumulative:   st.TraceDuration,
		What:         what,
	})

	return events
}

type renderEvent struct {
	IsStart, IsEnd bool
	Index          int
	When           time.Time
	Delta          time.Duration
	DeltaPercent   float64
	Cumulative     time.Duration
	What           string
	IsError        bool
	Stack          []trc.Frame
}
