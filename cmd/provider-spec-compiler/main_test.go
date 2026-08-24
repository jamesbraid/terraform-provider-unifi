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
		"dns_record.mapping.json",
	}
	runs := make([]map[string][]byte, 2)
	for i := range runs {
		outputDir := t.TempDir()
		var stderr bytes.Buffer
		exitCode := run([]string{
			"-bootstrap", filepath.Join(root, "provider-codegen/bootstrap/go-unifi-v1.102.0-dns-record.json"),
			"-policy", filepath.Join(root, "provider-codegen/policy/dns_record.json"),
			"-artifact-prefix", "dns_record",
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

// A list resource's mapping report is empty by design (see
// providercompiler.Compile); run() must write no file for it rather than a
// zero-byte one.
func TestRunWritesNoMappingReportForAListResource(t *testing.T) {
	dir := t.TempDir()
	bootstrapPath := filepath.Join(dir, "bootstrap.json")
	policyPath := filepath.Join(dir, "policy.json")
	writeFile(t, bootstrapPath, `{
		"format_version": 1,
		"source": {"repository": "r", "commit": "c", "specification_sha256": "d"},
		"resource": {"name": "unifi_site", "fields": [{"name": "name", "type": "string"}]}
	}`)
	writeFile(t, policyPath, `{
		"format_version": 1,
		"surface_kind": "list_resource",
		"resource": "unifi_site",
		"source_specification_sha256": "d",
		"description": "",
		"fields": [
			{"structural_name": "name", "terraform_name": "name", "disposition": "omitted"}
		],
		"provider_owned": []
	}`)
	outputDir := t.TempDir()
	var stderr bytes.Buffer
	exitCode := run([]string{
		"-bootstrap", bootstrapPath,
		"-policy", policyPath,
		"-artifact-prefix", "site_list",
		"-output-dir", outputDir,
	}, &stderr)
	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(outputDir, "site_list.mapping.json")); !os.IsNotExist(err) {
		t.Fatalf("site_list.mapping.json stat = %v, want it not to exist for a list resource", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "site_list.provider-code-spec.json")); err != nil {
		t.Fatalf("site_list.provider-code-spec.json: %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunRejectsInvalidArtifactPrefix(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	var stderr bytes.Buffer
	exitCode := run([]string{
		"-bootstrap", filepath.Join(root, "provider-codegen/bootstrap/go-unifi-v1.102.0-dns-record.json"),
		"-policy", filepath.Join(root, "provider-codegen/policy/dns_record.json"),
		"-artifact-prefix", "../dns-record",
		"-output-dir", t.TempDir(),
	}, &stderr)
	if exitCode == 0 || !bytes.Contains(stderr.Bytes(), []byte("artifact-prefix")) {
		t.Fatalf("run() exit code = %d, stderr = %q", exitCode, stderr.String())
	}
}
