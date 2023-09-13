package trc_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/peterbourgon/trc"
)

func TestStreamTraceDuration(t *testing.T) {
	t.Parallel()

	_, tr := trc.New(context.Background(), "source", "category")
	tr.Tracef("something")
	tr.Finish()

	wantDuration := tr.Duration()

	str1 := trc.NewStreamTrace(tr)
	data, _ := json.Marshal(str1)

	var str2 trc.StreamTrace
	json.Unmarshal(data, &str2)

	haveDuration := str2.Duration()

	if want, have := wantDuration, haveDuration; want != have {
		t.Errorf("Duration: want %v, have %v", want, have)
	}
}
