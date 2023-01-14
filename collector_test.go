package trc_test

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/peterbourgon/trc"
)

func TestCollector(t *testing.T) {
	ctx := context.Background()
	seed := int64(123456)
	rng := rand.New(rand.NewSource(seed))
	words := []string{"foo", "bar", "baz", "quux"}

	for _, categoryCount := range []int{5, 50} {
		for _, maxPerCategory := range []int{1000, 5000} {
			for _, traceCount := range []int{1000, 10000} {
				t.Run(fmt.Sprintf("%d %d %d", categoryCount, maxPerCategory, traceCount), func(t *testing.T) {
					categories := make([]string, categoryCount)
					for i := range categories {
						categories[i] = fmt.Sprintf("cat%d", i)
					}

					collector := trc.NewCollector(maxPerCategory)
					for i := 0; i < traceCount; i++ {
						category := categories[rng.Intn(len(categories))]
						_, tr := collector.NewTrace(ctx, category)
						word := words[rng.Intn(len(words))]
						errored := rng.Float64() < 0.2
						tr.Tracef("i=%d category=%s word=%s errored=%v", i, category, word, errored)
						if errored {
							tr.Errorf("errored")
						}
						tr.Finish()
					}

					ctx, tr := trc.NewTrace(ctx, "search")
					res, err := collector.Search(ctx, &trc.SearchRequest{Limit: 10, Query: "quux"})
					t.Logf("total=%d matched=%d selected=%d err=%v", res.Total, res.Matched, len(res.Selected), err)
					tr.Finish()
					t.Logf("\n%s\n", debugTrace(tr))
				})
			}
		}
	}
}

func debugTrace(tr trc.Trace) string {
	var sb strings.Builder
	start := tr.Start()
	fmt.Fprintf(&sb, "ID=%s\n", tr.ID())
	fmt.Fprintf(&sb, "Start=%s (%s ago)\n", start.Format(time.RFC3339), time.Since(start))
	fmt.Fprintf(&sb, "Active=%v\n", tr.Active())
	fmt.Fprintf(&sb, "Finished=%v\n", tr.Finished())
	fmt.Fprintf(&sb, "Errored=%v\n", tr.Errored())
	fmt.Fprintf(&sb, "Events=%d\n", len(tr.Events()))
	for _, ev := range tr.Events() {
		fmt.Fprintf(&sb, "\t+%s\t%s\n", ev.When.Sub(start), ev.What.String())
	}
	fmt.Fprintf(&sb, "Duration=%s\n", tr.Duration())
	return sb.String()
}
