package trctracehttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	trctrace "github.com/peterbourgon/trc/trctrace2"
	"github.com/peterbourgon/trc/trctrace2/trctracehttp"
)

func TestE2E(t *testing.T) {
	ctx := context.Background()
	collector := trctrace.NewCollector(100)
	traceServer := trctracehttp.NewServer("default origin", collector, collector)
	httpServer := httptest.NewServer(traceServer)
	defer httpServer.Close()
	traceClient := trctracehttp.NewClient(http.DefaultClient, httpServer.URL)

	for _, tuple := range []struct {
		category string
		message  string
		isError  bool
	}{
		{"foo", "alpha   F1 X1", false},
		{"foo", "beta    F1 X2", false},
		{"foo", "delta   F1 X3", false},
		{"bar", "alpha   B1 X1", false},
		{"bar", "beta    B1 X2", false},
		{"bar", "epsilon B1 X3", false},
		{"baz", "alpha   Z1 X1", true},
	} {
		_, tr := collector.NewTrace(ctx, tuple.category)
		tr.Tracef(tuple.message)
		if tuple.isError {
			tr.Errorf("error!")
		}
		tr.Finish()
	}

	testSearch := func(t *testing.T, req *trctrace.SearchRequest) {
		t.Helper()
		res1, err1 := collector.Search(ctx, req)
		t.Logf("direct: total %d, matched %d, selected %d, err %v", res1.Total, res1.Matched, len(res1.Selected), err1)
		res2, err2 := traceClient.Search(ctx, req)
		t.Logf("client: total %d, matched %d, selected %d, err %v", res2.Total, res2.Matched, len(res2.Selected), err2)
		opts := cmpopts.IgnoreFields(trctrace.SearchResponse{}, "Duration", "Origins", "Request.Regexp")
		if !cmp.Equal(res1, res2, opts) {
			t.Fatal(cmp.Diff(res1, res2, opts))
		}
	}

	t.Run("default", func(t *testing.T) { testSearch(t, &trctrace.SearchRequest{}) })
	t.Run("Limit=1", func(t *testing.T) { testSearch(t, &trctrace.SearchRequest{Limit: 1}) })
	t.Run("Query=beta", func(t *testing.T) { testSearch(t, &trctrace.SearchRequest{Query: "beta"}) })
	t.Run("IsFailed=true", func(t *testing.T) { testSearch(t, &trctrace.SearchRequest{IsFailed: true}) })
	t.Run("Query=doesnotexist", func(t *testing.T) { testSearch(t, &trctrace.SearchRequest{Query: "doesnotexist"}) })
	t.Run("Query=X1 Limit=2", func(t *testing.T) { testSearch(t, &trctrace.SearchRequest{Query: "X1", Limit: 2}) })
	t.Run("Query=(B1|Z1) Limit=2", func(t *testing.T) { testSearch(t, &trctrace.SearchRequest{Query: "(B1|Z1)", Limit: 2}) })
}
