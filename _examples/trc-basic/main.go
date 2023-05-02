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
	// Create a `kv` service in memory.
	kv := NewKV(NewStore())

	// Serve the `kv` API over HTTP.
	var apiHandler http.Handler
	{
		apiHandler = kv                                        // `kv`  already implements http.Handler
		apiHandler = eztrc.Middleware(apiCategory)(apiHandler) // this creates a trace for each `kv` request
	}

	// Generate random get/set/del requests to the API.
	go func() {
		load(context.Background(), apiHandler)
	}()

	// Serve the trc API over HTTP.
	var trcHandler http.Handler
	{
		trcHandler = eztrc.Handler()                                                              // this serves the singleton eztrc.Collector
		trcHandler = eztrc.Middleware(func(*http.Request) string { return "traces" })(trcHandler) // this creates a trace for each `trc` request
	}

	// Here's how you would change the number of traces per category.
	eztrc.Collector().Resize(context.Background(), 100)

	// Create a single serve mux for both API endpoints.
	mux := http.NewServeMux()
	mux.Handle("/api", http.StripPrefix("/api", apiHandler))
	mux.Handle("/trc", http.StripPrefix("/trc", trcHandler))

	// Run the server.
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
		time.Sleep(5 * time.Millisecond)
	}
}
