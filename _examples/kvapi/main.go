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
	ports := []string{"8081", "8082", "8083"}

	collectors := make([]*trc.Collector, len(ports))
	for i := range collectors {
		collectors[i] = trc.NewDefaultCollector()
	}

	kvs := make([]*KV, len(ports))
	for i := range kvs {
		kvs[i] = NewKV(NewStore())
	}

	apiHandlers := make([]http.Handler, len(ports))
	for i := range apiHandlers {
		apiHandlers[i] = kvs[i]
		apiHandlers[i] = trchttp.Middleware(collectors[i].NewTrace, apiCategory)(apiHandlers[i])
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

	trcHandlers := make([]http.Handler, len(collectors))
	for i := range trcHandlers {
		categorize := func(r *http.Request) string { return "traces" }
		server := trchttp.NewServer(collectors[i])

		trcHandlers[i] = server
		trcHandlers[i] = trchttp.Middleware(collectors[i].NewTrace, categorize)(trcHandlers[i])
	}

	var trcGlobal http.Handler
	{
		var ms trc.MultiSearcher
		for i := range ports {
			ms = append(ms, trchttp.NewClient(http.DefaultClient, fmt.Sprintf("http://localhost:%s/trc", ports[i])))
		}
		trcGlobal = trchttp.NewServer(ms)
	}

	httpServers := make([]*http.Server, len(ports))
	for i := range httpServers {
		addr := "localhost:" + ports[i]
		mux := http.NewServeMux()
		mux.Handle("/api", http.StripPrefix("/api", apiHandlers[i]))
		mux.Handle("/trc", http.StripPrefix("/trc", trcHandlers[i]))
		mux.Handle("/all", http.StripPrefix("/all", trcGlobal))
		s := &http.Server{Addr: addr, Handler: mux}
		go func() { log.Fatal(s.ListenAndServe()) }()
		log.Printf("http://localhost:%s/trc", ports[i])
	}

	go func() {
		http.Handle("/global", trcGlobal)
		log.Printf("http://localhost:8080/global")
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
		time.Sleep(time.Millisecond)
	}
}
