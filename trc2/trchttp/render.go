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
	"runtime/trace"
	"strings"
	"time"

	trc "github.com/peterbourgon/trc/trc2"
)

func Render(ctx context.Context, w http.ResponseWriter, r *http.Request, fs fs.FS, templateName string, data any) {
	var (
		asksForJSON = r.URL.Query().Has("json")
		acceptsHTML = RequestExplicitlyAccepts(r, "text/html")
		useHTML     = !asksForJSON && acceptsHTML
	)
	switch {
	case useHTML:
		renderHTML(ctx, w, fs, templateName, data)
	default:
		renderJSON(ctx, w, data)
	}
}

func renderJSON(ctx context.Context, w http.ResponseWriter, data any) {
	_, _, finish := trc.Region(ctx, "write JSON response")
	defer finish()
	w.Header().Set("content-type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "    ")
	enc.Encode(data)
}

func renderHTML(ctx context.Context, w http.ResponseWriter, fs fs.FS, templateName string, data any) {
	ctx, tr, finish := trc.Region(ctx, "renderHTML")
	defer finish()

	code := http.StatusOK
	body, err := renderTemplate(ctx, fs, templateName, data)
	if err != nil {
		code = http.StatusInternalServerError
		body = []byte(fmt.Sprintf(`<html><body><h1>Error</h1><p>%v</p>`, err))
	}

	tr.Tracef("template OK")

	{
		_, _, finish := trc.Region(ctx, "write HTML response")
		w.Header().Set("content-type", "text/html; charset=utf-8")
		w.WriteHeader(code)
		w.Write(body)
		finish()
	}

	tr.Tracef("write OK")
}

func renderTemplate(ctx context.Context, fs fs.FS, templateName string, data any) (_ []byte, err error) {
	ctx, task := trace.NewTask(ctx, "renderTemplate task")
	defer task.End()

	stdregion := trace.StartRegion(ctx, "renderTemplate region")
	defer stdregion.End()

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

	tr.Tracef("ParseFS OK")

	{
		var localFiles []string

		for _, tp := range templateRoot.Templates() {
			name := tp.Name()
			if name == "" {
				continue
			}
			if _, err := os.Stat(name); err != nil {
				continue
			}
			localFiles = append(localFiles, name)
		}

		if len(localFiles) > 0 {
			tt, err := templateRoot.ParseFiles(localFiles...)
			if err != nil {
				return nil, fmt.Errorf("parse local files: %w", err)
			}
			templateRoot = tt
		}

		tr.Tracef("check local template files OK, count %d", len(localFiles))
	}

	templateFile := templateRoot.Lookup(templateName)
	if templateFile == nil {
		return nil, fmt.Errorf("template (%s) not found", templateName)
	}

	tr.Tracef("Lookup OK")

	var (
		templateBuf bytes.Buffer
		executeErr  error
	)
	trace.WithRegion(ctx, "execute template", func() {
		executeErr = templateFile.Execute(&templateBuf, data)
	})
	if err := executeErr; err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	tr.Tracef("Execute OK, %dB, %.1fKB, %.2fMB", templateBuf.Len(), float64(templateBuf.Len())/1024, float64(templateBuf.Len())/1024/1024)

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

var templateFuncs = template.FuncMap{
	"intadd":             func(i, j int) int { return i + j },
	"floatadd":           func(i, j float64) float64 { return i + j },
	"category2class":     func(name string) string { return "category-" + sha256hex(name) },
	"timenow":            func() time.Time { return time.Now().UTC() },
	"timesince":          func(t time.Time) time.Duration { return time.Since(t) },
	"timediff":           func(a, b time.Time) time.Duration { return a.Sub(b) },
	"timeadd":            func(t time.Time, d time.Duration) time.Time { return t.Add(d) },
	"timetrunc":          func(t time.Time) string { return t.Format(timeFormat) },
	"timepercent":        func(n, d time.Duration) int { return int(100 * float64(n) / float64(d)) },
	"intpercent":         func(n, d int) int { return int(100 * float64(n) / float64(d)) },
	"queryescape":        func(s string) string { return url.QueryEscape(s) },
	"pathescape":         func(s string) string { return url.PathEscape(s) },
	"htmlescape":         func(s string) string { return template.HTMLEscapeString(s) },
	"insertbreaks":       func(s string) template.HTML { return template.HTML(breaksReplacer.Replace(s)) },
	"stringsjoinnewline": func(a []string) string { return strings.Join(a, string([]byte{0xa})) },
	"highlightclasses":   highlightclasses,
	"truncateduration":   truncateduration,
	"humanizeduration":   humanizeduration,
	"humanizefloat":      humanizefloat,
	"humanize":           humanize,
	"ratecalc":           ratecalc,
	"urihostpath":        urihostpath,
}

func sha256hex(name string) string {
	h := sha256.Sum256([]byte(name))
	s := hex.EncodeToString(h[:])
	return s
}

func highlightclasses(TODO any) []string {
	return []string{}

	// var classes []string
	//
	// if len(res.Stats.Request.IDs) > 0 {
	// return nil
	// }
	//
	// if res.Stats.Request.Category != "" {
	// classes = append(classes, "category-"+sha256hex(res.Stats.Request.Category))
	// }
	//
	// if res.Stats.Request.IsActive {
	// classes = append(classes, "active")
	// }
	//
	// if res.Stats.Request.IsErrored {
	// classes = append(classes, "errored")
	// }
	//
	// if res.Stats.Request.IsFinished {
	// classes = append(classes, "finished")
	// }
	//
	// if res.Stats.Request.IsSucceeded {
	// classes = append(classes, "succeeded")
	// }
	//
	// if res.Stats.Request.MinDuration != nil {
	// classes = append(classes, "min-"+res.Stats.Request.MinDuration.String())
	// }
	//
	// return classes
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

func humanize(d time.Duration) string {
	return humanizeduration(d)
}

func ratecalc(n int, d time.Duration) float64 {
	if d == 0 {
		return 0.0
	}
	return float64(n) / float64(d.Seconds())
}

func urihostpath(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return s
	}

	u.Scheme = ""
	u.RawQuery = ""
	u.RawFragment = ""

	return strings.TrimLeft(u.String(), "/")
}
