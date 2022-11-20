package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/peterbourgon/trc/trc2/eztrc"
	"github.com/peterbourgon/trc/trc2/trchttp"
	"github.com/peterbourgon/trc/trc2/trctrace"
)

func main() {
	// Define the API instances.
	ports := []int{8081, 8082, 8083}
	names := []string{"Larry", "Curly", "Moe"}
	originSlice := []trctrace.Origin{}
	originIndex := map[string]trctrace.Origin{}
	multiQueryer := trctrace.MultiQueryer{}

	// Walk the API instances.
	for i := range ports {
		port := ports[i]
		name := names[i]
		endpoint := fmt.Sprintf("http://localhost:%d/traces", port)
		queryer := trctrace.NewHTTPQueryClient(http.DefaultClient, name, endpoint)
		origin := trctrace.Origin{Name: name, Queryer: queryer}
		originSlice = append(originSlice, origin)
		originIndex[name] = origin
		multiQueryer = append(multiQueryer, queryer)
	}
	originSlice = append(originSlice, trctrace.Origin{Name: "all instances", Queryer: multiQueryer})

	// Make the API instances.
	instances := map[string]*apiInstance{}
	ctx, cancel := context.WithCancel(context.Background())
	taskGroup := &sync.WaitGroup{}
	for i := range ports {
		port := ports[i]
		name := names[i]
		hostport := fmt.Sprintf("localhost:%d", port)
		instance := newAPIInstance(originSlice...)

		// Spawn a goroutine that produces API requests to this instance.
		taskGroup.Add(1)
		go func() {
			defer taskGroup.Done()
			for ctx.Err() == nil {
				f := rand.Float64()
				switch {
				case f < 0.6:
					key := getWord()
					url := fmt.Sprintf("http://irrelevant/%s", key)
					req, _ := http.NewRequest("GET", url, nil)
					rec := httptest.NewRecorder()
					instance.apiHandler.ServeHTTP(rec, req)

				case f < 0.9:
					key := getWord()
					val := getWord()
					url := fmt.Sprintf("http://irrelevant/%s", key)
					req, _ := http.NewRequest("PUT", url, strings.NewReader(val))
					rec := httptest.NewRecorder()
					instance.apiHandler.ServeHTTP(rec, req)

				default:
					key := getWord()
					url := fmt.Sprintf("http://irrelevant/%s", key)
					req, _ := http.NewRequest("DELETE", url, nil)
					rec := httptest.NewRecorder()
					instance.apiHandler.ServeHTTP(rec, req)
				}
			}
		}()

		// Spawn a goroutine to serve this instance's traces endpoints.
		taskGroup.Add(1)
		go func() {
			defer taskGroup.Done()
			log.Printf("%s: http://%s/traces", name, hostport)
			server := &http.Server{Addr: hostport, Handler: instance}
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

	go func() {
		log.Printf("http://localhost:8080/debug/pprof")
		log.Fatal(http.ListenAndServe(":8080", nil))
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

// apiInstance serves GET/SET/DELETE /{key} + GET /traces.
type apiInstance struct {
	apiHandler    http.Handler
	tracesHandler http.Handler
}

func newAPIInstance(origins ...trctrace.Origin) *apiInstance {
	store := NewStore()
	api := NewAPI(store)

	localCollector := trctrace.NewCollector(1000)

	var apiHandler http.Handler
	apiHandler = api
	apiHandler = trchttp.Middleware(localCollector.NewTrace, getAPIMethod)(apiHandler)

	var tracesHandler http.Handler
	tracesHandler = trctrace.NewHTTPQueryHandlerDefault(localCollector, origins...)
	tracesHandler = GZipMiddleware(tracesHandler)
	tracesHandler = trchttp.Middleware(localCollector.NewTrace, getMethodPath)(tracesHandler)

	return &apiInstance{
		apiHandler:    apiHandler,
		tracesHandler: tracesHandler,
	}
}

func (i *apiInstance) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/traces"):
		i.tracesHandler.ServeHTTP(w, r)
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
	ctx, tr, finish := eztrc.Region(r.Context(), "handleSet")
	defer finish()

	key := getKey(r.URL.Path)
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

	tr.Tracef("val %q", val)

	a.s.Set(ctx, key, val)
}

func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	ctx, tr, finish := eztrc.Region(r.Context(), "handleGet")
	defer finish()

	key := getKey(r.URL.Path)
	if key == "" {
		tr.Errorf("key not provided")
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	val, ok := a.s.Get(ctx, key)
	if !ok {
		tr.Errorf("key not found")
		http.Error(w, "not found", http.StatusNoContent)
		return
	}

	tr.Tracef("val %q", val)

	fmt.Fprintln(w, val)
}

func (a *API) handleDel(w http.ResponseWriter, r *http.Request) {
	ctx, tr, finish := eztrc.Region(r.Context(), "handleDel")
	defer finish()

	key := getKey(r.URL.Path)
	if key == "" {
		tr.Errorf("key not provided")
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	ok := a.s.Del(ctx, key)

	if !ok {
		tr.Errorf("key not found")
		http.Error(w, "not found", http.StatusNoContent)
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

func (s *Store) Set(ctx context.Context, key, val string) {
	_, _, finish := eztrc.Region(ctx, "Set %s", key)
	defer finish()
	s.mtx.Lock()
	defer s.mtx.Unlock()
	time.Sleep(getDelay(key, 250*time.Microsecond))
	s.set[key] = val
}

func (s *Store) Get(ctx context.Context, key string) (string, bool) {
	_, _, finish := eztrc.Region(ctx, "Get %s", key)
	defer finish()
	s.mtx.Lock()
	defer s.mtx.Unlock()
	val, ok := s.set[key]
	time.Sleep(getDelay(key, 100*time.Microsecond))
	return val, ok
}

func (s *Store) Del(ctx context.Context, key string) bool {
	_, _, finish := eztrc.Region(ctx, "Del %s", key)
	defer finish()
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

var words = []string{
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

func getWord() string {
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
