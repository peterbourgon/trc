package trchttp

import (
	"mime"
	"net/http"
	"strings"
)

// RuleRouter routes HTTP requests to handlers according to rules, which are
// user-provided functions that take a request and return true or false.
type RuleRouter struct {
	rules    []routeRule
	fallback http.Handler
}

type routeRule struct {
	allow   func(r *http.Request) bool
	handler http.Handler
}

// NewRuleRouter returns an HTTP handler that routes all requests to the
// fallback handler by default. Rules can be added with the Add method.
func NewRuleRouter(fallback http.Handler) *RuleRouter {
	return &RuleRouter{
		fallback: fallback,
	}
}

// Add a new rule to the router, which will route the request to the handler, if
// the allow function returns true. Rules are evaluated in the order in which
// they're added.
func (rr *RuleRouter) Add(allow func(*http.Request) bool, handler http.Handler) {
	rr.rules = append(rr.rules, routeRule{allow: allow, handler: handler})
}

// ServeHTTP routes the request to the first rule handler where the allow
// function returns true, or else to the fallback handler.
func (rr *RuleRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, rule := range rr.rules {
		if rule.allow(r) {
			rule.handler.ServeHTTP(w, r)
			return
		}
	}
	rr.fallback.ServeHTTP(w, r)
}

// RequestHasContentType returns true if the request's Content-Type header
// includes any of the provided (acceptable) media types.
func RequestHasContentType(r *http.Request, acceptable ...string) bool {
	have := parseHeaderMediaTypes(r, "content-type")
	for _, want := range acceptable {
		if _, ok := have[want]; ok {
			return true
		}
	}
	return false
}

// RequestExplicitlyAccepts returns true if the request's Accept header includes
// any of the provided (acceptable) media types.
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
