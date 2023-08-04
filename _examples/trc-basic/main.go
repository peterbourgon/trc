package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	_ "net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/eztrc"
)

func main() {
	// Create a `kv` service in memory.
	kv := NewKV(NewStore())

	// Serve the `kv` API over HTTP.
	var apiHandler http.Handler
	{
		apiHandler = kv                                        // `kv` implements http.Handler
		apiHandler = eztrc.Middleware(apiCategory)(apiHandler) // create a trace for each API request
	}

	// Generate random get/set/del requests to the API handler.
	go func() {
		load(context.Background(), apiHandler)
	}()

	// Serve the trace API over HTTP.
	var tracesHandler http.Handler
	{
		tracesHandler = eztrc.Handler()                                                                 // this serves the singleton eztrc.Collector
		tracesHandler = eztrc.Middleware(func(*http.Request) string { return "traces" })(tracesHandler) // create a trace for each trace request
	}

	// Here's how you would change the number of traces per category.
	eztrc.Collector().SetCategorySize(100)
	eztrc.Collector().SetDecorators(trc.LogDecorator(os.Stderr))

	// Create a single serve mux for both API endpoints.
	mux := http.NewServeMux()
	mux.Handle("/api", http.StripPrefix("/api", apiHandler)) // technically unnecessary as it's not used by the loader
	mux.Handle("/traces", http.StripPrefix("/traces", tracesHandler))

	// Run the server.
	server := &http.Server{Addr: "localhost:8080", Handler: mux}
	log.Printf("http://localhost:8080/traces")
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
