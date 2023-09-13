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

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcweb"
)

func main() {
	// Open stack trace links in VS Code.
	trcweb.FileLineURL = trcweb.FileLineURLVSCode

	// Each port is a distinct instance.
	ports := []string{"8081", "8082", "8083"}

	// Create a trace collector for each instance.
	instanceCollectors := make([]*trc.Collector, len(ports))
	for i := range ports {
		instanceCollectors[i] = trc.NewCollector(trc.CollectorConfig{Source: ports[i]})
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
		apiHandlers[i] = trcweb.Middleware(instanceCollectors[i].NewTrace, apiCategory)(apiHandlers[i])
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
	instanceHandlers := make([]http.Handler, len(instanceCollectors))
	for i := range instanceHandlers {
		instanceHandlers[i] = trcweb.NewCollectorServer(trcweb.CollectorServerConfig{Collector: instanceCollectors[i]})
		instanceHandlers[i] = trcweb.Middleware(instanceCollectors[i].NewTrace, trcweb.TraceServerCategory)(instanceHandlers[i])
	}

	// TODO
	var globalCollector *trc.Collector
	{
		globalCollector = trc.NewCollector(trc.CollectorConfig{Source: "global"})
	}

	// We can also create a "global" traces handler, which serves aggregate
	// results from all of the individual trace handlers for each instance.
	var globalHandler http.Handler
	{
		// MultiSearcher allows multiple sources to be treated as one.
		var globalSearcher trc.MultiSearcher
		for i := range ports {
			// Each instance is modeled with an HTTP client querying the
			// corresponding trace HTTP handler. This is usually how it would
			// work, as different instances are usually on different hosts.
			globalSearcher = append(globalSearcher, trcweb.NewTraceClient(http.DefaultClient, fmt.Sprintf("localhost:%s/traces", ports[i])))
		}

		// Let's also trace requests to this global handler in a distinct trace
		// collector, and include that collector in the multi-searcher.
		globalSearcher = append(globalSearcher, globalCollector)

		globalHandler = trcweb.NewCollectorServer(trcweb.CollectorServerConfig{Collector: globalCollector, Searcher: globalSearcher})
		globalHandler = trcweb.Middleware(globalCollector.NewTrace, trcweb.TraceServerCategory)(globalHandler)
	}

	// Now we run HTTP servers for each instance.
	httpServers := make([]*http.Server, len(ports))
	for i := range httpServers {
		addr := "localhost:" + ports[i]
		mux := http.NewServeMux()
		mux.Handle("/traces", http.StripPrefix("/traces", instanceHandlers[i]))
		s := &http.Server{Addr: addr, Handler: mux}
		go func() { log.Fatal(s.ListenAndServe()) }()
		log.Printf("http://localhost:%s/traces", ports[i])
	}

	// And an extra HTTP server for the global trace handler. We'll use this
	// server for additional stuff like profiling endpoints.
	go func() {
		http.Handle("/traces/", http.StripPrefix("/traces", globalHandler))
		http.Handle("/debug/fgprof", fgprof.Handler())
		log.Printf("http://localhost:8080/traces")
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
