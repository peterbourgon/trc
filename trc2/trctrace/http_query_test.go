package trctrace_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/peterbourgon/trc/trc2/trctrace"
)

func TestHTTPQuery(t *testing.T) {
	ctx := context.Background()

	collector := trctrace.NewCollector(100)
	handler := trctrace.NewHTTPQueryHandler(collector)
	server := httptest.NewServer(handler)
	t.Cleanup(func() { server.Close() })

	client := trctrace.NewHTTPQueryClient(http.DefaultClient, server.URL)

	category := "my category"
	message := fmt.Sprintf("hello %d", time.Now().UnixNano())

	_, tr0 := collector.NewTrace(ctx, category)
	tr0.Tracef(message)
	tr0.Finish()

	res, err := client.Query(ctx, &trctrace.QueryRequest{})
	if err != nil {
		t.Fatal(err)
	}

	tr1 := res.Selected[0]
	if want, have := category, tr1.Category(); want != have {
		t.Fatalf("category: want %q, have %q", want, have)
	}

	ev := tr1.Events()[0]
	if want, have := message, ev.What.String(); want != have {
		t.Fatalf("event: want %q, have %q", want, have)
	}
}
