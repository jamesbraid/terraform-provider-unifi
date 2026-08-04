package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesCanonicalSchemaAndDigests(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "raw.json")
	canonical := filepath.Join(dir, "canonical.json")
	digests := filepath.Join(dir, "digests.json")
	raw := `{"format_version":"1.0","provider_schemas":{"registry.terraform.io/ubiquiti-community/unifi":{"provider":{"block":{}},"resource_schemas":{"unifi_dns_record":{"version":1,"block":{}}}}}}`
	if err := os.WriteFile(input, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	err := run([]string{
		"-input", input,
		"-canonical-output", canonical,
		"-digests-output", digests,
	}, &stderr)
	if err != nil {
		t.Fatalf("run() error = %v, stderr = %s", err, stderr.String())
	}

	canonicalData, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(canonicalData), "format_version") || !strings.Contains(string(canonicalData), "unifi_dns_record") {
		t.Fatalf("unexpected canonical output: %s", canonicalData)
	}
	digestData, err := os.ReadFile(digests)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(digestData), "resource_schemas.unifi_dns_record") {
		t.Fatalf("unexpected digest output: %s", digestData)
	}
}

func TestRunRequiresAllPaths(t *testing.T) {
	var stderr bytes.Buffer
	err := run(nil, &stderr)
	if err == nil || !strings.Contains(err.Error(), "input") {
		t.Fatalf("run() error = %v, want missing input", err)
	}
}
