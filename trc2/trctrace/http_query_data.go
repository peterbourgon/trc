package trctrace

import (
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"net/url"
	"strconv"
)

type HTTPQueryData struct {
	PageTitle        string         `json:"-"`
	AvailableOrigins []string       `json:"available_origins"`
	ResponseOrigin   string         `json:"response_origin,omitempty"`
	Request          *QueryRequest  `json:"request"`
	Response         *QueryResponse `json:"response"`
}

func (d *HTTPQueryData) QueryParams() template.URL {
	base := url.Values{}
	if d.ResponseOrigin != "" {
		base.Set("origin", d.ResponseOrigin)
	}
	if d.Request.Limit > 0 {
		base.Set("n", strconv.Itoa(d.Request.Limit))
	}
	if d.Request.Search != "" {
		base.Set("q", d.Request.Search)
	}
	return template.URL(base.Encode())
}

var templateFuncs = template.FuncMap{
	"category2class":   category2class,
	"highlightclasses": highlightclasses,
}

func category2class(name string) string {
	return "category-" + sha256hex(name)
}

func highlightclasses(req *QueryRequest) []string {
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
	if req.IsErrored {
		classes = append(classes, "errored")
	}
	if req.IsFinished {
		classes = append(classes, "finished")
	}
	if req.IsSucceeded {
		classes = append(classes, "succeeded")
	}
	if req.MinDuration != nil {
		classes = append(classes, "min-"+req.MinDuration.String())
	}
	return classes
}

func sha256hex(input string) string {
	h := sha256.Sum256([]byte(input))
	s := hex.EncodeToString(h[:])
	return s
}
