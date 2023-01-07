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
	"time"

	"github.com/peterbourgon/trc"
	trctrace "github.com/peterbourgon/trc/trctrace2"
	"github.com/peterbourgon/trc/trctrace2/trctracehttp"
)

func main() {
	ports := []string{"8080", "8081", "8082"}

	collectors := make([]*trctrace.Collector, len(ports))
	for i := range collectors {
		src := trc.Source{Name: ports[i], URL: fmt.Sprintf("http://localhost:%s/trc", ports[i])}
		collectors[i] = trctrace.NewCollector(src, 1000)
	}

	kvs := make([]*KV, len(ports))
	for i := range kvs {
		kvs[i] = NewKV(NewStore())
	}

	apiHandlers := make([]http.Handler, len(ports))
	for i := range apiHandlers {
		apiHandlers[i] = kvs[i]
		apiHandlers[i] = trctracehttp.Middleware(collectors[i].NewTrace, func(r *http.Request) string { return r.Method })(apiHandlers[i])
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

	trcClients := make([]*trctracehttp.Client, len(ports))
	for i := range trcClients {
		trcClients[i] = trctracehttp.NewClient(http.DefaultClient, fmt.Sprintf("http://localhost:%s/trc", ports[i]))
	}
	//trcClients = append(trcClients, trctracehttp.NewClient(http.DefaultClient, "http://localhost:9999/trc"))

	globalSearcher := make(trctrace.MultiSearcher, len(trcClients))
	for i := range trcClients {
		globalSearcher[i] = trcClients[i]
	}

	globalTarget := &trctracehttp.Target{
		Name:     "global",
		Searcher: globalSearcher,
	}

	trcHandlers := make([]http.Handler, len(collectors))
	for i := range trcHandlers {
		localTarget := &trctracehttp.Target{Name: "local", Searcher: collectors[i]}
		trcHandlers[i] = trctracehttp.NewServer(trctracehttp.ServerConfig{
			Local:   localTarget,
			Other:   []*trctracehttp.Target{globalTarget},
			Default: globalTarget,
		})
		trcHandlers[i] = trctracehttp.Middleware(collectors[i].NewTrace, func(r *http.Request) string { return "traces" })(trcHandlers[i])
	}

	httpServers := make([]*http.Server, len(ports))
	for i := range httpServers {
		addr := "localhost:" + ports[i]
		mux := http.NewServeMux()
		mux.Handle("/api", http.StripPrefix("/api", apiHandlers[i]))
		mux.Handle("/trc", http.StripPrefix("/trc", trcHandlers[i]))
		s := &http.Server{Addr: addr, Handler: mux}
		log.Printf("using addr %s", addr)
		go func() { log.Fatal(s.ListenAndServe()) }()
		log.Printf("http://localhost:%[1]s/api http://localhost:%[1]s/trc", ports[i])
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
		time.Sleep(time.Millisecond)
	}
}
