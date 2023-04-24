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

	"github.com/peterbourgon/trc/eztrc"
)

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

func (a *KV) handleGet(w http.ResponseWriter, r *http.Request) {
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

func (a *KV) handleDel(w http.ResponseWriter, r *http.Request) {
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
	_, _, finish := eztrc.Region(ctx, "Store.Set", key)
	defer finish()
	s.mtx.Lock()
	defer s.mtx.Unlock()
	time.Sleep(getDelay(key, 250*time.Microsecond))
	s.set[key] = val
}

func (s *Store) Get(ctx context.Context, key string) (string, bool) {
	_, _, finish := eztrc.Region(ctx, "Store.Get", key)
	defer finish()
	s.mtx.Lock()
	defer s.mtx.Unlock()
	val, ok := s.set[key]
	time.Sleep(getDelay(key, 100*time.Microsecond))
	return val, ok
}

func (s *Store) Del(ctx context.Context, key string) bool {
	_, _, finish := eztrc.Region(ctx, "Store.Del", key)
	defer finish()
	s.mtx.Lock()
	defer s.mtx.Unlock()
	_, ok := s.set[key]
	delete(s.set, key)
	time.Sleep(getDelay(key, 10*time.Microsecond))
	return ok
}
