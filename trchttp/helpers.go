package trchttp

import (
	"mime"
	"net/http"
	"strings"
)

func RequestHasContentType(r *http.Request, acceptable ...string) bool {
	have := parseHeaderMediaTypes(r, "content-type")
	for _, want := range acceptable {
		if _, ok := have[want]; ok {
			return true
		}
	}
	return false
}

func RequestExplicitlyAccepts(r *http.Request, acceptable ...string) bool {
	have := parseHeaderMediaTypes(r, "accept")
	for _, want := range acceptable {
		if _, ok := have[want]; ok {
			return true
		}
	}
	return false
}

func parseHeaderMediaTypes(r *http.Request, header string) map[string]map[string]string {
	mediaTypes := map[string]map[string]string{} // type: params
	for _, val := range strings.Split(r.Header.Get(header), ",") {
		mediaType, params, err := mime.ParseMediaType(val)
		if err != nil {
			continue
		}
		mediaTypes[mediaType] = params
	}
	return mediaTypes
}
