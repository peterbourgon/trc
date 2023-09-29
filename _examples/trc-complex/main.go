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
	"time"

	"github.com/felixge/fgprof"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
	"github.com/peterbourgon/trc/trcstream"
)

func main() {
	log.SetFlags(0)

	fs := ff.NewFlagSet("trc-complex")
	var (
		publish = fs.StringEnum('p', "publish", "what to publish: nothing, traces, events", "nothing", "traces", "events")
		workers = fs.Int('w', "workers", 1, "loadgen workers")
		delay   = fs.Duration('d', "delay", 0, "delay between loadgen requests")
	)
	if err := ff.Parse(fs, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", ffhelp.Flags(fs))
		log.Fatal(err)
	}

	log.Printf("publish %s", *publish)
	log.Printf("workers %d", *workers)
	log.Printf("delay %s", *delay)

	// Open stack trace links in VS Code.
	trchttp.SetSourceLinkFunc(trchttp.SourceLinkVSCode)

	// Each port is a distinct instance.
	ports := []string{"8081", "8082", "8083"}

	// Construct the instances.
	var instances []*instance
	for _, port := range ports {
		instances = append(instances, newInstance(port, *publish))
	}

	// Generate random load for each instance.
	for i := 0; i < *workers; i++ {
		go load(context.Background(), *delay, instances...)
	}

	// Construct a "global" instance, abstracting over the concrete instances.
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
	default:
		// no publishing
	}

	collector := trc.NewCollector(trc.CollectorConfig{
		Source:     port,
		Decorators: decorators,
	})

	var apiHandler http.Handler
	apiHandler = NewKV(NewStore())
	apiHandler = trchttp.Middleware(collector.NewTrace, apiCategory)(apiHandler)

	var streamHandler http.Handler
	streamHandler = trchttp.NewStreamServer(broker)
	streamHandler = trchttp.Middleware(collector.NewTrace, func(r *http.Request) string { return "stream" })(streamHandler)

	var searchHandler http.Handler
	searchHandler = trchttp.NewSearchServer(collector)
	searchHandler = trchttp.Middleware(collector.NewTrace, func(r *http.Request) string { return "traces" })(searchHandler)

	tracesHandler := trchttp.NewRuleRouter(searchHandler)
	tracesHandler.Add(func(r *http.Request) bool { return trchttp.RequestExplicitlyAccepts(r, "text/event-stream") }, streamHandler)

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
	default:
		// no publishing
	}

	collector := trc.NewCollector(trc.CollectorConfig{
		Source:     "global",
		Decorators: decorators,
	})

	var searcher trc.MultiSearcher
	for _, port := range ports {
		searcher = append(searcher, trchttp.NewSearchClient(http.DefaultClient, "localhost:"+port+"/traces"))
	}
	searcher = append(searcher, collector)

	var streamHandler http.Handler
	streamHandler = trchttp.NewStreamServer(broker)
	streamHandler = trchttp.Middleware(collector.NewTrace, func(r *http.Request) string { return "stream" })(streamHandler)

	var searchHandler http.Handler
	searchHandler = trchttp.NewSearchServer(searcher)
	searchHandler = trchttp.Middleware(collector.NewTrace, func(r *http.Request) string { return "traces" })(searchHandler)

	tracesHandler := trchttp.NewRuleRouter(searchHandler)
	tracesHandler.Add(func(r *http.Request) bool { return trchttp.RequestExplicitlyAccepts(r, "text/event-stream") }, streamHandler)

	return &global{
		broker:        broker,
		collector:     collector,
		tracesHandler: tracesHandler,
	}
}

func load(ctx context.Context, delay time.Duration, instances ...*instance) {
	rec := httptest.NewRecorder()
	for ctx.Err() == nil {
		f := rand.Float64()
		switch {
		case f < 0.6:
			key := getWord()
			req := httptest.NewRequest("GET", "http://irrelevant/"+key, nil)
			instances[0].apiHandler.ServeHTTP(rec, req)

		case f < 0.9:
			key := getWord()
			val := getWord()
			req := httptest.NewRequest("PUT", "http://irrelevant/"+key, strings.NewReader(val))
			instances[0].apiHandler.ServeHTTP(rec, req)

		default:
			key := getWord()
			req := httptest.NewRequest("DELETE", "http://irrelevant/"+key, nil)
			instances[0].apiHandler.ServeHTTP(rec, req)
		}
		instances = append(instances[1:], instances[0])
		time.Sleep(delay)
	}
}
