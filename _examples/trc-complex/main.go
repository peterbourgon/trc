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
	"github.com/peterbourgon/trc/trcstream"
	"github.com/peterbourgon/trc/trcweb"
)

func main() {
	// Open stack trace links in VS Code.
	trcweb.FileLineURL = trcweb.FileLineURLVSCode

	// Each port is a distinct instance.
	ports := []string{"8081", "8082", "8083"}

	// Create a trace collector for each instance.
	instanceCollectors := make([]*trc.Collector, len(ports))
	instanceBrokers := make([]*trcstream.Broker, len(ports))
	for i := range instanceCollectors {
		instanceBrokers[i] = trcstream.NewBroker()
		instanceCollectors[i] = trc.NewCollector(ports[i], trc.New, trc.PublishDecorator(instanceBrokers[i]))
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
	tracesHandlers := make([]http.Handler, len(instanceCollectors))
	for i := range tracesHandlers {
		tracesHandlers[i] = trcweb.NewSearchServer(instanceCollectors[i])
		tracesHandlers[i] = trcweb.Middleware(instanceCollectors[i].NewTrace, func(r *http.Request) string { return "traces" })(tracesHandlers[i])
	}

	// TODO
	streamHandlers := make([]http.Handler, len(instanceBrokers))
	for i := range streamHandlers {
		streamHandlers[i] = trcweb.NewStreamServer(instanceBrokers[i])
		streamHandlers[i] = trcweb.Middleware(instanceCollectors[i].NewTrace, func(r *http.Request) string { return "stream" })(streamHandlers[i])
	}

	// TODO
	var globalBroker *trcstream.Broker
	var globalCollector *trc.Collector
	{
		globalBroker = trcstream.NewBroker()
		globalCollector = trc.NewCollector("global", trc.New, trc.PublishDecorator(globalBroker))
	}

	// We can also create a "global" traces handler, which serves aggregate
	// results from all of the individual trace handlers for each instance.
	var globalHandler http.Handler
	{
		// MultiSearcher allows multiple sources to be treated as one.
		var ms trc.MultiSearcher
		for i := range ports {
			// Each instance is modeled with an HTTP client querying the
			// corresponding trace HTTP handler. This is usually how it would
			// work, as different instances are usually on different hosts.
			ms = append(ms, trcweb.NewSearchClient(http.DefaultClient, fmt.Sprintf("http://localhost:%s/traces", ports[i])))
		}

		// Let's also trace requests to this global handler in a distinct trace
		// collector, and include that collector in the multi-searcher.
		ms = append(ms, globalCollector)

		globalHandler = trcweb.NewSearchServer(ms)
		globalHandler = trcweb.Middleware(globalCollector.NewTrace, func(r *http.Request) string { return "traces" })(globalHandler)
	}

	var streamHandler http.Handler
	{
		streamHandler = trcweb.NewStreamServer(globalBroker)
		streamHandler = trcweb.Middleware(globalCollector.NewTrace, func(r *http.Request) string { return "stream" })(streamHandler)
	}

	// Now we run HTTP servers for each instance.
	httpServers := make([]*http.Server, len(ports))
	for i := range httpServers {
		addr := "localhost:" + ports[i]
		mux := http.NewServeMux()
		mux.Handle("/api", http.StripPrefix("/api", apiHandlers[i])) // technically unnecessary as the loader calls the handler directly
		mux.Handle("/traces", http.StripPrefix("/traces", tracesHandlers[i]))
		mux.Handle("/stream", http.StripPrefix("/stream", streamHandlers[i]))
		s := &http.Server{Addr: addr, Handler: mux}
		go func() { log.Fatal(s.ListenAndServe()) }()
		log.Printf("http://localhost:%s/traces", ports[i])
	}

	// And an extra HTTP server for the global trace handler. We'll use this
	// server for additional stuff like profiling endpoints.
	go func() {
		http.Handle("/traces", globalHandler)
		http.Handle("/stream", streamHandler)
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
