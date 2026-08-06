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
	if !strings.Contains(stderr.String(), "inventory") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDecodeStrictFileRejectsUnknownAndTrailingJSON(t *testing.T) {
	directory := t.TempDir()
	for name, data := range map[string]string{
		"unknown":  `{"format_version":1,"gate":"catalog-build-schema","extra":true}`,
		"trailing": `{"format_version":1}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(directory, name+".json")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			var value catalogparity.BuildSchemaReceipt
			if _, err := decodeStrictFile(path, &value); err == nil {
				t.Fatal("decodeStrictFile() succeeded")
			}
		})
	}
}
