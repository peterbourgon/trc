package trcstream_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/peterbourgon/trc"
	"github.com/peterbourgon/trc/trcstream"
)

func TestStreamTraceDuration(t *testing.T) {
	t.Parallel()

	_, tr := trc.New(context.Background(), "source", "category")
	tr.Tracef("something")
	tr.Finish()

	wantDuration := tr.Duration()

	str1 := trcstream.NewStreamTrace(tr)
	data, _ := json.Marshal(str1)

	var str2 trcstream.StreamTrace
	json.Unmarshal(data, &str2)

	haveDuration := str2.Duration()

	if want, have := wantDuration, haveDuration; want != have {
		t.Errorf("Duration: want %v, have %v", want, have)
	}
}
