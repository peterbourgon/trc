package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/eztrc"
	"github.com/peterbourgon/trc/trchttp"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	taskGroup := &sync.WaitGroup{}

	// Make some API instances.
	instances := map[string]*apiInstance{}
	for i := 0; i < 3; i++ {
		// Each one sits on a port.
		hostport := fmt.Sprintf("localhost:%d", 8080+i)
		instance := newAPIInstance(hostport)

		// Load the API with requests, but directly, to the API handler.
		taskGroup.Add(1)
		go func() {
			defer taskGroup.Done()
			for ctx.Err() == nil {
				f := rand.Float64()
				switch {
				case f < 0.6:
					key := getWord()
					url := fmt.Sprintf("http://localhost/%s", key)
					req, _ := http.NewRequest("GET", url, nil)
					rec := httptest.NewRecorder()
					instance.apiHandler.ServeHTTP(rec, req)

				case f < 0.9:
					key := getWord()
					val := getWord()
					url := fmt.Sprintf("http://localhost/%s", key)
					req, _ := http.NewRequest("PUT", url, strings.NewReader(val))
					rec := httptest.NewRecorder()
					instance.apiHandler.ServeHTTP(rec, req)

				default:
					key := getWord()
					url := fmt.Sprintf("http://localhost/%s", key)
					req, _ := http.NewRequest("DELETE", url, nil)
					rec := httptest.NewRecorder()
					instance.apiHandler.ServeHTTP(rec, req)
				}
			}
		}()

		// Serve the traces endpoint over proper HTTP.
		taskGroup.Add(1)
		go func() {
			defer taskGroup.Done()
			mux := http.NewServeMux()
			mux.Handle("/traces", instance.trcHandler)
			log.Printf("http://%s/traces", hostport)
			server := &http.Server{Addr: hostport, Handler: mux}
			errc := make(chan error, 1)
			go func() { errc <- server.ListenAndServe() }()
			<-ctx.Done()
			log.Printf("%s shutting down", hostport)
			server.Close()
			log.Printf("%s waiting for done", hostport)
			<-errc
			log.Printf("%s done", hostport)
		}()

		instances[hostport] = instance
	}

	// An HTTP server for the meta-traces endpoint by itself.
	taskGroup.Add(1)
	go func() {
		defer taskGroup.Done()

		hostport := fmt.Sprintf("localhost:%d", 8080+len(instances))
		var uris []string
		for hostport := range instances {
			uris = append(uris, fmt.Sprintf("http://%s/traces", hostport))
		}

		distCollector := trc.NewDistributedTraceCollector(http.DefaultClient, uris...)
		distHandler := trchttp.TraceCollectorHandler(distCollector)
		metaCollector := trc.NewTraceCollector(100)
		distHandlerInst := trchttp.Middleware(metaCollector, getMethodPath)(distHandler)
		metaHandler := trchttp.TraceCollectorHandler(metaCollector)
		metaHandlerInst := trchttp.Middleware(metaCollector, getMethodPath)(metaHandler)

		mux := http.NewServeMux()
		mux.Handle("/dist", distHandlerInst)
		mux.Handle("/meta", metaHandlerInst)
		log.Printf("http://%s/dist -- proxy to other instances", hostport)
		log.Printf("http://%s/meta -- traces for this instance", hostport)

		server := &http.Server{Addr: hostport, Handler: mux}
		errc := make(chan error, 1)
		go func() { errc <- server.ListenAndServe() }()
		<-ctx.Done()
		log.Printf("%s shutting down", hostport)
		server.Close()
		log.Printf("%s waiting for done", hostport)
		<-errc
		log.Printf("%s done", hostport)
	}()

	log.Printf("running")

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt)
	log.Printf("got signal %s", <-sigc)
	cancel()

	log.Printf("waiting for shutdown")
	taskGroup.Wait()

	log.Printf("done")
}

//
//
//

type apiInstance struct {
	collector  *trc.TraceCollector
	trcHandler http.Handler
	apiHandler http.Handler
}

func newAPIInstance(id string) *apiInstance {
	var (
		collector = trc.NewTraceCollector(1000)
		store     = NewStore()
		api       = NewAPI(store)
	)
	return &apiInstance{
		collector:  collector,
		trcHandler: trchttp.Middleware(collector, getMethodPath)(trchttp.TraceCollectorHandler(collector)),
		apiHandler: trchttp.Middleware(collector, getAPIMethod)(api),
	}
}

func (i *apiInstance) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/traces"):
		i.trcHandler.ServeHTTP(w, r)
	default:
		i.apiHandler.ServeHTTP(w, r)
	}
}

//
//
//

type API struct {
	s *Store
}

func NewAPI(s *Store) *API {
	return &API{s: s}
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "GET":
		a.handleGet(w, r)
	case r.Method == "PUT":
		a.handleSet(w, r)
	case r.Method == "DELETE":
		a.handleDel(w, r)
	default:
		eztrc.Tracef(r.Context(), "method %s not allowed", r.Method)
		http.Error(w, "method must be GET, PUT, or DELETE", http.StatusMethodNotAllowed)
	}
}

func (a *API) handleSet(w http.ResponseWriter, r *http.Request) {
	var (
		ctx = r.Context()
		key = getKey(r.URL.Path)
	)

	eztrc.Tracef(ctx, "set %q", key)

	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	valbuf, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "couldn't read body", http.StatusBadRequest)
		return
	}

	val := strings.TrimSpace(string(valbuf))

	if val == "" {
		http.Error(w, "val required", http.StatusBadRequest)
		return
	}

	eztrc.Tracef(ctx, "val %q", val)

	a.s.Set(key, val)
}

func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	var (
		ctx = r.Context()
		key = getKey(r.URL.Path)
	)

	eztrc.Tracef(ctx, "get %q", key)

	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	val, ok := a.s.Get(key)
	if !ok {
		eztrc.Errorf(ctx, "key not found")
		http.Error(w, "no content", http.StatusNoContent)
		return
	}

	eztrc.Tracef(ctx, "val %q", val)

	fmt.Fprintln(w, val)
}

func (a *API) handleDel(w http.ResponseWriter, r *http.Request) {
	var (
		ctx = r.Context()
		key = getKey(r.URL.Path)
	)

	eztrc.Tracef(ctx, "del %q", key)

	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	ok := a.s.Del(key)

	if !ok {
		eztrc.Errorf(ctx, "key not found")
		http.Error(w, "no content", http.StatusNoContent)
		return
	}
}

//
//
//

type Store struct {
	mtx sync.Mutex
	set map[string]string
}

func NewStore() *Store {
	return &Store{
		set: map[string]string{},
	}
}

func (s *Store) Set(key, val string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	time.Sleep(getDelay(key, 1000*time.Microsecond))
	s.set[key] = val
}

func (s *Store) Get(key string) (string, bool) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	val, ok := s.set[key]
	time.Sleep(getDelay(key, 100*time.Microsecond))
	return val, ok
}

func (s *Store) Del(key string) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	_, ok := s.set[key]
	delete(s.set, key)
	time.Sleep(getDelay(key, 10*time.Microsecond))
	return ok
}

//
//
//

func getKey(path string) string {
	return strings.TrimPrefix(path, "/")
}

func getWord() string {
	words := []string{
		"air", "area", "art", "back", "body",
		"book", "business", "car", "case", "change",
		"child", "city", "community", "company", "country",
		"day", "door", "education", "end", "eye",
		"face", "fact", "family", "father", "force",
		"friend", "game", "girl", "government", "group",
		"guy", "hand", "head", "health", "history",
		"home", "hour", "house", "idea", "information",
		"issue", "job", "kid", "kind", "law",
		"level", "life", "line", "lot", "man",
		"member", "minute", "moment", "money", "month",
		"morning", "mother", "name", "night", "number",
		"office", "others", "parent", "part", "party",
		"people", "person", "place", "point", "power",
		"president", "problem", "program", "question", "reason",
		"research", "result", "right", "room", "school",
		"service", "side", "state", "story", "student",
		"study", "system", "teacher", "team", "thing",
		"time", "war", "water", "way", "week",
		"woman", "word", "work", "world", "year",
	}
	return words[rand.Intn(len(words))]
}

func getDelay(word string, base time.Duration) time.Duration {
	return time.Duration(len(word)) * base
}

func getAPIMethod(r *http.Request) string {
	switch r.Method {
	case "PUT":
		return "API Set"
	case "GET":
		return "API Get"
	case "DELETE":
		return "API Del"
	default:
		return "API invalid " + r.Method
	}
}

func getMethodPath(r *http.Request) string {
	return r.Method + " " + r.URL.Path
}
