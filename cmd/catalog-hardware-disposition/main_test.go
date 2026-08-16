package main

import (
	"bytes"
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
	if _, err := decodeStrictFile(path, &receipt); err == nil {
		t.Fatal("decodeStrictFile() succeeded")
	}
}
