package main

import (
	"bytes"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func TestRunRejectsIncompleteArguments(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, &stderr); code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "controller") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDecodeStrictFileRejectsUnknownJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	if err := os.WriteFile(path, []byte(`{"format_version":1,"extra":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var receipt catalogparity.ControllerDifferentialReceipt
	if _, err := cmdio.DecodeStrictFile(path, &receipt); err == nil {
		t.Fatal("cmdio.DecodeStrictFile() succeeded")
	}
}

// TestRefusesWithoutATreeState makes the refusal a check rather than an
// intention.
//
// A missing -tree-state must fail, never default. A default -- "unknown", or a
// zero value -- would put a tree_state in every receipt that no measurement
// produced, and any gate reading it would be comparing a constant against
// itself. That is the shape where one producer writes a literal and the
// consumers dutifully check it, and it passes review because the branch is
// reachable in a test while the input can never vary in production.
//
// Deleting the ParseTreeState call, or giving it a fallback, fails this.
func TestRefusesWithoutATreeState(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"-controller", "/dev/null", "-output", "/dev/null"}, &stderr)
	if code == 0 {
		t.Fatalf("catalog-hardware-disposition ran without a tree state; exit = 0")
	}
	if !strings.Contains(stderr.String(), "tree state is required") {
		t.Fatalf("stderr = %q, want it to say the tree state is required", stderr.String())
	}
}
