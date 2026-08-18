package main

import (
	"bytes"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasequalification"
)

func TestRunRejectsIncompleteArguments(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, &stderr); code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "ledger") || !strings.Contains(stderr.String(), "confidentiality") {
		t.Fatalf("stderr = %q", stderr.String())
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
			var receipt releasequalification.FleetSoakReceipt
			if _, err := cmdio.DecodeStrictFile(path, &receipt); err == nil {
				t.Fatal("cmdio.DecodeStrictFile() succeeded")
			}
		})
	}
}

// TestWriteAtomicUsesPrivatePermissions now exercises the shared writer through
// the exact call this command makes -- the bare form, no options. It asserts
// what it always asserted: the parent directory is created and the artifact is
// readable only by its owner. Pointed at cmdio rather than deleted, so the
// assertion still runs for THIS command's chosen behaviour.
func TestWriteAtomicUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "receipt.json")
	if err := cmdio.WriteAtomic(path, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions = %o, want 600", got)
	}
}
