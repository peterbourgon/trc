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
	"sync"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
)

func main() {
	// Each port is a distinct instance.
	ports := []string{"8081", "8082", "8083"}

	// Create a trace collector for each instance.
	collectors := make([]*trc.Collector, len(ports))
	for i := range collectors {
		collectors[i] = trc.NewDefaultCollector()
	}

	// Create a `kv` service for each instance.
	kvs := make([]*KV, len(ports))
	for i := range kvs {
		kvs[i] = NewKV(NewStore())
	}

	// Create a `kv` API HTTP handler for each instance.
	// Trace each request in the corresponding collector.
	apiHandlers := make([]http.Handler, len(ports))
	for i := range apiHandlers {
		apiHandlers[i] = kvs[i]
		apiHandlers[i] = trchttp.Middleware(collectors[i].NewTrace, apiCategory)(apiHandlers[i])
	}

	// Generate random load for each `kv` instance.
	apiWorkers := sync.WaitGroup{}
	for _, h := range apiHandlers {
		apiWorkers.Add(1)
		go func(h http.Handler) {
			defer apiWorkers.Done()
			load(context.Background(), h)
		}(h)
	}

	// Create a traces HTTP handler for each instance.
	// We'll also trace each request to this endpoint.
	trcHandlers := make([]http.Handler, len(collectors))
	for i := range trcHandlers {
		trcHandlers[i] = trchttp.NewServer(collectors[i])
		trcHandlers[i] = trchttp.Middleware(collectors[i].NewTrace, func(r *http.Request) string { return "traces" })(trcHandlers[i])
	}

	// We can also create a "global" traces handler, which serves collective
	// results from all of the individual trace handlers for each instance.
	var trcGlobal http.Handler
	{
		var ms trc.MultiSearcher
		for i := range ports {
			// We model instances with HTTP clients querying the corresponding
			// trace HTTP handler. That's usually how you'd do it, when you want
			// to abstract over instances on different hosts.
			ms = append(ms, trchttp.NewClient(http.DefaultClient, fmt.Sprintf("http://localhost:%s/trc", ports[i])))
		}

		// We want to collect traces for requests to this handler, too.
		globalCollector := trc.NewDefaultCollector()
		ms = append(ms, globalCollector)

		// MultiSearcher satisfies the Searcher interface required by the HTTP
		// server, same as an e.g. collector.
		trcGlobal = trchttp.NewServer(ms)
		trcGlobal = trchttp.Middleware(globalCollector.NewTrace, func(r *http.Request) string { return "global" })(trcGlobal)
	}

	// Now we run HTTP servers for each instance.
	httpServers := make([]*http.Server, len(ports))
	for i := range httpServers {
		addr := "localhost:" + ports[i]
		mux := http.NewServeMux()
		mux.Handle("/api", http.StripPrefix("/api", apiHandlers[i]))
		mux.Handle("/trc", http.StripPrefix("/trc", trcHandlers[i]))
		s := &http.Server{Addr: addr, Handler: mux}
		go func() { log.Fatal(s.ListenAndServe()) }()
		log.Printf("http://localhost:%s/trc", ports[i])
	}

	// And an extra HTTP server for the global trace handler.
	go func() {
		http.Handle("/trc", trcGlobal)
		log.Printf("http://localhost:8080/trc")
		log.Fatal(http.ListenAndServe("localhost:8080", nil))
	}()

	select {}
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
