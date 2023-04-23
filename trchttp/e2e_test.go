package trchttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trchttp"
)

func TestE2E(t *testing.T) {
	ctx := context.Background()
	collector := trc.NewCollector()
	traceServer := trchttp.NewServer(collector)
	httpServer := httptest.NewServer(traceServer)
	defer httpServer.Close()
	traceClient := trchttp.NewClient(http.DefaultClient, httpServer.URL)

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
			tr.Errorf("error")
		}
		tr.Finish()
	}

	testSearch := func(t *testing.T, req *trc.SearchRequest) {
		t.Helper()
		res1, err1 := collector.Search(ctx, req)
		if err1 != nil {
			t.Fatal(err1)
		}
		t.Logf("direct: total %d, matched %d, selected %d, err %v", res1.Total, res1.Matched, len(res1.Selected), err1)
		res2, err2 := traceClient.Search(ctx, req)
		if err2 != nil {
			t.Fatal(err2)
		}
		t.Logf("client: total %d, matched %d, selected %d, err %v", res2.Total, res2.Matched, len(res2.Selected), err2)
		opts := []cmp.Option{
			cmpopts.IgnoreFields(trc.SearchResponse{}, "Duration"),
			cmpopts.IgnoreFields(trc.SelectedTrace{}, "Via"),
		}
		if !cmp.Equal(res1, res2, opts...) {
			t.Fatal(cmp.Diff(res1, res2, opts...))
		}
	}

	t.Run("default", func(t *testing.T) { testSearch(t, &trc.SearchRequest{}) })
	t.Run("Limit=1", func(t *testing.T) { testSearch(t, &trc.SearchRequest{Limit: 1}) })
	t.Run("Query=beta", func(t *testing.T) { testSearch(t, &trc.SearchRequest{Query: "beta"}) })
	t.Run("IsFailed=true", func(t *testing.T) { testSearch(t, &trc.SearchRequest{IsFailed: true}) })
	t.Run("Query=doesnotexist", func(t *testing.T) { testSearch(t, &trc.SearchRequest{Query: "doesnotexist"}) })
	t.Run("Query=X1 Limit=2", func(t *testing.T) { testSearch(t, &trc.SearchRequest{Query: "X1", Limit: 2}) })
	t.Run("Query=(B1|Z1) Limit=2", func(t *testing.T) { testSearch(t, &trc.SearchRequest{Query: "(B1|Z1)", Limit: 2}) })
}
