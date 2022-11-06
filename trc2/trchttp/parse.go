package trchttp

import (
	"fmt"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func ParseDefault[T any](s string, parse func(string) (T, error), def T) T {
	if v, err := parse(s); err == nil {
		return v
	}
	return def
}

func ParseDurationPointer(s string) (*time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

//
//
//

func GetBestContentType(r *http.Request) string {
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

//
//
//

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

func RequestExplicitlyAccepts(r *http.Request, acceptable ...string) bool {
	accept := parseAcceptMediaTypes(r)
	for _, want := range acceptable {
		if _, ok := accept[want]; ok {
			return true
		}
	}
	return false
}
