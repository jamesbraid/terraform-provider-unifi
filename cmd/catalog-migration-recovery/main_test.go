package main

import (
	"bytes"
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
	if _, err := decodeStrictFile(path, &receipt); err != nil {
		t.Fatalf("decodeStrictFile() error = %v", err)
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
			if _, err := decodeStrictFile(path, &receipt); err == nil {
				t.Fatal("decodeStrictFile() succeeded")
			}
		})
	}
}
