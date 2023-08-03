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

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/eztrc"
	"github.com/peterbourgon/trc/trcsrc"
	"github.com/peterbourgon/trc/trcstream"
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

	broker := trcstream.NewBroker()
	eztrc.Collector().SetDecorators(trcsrc.PublishDecorator(broker))

	go func() {
		ctx := context.Background()
		c := make(chan trc.Trace)
		f := trcsrc.Filter{}
		if err := broker.Subscribe(ctx, c, f); err != nil {
			log.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for x := range c {
			stats, _ := broker.Stats(ctx, c)
			log.Printf("got trace %s, sends=%d drops=%d", x.ID(), stats.Sends, stats.Drops)
			events := x.Events()
			if len(events) > 0 {
				last := events[len(events)-1]
				log.Printf(" last event: %s", last.What)
			}
			if time.Now().After(deadline) {
				break
			}
		}
		if err := broker.Unsubscribe(ctx, c); err != nil {
			log.Fatal(err)
		}
		log.Printf("unsubscribed")
	}()

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
