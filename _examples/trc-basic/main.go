package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	_ "net/http/pprof"
	"strings"
	"time"

	"github.com/peterbourgon/trc/eztrc"
)

func main() {
	kv := NewKV(NewStore())

	var apiHandler http.Handler
	{
		apiHandler = kv
		apiHandler = eztrc.Middleware(apiCategory)(apiHandler)
	}

	go func() {
		load(context.Background(), apiHandler)
	}()

	var trcHandler http.Handler
	{
		trcHandler = eztrc.Handler()
		trcHandler = eztrc.Middleware(func(r *http.Request) string { return "traces" })(trcHandler)
	}

	eztrc.Collector().Resize(context.Background(), 500)

	mux := http.NewServeMux()
	mux.Handle("/api", http.StripPrefix("/api", apiHandler))
	mux.Handle("/trc", http.StripPrefix("/trc", trcHandler))

	server := &http.Server{Addr: "localhost:8080", Handler: mux}
	log.Printf("http://localhost:8080/trc")
	log.Fatal(server.ListenAndServe())
}

func load(ctx context.Context, dst http.Handler) {
	for ctx.Err() == nil {
		f := rand.Float64()
		switch {
		case f < 0.6:
			key := getWord()
			url := fmt.Sprintf("http://irrelevant/%s", key)
			req, _ := http.NewRequest("GET", url, nil)
			rec := httptest.NewRecorder()
			dst.ServeHTTP(rec, req)

		case f < 0.9:
			key := getWord()
			val := getWord()
			url := fmt.Sprintf("http://irrelevant/%s", key)
			req, _ := http.NewRequest("PUT", url, strings.NewReader(val))
			rec := httptest.NewRecorder()
			dst.ServeHTTP(rec, req)

		default:
			key := getWord()
			url := fmt.Sprintf("http://irrelevant/%s", key)
			req, _ := http.NewRequest("DELETE", url, nil)
			rec := httptest.NewRecorder()
			dst.ServeHTTP(rec, req)
		}
		time.Sleep(time.Millisecond)
	}
}
