package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func TestRunWritesDeterministicCompleteEvidenceInventory(t *testing.T) {
	root := filepath.Join("..", "..")
	first := filepath.Join(t.TempDir(), "first.json")
	second := filepath.Join(t.TempDir(), "second.json")
	args := []string{
		"-baseline", filepath.Join(root, "build", "m0", "provider-schema-digests.json"),
		"-contracts", filepath.Join(root, "provider-codegen", "generated", "catalog-surface-contracts.json"),
		"-policy", filepath.Join(root, "provider-codegen", "policy", "catalog-evidence.json"),
		"-released-root", root,
		"-candidate-root", root,
	}
	if code := run(append(args, "-output", first), os.Stderr); code != 0 {
		t.Fatalf("first run exit = %d", code)
	}
	if code := run(append(args, "-output", second), os.Stderr); code != 0 {
		t.Fatalf("second run exit = %d", code)
	}
	firstData, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondData, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstData) != string(secondData) {
		t.Fatal("catalog evidence output is nondeterministic")
	}
	var inventory catalogparity.EvidenceInventory
	if err := json.Unmarshal(firstData, &inventory); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Surfaces) != 67 || inventory.CoverageCounts["scenario_owner"] != 67 {
		t.Fatalf("inventory coverage = surfaces:%d owners:%d", len(inventory.Surfaces), inventory.CoverageCounts["scenario_owner"])
	}
}

func TestRunRejectsIncompleteArguments(t *testing.T) {
	if code := run(nil, os.Stderr); code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
}
