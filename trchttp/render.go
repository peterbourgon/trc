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
	ctx, tr, finish := trc.Region(ctx, "render HTML")
	defer finish()

	code := http.StatusOK
	body, err := renderTemplate(ctx, fs, templateName, funcs, data)
	if err != nil {
		code = http.StatusInternalServerError
		tr.LazyErrorf("render template: %v", err)
		body = []byte(fmt.Sprintf(`<html><body><h1>Error</h1><p>%v</p>`, err))
	} else {
		tr.LazyTracef("rendered template (%s)", humanizebytes(len(body)))
	}

	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	w.Write(body)
}

func renderJSON(ctx context.Context, w http.ResponseWriter, data any) {
	_, tr, finish := trc.Region(ctx, "render JSON")
	defer finish()

	code := http.StatusOK
	body, err := json.Marshal(data)
	if err != nil {
		code = http.StatusInternalServerError
		tr.LazyErrorf("marshal JSON: %v", err)
		body = []byte(`{"error":"failed to marshal response"}`)
	} else {
		tr.LazyTracef("marshaled JSON response (%s)", humanizebytes(len(body)))
	}

	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	w.Write(body)
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

//
//
//

// AssetsDirEnvKey is the environment variable key that specifies a directory
// containing trace assets such as HTML and/or CSS files. By default, the
// current working directory is used.
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

	tr.LazyTracef("template ParseFS OK")

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
			tr.Tracef("using local asset file %s", assetName)
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

	tr.LazyTracef("template (%s) lookup OK (%s)", templateName, templateFile.Name())

	var templateBuf bytes.Buffer
	if err := templateFile.Execute(&templateBuf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	tr.LazyTracef("template (%s) execute OK (%s)", templateName, humanizebytes(templateBuf.Len()))

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

var templateFuncs = template.FuncMap{
	"filelineurl":         func(fileline string) template.URL { return FileLineURL(fileline) },
	"intadd":              func(i, j int) int { return i + j },
	"floatadd":            func(i, j float64) float64 { return i + j },
	"timenow":             func() time.Time { return time.Now().UTC() },
	"timesince":           func(t time.Time) time.Duration { return time.Since(t) },
	"timediff":            func(a, b time.Time) time.Duration { return a.Sub(b) },
	"timeadd":             func(t time.Time, d time.Duration) time.Time { return t.Add(d) },
	"timetrunc":           func(t time.Time) string { return t.Format(timeFormat) },
	"timerfc3339":         func(t time.Time) string { return t.Format(time.RFC3339) },
	"durationpercent":     func(n, d time.Duration) int { return int(100 * float64(n) / float64(d)) },
	"uint64percent":       func(n, d uint64) int { return int(100 * float64(n) / float64(d)) },
	"intpercent":          func(n, d int) int { return int(100 * float64(n) / float64(d)) },
	"queryescape":         func(s string) string { return url.QueryEscape(s) },
	"pathescape":          func(s string) string { return url.PathEscape(s) },
	"htmlescape":          func(s string) string { return template.HTMLEscapeString(s) },
	"insertbreaks":        func(s string) template.HTML { return template.HTML(breaksReplacer.Replace(s)) },
	"urlencode":           func(s string) template.URL { return template.URL(url.QueryEscape(s)) },
	"safeurl":             func(s string) template.URL { return template.URL(s) },
	"stringsjoinnewline":  func(a []string) string { return strings.Join(a, string([]byte{0xa})) },
	"defaultbucketing":    func() []time.Duration { return trcstore.DefaultBucketing },
	"reflectdeepequal":    func(a, b any) bool { return reflect.DeepEqual(a, b) },
	"positiveduration":    positiveduration,
	"truncateduration":    truncateduration,
	"humanizeduration":    humanizeduration,
	"humanizebytes":       humanizebytes,
	"humanizefloat":       humanizefloat,
	"ratecalc":            ratecalc,
	"category2class":      category2class,
	"highlightclasses":    highlightclasses,
	"fileline2filepath":   fileline2filepath,
	"stringslicecontains": stringslicecontains,
	"debuginfo":           debuginfo,
}

func sha256hex(input string) string {
	h := sha256.Sum256([]byte(input))
	s := hex.EncodeToString(h[:])
	return s
}

func category2class(name string) string {
	return "category-" + sha256hex(name)
}

func highlightclasses(req *trcstore.SearchRequest) []string {
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
	if req.IsErrored {
		classes = append(classes, "errored")
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

func positiveduration(d time.Duration) time.Duration {
	if d < 0 {
		d = 0
	}
	return d
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
	case f >= 1:
		return fmt.Sprintf("%.0f", f) // 812.3 -> 821
	case f == 0:
		return "0"
	default:
		return fmt.Sprintf("%0.01f", f) // 0.15845 -> 0.1
	}
}

func humanizebytes(n int) string {
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

func ratecalc(n int, d time.Duration) float64 {
	if d == 0 {
		return 0.0
	}
	return float64(n) / float64(d.Seconds())
}

func fileline2filepath(fileline string) string {
	if strings.HasPrefix(fileline, "github.com/peterbourgon") {
		homedir, _ := os.UserHomeDir()
		homedir, _ = filepath.Abs(homedir)
		fileline = strings.TrimPrefix(fileline, "github.com/peterbourgon")
		fileline = filepath.Join(homedir, "src", "peterbourgon", fileline)
		return fileline
	}

	return ""
}

func stringslicecontains(ss []string, s string) bool {
	for _, candidate := range ss {
		if candidate == s {
			return true
		}
	}
	return false
}

func debuginfo() string {
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
