// Package failable holds tests that CAN fail. Any of these appearing in a scan
// is a false positive, and the helper cases are the ones that matter: a test
// whose only assertion is one call away is the common shape in this repository.
package failable

import (
	"os"
	"testing"
)

func TestDirectAssertion(t *testing.T) {
	if 1+1 != 2 {
		t.Fatal("arithmetic")
	}
}

// The assertion is one call away.
func TestViaHelper(t *testing.T) {
	checkIt(t, 3)
}

func checkIt(t *testing.T, n int) {
	if n != 3 {
		t.Fatalf("want 3, got %d", n)
	}
}

// The assertion is two calls away.
func TestViaHelperChain(t *testing.T) {
	outer(t)
}

func outer(t *testing.T) { inner(t) }
func inner(t *testing.T) { t.Error("boom") }

// require fails the test without ever naming a fail method.
func TestViaRequire(t *testing.T) {
	require.NoError(t, os.ErrNotExist)
}

func TestSubtestAsserts(t *testing.T) {
	t.Run("sub", func(t *testing.T) {
		t.Fatalf("nope")
	})
}

// A GUARDED skip is not a stub: this test runs when the condition is met, which
// is the TF_ACC pattern the acceptance suite is built on.
func TestConditionalSkipStillRuns(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test")
	}
	if 1 != 1 {
		t.Fatal("impossible")
	}
}

func TestTableWithCases(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{name: "one", want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.want != 1 {
				t.Errorf("got %d", tt.want)
			}
		})
	}
}

// An accumulator is DECLARED empty and then filled. Reading only the
// declaration reports a test that runs and asserts as one whose body never
// executes. This was a real false positive: a real test elsewhere asserted
// perfectly well and was reported as empty-table.
func TestAccumulatorIsNotAnEmptyTable(t *testing.T) {
	census := map[string]int{}
	for _, name := range []string{"a", "b"} {
		census[name]++
	}
	for name, count := range census {
		if count != 1 {
			t.Errorf("%s: %d", name, count)
		}
	}
}

// A slice accumulator, same shape, built with append rather than indexing.
func TestAppendAccumulatorIsNotAnEmptyTable(t *testing.T) {
	got := []string{}
	for _, name := range []string{"a"} {
		got = append(got, name)
	}
	for _, name := range got {
		if name == "" {
			t.Error("empty")
		}
	}
}

// Ranges over values the analyzer CANNOT resolve: a directory listing and a
// map the loop above fills. This is the shape that was reported as an empty
// table before the detector required positive proof -- a real test elsewhere
// reads a directory with os.ReadDir and asserts on what it finds, the same
// pattern as the loop below.
//
// The point of the fixture is which way the DEFAULT falls. An analyzer that
// cannot tell must not report a defect: "I could not resolve this" and "this
// test cannot fail" are different statements, and reporting the second when
// only the first is true is the same disease the audit exists to find.
func TestRangeOverRuntimeValueIsNotAnEmptyTable(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.Name()] = true
	}
	for name := range seen {
		if name == "" {
			t.Error("empty name")
		}
	}
}
