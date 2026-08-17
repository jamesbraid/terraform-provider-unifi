package catalogparity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseBaselineFindsCompleteCatalog(t *testing.T) {
	baseline := releasedBaseline(t)
	want := map[SurfaceKind]int{
		ManagedResource: 28,
		DataSource:      13,
		ListResource:    25,
		Action:          1,
	}
	if got := baseline.Counts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Counts() = %#v, want %#v", got, want)
	}
	if len(baseline.Surfaces) != 67 {
		t.Fatalf("surface count = %d, want 67", len(baseline.Surfaces))
	}

	key := SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"}
	surface, ok := baseline.Surface(key)
	if !ok {
		t.Fatalf("Surface(%#v) is missing", key)
	}
	if surface.BaselineSchemaSHA256 != "1bdb6740d88d68bf232d79874c34d0e3811d382f55948352add15c2a28e5e93c" {
		t.Fatalf("DNS schema digest = %q", surface.BaselineSchemaSHA256)
	}
}

func TestParseBaselineRejectsInvalidManifest(t *testing.T) {
	tests := map[string]func(map[string]any){
		"format version": func(document map[string]any) {
			document["format_version"] = float64(2)
		},
		"provider address": func(document map[string]any) {
			document["provider_address"] = "registry.example.invalid/other/unifi"
		},
		"canonical digest": func(document map[string]any) {
			document["canonical_schema_sha256"] = "short"
		},
		"surface digest": func(document map[string]any) {
			schemaDigests(document)["resource_schemas.unifi_dns_record"] = "short"
		},
		"unknown category": func(document map[string]any) {
			schemaDigests(document)["function_schemas.unifi_unknown"] = strings.Repeat("a", 64)
		},
		"malformed surface name": func(document map[string]any) {
			digests := schemaDigests(document)
			digests["resource_schemas.unifi_dns_record.extra"] = digests["resource_schemas.unifi_dns_record"]
			delete(digests, "resource_schemas.unifi_dns_record")
		},
		"missing surface": func(document map[string]any) {
			delete(schemaDigests(document), "resource_schemas.unifi_dns_record")
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			document := releasedBaselineDocument(t)
			mutate(document)
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseBaseline(data); err == nil {
				t.Fatal("ParseBaseline() succeeded")
			}
		})
	}
}

func TestParseBaselineRejectsUnknownTopLevelField(t *testing.T) {
	document := releasedBaselineDocument(t)
	document["unexpected"] = true
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseBaseline(data); err == nil {
		t.Fatal("ParseBaseline() succeeded")
	}
}

func releasedBaseline(t *testing.T) Baseline {
	t.Helper()
	data, err := os.ReadFile(baselineFixturePath())
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := ParseBaseline(data)
	if err != nil {
		t.Fatal(err)
	}
	return baseline
}

func releasedBaselineDocument(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(baselineFixturePath())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func schemaDigests(document map[string]any) map[string]any {
	return jsonObject(document["schema_sha256"])
}

func baselineFixturePath() string {
	return filepath.Join("..", "..", "build", "m0", "provider-schema-digests.json")
}
