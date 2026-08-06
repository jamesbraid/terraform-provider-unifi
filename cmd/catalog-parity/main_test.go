package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func TestRunProducesDeterministicCatalogArtifacts(t *testing.T) {
	first := runCatalogParity(t)
	second := runCatalogParity(t)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("catalog parity outputs differ across identical runs")
	}

	var ledger catalogparity.Ledger
	decodeOutput(t, first["catalog-parity-ledger.json"], &ledger)
	if len(ledger.Entries) != 67 {
		t.Fatalf("ledger entries = %d, want 67", len(ledger.Entries))
	}
	var manifest catalogparity.MigrationManifest
	decodeOutput(t, first["catalog-migration-manifest.json"], &manifest)
	if len(manifest.Entries) != 67 {
		t.Fatalf("migration entries = %d, want 67", len(manifest.Entries))
	}
	var report catalogparity.MigrationReport
	decodeOutput(t, first["catalog-migration-report.json"], &report)
	if report.StrategyCounts[string(catalogparity.IdentityTransform)] != 67 || len(report.StateCommands) != 0 {
		t.Fatalf("migration report = %#v", report)
	}
}

func TestRunReplacesOnlyManifestedOutputs(t *testing.T) {
	root := repositoryRoot()
	outputDir := t.TempDir()
	args := catalogArgs(root, outputDir)
	if exitCode := run(args, io.Discard); exitCode != 0 {
		t.Fatalf("first run exit code = %d", exitCode)
	}
	if exitCode := run(args, io.Discard); exitCode != 0 {
		t.Fatalf("second run exit code = %d", exitCode)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "catalog-stale.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if exitCode := run(args, &stderr); exitCode == 0 || !strings.Contains(stderr.String(), "unexpected catalog output") {
		t.Fatalf("run() exit code = %d, stderr = %q", exitCode, stderr.String())
	}
}

func TestRunRejectsMissingArguments(t *testing.T) {
	for name, args := range map[string][]string{
		"all":        nil,
		"schema":     {"-baseline", "baseline", "-status", "status", "-migration", "migration", "-output-dir", "output"},
		"output dir": {"-baseline", "baseline", "-schema", "schema", "-status", "status", "-migration", "migration"},
	} {
		t.Run(name, func(t *testing.T) {
			if exitCode := run(args, io.Discard); exitCode == 0 {
				t.Fatal("run() succeeded")
			}
		})
	}
}

func TestRunRejectsMalformedInput(t *testing.T) {
	root := repositoryRoot()
	statusPath := filepath.Join(t.TempDir(), "status.json")
	if err := os.WriteFile(statusPath, []byte(`{"format_version":1,"default_state":"legacy_authoritative","overrides":[],"extra":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	args := catalogArgs(root, t.TempDir())
	for index := range args {
		if args[index] == "-status" {
			args[index+1] = statusPath
		}
	}
	var stderr bytes.Buffer
	if exitCode := run(args, &stderr); exitCode == 0 || !strings.Contains(stderr.String(), "status") {
		t.Fatalf("run() exit code = %d, stderr = %q", exitCode, stderr.String())
	}
}

func runCatalogParity(t *testing.T) map[string][]byte {
	t.Helper()
	outputDir := t.TempDir()
	if exitCode := run(catalogArgs(repositoryRoot(), outputDir), io.Discard); exitCode != 0 {
		t.Fatalf("run() exit code = %d", exitCode)
	}
	outputs := map[string][]byte{}
	for _, name := range catalogOutputNames() {
		data, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatal(err)
		}
		outputs[name] = data
	}
	return outputs
}

func catalogArgs(root, outputDir string) []string {
	return []string{
		"-baseline", filepath.Join(root, "build", "m0", "provider-schema-digests.json"),
		"-schema", filepath.Join(root, "provider-contracts", "schema", "terraform-1.15.8.json"),
		"-status", filepath.Join(root, "provider-codegen", "parity", "status.json"),
		"-migration", filepath.Join(root, "provider-codegen", "migrations", "v0.101.2-to-next.json"),
		"-output-dir", outputDir,
	}
}

func catalogOutputNames() []string {
	return []string{
		"catalog-parity-ledger.json",
		"catalog-migration-manifest.json",
		"catalog-migration-report.json",
	}
}

func repositoryRoot() string {
	return filepath.Clean(filepath.Join("..", ".."))
}

func decodeOutput(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
