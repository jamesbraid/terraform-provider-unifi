// Package unfailable holds tests that CANNOT FAIL, on purpose.
//
// It lives under testdata so the analyzer's own skipDirs keeps it out of the
// repository inventory: a fixture and a finding must never be confusable.
package unfailable

import "testing"

// Runs, and nothing in it can fail.
func TestNothingHappens(t *testing.T) {
	x := 1 + 1
	_ = x
	t.Log("looks busy")
}

// The table is empty, so the body never runs. This is the gotests scaffold.
func TestEmptyTable(t *testing.T) {
	tests := []struct{ name string }{}
	for _, tt := range tests {
		if tt.name == "" {
			t.Fatal("unreachable")
		}
	}
}

// The worst shape: a real case, the code called, nothing checked. It executes,
// so it produces coverage and a green PASS.
func TestPopulatedButMute(t *testing.T) {
	tests := []struct {
		name string
		in   int
	}{
		{name: "a case", in: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = double(tt.in)
		})
	}
}

func double(n int) int { return n * 2 }

// Names a behaviour and declines to test it, permanently.
func TestUnconditionalSkip(t *testing.T) {
	t.Skip("requires terraform state machinery")
}
