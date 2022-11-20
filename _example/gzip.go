package main

import (
	"net/http"
)

func GZipMiddleware(next http.Handler) http.Handler {
	//return gziphandler.GzipHandler(next)
	return next
}
