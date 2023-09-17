package trcweb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcweb"
)

func TestE2E(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	collector := trc.NewDefaultCollector()
	collectorServer := trcweb.NewTraceServer(collector)
	httpServer := httptest.NewServer(collectorServer)
	defer httpServer.Close()
	traceClient := trcweb.NewSearchClient(http.DefaultClient, httpServer.URL)

	for _, tuple := range []struct {
		category string
		message  string
		isError  bool
	}{
		{"foo", "alpha   F 1", false},
		{"foo", "beta    F 2", false},
		{"foo", "delta   F 3", false},
		{"bar", "alpha   B 1", false},
		{"bar", "beta    B 2", false},
		{"bar", "epsilon B 3", false},
		{"baz", "alpha   Z 1", true},
	} {
		_, tr := collector.NewTrace(ctx, tuple.category)
		tr.Tracef(tuple.message)
		if tuple.isError {
			tr.Errorf("error")
		}
		tr.Finish()
		// if runtime.GOOS == "windows" {
		// time.Sleep(time.Millisecond)
		// }
	}

	testSelect := func(t *testing.T, req *trc.SearchRequest) {
		t.Helper()

		res1, err1 := collector.Search(ctx, req)
		if err1 != nil {
			t.Fatal(err1)
		}

		t.Logf("direct: total %d, matched %d, selected %d, err %v", res1.TotalCount, res1.MatchCount, len(res1.Traces), err1)

		res2, err2 := traceClient.Search(ctx, req)
		if err2 != nil {
			t.Fatal(err2)
		}

		t.Logf("client: total %d, matched %d, selected %d, err %v", res2.TotalCount, res2.MatchCount, len(res2.Traces), err2)

		opts := []cmp.Option{
			cmpopts.IgnoreFields(trc.SearchResponse{}, "Duration", "Sources"),
			cmpopts.IgnoreFields(trc.StaticTrace{}, "TraceSource"),
			cmpopts.IgnoreFields(trc.Event{}, "Stack"),
			cmpopts.IgnoreUnexported(trc.CategoryStats{}),
			cmpopts.IgnoreUnexported(trc.Filter{}),
		}
		if !cmp.Equal(res1, res2, opts...) {
			t.Fatal(cmp.Diff(res1, res2, opts...))
		}
	}

	t.Run("default", func(t *testing.T) { testSelect(t, &trc.SearchRequest{}) })
	t.Run("Limit=1", func(t *testing.T) { testSelect(t, &trc.SearchRequest{Limit: 1}) })
	t.Run("Query=beta", func(t *testing.T) { testSelect(t, &trc.SearchRequest{Filter: trc.Filter{Query: "beta"}}) })
	t.Run("IsErrored=true", func(t *testing.T) { testSelect(t, &trc.SearchRequest{Filter: trc.Filter{IsErrored: true}}) })
	t.Run("Query=doesnotexist", func(t *testing.T) { testSelect(t, &trc.SearchRequest{Filter: trc.Filter{Query: "doesnotexist"}}) })
	t.Run("Query=1 Limit=2", func(t *testing.T) { testSelect(t, &trc.SearchRequest{Filter: trc.Filter{Query: "1"}, Limit: 2}) })
	t.Run("(B|Z)", func(t *testing.T) { testSelect(t, &trc.SearchRequest{Filter: trc.Filter{Query: "(B|Z)"}}) })
}
