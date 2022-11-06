package main

import (
	"net/http"

	"github.com/NYTimes/gziphandler"
)

func GZipMiddleware(next http.Handler) http.Handler {
	return gziphandler.GzipHandler(next)
}
