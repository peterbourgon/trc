package trcweb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/peterbourgon/trc/trcsrc"
	"github.com/peterbourgon/trc/trcweb"
)

func TestE2E(t *testing.T) {
	ctx := context.Background()
	source := trcsrc.NewDefaultCollector()
	traceServer := trcweb.NewServer(source)
	httpServer := httptest.NewServer(traceServer)
	defer httpServer.Close()
	traceClient := trcweb.NewClient(http.DefaultClient, httpServer.URL)

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
		_, tr := source.NewTrace(ctx, tuple.category)
		tr.Tracef(tuple.message)
		if tuple.isError {
			tr.Errorf("error")
		}
		tr.Finish()
	}

	testSelect := func(t *testing.T, req *trcsrc.SelectRequest) {
		t.Helper()

		res1, err1 := source.Select(ctx, req)
		if err1 != nil {
			t.Fatal(err1)
		}

		t.Logf("direct: total %d, matched %d, selected %d, err %v", res1.TotalCount, res1.MatchCount, len(res1.Traces), err1)

		res2, err2 := traceClient.Select(ctx, req)
		if err2 != nil {
			t.Fatal(err2)
		}

		t.Logf("client: total %d, matched %d, selected %d, err %v", res2.TotalCount, res2.MatchCount, len(res2.Traces), err2)

		opts := []cmp.Option{
			cmpopts.IgnoreFields(trcsrc.SelectResponse{}, "Duration", "Sources"),
			cmpopts.IgnoreFields(trcsrc.SelectedTrace{}, "Source"),
			cmpopts.IgnoreUnexported(trcsrc.CategoryStats{}),
			cmpopts.IgnoreUnexported(trcsrc.Filter{}),
		}
		if !cmp.Equal(res1, res2, opts...) {
			t.Fatal(cmp.Diff(res1, res2, opts...))
		}
	}

	t.Run("default", func(t *testing.T) { testSelect(t, &trcsrc.SelectRequest{}) })
	t.Run("Limit=1", func(t *testing.T) { testSelect(t, &trcsrc.SelectRequest{Limit: 1}) })
	t.Run("Query=beta", func(t *testing.T) { testSelect(t, &trcsrc.SelectRequest{Filter: trcsrc.Filter{Query: "beta"}}) })
	t.Run("IsErrored=true", func(t *testing.T) { testSelect(t, &trcsrc.SelectRequest{Filter: trcsrc.Filter{IsErrored: true}}) })
	t.Run("Query=doesnotexist", func(t *testing.T) { testSelect(t, &trcsrc.SelectRequest{Filter: trcsrc.Filter{Query: "doesnotexist"}}) })
	t.Run("Query=X1 Limit=2", func(t *testing.T) { testSelect(t, &trcsrc.SelectRequest{Filter: trcsrc.Filter{Query: "X1"}, Limit: 2}) })
	t.Run("Query=(B1|Z1) Limit=2", func(t *testing.T) {
		testSelect(t, &trcsrc.SelectRequest{Filter: trcsrc.Filter{Query: "(B1|Z1)"}, Limit: 2})
	})
}
