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

	"github.com/NYTimes/gziphandler"
	"github.com/peterbourgon/trc/trccoll"
	"github.com/peterbourgon/trc/trchttp"
)

func main() {
	ports := []string{"8080", "8081", "8082"}

	collectors := make([]*trccoll.Collector, len(ports))
	for i := range collectors {
		collectors[i] = trccoll.NewCollector(1000)
	}

	kvs := make([]*KV, len(ports))
	for i := range kvs {
		kvs[i] = NewKV(NewStore())
	}

	apiHandlers := make([]http.Handler, len(ports))
	for i := range apiHandlers {
		apiHandlers[i] = kvs[i]
		apiHandlers[i] = trchttp.Middleware(collectors[i].NewTrace, func(r *http.Request) string { return r.Method })(apiHandlers[i])
	}

	apiWorkers := sync.WaitGroup{}
	for _, h := range apiHandlers {
		apiWorkers.Add(1)
		go func(h http.Handler) {
			defer apiWorkers.Done()
			load(context.Background(), h)
		}(h)
	}

	//
	//
	//

	trcClients := make([]*trchttp.Client, len(ports))
	for i := range trcClients {
		trcClients[i] = trchttp.NewClient(http.DefaultClient, fmt.Sprintf("http://localhost:%s/trc", ports[i]))
	}

	globalSearcher := make(trccoll.MultiSearcher, len(trcClients))
	for i := range trcClients {
		globalSearcher[i] = trcClients[i]
	}

	trcHandlers := make([]http.Handler, len(collectors))
	for i := range trcHandlers {
		categorize := func(r *http.Request) string { return "traces" }
		server := trchttp.NewServer(collectors[i])

		trcHandlers[i] = server
		trcHandlers[i] = gziphandler.GzipHandler(trcHandlers[i])
		trcHandlers[i] = trchttp.Middleware(collectors[i].NewTrace, categorize)(trcHandlers[i])
	}

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

	go func() {
		log.Printf("http://localhost:8079/debug/pprof")
		log.Fatal(http.ListenAndServe(":8079", nil))
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
		time.Sleep(time.Millisecond)
	}
}
