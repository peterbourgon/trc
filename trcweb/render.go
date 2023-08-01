package trcweb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
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
	"github.com/peterbourgon/trc/trcsrc"
	"github.com/peterbourgon/trc/trcstore"
)

//go:embed assets/*
var assetsRoot embed.FS

var assets = func() fs.FS {
	assets, err := fs.Sub(assetsRoot, "assets")
	if err != nil {
		panic(err)
	}
	return assets
}()

//
//
//

func renderResponse(ctx context.Context, w http.ResponseWriter, r *http.Request, fs fs.FS, templateName string, funcs template.FuncMap, data any) {
	var (
		asksForJSON = r.URL.Query().Has("json")
		acceptsJSON = requestExplicitlyAccepts(r, "application/json")
		acceptsHTML = requestExplicitlyAccepts(r, "text/html")
		useHTML     = acceptsHTML && !asksForJSON
		useJSON     = acceptsJSON || asksForJSON
	)
	switch {
	case useHTML:
		renderHTML(ctx, w, fs, templateName, funcs, data)
	case useJSON:
		renderJSON(ctx, w, data)
	default:
		renderJSON(ctx, w, data)
	}
}

func renderHTML(ctx context.Context, w http.ResponseWriter, fs fs.FS, templateName string, funcs template.FuncMap, data any) {
	tr := trc.Get(ctx)

	code := http.StatusOK
	body, err := renderTemplate(ctx, fs, templateName, funcs, data)
	if err != nil {
		tr.LazyErrorf("render template: %v", err)
		code = http.StatusInternalServerError
		body = []byte(fmt.Sprintf(`<html><body><h1>Error</h1><p>%v</p>`, err))
	}

	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	w.Write(body)
}

func renderJSON(ctx context.Context, w http.ResponseWriter, data any) {
	tr := trc.Get(ctx)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "    ")

	code := http.StatusOK
	if err := enc.Encode(data); err != nil {
		code = http.StatusInternalServerError
		tr.LazyErrorf("marshal JSON: %v", err)
		buf.Reset()
		buf.WriteString(`{"error":"failed to marshal response"}`)
	} else {
		tr.LazyTracef("marshaled JSON response (%s)", trcutil.HumanizeBytes(buf.Len()))
	}

	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	buf.WriteTo(w)
}

func requestExplicitlyAccepts(r *http.Request, acceptable ...string) bool {
	accept := parseAcceptMediaTypes(r)
	for _, want := range acceptable {
		if _, ok := accept[want]; ok {
			return true
		}
	}
	return false
}

func parseAcceptMediaTypes(r *http.Request) map[string]map[string]string {
	mediaTypes := map[string]map[string]string{} // type: params
	for _, a := range strings.Split(r.Header.Get("accept"), ",") {
		mediaType, params, err := mime.ParseMediaType(a)
		if err != nil {
			continue
		}
		mediaTypes[mediaType] = params
	}
	return mediaTypes
}

const AssetsDirEnvKey = "TRC_ASSETS_DIR"

func renderTemplate(ctx context.Context, fs fs.FS, templateName string, userFuncs template.FuncMap, data any) (_ []byte, err error) {
	_, tr, finish := trc.Region(ctx, "renderTemplate")
	defer finish()

	defer func() {
		if x := recover(); x != nil {
			err = fmt.Errorf("PANIC: %v", x)
		}
	}()

	templateRoot, err := template.New("root").Funcs(templateFuncs).Funcs(userFuncs).ParseFS(fs, "*")
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
			tr.LazyTracef("using local asset file %s", assetName)
		}
		if len(localFiles) > 0 {
			tt, err := templateRoot.ParseFiles(localFiles...)
			if err != nil {
				return nil, fmt.Errorf("parse local files: %w", err)
			}
			templateRoot = tt
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

// FileLineURL converts a local source code file and line to a URL that can be
// opened by a browser. The default value is FileLineURLNop.
var FileLineURL = FileLineURLNop

// FileLineURLNop returns an empty string, preventing any clickable link.
func FileLineURLNop(string) template.URL { return "" }

// FileLineURLVSCode opens the source file in VS Code.
func FileLineURLVSCode(fileline string) template.URL {
	return template.URL("vscode://file/" + fileline)
}

//
//
//

var templateFuncs = template.FuncMap{
	"FileLineURL":         func(fileline string) template.URL { return FileLineURL(fileline) },
	"AddInt":              func(i, j int) int { return i + j },
	"AddFloat":            func(i, j float64) float64 { return i + j },
	"PercentInt":          func(n, d int) int { return int(100 * float64(n) / float64(d)) },
	"PercentUint64":       func(n, d uint64) int { return int(100 * float64(n) / float64(d)) },
	"PercentDuration":     func(n, d time.Duration) int { return int(100 * float64(n) / float64(d)) },
	"TimeNow":             func() time.Time { return time.Now().UTC() },
	"TimeSince":           func(t time.Time) time.Duration { return time.Since(t) },
	"TimeDiff":            func(a, b time.Time) time.Duration { return a.Sub(b) },
	"TimeAdd":             func(t time.Time, d time.Duration) time.Time { return t.Add(d) },
	"TimeTrunc":           func(t time.Time) string { return t.Format(timeFormat) },
	"TimeRFC3339":         func(t time.Time) string { return t.Format(time.RFC3339) },
	"QueryEscape":         func(s string) string { return url.QueryEscape(s) },
	"PathEscape":          func(s string) string { return url.PathEscape(s) },
	"HTMLEscape":          func(s string) string { return template.HTMLEscapeString(s) },
	"InsertBreaks":        func(s string) template.HTML { return template.HTML(breaksReplacer.Replace(s)) },
	"URLEncode":           func(s string) template.URL { return template.URL(url.QueryEscape(s)) },
	"SafeURL":             func(s string) template.URL { return template.URL(s) },
	"DefaultBucketing":    func() []time.Duration { return trcstore.DefaultBucketing },
	"StringsJoinNewline":  func(a []string) string { return strings.Join(a, string([]byte{0xa})) },
	"ReflectDeepEqual":    func(a, b any) bool { return reflect.DeepEqual(a, b) },
	"PositiveDuration":    func(d time.Duration) time.Duration { return iff(d > 0, d, 0) },
	"RateCalc":            func(n int, d time.Duration) float64 { return iff(d > 0, float64(n)/float64(d.Seconds()), 0) },
	"StringSliceContains": func(ss []string, s string) bool { return contains(ss, s) },
	"TruncateDuration":    trcutil.TruncateDuration,
	"HumanizeDuration":    trcutil.HumanizeDuration,
	"HumanizeFloat":       trcutil.HumanizeFloat,
	"HumanizeBytes":       trcutil.HumanizeBytes,
	"CategoryClass":       categoryClass,
	"HighlightClasses":    highlightClasses,
	"DebugInfo":           debugInfo,
}

func categoryClass(category string) string {
	return "category-" + sha256hex(category)
}

func highlightClasses(f trcsrc.Filter) []string {
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
	var (
		tn  = trcdebug.CoreTraceNewCount.Load()
		ta  = trcdebug.CoreTraceAllocCount.Load()
		tf  = trcdebug.CoreTraceFreeCount.Load()
		tl  = trcdebug.CoreTraceLostCount.Load()
		tr  = 100 * float64(tf) / float64(tn)
		en  = trcdebug.CoreEventNewCount.Load()
		ea  = trcdebug.CoreEventAllocCount.Load()
		ef  = trcdebug.CoreEventFreeCount.Load()
		el  = trcdebug.CoreEventLostCount.Load()
		er  = 100 * float64(ef) / float64(en)
		sn  = trcdebug.StringerNewCount.Load()
		sa  = trcdebug.StringerAllocCount.Load()
		sf  = trcdebug.StringerFreeCount.Load()
		sl  = trcdebug.StringerLostCount.Load()
		sr  = 100 * float64(sf) / float64(sn)
		buf = &bytes.Buffer{}
		tw  = tabwriter.NewWriter(buf, 0, 2, 2, ' ', 0)
	)
	fmt.Fprintf(tw, "KIND\tNEW\tALLOC\tFREE\tLOST\tREUSE\n")
	fmt.Fprintf(tw, "coreTrace\t%d\t%d\t%d\t%d\t%.2f%%\n", tn, ta, tf, tl, tr)
	fmt.Fprintf(tw, "coreEvent\t%d\t%d\t%d\t%d\t%.2f%%\n", en, ea, ef, el, er)
	fmt.Fprintf(tw, "stringer\t%d\t%d\t%d\t%d\t%.2f%%\n", sn, sa, sf, sl, sr)
	tw.Flush()
	return buf.String()
}

func sha256hex(input string) string {
	h := sha256.Sum256([]byte(input))
	s := hex.EncodeToString(h[:])
	return s
}

func iff[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}

func contains[T comparable](haystack []T, needle T) bool {
	for _, elem := range haystack {
		if elem == needle {
			return true
		}
	}
	return false
}
