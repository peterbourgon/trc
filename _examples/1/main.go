package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/peterbourgon/trc/trchttp"
	trctrace "github.com/peterbourgon/trc/trctrace2"
	"github.com/peterbourgon/trc/trctrace2/trctracehttp"
)

func main() {
	origins := []string{"arena", "bravo", "cable"}

	collectors := make([]*trctrace.Collector, len(origins))
	for i := range collectors {
		collectors[i] = trctrace.NewCollector(1000)
	}

	kvs := make([]*KV, len(origins))
	for i := range kvs {
		kvs[i] = NewKV(NewStore())
	}

	apiHandlers := make([]http.Handler, len(origins))
	for i := range apiHandlers {
		apiHandlers[i] = kvs[i]
		apiHandlers[i] = trchttp.Middleware(collectors[i].NewTrace, func(r *http.Request) string { return r.Method })(apiHandlers[i])
	}

	apiWorkers := sync.WaitGroup{}
	for _, h := range apiHandlers {
		apiWorkers.Add(1)
		go func(h http.Handler) { defer apiWorkers.Done(); load(context.Background(), h) }(h)
	}

	trcHandlers := make([]http.Handler, len(origins))
	for i := range trcHandlers {
		trcHandlers[i] = &trctracehttp.Server{Origin: origins[i], Local: collectors[i]}
	}

	testServers := make([]*httptest.Server, len(origins))
	for i := range testServers {
		mux := http.NewServeMux()
		mux.Handle("/api", http.StripPrefix("/api", apiHandlers[i]))
		mux.Handle("/trc", http.StripPrefix("/trc", trcHandlers[i]))
		testServers[i] = httptest.NewServer(mux)
		defer testServers[i].Close()
		log.Printf("%[2]s/api %[2]s/trc", origins[i], testServers[i].URL)
	}

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
