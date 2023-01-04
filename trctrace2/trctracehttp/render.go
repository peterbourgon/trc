package trctracehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"runtime/trace"
	"strings"

	"github.com/peterbourgon/trc"
)

func Render(ctx context.Context, w http.ResponseWriter, r *http.Request, fs fs.FS, templateName string, funcs template.FuncMap, data any) {
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
	ctx, tr, finish := trc.Region(ctx, "renderHTML")
	defer finish()

	code := http.StatusOK
	body, err := renderTemplate(ctx, fs, templateName, funcs, data)
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

func renderJSON(ctx context.Context, w http.ResponseWriter, data any) {
	_, _, finish := trc.Region(ctx, "write JSON response")
	defer finish()
	w.Header().Set("content-type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "    ")
	enc.Encode(data)
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

func renderTemplate(ctx context.Context, fs fs.FS, templateName string, userFuncs template.FuncMap, data any) (_ []byte, err error) {
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

	templateRoot, err := template.New("root").Funcs(templateFuncs).Funcs(userFuncs).ParseFS(fs, "*")
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
