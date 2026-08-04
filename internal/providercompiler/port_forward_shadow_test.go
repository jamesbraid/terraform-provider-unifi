package providercompiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

type portForwardShadow struct {
	FormatVersion int    `json:"format_version"`
	Mode          string `json:"mode"`
	Resource      string `json:"resource"`
	Source        struct {
		SchemaSHA256         string `json:"schema_sha256"`
		ResourceSchemaSHA256 string `json:"resource_schema_sha256"`
	} `json:"source"`
	Attributes map[string]struct {
		NestingMode string   `json:"nesting_mode"`
		Attributes  []string `json:"attributes"`
	} `json:"attributes"`
	Claims struct {
		SchemaOnly        bool `json:"schema_only"`
		RuntimeChanged    bool `json:"runtime_changed"`
		LifecycleAdmitted bool `json:"lifecycle_admitted"`
	} `json:"claims"`
}

type canonicalProviderSchema struct {
	ResourceSchemas map[string]struct {
		Block struct {
			Attributes map[string]struct {
				NestedType *struct {
					NestingMode string                     `json:"nesting_mode"`
					Attributes  map[string]json.RawMessage `json:"attributes"`
				} `json:"nested_type"`
			} `json:"attributes"`
		} `json:"block"`
	} `json:"resource_schemas"`
}

func testFileSHA256(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func TestPortForwardSchemaShadowMatchesBuiltProvider(t *testing.T) {
	root := filepath.Join("..", "..")
	shadowPath := filepath.Join(root, "provider-codegen", "shadow", "port_forward.schema-shadow.json")
	var shadow portForwardShadow
	content, err := os.ReadFile(shadowPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &shadow); err != nil {
		t.Fatal(err)
	}
	if shadow.FormatVersion != 1 || shadow.Mode != "schema_only" || shadow.Resource != "unifi_port_forward" {
		t.Fatalf("invalid shadow identity: %#v", shadow)
	}
	if !shadow.Claims.SchemaOnly || shadow.Claims.RuntimeChanged || shadow.Claims.LifecycleAdmitted {
		t.Fatalf("shadow makes a runtime or lifecycle claim: %#v", shadow.Claims)
	}

	schemaPath := filepath.Join(root, "provider-contracts", "schema", "terraform-1.15.8.json")
	if got := testFileSHA256(t, schemaPath); got != shadow.Source.SchemaSHA256 {
		t.Fatalf("provider schema mismatch: expected=%q actual=%q", shadow.Source.SchemaSHA256, got)
	}
	var providerSchema canonicalProviderSchema
	schemaContent, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(schemaContent, &providerSchema); err != nil {
		t.Fatal(err)
	}
	resource := providerSchema.ResourceSchemas[shadow.Resource]
	for name, expected := range shadow.Attributes {
		actual := resource.Block.Attributes[name].NestedType
		if actual == nil {
			t.Fatalf("nested attribute %q is missing", name)
		}
		children := make([]string, 0, len(actual.Attributes))
		for child := range actual.Attributes {
			children = append(children, child)
		}
		sort.Strings(children)
		if actual.NestingMode != expected.NestingMode || !reflect.DeepEqual(children, expected.Attributes) {
			t.Fatalf("nested attribute %q mismatch: expected=%#v actual_mode=%q actual_attributes=%#v", name, expected, actual.NestingMode, children)
		}
	}

	var digests struct {
		SchemaSHA256 map[string]string `json:"schema_sha256"`
	}
	digestContent, err := os.ReadFile(filepath.Join(root, "build", "m0", "provider-schema-digests.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(digestContent, &digests); err != nil {
		t.Fatal(err)
	}
	if got := digests.SchemaSHA256["resource_schemas.unifi_port_forward"]; got != shadow.Source.ResourceSchemaSHA256 {
		t.Fatalf("resource schema digest mismatch: expected=%q actual=%q", shadow.Source.ResourceSchemaSHA256, got)
	}
}
