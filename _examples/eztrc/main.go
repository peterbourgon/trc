package main

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	eztrc "github.com/peterbourgon/trc/eztrc2"
	"github.com/peterbourgon/trc/trchttp"
)

func main() {
	store := NewStore()
	api := NewAPI(store)
	instrumentedAPI := trchttp.Middleware2(eztrc.Collector(), getAPIMethod)(api)
	server := httptest.NewServer(instrumentedAPI)
	defer server.Close()

	reqc := make(chan *http.Request)

	set := func() *http.Request {
		key := getWord()
		val := getWord()
		url := fmt.Sprintf("%s/%s", server.URL, key)
		req, _ := http.NewRequest("PUT", url, strings.NewReader(val))
		return req
	}

	get := func() *http.Request {
		key := getWord()
		url := fmt.Sprintf("%s/%s", server.URL, key)
		req, _ := http.NewRequest("GET", url, nil)
		return req
	}

	del := func() *http.Request {
		key := getWord()
		url := fmt.Sprintf("%s/%s", server.URL, key)
		req, _ := http.NewRequest("DELETE", url, nil)
		return req
	}

	var wg sync.WaitGroup

	nworkers := 4
	for i := 1; i <= nworkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c := fmt.Sprintf("worker %d/%d", id, nworkers)
			for req := range reqc {
				begin := time.Now()
				resp, err := http.DefaultClient.Do(req)
				took := time.Since(begin)
				eztrc.Logf(c, "%s %s in %s, err %v", req.Method, req.URL.Path, took, err)
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				time.Sleep(took * time.Duration(id))
			}
		}(i)
	}

	{
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				r := rand.Float64()
				d := time.Duration(r*100) * time.Millisecond
				eztrc.Logf("generator", "r=%.3f -> d=%s", r, d)
				switch {
				case r <= 0.7:
					reqc <- set()
				case r <= 0.9:
					reqc <- get()
				case r <= 1.0:
					reqc <- del()
				}
			}
		}()
	}

	{
		wg.Add(1)
		go func() {
			defer wg.Done()
			instrumentedTraces := trchttp.Middleware2(eztrc.Collector(), getMethodPath)(eztrc.TraceHandler)
			http.Handle("/traces", instrumentedTraces)
			log.Printf("http://localhost:8080/traces")
			http.ListenAndServe(":8080", nil)
		}()
	}

	wg.Wait()
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
