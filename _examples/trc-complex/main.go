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

	"github.com/felixge/fgprof"

	"github.com/peterbourgon/trc/trchttp"
	"github.com/peterbourgon/trc/trcstore"
)

func main() {
	// Each port is a distinct instance.
	ports := []string{"8081", "8082", "8083"}

	// Create a trace collector for each instance.
	collectors := make([]*trcstore.Collector, len(ports))
	for i := range collectors {
		collectors[i] = trcstore.NewCollector(trcstore.CollectorConfig{
			Source: ports[i],
		})
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

	// We can also create a "global" traces handler, which serves aggregate
	// results from all of the individual trace handlers for each instance.
	var trcGlobal http.Handler
	{
		// MultiSearcher allows multiple searchers to be treated as one. In this
		// case, the searchers are the collectors for each instance.
		var ms trcstore.MultiSearcher
		for i := range ports {
			// Each instance is modeled with an HTTP client querying the
			// corresponding trace HTTP handler. This is usually how it would
			// work, as different instances are usually on different hosts.
			ms = append(ms, trchttp.NewClient(http.DefaultClient, fmt.Sprintf("http://localhost:%s/trc", ports[i])))
		}

		// Let's also trace requests to this global handler in a distinct trace
		// collector, and include that collector in the multi-searcher.
		globalCollector := trcstore.NewCollector(trcstore.CollectorConfig{
			Source: "global",
		})
		ms = append(ms, globalCollector)

		trcGlobal = trchttp.NewServer(ms)
		trcGlobal = trchttp.Middleware(globalCollector.NewTrace, func(r *http.Request) string { return "traces" })(trcGlobal)
	}

	// Now we run HTTP servers for each instance.
	httpServers := make([]*http.Server, len(ports))
	for i := range httpServers {
		addr := "localhost:" + ports[i]
		mux := http.NewServeMux()
		mux.Handle("/api", http.StripPrefix("/api", apiHandlers[i])) // technically unnecessary as it's not used by the loader
		mux.Handle("/trc", http.StripPrefix("/trc", trcHandlers[i]))
		s := &http.Server{Addr: addr, Handler: mux}
		go func() { log.Fatal(s.ListenAndServe()) }()
		log.Printf("http://localhost:%s/trc", ports[i])
	}

	// And an extra HTTP server for the global trace handler. We'll use this
	// server for additional stuff like profiling endpoints.
	go func() {
		http.Handle("/trc", trcGlobal)
		http.Handle("/debug/fgprof", fgprof.Handler())
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
	}
}
