package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/eztrc"
)

func apiCategory(r *http.Request) string {
	switch r.Method {
	case "DELETE":
		return "KV Del"
	case "GET":
		return "KV Get"
	case "PUT":
		return "KV Set"
	default:
		return "KV " + r.Method
	}
}

type KV struct {
	s *Store
}

func NewKV(s *Store) *KV {
	return &KV{s: s}
}

func (a *KV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

func (a *KV) handleSet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tr := trc.Get(ctx)

	key := getKey(r.URL.Path)
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	tr.Tracef("key %q", key)

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

func (a *KV) handleGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tr := trc.Get(ctx)

	key := getKey(r.URL.Path)
	if key == "" {
		tr.Errorf("key not provided")
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	tr.Tracef("key %q", key)

	val, ok := a.s.Get(ctx, key)
	if !ok {
		tr.Errorf("key not found")
		http.Error(w, "not found", http.StatusNoContent)
		return
	}

	tr.Tracef("val %q", val)

	fmt.Fprintln(w, val)
}

func (a *KV) handleDel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tr := trc.Get(ctx)

	key := getKey(r.URL.Path)
	if key == "" {
		tr.Errorf("key not provided")
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	tr.Tracef("key %q", key)

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

func getKey(path string) string {
	return strings.TrimPrefix(path, "/")
}

var words = strings.Fields(`
	air      area       art      back      body        book     business   car
	case     change     child    city      community   company  country    day
	door     education  end      eye       face        fact     family     father
	force    friend     game     girl      government  group    guy        hand
	head     health     history  home      hour        house    idea       information
	issue    job        kid      kind      law         level    life       line
	lot      man        member   minute    moment      money    month      morning
	mother   name       night    number    office      others   parent     part
	party    people     person   place     point       power    president  problem
	program  question   reason   research  result      right    room       school
	service  side       state    story     student     study    system     teacher
	team     thing      time     war       water       way      week       woman
	word     work       world    year      yellow      yonder   zebra      zelda
`)

func getWord() string {
	return words[rand.Intn(len(words))]
}

func getDelay(word string, base time.Duration) time.Duration {
	return time.Duration(len(word)) * base
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
	s.mtx.Lock()
	defer s.mtx.Unlock()
	time.Sleep(getDelay(key, 4*time.Nanosecond)) // fake some processing time
	s.set[key] = val
}

func (s *Store) Get(ctx context.Context, key string) (string, bool) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	val, ok := s.set[key]
	time.Sleep(getDelay(key, 2*time.Nanosecond)) // fake some processing time
	return val, ok
}

func (s *Store) Del(ctx context.Context, key string) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	_, ok := s.set[key]
	delete(s.set, key)
	time.Sleep(getDelay(key, 1*time.Nanosecond)) // fake some processing time
	return ok
}
