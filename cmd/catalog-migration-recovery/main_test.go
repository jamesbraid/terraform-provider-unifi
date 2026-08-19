package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasequalification"
)

func TestRunRejectsIncompleteArguments(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, &stderr); code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "admission") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDecodeStrictFileAcceptsM3StateUpgradeSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns-lifecycle.json")
	data := `{"format_version":1,"state_upgrade_source":{"version":"0.41.11","commit":"0123456789abcdef0123456789abcdef01234567","binary_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var receipt releasequalification.DNSLifecycleReceipt
	if _, err := cmdio.DecodeStrictFile(path, &receipt); err != nil {
		t.Fatalf("cmdio.DecodeStrictFile() error = %v", err)
	}
}

func TestDecodeStrictFileRejectsUnknownAndTrailingJSON(t *testing.T) {
	directory := t.TempDir()
	for name, data := range map[string]string{
		"unknown":  `{"format_version":1,"extra":true}`,
		"trailing": `{"format_version":1}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(directory, name+".json")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			var receipt releasequalification.DNSLifecycleReceipt
			if _, err := cmdio.DecodeStrictFile(path, &receipt); err == nil {
				t.Fatal("cmdio.DecodeStrictFile() succeeded")
			}
		})
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
	code := run([]string{"-policy", "/dev/null", "-admission", "/dev/null", "-build-schema", "/dev/null", "-controller", "/dev/null", "-inventory", "/dev/null", "-migration-manifest", "/dev/null", "-dns-lifecycle", "/dev/null", "-output", "/dev/null"}, &stderr)
	if code == 0 {
		t.Fatalf("catalog-migration-recovery ran without a tree state; exit = 0")
	}
	if !strings.Contains(stderr.String(), "tree state is required") {
		t.Fatalf("stderr = %q, want it to say the tree state is required", stderr.String())
	}
}
