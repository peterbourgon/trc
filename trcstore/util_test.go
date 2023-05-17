package trcstore_test

import (
	"testing"
)

func AssertEqual[X comparable](t *testing.T, want, have X) {
	t.Helper()
	if want != have {
		t.Fatalf("want %v, have %v", want, have)
	}
}

func AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("error %v", err)
	}
}

func ExpectEqual[X comparable](t *testing.T, want, have X) {
	t.Helper()
	if want != have {
		t.Errorf("want %v, have %v", want, have)
	}
}

func ExpectNotEqual[X comparable](t *testing.T, want, have X) {
	t.Helper()
	if want == have {
		t.Errorf("want %v, have %v", want, have)
	}
}
