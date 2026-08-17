package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRefusesWithoutATreeState makes the refusal a check rather than an
// intention, and it is one of four that did not exist until the cutover wired
// this command into a workflow.
//
// A missing -tree-state must fail, never default. A default -- "unknown", or a
// zero value -- would put a tree_state in every receipt that no measurement
// produced, and any gate reading it would be comparing a constant against
// itself.
//
// THE PRODUCTION FAILURE THIS DESCRIBES IS NOT A FORGOTTEN FLAG. The workflow
// passes -tree-state "$(go run ./cmd/tree-state ...)", and a shell command
// substitution whose inner command FAILS yields the empty string while the
// outer command runs on. So a dirty tree reaches this binary as exactly this
// input, and refusing it is the second half of the guard -- the first half
// being cmd/tree-state exiting non-zero, which check_tree_state_coverage_test.go
// exercises end to end.
//
// -output is supplied because it is checked first: without it the command
// refuses for a different reason and this test would pass having never reached
// the tree state at all.
func TestRefusesWithoutATreeState(t *testing.T) {
	err := run([]string{"-output", filepath.Join(t.TempDir(), "receipt.json"), "-tree-state", ""})
	if err == nil {
		t.Fatal("catalog-dependency-publishability ran without a tree state")
	}
	if !strings.Contains(err.Error(), "tree state is required") {
		t.Fatalf("err = %v, want the ParseTreeState refusal", err)
	}
}
