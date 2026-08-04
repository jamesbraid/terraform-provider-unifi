package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRunProducesDeterministicDNSArtifacts(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	outputs := []string{
		"dns_record.provider-code-spec.json",
		"dns_record.impact.json",
		"dns_record.mapping.json",
	}
	runs := make([]map[string][]byte, 2)
	for i := range runs {
		outputDir := t.TempDir()
		var stderr bytes.Buffer
		exitCode := run([]string{
			"-catalog", filepath.Join(root, "provider-codegen/catalog/go-unifi-v1.102.0-dns-record.catalog.json"),
			"-policy", filepath.Join(root, "provider-codegen/policy/dns_record.json"),
			"-baseline", filepath.Join(root, "build/m0/provider-schema-digests.json"),
			"-output-dir", outputDir,
		}, &stderr)
		if exitCode != 0 {
			t.Fatalf("run %d exit code = %d, stderr = %s", i+1, exitCode, stderr.String())
		}
		runs[i] = make(map[string][]byte, len(outputs))
		for _, name := range outputs {
			data, err := os.ReadFile(filepath.Join(outputDir, name))
			if err != nil {
				t.Fatal(err)
			}
			runs[i][name] = data
		}
	}
	if !reflect.DeepEqual(runs[0], runs[1]) {
		t.Fatal("compiler outputs differ across identical runs")
	}

	var impact struct {
		Unresolved []string `json:"unresolved_fields"`
		Stale      []string `json:"stale_policy_fields"`
	}
	if err := json.Unmarshal(runs[0]["dns_record.impact.json"], &impact); err != nil {
		t.Fatal(err)
	}
	if len(impact.Unresolved) != 0 || len(impact.Stale) != 0 {
		t.Fatalf("impact contains unresolved=%v stale=%v", impact.Unresolved, impact.Stale)
	}

	var mapping struct {
		Fields        []json.RawMessage `json:"fields"`
		ProviderOwned []json.RawMessage `json:"provider_owned"`
	}
	if err := json.Unmarshal(runs[0]["dns_record.mapping.json"], &mapping); err != nil {
		t.Fatal(err)
	}
	if len(mapping.Fields) != 8 || len(mapping.ProviderOwned) != 3 {
		t.Fatalf("mapping counts = %d structural, %d provider-owned; want 8 and 3", len(mapping.Fields), len(mapping.ProviderOwned))
	}
}

func TestRunRejectsMissingArguments(t *testing.T) {
	var stderr bytes.Buffer
	if exitCode := run(nil, &stderr); exitCode == 0 {
		t.Fatal("run() succeeded without required arguments")
	}
}
