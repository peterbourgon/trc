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

	"github.com/felixge/fgprof"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcstream"
	"github.com/peterbourgon/trc/trcweb"
)

func main() {
	log.SetFlags(0)

	fs := ff.NewFlagSet("trc-complex")
	var (
		publish = fs.StringEnum('p', "publish", "what to publish: nothing, traces, events", "nothing", "traces", "events")
		workers = fs.Int('w', "workers", 1, "load generation workers")
	)
	if err := ff.Parse(fs, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", ffhelp.Flags(fs))
		log.Fatal(err)
	}

	log.Printf("publish %s", *publish)
	log.Printf("workers %d", *workers)

	// Open stack trace links in VS Code.
	trcweb.SetSourceLinkFunc(trcweb.SourceLinkVSCode)

	// Each port is a distinct instance.
	ports := []string{"8081", "8082", "8083"}

	// TODO
	var instances []*instance
	for _, port := range ports {
		instances = append(instances, newInstance(port, *publish))
	}

	// Generate random load for each instance.
	for i := 0; i < *workers; i++ {
		go load(context.Background(), instances...)
	}

	// TODO
	global := newGlobal(ports, *publish)

	// Run an HTTP server for each instance.
	httpServers := make([]*http.Server, len(ports))
	for i := range httpServers {
		addr := "localhost:" + ports[i]
		mux := http.NewServeMux()
		mux.Handle("/traces", http.StripPrefix("/traces", instances[i].tracesHandler))
		s := &http.Server{Addr: addr, Handler: mux}
		go func() { log.Fatal(s.ListenAndServe()) }()
		log.Printf("http://localhost:%s/traces", ports[i])
	}

	// Run an HTTP server for the global traces handler. We'll use this server
	// for additional stuff like profiling endpoints.
	go func() {
		http.Handle("/traces/", http.StripPrefix("/traces", global.tracesHandler))
		http.Handle("/debug/fgprof", fgprof.Handler())
		log.Printf("http://localhost:8080/traces")
		log.Fatal(http.ListenAndServe("localhost:8080", nil))
	}()

	select {}
}

type instance struct {
	broker        *trcstream.Broker
	collector     *trc.Collector
	apiHandler    http.Handler
	tracesHandler http.Handler
}

func newInstance(port string, publish string) *instance {
	broker := trcstream.NewBroker()

	var decorators []trc.DecoratorFunc
	switch publish {
	case "traces":
		decorators = append(decorators, broker.PublishTracesDecorator())
	case "events":
		decorators = append(decorators, broker.PublishEventsDecorator())
	}

	collector := trc.NewCollector(trc.CollectorConfig{
		Source:     port,
		Decorators: decorators,
	})

	var apiHandler http.Handler
	apiHandler = NewKV(NewStore())
	apiHandler = trcweb.Middleware(collector.NewTrace, apiCategory)(apiHandler)

	var streamHandler http.Handler
	streamHandler = trcweb.NewStreamServer(broker)
	streamHandler = trcweb.Middleware(collector.NewTrace, func(r *http.Request) string { return "stream" })(streamHandler)

	var searchHandler http.Handler
	searchHandler = trcweb.NewSearchServer(collector)
	searchHandler = trcweb.Middleware(collector.NewTrace, func(r *http.Request) string { return "traces" })(searchHandler)

	tracesHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case trcweb.RequestExplicitlyAccepts(r, "text/event-stream"):
			streamHandler.ServeHTTP(w, r)
		default:
			searchHandler.ServeHTTP(w, r)
		}
	})

	return &instance{
		broker:        broker,
		collector:     collector,
		apiHandler:    apiHandler,
		tracesHandler: tracesHandler,
	}
}

type global struct {
	broker        *trcstream.Broker
	collector     *trc.Collector
	tracesHandler http.Handler
}

func newGlobal(ports []string, publish string) *global {
	broker := trcstream.NewBroker()

	var decorators []trc.DecoratorFunc
	switch publish {
	case "traces":
		decorators = append(decorators, broker.PublishTracesDecorator())
	case "events":
		decorators = append(decorators, broker.PublishEventsDecorator())
	}

	collector := trc.NewCollector(trc.CollectorConfig{
		Source:     "global",
		Decorators: decorators,
	})

	var searcher trc.MultiSearcher
	for _, port := range ports {
		searcher = append(searcher, trcweb.NewSearchClient(http.DefaultClient, "localhost:"+port+"/traces"))
	}
	searcher = append(searcher, collector)

	var streamHandler http.Handler
	streamHandler = trcweb.NewStreamServer(broker)
	streamHandler = trcweb.Middleware(collector.NewTrace, func(r *http.Request) string { return "stream" })(streamHandler)

	var searchHandler http.Handler
	searchHandler = trcweb.NewSearchServer(searcher)
	searchHandler = trcweb.Middleware(collector.NewTrace, func(r *http.Request) string { return "traces" })(searchHandler)

	tracesHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case trcweb.RequestExplicitlyAccepts(r, "text/event-stream"):
			streamHandler.ServeHTTP(w, r)
		default:
			searchHandler.ServeHTTP(w, r)
		}
	})

	return &global{
		broker:        broker,
		collector:     collector,
		tracesHandler: tracesHandler,
	}
}

func load(ctx context.Context, instances ...*instance) {
	for i := 0; ctx.Err() == nil; i = (i + 1) % len(instances) {
		f := rand.Float64()
		switch {
		case f < 0.6:
			key := getWord()
			url := fmt.Sprintf("http://irrelevant/%s", key)
			req, _ := http.NewRequest("GET", url, nil)
			rec := httptest.NewRecorder()
			instances[i].apiHandler.ServeHTTP(rec, req)

		case f < 0.9:
			key := getWord()
			val := getWord()
			url := fmt.Sprintf("http://irrelevant/%s", key)
			req, _ := http.NewRequest("PUT", url, strings.NewReader(val))
			rec := httptest.NewRecorder()
			instances[i].apiHandler.ServeHTTP(rec, req)

		default:
			key := getWord()
			url := fmt.Sprintf("http://irrelevant/%s", key)
			req, _ := http.NewRequest("DELETE", url, nil)
			rec := httptest.NewRecorder()
			instances[i].apiHandler.ServeHTTP(rec, req)
		}
	}
}
