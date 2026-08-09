package providercompiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

const testSpecificationDigest = "3ddcc597a631259089c823553f3bf696725ad0bbf7d78d2f412b111e8e3427ad"

func TestCompileResolvesCompletePolicy(t *testing.T) {
	result, err := Compile(CompileInput{
		Bootstrap:       testBootstrap(t, dnsFieldNames()),
		Policy:          testPolicy(t, dnsFieldNames(), testSpecificationDigest),
		BaselineDigests: testBaseline(t),
		Ledger:          testLedger(t, catalogparity.Admitted),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	for name, output := range map[string][]byte{
		"provider code specification": result.ProviderCodeSpec,
		"impact report":               result.ImpactReport,
		"mapping report":              result.MappingReport,
	} {
		if !json.Valid(output) {
			t.Fatalf("%s is not valid JSON: %s", name, output)
		}
		if len(output) == 0 || output[len(output)-1] != '\n' {
			t.Fatalf("%s does not end with a newline", name)
		}
	}

	var mapping struct {
		Fields []struct {
			StructuralName string `json:"structural_name"`
			TerraformName  string `json:"terraform_name"`
			Disposition    string `json:"disposition"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(result.MappingReport, &mapping); err != nil {
		t.Fatal(err)
	}
	if len(mapping.Fields) != 8 {
		t.Fatalf("mapping field count = %d, want 8", len(mapping.Fields))
	}
	for i, field := range mapping.Fields {
		if field.StructuralName != dnsFieldNames()[i] {
			t.Fatalf("mapping field %d = %q, want %q", i, field.StructuralName, dnsFieldNames()[i])
		}
		if field.Disposition != "managed" {
			t.Fatalf("mapping disposition for %q = %q, want managed", field.StructuralName, field.Disposition)
		}
	}
	if mapping.Fields[1].TerraformName != "name" {
		t.Fatalf("key maps to %q, want name", mapping.Fields[1].TerraformName)
	}
}

func TestCompileFailsClosed(t *testing.T) {
	tests := map[string]struct {
		bootstrapFields []string
		policyFields    []string
		digest          string
		mutatePolicy    func(map[string]any)
		want            string
	}{
		"unclassified structural field": {
			bootstrapFields: append(dnsFieldNames(), "new_field"),
			policyFields:    dnsFieldNames(),
			digest:          testSpecificationDigest,
			want:            "unclassified structural field",
		},
		"stale policy field": {
			bootstrapFields: dnsFieldNames(),
			policyFields:    append(dnsFieldNames(), "removed_field"),
			digest:          testSpecificationDigest,
			want:            "stale policy field",
		},
		"duplicate terraform attribute": {
			bootstrapFields: dnsFieldNames(),
			policyFields:    dnsFieldNames(),
			digest:          testSpecificationDigest,
			mutatePolicy: func(policy map[string]any) {
				fields := policy["fields"].([]any)
				fields[0].(map[string]any)["terraform_name"] = "name"
			},
			want: "duplicate terraform attribute",
		},
		"bootstrap digest mismatch": {
			bootstrapFields: dnsFieldNames(),
			policyFields:    dnsFieldNames(),
			digest:          strings.Repeat("0", 64),
			want:            "bootstrap digest mismatch",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			policy := testPolicyObject(test.policyFields, test.digest)
			if test.mutatePolicy != nil {
				test.mutatePolicy(policy)
			}
			_, err := Compile(CompileInput{
				Bootstrap:       testBootstrap(t, test.bootstrapFields),
				Policy:          mustJSON(t, policy),
				BaselineDigests: testBaseline(t),
				Ledger:          testLedger(t, catalogparity.Admitted),
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestCompilePinnedDNSInputs(t *testing.T) {
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	result, err := Compile(CompileInput{
		Catalog:         read("../../provider-codegen/catalog/go-unifi-v1.102.0-dns-record.catalog.json"),
		Policy:          read("../../provider-codegen/policy/dns_record.json"),
		BaselineDigests: read("../../build/m0/provider-schema-digests.json"),
		Ledger:          read("../../provider-codegen/generated/catalog-parity-ledger.json"),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if !strings.Contains(string(result.ProviderCodeSpec), `"name": "dns_record"`) {
		t.Fatalf("provider code specification does not contain dns_record: %s", result.ProviderCodeSpec)
	}
}

func TestCompileCatalogMatchesBootstrapSpecification(t *testing.T) {
	policyObject := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	policyObject["catalog_id"] = "unifi.network.dns_record@10.4.57"
	policyObject["operation_digest"] = "operation-digest"
	catalog := testCatalog(t, dnsFieldNames())
	policyObject["catalog_sha256"] = byteDigest(catalog)
	addTestCatalogSource(policyObject)
	policy := mustJSON(t, policyObject)
	baseline := testBaseline(t)
	bootstrapResult, err := Compile(CompileInput{
		Bootstrap:       testBootstrap(t, dnsFieldNames()),
		Policy:          policy,
		BaselineDigests: baseline,
		Ledger:          testLedger(t, catalogparity.Admitted),
	})
	if err != nil {
		t.Fatalf("bootstrap Compile() error = %v", err)
	}
	catalogResult, err := Compile(CompileInput{
		Catalog:         catalog,
		Policy:          policy,
		BaselineDigests: baseline,
		Ledger:          testLedger(t, catalogparity.Admitted),
	})
	if err != nil {
		t.Fatalf("catalog Compile() error = %v", err)
	}
	if string(catalogResult.ProviderCodeSpec) != string(bootstrapResult.ProviderCodeSpec) {
		t.Fatalf("catalog changed provider code specification:\nbootstrap: %s\ncatalog: %s", bootstrapResult.ProviderCodeSpec, catalogResult.ProviderCodeSpec)
	}
}

func TestCompileCatalogFailsClosed(t *testing.T) {
	tests := map[string]struct {
		mutate func(map[string]any)
		want   string
	}{
		"unresolved conflict": {
			mutate: func(catalog map[string]any) {
				catalog["conflicts"] = []any{map[string]any{"kind": "type_mismatch", "field": "ttl"}}
			},
			want: "unresolved conflicts",
		},
		"missing locked target": {
			mutate: func(catalog map[string]any) {
				delete(catalog, "target")
			},
			want: "complete locked catalog target",
		},
		"locked target policy mismatch": {
			mutate: func(catalog map[string]any) {
				catalog["target"].(map[string]any)["name"] = "different-target"
			},
			want: "locked catalog target does not match provider policy",
		},
		"missing source digests": {
			mutate: func(catalog map[string]any) {
				delete(catalog, "sources")
			},
			want: "complete catalog source digests",
		},
		"source digest policy mismatch": {
			mutate: func(catalog map[string]any) {
				catalog["sources"].(map[string]any)["capture_lock_sha256"] = strings.Repeat("8", 64)
			},
			want: "catalog source digests do not match provider policy",
		},
		"incomplete coverage": {
			mutate: func(catalog map[string]any) {
				catalog["coverage"] = catalog["coverage"].([]any)[1:]
			},
			want: "incomplete catalog coverage",
		},
		"missing observed record": {
			mutate: func(catalog map[string]any) {
				catalog["observed_records"] = catalog["observed_records"].([]any)[1:]
			},
			want: "missing observed record",
		},
		"duplicate observed semantic ID": {
			mutate: func(catalog map[string]any) {
				records := catalog["observed_records"].([]any)
				records[1].(map[string]any)["id"] = records[0].(map[string]any)["id"]
			},
			want: "duplicate observed semantic ID",
		},
		"duplicate policy semantic ID": {
			mutate: func(catalog map[string]any) {},
			want:   "duplicate policy semantic ID",
		},
		"unknown observed semantic ID": {
			mutate: func(catalog map[string]any) {
				records := catalog["observed_records"].([]any)
				records[0].(map[string]any)["id"] = "unifi.network.dns_record.field.unknown"
			},
			want: "unknown observed semantic ID",
		},
		"definition digest disagreement": {
			mutate: func(catalog map[string]any) {
				catalog["structural_records"].([]any)[0].(map[string]any)["definition_sha256"] = strings.Repeat("0", 64)
			},
			want: "definition digest mismatch",
		},
		"observed type disagreement": {
			mutate: func(catalog map[string]any) {
				catalog["observed_records"].([]any)[0].(map[string]any)["json_type"] = "string"
			},
			want: "observed type mismatch",
		},
		"uncovered field": {
			mutate: func(catalog map[string]any) {
				catalog["coverage"].([]any)[0].(map[string]any)["state"] = "not_observed"
			},
			want: "incomplete catalog coverage",
		},
		"unsafe secret candidate": {
			mutate: func(catalog map[string]any) {
				catalog["structural_records"].([]any)[0].(map[string]any)["secret_candidate"] = true
				rehashCatalogDefinition(t, catalog["structural_records"].([]any)[0].(map[string]any))
			},
			want: "secret candidate",
		},
		"unresolved tombstone": {
			mutate: func(catalog map[string]any) {
				catalog["tombstones"] = []any{"unifi.network.dns_record.field.removed"}
			},
			want: "unresolved tombstone",
		},
		"incomplete migration": {
			mutate: func(catalog map[string]any) {
				catalog["migrations"] = []any{map[string]any{
					"from_id": "unifi.network.dns_record.field.old_key",
					"to_id":   "unifi.network.dns_record.field.key",
					"reason":  "renamed",
				}}
			},
			want: "incomplete migration",
		},
		"unstable semantic ID": {
			mutate: func(catalog map[string]any) {
				catalog["structural_records"].([]any)[0].(map[string]any)["id"] = "dns.enabled"
			},
			want: "unstable catalog ID",
		},
		"unpinned operation": {
			mutate: func(catalog map[string]any) {
				catalog["admission"].(map[string]any)["operation_digest"] = "different-operation"
			},
			want: "admitted operation digest mismatch",
		},
		"unpinned catalog": {
			mutate: func(catalog map[string]any) {},
			want:   "admitted catalog digest mismatch",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var catalog map[string]any
			if err := json.Unmarshal(testCatalog(t, dnsFieldNames()), &catalog); err != nil {
				t.Fatal(err)
			}
			test.mutate(catalog)
			catalogBytes := mustJSON(t, catalog)
			policy := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
			policy["catalog_id"] = "unifi.network.dns_record@10.4.57"
			policy["operation_digest"] = "operation-digest"
			policy["catalog_sha256"] = byteDigest(catalogBytes)
			addTestCatalogSource(policy)
			if name == "duplicate policy semantic ID" {
				fields := policy["fields"].([]any)
				fields[1].(map[string]any)["semantic_id"] = fields[0].(map[string]any)["semantic_id"]
			}
			if name == "unpinned catalog" {
				policy["catalog_sha256"] = strings.Repeat("0", 64)
			}
			_, err := Compile(CompileInput{
				Catalog:         catalogBytes,
				Policy:          mustJSON(t, policy),
				BaselineDigests: testBaseline(t),
				Ledger:          testLedger(t, catalogparity.Admitted),
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func dnsFieldNames() []string {
	return []string{"enabled", "key", "port", "priority", "record_type", "ttl", "value", "weight"}
}

func testBootstrap(t *testing.T, fieldNames []string) []byte {
	t.Helper()
	fields := make([]map[string]any, 0, len(fieldNames))
	for _, name := range fieldNames {
		fieldType := "int64"
		switch name {
		case "enabled":
			fieldType = "bool"
		case "key", "record_type", "value", "new_field":
			fieldType = "string"
		case "tags":
			fieldType = "array<string>"
		case "ports":
			fieldType = "array<int64>"
		}
		fields = append(fields, map[string]any{"name": name, "type": fieldType})
	}
	return mustJSON(t, map[string]any{
		"format_version": 1,
		"source": map[string]any{
			"repository":           "github.com/ubiquiti-community/go-unifi",
			"commit":               "e255518385e0104eb838be56c2a491de158f3194",
			"specification_sha256": testSpecificationDigest,
		},
		"resource": map[string]any{
			"name":   "unifi_dns_record",
			"fields": fields,
		},
	})
}

func testPolicy(t *testing.T, fieldNames []string, digest string) []byte {
	t.Helper()
	return mustJSON(t, testPolicyObject(fieldNames, digest))
}

func testPolicyObject(fieldNames []string, digest string) map[string]any {
	fields := make([]any, 0, len(fieldNames))
	for _, name := range fieldNames {
		terraformName := name
		if name == "key" {
			terraformName = "name"
		}
		fields = append(fields, map[string]any{
			"structural_name": name,
			"semantic_id":     "unifi.network.dns_record.field." + name,
			"terraform_name":  terraformName,
			"disposition":     "managed",
			"attribute": map[string]any{
				"computed_optional_required": "optional",
			},
		})
	}
	return map[string]any{
		"format_version":              1,
		"surface_kind":                "managed_resource",
		"resource":                    "unifi_dns_record",
		"source_specification_sha256": digest,
		"fields":                      fields,
		"provider_owned": []any{
			map[string]any{
				"terraform_name": "id",
				"disposition":    "computed",
				"generated":      false,
			},
			map[string]any{
				"terraform_name": "site",
				"disposition":    "managed",
				"generated":      false,
			},
			map[string]any{
				"terraform_name": "timeouts",
				"disposition":    "managed",
				"generated":      false,
			},
		},
		"baseline_digests": map[string]any{
			"resource":      "1bdb6740d88d68bf232d79874c34d0e3811d382f55948352add15c2a28e5e93c",
			"identity":      "1a6e443309d9484e62e9f1fe71a83b60cf348f4acbe3a92d8f7b8bb7d3274d33",
			"list_resource": "c914929e71ab8ce0e8977518615ee3cf81c31a411ec77c9f58a2350145c6ee95",
		},
	}
}

func byteDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return fmt.Sprintf("%x", digest)
}

func addTestCatalogSource(policy map[string]any) {
	policy["catalog_source"] = map[string]any{
		"repository": "github.com/ubiquiti-community/go-unifi",
		"commit":     strings.Repeat("a", 40),
		"path":       "catalogs/network-10.4.57/dns_record.admitted-catalog.json",
	}
	policy["catalog_target"] = testCatalogTarget()
	policy["catalog_sources"] = testCatalogSources()
}

func testCatalogTarget() map[string]any {
	return map[string]any{
		"name":                   "network-10.4.57-seeded",
		"product":                "unifi-network",
		"version":                "10.4.57",
		"architecture":           "amd64",
		"image_index_sha256":     "sha256:" + strings.Repeat("1", 64),
		"image_manifest_sha256":  "sha256:" + strings.Repeat("2", 64),
		"controller_fingerprint": "sha256:" + strings.Repeat("3", 64),
	}
}

func testCatalogSources() map[string]any {
	return map[string]any{
		"capture_lock_sha256":          strings.Repeat("4", 64),
		"structural_projection_sha256": strings.Repeat("5", 64),
		"semantic_predecessor_sha256":  strings.Repeat("6", 64),
		"semantic_ids_sha256":          strings.Repeat("7", 64),
	}
}

func testBaseline(t *testing.T) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"schema_sha256": map[string]any{
			"resource_schemas.unifi_dns_record":          "1bdb6740d88d68bf232d79874c34d0e3811d382f55948352add15c2a28e5e93c",
			"resource_identity_schemas.unifi_dns_record": "1a6e443309d9484e62e9f1fe71a83b60cf348f4acbe3a92d8f7b8bb7d3274d33",
			"list_resource_schemas.unifi_dns_record":     "c914929e71ab8ce0e8977518615ee3cf81c31a411ec77c9f58a2350145c6ee95",
		},
	})
}

// collectionInput compiles the dns_record fixture plus one collection field,
// letting a test alter that field's policy to exercise the semantics the SDK
// type cannot supply.
func collectionInput(t *testing.T, mutate func(field map[string]any)) CompileInput {
	t.Helper()
	names := append(dnsFieldNames(), "tags")
	rules := testPolicyObject(names, testSpecificationDigest)
	for _, raw := range rules["fields"].([]any) {
		field, ok := raw.(map[string]any)
		if !ok || field["structural_name"] != "tags" {
			continue
		}
		field["terraform_type"] = "set"
		field["attribute"] = map[string]any{
			"computed_optional_required": "optional",
			"element_type":               map[string]any{"string": map[string]any{}},
		}
		if mutate != nil {
			mutate(field)
		}
	}
	return CompileInput{
		Bootstrap:       testBootstrap(t, names),
		Policy:          mustJSON(t, rules),
		BaselineDigests: testBaseline(t),
		Ledger:          testLedger(t, catalogparity.Admitted),
	}
}

func collectionAttribute(t *testing.T, spec []byte, name string) map[string]json.RawMessage {
	t.Helper()
	var document struct {
		Resources []struct {
			Schema struct {
				Attributes []map[string]json.RawMessage `json:"attributes"`
			} `json:"schema"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(spec, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(document.Resources))
	}
	for _, attribute := range document.Resources[0].Schema.Attributes {
		var attributeName string
		if err := json.Unmarshal(attribute["name"], &attributeName); err != nil {
			t.Fatal(err)
		}
		if attributeName == name {
			return attribute
		}
	}
	t.Fatalf("attribute %q is absent from the specification", name)
	return nil
}

// The specification member and the element type both have to survive to the
// emitted document; a collection that lands under the wrong member generates
// nothing, exactly as a misplaced data source would.
func TestCompileEmitsCollectionUnderThePolicyDeclaredMember(t *testing.T) {
	for _, declared := range []string{"set", "list"} {
		t.Run(declared, func(t *testing.T) {
			result, err := Compile(collectionInput(t, func(field map[string]any) {
				field["terraform_type"] = declared
			}))
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			attribute := collectionAttribute(t, result.ProviderCodeSpec, "tags")
			body, present := attribute[declared]
			if !present {
				t.Fatalf("attribute has no %q member, got members %v", declared, attributeMembers(attribute))
			}
			var definition struct {
				ElementType map[string]json.RawMessage `json:"element_type"`
			}
			if err := json.Unmarshal(body, &definition); err != nil {
				t.Fatal(err)
			}
			if _, ok := definition.ElementType["string"]; !ok || len(definition.ElementType) != 1 {
				t.Fatalf("element_type = %v, want exactly string", definition.ElementType)
			}
		})
	}
}

func attributeMembers(attribute map[string]json.RawMessage) []string {
	members := make([]string, 0, len(attribute))
	for member := range attribute {
		members = append(members, member)
	}
	sort.Strings(members)
	return members
}

// Order sensitivity is a human decision. The compiler must refuse to pick,
// rather than defaulting to either and producing a spurious diff on every plan.
func TestCompileRejectsCollectionsWithoutASemanticDecision(t *testing.T) {
	tests := map[string]struct {
		mutate func(field map[string]any)
		want   string
	}{
		"no terraform type": {
			mutate: func(field map[string]any) { delete(field, "terraform_type") },
			want:   "must declare terraform_type as list or set",
		},
		"scalar terraform type": {
			mutate: func(field map[string]any) { field["terraform_type"] = "string" },
			want:   `declares terraform_type "string", want list or set`,
		},
		"element type disagrees with the catalog": {
			mutate: func(field map[string]any) {
				field["attribute"] = map[string]any{
					"computed_optional_required": "optional",
					"element_type":               map[string]any{"int64": map[string]any{}},
				}
			},
			want: `declares element type "int64" but the catalog observed "string"`,
		},
		"no element type": {
			mutate: func(field map[string]any) {
				field["attribute"] = map[string]any{"computed_optional_required": "optional"}
			},
			want: "must declare exactly one element_type, found 0",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Compile(collectionInput(t, test.mutate))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want %q", err, test.want)
			}
		})
	}
}

// The vocabulary stops at the element types the estate measurably uses. An
// unmeasured one must be reported by name rather than coerced to string.
func TestProviderStructuralTypeAcceptsOnlyMeasuredCollections(t *testing.T) {
	// object and array<object> carry their members in bootstrapField.Fields
	// rather than in the type string, so they are accepted here and resolved
	// to a specification member by objectTerraformType.
	for _, accepted := range []string{"array<string>", "array<int64>", "object", "array<object>"} {
		resolved, err := providerStructuralType(accepted)
		if err != nil || resolved != accepted {
			t.Fatalf("providerStructuralType(%q) = %q, %v", accepted, resolved, err)
		}
	}
	for _, rejected := range []string{"array", "array<bool>", "array<array<string>>", "map<string>", "object<string>"} {
		if _, err := providerStructuralType(rejected); err == nil {
			t.Fatalf("providerStructuralType(%q) was accepted", rejected)
		}
	}
}

// nestedInput adds one object field to the dns_record fixture. The member list
// comes from the bootstrap, i.e. the catalog side, and the per-member decisions
// from the policy — the split that makes this a derivation rather than a
// hand-authored body.
func nestedInput(t *testing.T, structuralType string, mutate func(field map[string]any)) CompileInput {
	t.Helper()
	names := append(dnsFieldNames(), "endpoint")
	rules := testPolicyObject(names, testSpecificationDigest)
	for _, raw := range rules["fields"].([]any) {
		field, ok := raw.(map[string]any)
		if !ok || field["structural_name"] != "endpoint" {
			continue
		}
		field["attribute"] = map[string]any{"computed_optional_required": "optional"}
		if structuralType == "array<object>" {
			field["terraform_type"] = "list_nested"
		}
		field["fields"] = []any{
			map[string]any{
				"structural_name": "host", "terraform_name": "host", "disposition": "managed",
				"attribute": map[string]any{"computed_optional_required": "required"},
			},
			map[string]any{
				"structural_name": "port", "terraform_name": "port", "disposition": "managed",
				"attribute": map[string]any{"computed_optional_required": "optional"},
			},
		}
		if mutate != nil {
			mutate(field)
		}
	}

	var source map[string]any
	if err := json.Unmarshal(testBootstrap(t, names), &source); err != nil {
		t.Fatal(err)
	}
	for _, raw := range source["resource"].(map[string]any)["fields"].([]any) {
		field, ok := raw.(map[string]any)
		if !ok || field["name"] != "endpoint" {
			continue
		}
		field["type"] = structuralType
		field["fields"] = []any{
			map[string]any{"name": "host", "type": "string"},
			map[string]any{"name": "port", "type": "int64"},
		}
	}

	return CompileInput{
		Bootstrap:       mustJSON(t, source),
		Policy:          mustJSON(t, rules),
		BaselineDigests: testBaseline(t),
		Ledger:          testLedger(t, catalogparity.Admitted),
	}
}

// The member list must be DERIVED from the catalog. A policy that hand-authors
// it is the failure this exists to prevent: it would look like a migration
// while carrying the same hand-written schema in a different file.
func TestCompileDerivesNestedMembersFromTheCatalog(t *testing.T) {
	for structuralType, member := range map[string]string{
		"object":        "single_nested",
		"array<object>": "list_nested",
	} {
		t.Run(structuralType, func(t *testing.T) {
			result, err := Compile(nestedInput(t, structuralType, nil))
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			attribute := collectionAttribute(t, result.ProviderCodeSpec, "endpoint")
			body, present := attribute[member]
			if !present {
				t.Fatalf("attribute has no %q member, got %v", member, attributeMembers(attribute))
			}
			var definition struct {
				Attributes   []map[string]json.RawMessage `json:"attributes"`
				NestedObject struct {
					Attributes []map[string]json.RawMessage `json:"attributes"`
				} `json:"nested_object"`
			}
			if err := json.Unmarshal(body, &definition); err != nil {
				t.Fatal(err)
			}
			members := definition.Attributes
			if member == "list_nested" {
				members = definition.NestedObject.Attributes
			}
			if len(members) != 2 {
				t.Fatalf("derived %d members, want 2 (host, port)", len(members))
			}
			var first, second string
			if err := json.Unmarshal(members[0]["name"], &first); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(members[1]["name"], &second); err != nil {
				t.Fatal(err)
			}
			if first != "host" || second != "port" {
				t.Fatalf("members = %q, %q; want host, port", first, second)
			}
			// The member's own type must be derived too, not defaulted.
			if _, ok := members[1]["int64"]; !ok {
				t.Fatalf("port member is %v, want int64 from the catalog", attributeMembers(members[1]))
			}
		})
	}
}

func TestCompileRejectsUnderivableNesting(t *testing.T) {
	tests := map[string]struct {
		structuralType string
		mutate         func(field map[string]any)
		want           string
	}{
		"object collection without a semantic decision": {
			structuralType: "array<object>",
			mutate:         func(field map[string]any) { delete(field, "terraform_type") },
			want:           "must declare terraform_type as list_nested or set_nested",
		},
		"single object declared as a collection": {
			structuralType: "object",
			mutate:         func(field map[string]any) { field["terraform_type"] = "list_nested" },
			want:           `declares terraform_type "list_nested", want single_nested`,
		},
		"member the catalog does not observe": {
			structuralType: "object",
			mutate: func(field map[string]any) {
				field["fields"] = append(field["fields"].([]any), map[string]any{
					"structural_name": "ghost", "terraform_name": "ghost", "disposition": "managed",
					"attribute": map[string]any{"computed_optional_required": "optional"},
				})
			},
			want: `policy for member "ghost" that the catalog does not observe`,
		},
		"unclassified member": {
			structuralType: "object",
			mutate: func(field map[string]any) {
				field["fields"] = []any{field["fields"].([]any)[0]}
			},
			want: `member "port" is unclassified`,
		},
		"hand-authored member list": {
			structuralType: "object",
			mutate: func(field map[string]any) {
				field["attribute"] = map[string]any{
					"computed_optional_required": "optional",
					"attributes":                 []any{},
				}
			},
			want: "hand-authors \"attributes\"; the member list is derived from the catalog",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Compile(nestedInput(t, test.structuralType, test.mutate))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want %q", err, test.want)
			}
		})
	}
}

// groupingInput models the real shape: two observed flat fields grouped into a
// nested attribute the SDK does not have, which is what port_forward's wan does
// with PfwdInterface, DestinationIP and DstPort.
func groupingInput(t *testing.T, mutate func(rules map[string]any)) CompileInput {
	t.Helper()
	names := dnsFieldNames()
	rules := testPolicyObject(names, testSpecificationDigest)

	// port and priority move out of the top level and into the grouping.
	kept := []any{}
	for _, raw := range rules["fields"].([]any) {
		field := raw.(map[string]any)
		if field["structural_name"] == "port" || field["structural_name"] == "priority" {
			continue
		}
		kept = append(kept, field)
	}
	rules["fields"] = kept
	rules["groupings"] = []any{
		map[string]any{
			"terraform_name": "endpoint",
			"terraform_type": "single_nested",
			"attribute":      map[string]any{"computed_optional_required": "optional"},
			"members": []any{
				map[string]any{
					"structural_name": "port", "terraform_name": "port", "disposition": "managed",
					"attribute": map[string]any{"computed_optional_required": "optional"},
				},
				map[string]any{
					"structural_name": "priority", "terraform_name": "priority", "disposition": "managed",
					"attribute": map[string]any{"computed_optional_required": "optional"},
				},
			},
		},
	}
	if mutate != nil {
		mutate(rules)
	}
	return CompileInput{
		Bootstrap:       testBootstrap(t, names),
		Policy:          mustJSON(t, rules),
		BaselineDigests: testBaseline(t),
		Ledger:          testLedger(t, catalogparity.Admitted),
	}
}

func firstGrouping(rules map[string]any) map[string]any {
	return rules["groupings"].([]any)[0].(map[string]any)
}

func groupingMembers(rules map[string]any) []any {
	return firstGrouping(rules)["members"].([]any)
}

func TestCompileEmitsADeclaredGrouping(t *testing.T) {
	result, err := Compile(groupingInput(t, nil))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	attribute := collectionAttribute(t, result.ProviderCodeSpec, "endpoint")
	body, present := attribute["single_nested"]
	if !present {
		t.Fatalf("grouping emitted as %v, want single_nested", attributeMembers(attribute))
	}
	var definition struct {
		Attributes []map[string]json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(body, &definition); err != nil {
		t.Fatal(err)
	}
	if len(definition.Attributes) != 2 {
		t.Fatalf("grouping has %d members, want 2", len(definition.Attributes))
	}
	// Member types come from the catalog, not from the policy: both fields are
	// int64 in the bootstrap and must arrive that way.
	for _, member := range definition.Attributes {
		if _, ok := member["int64"]; !ok {
			t.Fatalf("member %v did not take its type from the catalog", attributeMembers(member))
		}
	}
	// The grouped fields must not also appear at the top level.
	if _, found := attribute["port"]; found {
		t.Fatal("a grouped field is still emitted as a top-level attribute")
	}
}

// The guard that makes a grouping a migration rather than hand-authoring.
func TestCompileRejectsGroupingsThatAreNotDerivations(t *testing.T) {
	tests := map[string]struct {
		mutate func(rules map[string]any)
		want   string
	}{
		"member names no observed field": {
			mutate: func(rules map[string]any) {
				groupingMembers(rules)[0].(map[string]any)["structural_name"] = "nonexistent"
			},
			want: `consumes "nonexistent", which the catalog does not observe`,
		},
		"member claims a field also classified at the top level": {
			mutate: func(rules map[string]any) {
				rules["fields"] = append(rules["fields"].([]any), map[string]any{
					"structural_name": "port", "semantic_id": "unifi.network.dns_record.field.port",
					"terraform_name": "port_again", "disposition": "managed",
					"attribute": map[string]any{"computed_optional_required": "optional"},
				})
			},
			want: "consumed by grouping \"endpoint\" and also classified at the top level",
		},
		"two groupings consume the same field": {
			mutate: func(rules map[string]any) {
				rules["groupings"] = append(rules["groupings"].([]any), map[string]any{
					"terraform_name": "other", "terraform_type": "single_nested",
					"attribute": map[string]any{"computed_optional_required": "optional"},
					"members": []any{map[string]any{
						"structural_name": "port", "terraform_name": "port", "disposition": "managed",
						"attribute": map[string]any{"computed_optional_required": "optional"},
					}},
				})
			},
			want: `is consumed by groupings`,
		},
		"member names nothing and is not declared invented": {
			mutate: func(rules map[string]any) {
				delete(groupingMembers(rules)[0].(map[string]any), "structural_name")
			},
			want: "names no structural field and is not declared invented",
		},
		"invented member also claims an observed field": {
			mutate: func(rules map[string]any) {
				member := groupingMembers(rules)[0].(map[string]any)
				member["invented"] = "computed from whether a group is set"
			},
			want: "declared invented and also names structural field",
		},
		"grouping hand-authors its member list": {
			mutate: func(rules map[string]any) {
				firstGrouping(rules)["attribute"] = map[string]any{
					"computed_optional_required": "optional",
					"attributes":                 []any{},
				}
			},
			want: "hand-authors \"attributes\"; members come from its declared member list",
		},
		"grouping declares no nesting kind": {
			mutate: func(rules map[string]any) {
				delete(firstGrouping(rules), "terraform_type")
			},
			want: "must declare terraform_type as single_nested, list_nested or set_nested",
		},
		"grouping declares a scalar kind": {
			mutate: func(rules map[string]any) {
				firstGrouping(rules)["terraform_type"] = "string"
			},
			want: `declares terraform_type "string", which is not a nested member`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Compile(groupingInput(t, test.mutate))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want %q", err, test.want)
			}
		})
	}
}

// An invented attribute corresponds to no observed field and must say so.
// port_forward's source_limiting.type and bgp's peers are the two in the
// estate; both are computed by the provider from something the catalog cannot
// describe, so they are admitted by declaration rather than by weakening the
// observed-fields guard.
func TestCompileAdmitsAnInventedMemberOnlyWhenDeclared(t *testing.T) {
	declared := func(rules map[string]any) {
		firstGrouping(rules)["members"] = append(groupingMembers(rules), map[string]any{
			"terraform_name": "kind", "terraform_type": "string", "disposition": "managed",
			"invented":  "computed from whether a firewall group is set; no wire field carries it",
			"attribute": map[string]any{"computed_optional_required": "computed"},
		})
	}
	result, err := Compile(groupingInput(t, declared))
	if err != nil {
		t.Fatalf("Compile() rejected a declared invented member: %v", err)
	}
	attribute := collectionAttribute(t, result.ProviderCodeSpec, "endpoint")
	var definition struct {
		Attributes []map[string]json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(attribute["single_nested"], &definition); err != nil {
		t.Fatal(err)
	}
	if len(definition.Attributes) != 3 {
		t.Fatalf("grouping has %d members, want 3 with the invented one", len(definition.Attributes))
	}

	// Without a type there is nothing to emit, since no observed field supplies one.
	_, err = Compile(groupingInput(t, func(rules map[string]any) {
		declared(rules)
		members := groupingMembers(rules)
		delete(members[len(members)-1].(map[string]any), "terraform_type")
	}))
	if err == nil || !strings.Contains(err.Error(), "must declare terraform_type") {
		t.Fatalf("Compile() error = %v, want a missing type failure", err)
	}
}

// The mapping report must not claim an invented member came from the wire.
func TestMappingRecordsAnInventedMemberAsInvented(t *testing.T) {
	result, err := Compile(groupingInput(t, func(rules map[string]any) {
		firstGrouping(rules)["members"] = append(groupingMembers(rules), map[string]any{
			"terraform_name": "kind", "terraform_type": "string", "disposition": "managed",
			"invented":  "computed by the provider",
			"attribute": map[string]any{"computed_optional_required": "computed"},
		})
	}))
	if err != nil {
		t.Fatal(err)
	}
	var mapping struct {
		Fields []struct {
			TerraformName  string `json:"terraform_name"`
			StructuralName string `json:"structural_name"`
			StructuralType string `json:"structural_type"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(result.MappingReport, &mapping); err != nil {
		t.Fatal(err)
	}
	for _, field := range mapping.Fields {
		if field.TerraformName != "endpoint.kind" {
			continue
		}
		if field.StructuralName != "" || field.StructuralType != "invented" {
			t.Fatalf("invented member recorded as structural %q/%q, want empty and \"invented\"",
				field.StructuralName, field.StructuralType)
		}
		return
	}
	t.Fatal("mapping report does not mention the invented member")
}

// flatteningInput models power_supervisor's real shape: an observed nested
// struct whose members the schema presents as top-level attributes.
func flatteningInput(t *testing.T, mutate func(rules map[string]any)) CompileInput {
	t.Helper()
	names := append(dnsFieldNames(), "settings")
	rules := testPolicyObject(names, testSpecificationDigest)

	kept := []any{}
	for _, raw := range rules["fields"].([]any) {
		field := raw.(map[string]any)
		if field["structural_name"] == "settings" {
			continue
		}
		kept = append(kept, field)
	}
	rules["fields"] = kept
	rules["flattenings"] = []any{
		map[string]any{
			"structural_name": "settings",
			"members": []any{
				map[string]any{
					"structural_name": "heartbeat_interval", "terraform_name": "heartbeat_interval",
					"disposition": "managed",
					"attribute":   map[string]any{"computed_optional_required": "computed_optional"},
				},
				map[string]any{
					"structural_name": "silence_threshold", "terraform_name": "silence_threshold",
					"disposition": "managed",
					"attribute":   map[string]any{"computed_optional_required": "computed_optional"},
				},
			},
		},
	}

	var source map[string]any
	if err := json.Unmarshal(testBootstrap(t, names), &source); err != nil {
		t.Fatal(err)
	}
	for _, raw := range source["resource"].(map[string]any)["fields"].([]any) {
		field := raw.(map[string]any)
		if field["name"] != "settings" {
			continue
		}
		field["type"] = "object"
		field["fields"] = []any{
			map[string]any{"name": "heartbeat_interval", "type": "int64"},
			map[string]any{"name": "silence_threshold", "type": "int64"},
		}
	}
	if mutate != nil {
		mutate(rules)
	}
	return CompileInput{
		Bootstrap:       mustJSON(t, source),
		Policy:          mustJSON(t, rules),
		BaselineDigests: testBaseline(t),
		Ledger:          testLedger(t, catalogparity.Admitted),
	}
}

func firstFlattening(rules map[string]any) map[string]any {
	return rules["flattenings"].([]any)[0].(map[string]any)
}

func flattenedMembers(rules map[string]any) []any {
	return firstFlattening(rules)["members"].([]any)
}

// A flattened member becomes a top-level attribute, taking its type from the
// catalog member it promotes, and the struct itself is not emitted.
func TestCompileFlattensAnObservedStructOutward(t *testing.T) {
	result, err := Compile(flatteningInput(t, nil))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for _, name := range []string{"heartbeat_interval", "silence_threshold"} {
		attribute := collectionAttribute(t, result.ProviderCodeSpec, name)
		if _, ok := attribute["int64"]; !ok {
			t.Fatalf("%s is %v, want int64 taken from the promoted member", name, attributeMembers(attribute))
		}
	}
	var document struct {
		Resources []struct {
			Schema struct {
				Attributes []map[string]json.RawMessage `json:"attributes"`
			} `json:"schema"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(result.ProviderCodeSpec, &document); err != nil {
		t.Fatal(err)
	}
	for _, attribute := range document.Resources[0].Schema.Attributes {
		var name string
		if err := json.Unmarshal(attribute["name"], &name); err != nil {
			t.Fatal(err)
		}
		if name == "settings" {
			t.Fatal("the spread struct is still emitted as an attribute of its own")
		}
	}
}

// The accounting, in the other direction. A member may not be promoted twice,
// dropped without a decision, or promoted alongside the struct itself.
func TestCompileRejectsFlatteningsThatLoseTrackOfMembers(t *testing.T) {
	tests := map[string]struct {
		mutate func(rules map[string]any)
		want   string
	}{
		"member the object does not carry": {
			mutate: func(rules map[string]any) {
				flattenedMembers(rules)[0].(map[string]any)["structural_name"] = "nonexistent"
			},
			want: `promotes "nonexistent", which that object does not carry`,
		},
		"member left undecided": {
			mutate: func(rules map[string]any) {
				firstFlattening(rules)["members"] = []any{flattenedMembers(rules)[0]}
			},
			want: `leaves member "silence_threshold" undecided: promote it or omit it`,
		},
		"member promoted twice": {
			mutate: func(rules map[string]any) {
				members := flattenedMembers(rules)
				duplicate := map[string]any{
					"structural_name": "heartbeat_interval", "terraform_name": "again",
					"disposition": "managed",
					"attribute":   map[string]any{"computed_optional_required": "optional"},
				}
				firstFlattening(rules)["members"] = append(members, duplicate)
			},
			want: `promotes "heartbeat_interval" twice`,
		},
		"struct also classified at the top level": {
			mutate: func(rules map[string]any) {
				rules["fields"] = append(rules["fields"].([]any), map[string]any{
					"structural_name": "settings", "terraform_name": "settings_too",
					"disposition": "managed",
					"attribute":   map[string]any{"computed_optional_required": "optional"},
				})
			},
			want: "spread by a flattening and also classified at the top level",
		},
		"spreads something that is not an object": {
			mutate: func(rules map[string]any) {
				firstFlattening(rules)["structural_name"] = "enabled"
				firstFlattening(rules)["members"] = []any{map[string]any{
					"structural_name": "x", "terraform_name": "x", "disposition": "managed",
				}}
			},
			want: `which is type "bool" rather than an object`,
		},
		"spreads something unobserved": {
			mutate: func(rules map[string]any) {
				firstFlattening(rules)["structural_name"] = "nonexistent"
			},
			want: `spreads "nonexistent", which the catalog does not observe`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Compile(flatteningInput(t, test.mutate))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want %q", err, test.want)
			}
		})
	}
}

// Omitting a member is a decision and must be allowed, so long as it is stated.
func TestCompileAcceptsAnOmittedFlattenedMember(t *testing.T) {
	result, err := Compile(flatteningInput(t, func(rules map[string]any) {
		flattenedMembers(rules)[1].(map[string]any)["disposition"] = "omitted"
	}))
	if err != nil {
		t.Fatalf("Compile() rejected an omitted member: %v", err)
	}
	var document struct {
		Resources []struct {
			Schema struct {
				Attributes []map[string]json.RawMessage `json:"attributes"`
			} `json:"schema"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(result.ProviderCodeSpec, &document); err != nil {
		t.Fatal(err)
	}
	for _, attribute := range document.Resources[0].Schema.Attributes {
		var name string
		if err := json.Unmarshal(attribute["name"], &name); err != nil {
			t.Fatal(err)
		}
		if name == "silence_threshold" {
			t.Fatal("an omitted member was still emitted")
		}
	}
}

const dnsDataSourceBaselineDigest = "e9217234de7678441bcdd4db0fd285d32d6856ddaf9748cc2b69cb7b82f44646"

// dataSourceBaseline mirrors the committed ledger's data_source digest for
// unifi_dns_record so admission's per-surface digest check lines up.
func dataSourceBaseline(t *testing.T) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"schema_sha256": map[string]any{
			"data_source_schemas.unifi_dns_record": dnsDataSourceBaselineDigest,
		},
	})
}

// surfaceKindInput builds a self-consistent compile input for one surface
// kind: the policy, the baseline manifest and the ledger all agree, so a
// failure is attributable to the code under test rather than to the fixture.
func surfaceKindInput(t *testing.T, kind catalogparity.SurfaceKind) CompileInput {
	t.Helper()
	baseline := dataSourceBaseline(t)

	rules := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	rules["surface_kind"] = string(kind)
	rules["generator_name"] = "dns_record"
	rules["baseline_digests"] = map[string]any{
		"resource":    dnsDataSourceBaselineDigest,
		"data_source": dnsDataSourceBaselineDigest,
	}

	data, err := os.ReadFile("../../provider-codegen/generated/catalog-parity-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	var ledger catalogparity.Ledger
	if err := json.Unmarshal(data, &ledger); err != nil {
		t.Fatal(err)
	}
	ledger.BaselineSHA256 = byteDigest(baseline)
	for index := range ledger.Entries {
		entry := &ledger.Entries[index]
		if entry.Kind != kind || entry.Name != "unifi_dns_record" {
			continue
		}
		entry.State = catalogparity.GeneratedShadow
		entry.ReceiptSHA256 = ""
		entry.Migration = testMigrationReason
		entry.Implementation = "shadow"
		entry.BaselineSchemaSHA256 = dnsDataSourceBaselineDigest
	}
	return CompileInput{
		Bootstrap:       testBootstrap(t, dnsFieldNames()),
		Policy:          mustJSON(t, rules),
		BaselineDigests: baseline,
		Ledger:          mustJSON(t, ledger),
	}
}

// A data source must be emitted under the specification's "datasources"
// member. Emitted under "resources" the generator accepts the file, generates
// nothing, and go generate stays green, so the mistake only appears several
// steps later as an undefined symbol.
func TestCompileEmitsDataSourceUnderDataSourcesMember(t *testing.T) {
	result, err := Compile(surfaceKindInput(t, catalogparity.DataSource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	var specification struct {
		Resources []struct {
			Name string `json:"name"`
		} `json:"resources"`
		DataSources []struct {
			Name string `json:"name"`
		} `json:"datasources"`
	}
	if err := json.Unmarshal(result.ProviderCodeSpec, &specification); err != nil {
		t.Fatal(err)
	}
	if len(specification.DataSources) != 1 || specification.DataSources[0].Name != "dns_record" {
		t.Fatalf("datasources = %+v, want one entry named dns_record", specification.DataSources)
	}
	if len(specification.Resources) != 0 {
		t.Fatalf("resources = %+v, want none for a data source", specification.Resources)
	}
}

// A surface kind with no emission path must fail loudly. Emitting it under
// whichever member happens to exist is the silent-wrong-answer case.
func TestCompileRejectsSurfaceKindsWithoutAnEmissionPath(t *testing.T) {
	for _, kind := range []catalogparity.SurfaceKind{
		catalogparity.ListResource,
		catalogparity.Action,
	} {
		t.Run(string(kind), func(t *testing.T) {
			_, err := Compile(surfaceKindInput(t, kind))
			if err == nil || !strings.Contains(err.Error(), "no code specification member") {
				t.Fatalf("Compile() error = %v, want a missing emission path failure", err)
			}
		})
	}
}

func TestValidateBaselineTreatsCompanionsAsPerSurfaceOptional(t *testing.T) {
	const (
		resource = "aaaa000000000000000000000000000000000000000000000000000000000000"
		identity = "bbbb000000000000000000000000000000000000000000000000000000000000"
		list     = "cccc000000000000000000000000000000000000000000000000000000000000"
	)
	managed := catalogparity.SurfaceKey{Kind: catalogparity.ManagedResource, Name: "unifi_bgp"}

	tests := map[string]struct {
		expected baselineDigestSet
		actual   map[string]string
		want     string
	}{
		// unifi_bgp has neither companion. Before this, it failed with
		// "baseline identity digest mismatch" and sent the reader looking for
		// a digest to regenerate that never existed.
		"no companions declared or present": {
			expected: baselineDigestSet{Resource: resource},
			actual:   map[string]string{"resource_schemas.unifi_bgp": resource},
		},
		"both companions declared and present": {
			expected: baselineDigestSet{Resource: resource, Identity: identity, ListResource: list},
			actual: map[string]string{
				"resource_schemas.unifi_bgp":          resource,
				"resource_identity_schemas.unifi_bgp": identity,
				"list_resource_schemas.unifi_bgp":     list,
			},
		},
		"companion present but undeclared": {
			expected: baselineDigestSet{Resource: resource},
			actual: map[string]string{
				"resource_schemas.unifi_bgp":          resource,
				"resource_identity_schemas.unifi_bgp": identity,
			},
			want: "declares no identity schema for unifi_bgp but the baseline has one",
		},
		"companion declared but absent": {
			expected: baselineDigestSet{Resource: resource, ListResource: list},
			actual:   map[string]string{"resource_schemas.unifi_bgp": resource},
			want:     "declares a list resource schema for unifi_bgp but the baseline has none",
		},
		"companion declared and stale": {
			expected: baselineDigestSet{Resource: resource, Identity: identity},
			actual: map[string]string{
				"resource_schemas.unifi_bgp":          resource,
				"resource_identity_schemas.unifi_bgp": list,
			},
			want: "baseline identity digest mismatch for unifi_bgp",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateBaseline(test.expected, test.actual, managed)
			if test.want == "" {
				if err != nil {
					t.Fatalf("validateBaseline() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateBaseline() error = %v, want %q", err, test.want)
			}
		})
	}
}

// A data source has no identity or list companion, so it must be validated
// against its own schema key rather than the managed resource's.
func TestValidateBaselineUsesTheSurfaceKindsOwnKey(t *testing.T) {
	const digest = "dddd000000000000000000000000000000000000000000000000000000000000"
	key := catalogparity.SurfaceKey{Kind: catalogparity.DataSource, Name: "unifi_dns_record"}
	expected := baselineDigestSet{DataSource: digest}
	actual := map[string]string{"data_source_schemas.unifi_dns_record": digest}
	if err := validateBaseline(expected, actual, key); err != nil {
		t.Fatalf("validateBaseline() error = %v, want nil", err)
	}
	// The managed resource digest must not stand in for the data source one.
	if err := validateBaseline(
		baselineDigestSet{Resource: digest},
		map[string]string{"resource_schemas.unifi_dns_record": digest},
		key,
	); err == nil {
		t.Fatal("validateBaseline() accepted a managed resource digest for a data source")
	}
}

// admissionGateInput pairs the synthetic baseline with the synthetic ledger.
// pinnedDNSInput reads the committed baseline digests instead, which never
// match testLedger's baseline, so Compile fails on the digest comparison
// before it ever reaches the state gate.
func admissionGateInput(t *testing.T, state catalogparity.AdmissionState) CompileInput {
	t.Helper()
	return CompileInput{
		Bootstrap:       testBootstrap(t, dnsFieldNames()),
		Policy:          testPolicy(t, dnsFieldNames(), testSpecificationDigest),
		BaselineDigests: testBaseline(t),
		Ledger:          testLedger(t, state),
	}
}

func TestCompileRequiresAdmittedLedgerEntry(t *testing.T) {
	_, err := Compile(admissionGateInput(t, catalogparity.LegacyAuthoritative))
	// Assert the state gate's own wording. "admission" alone also matches the
	// baseline digest mismatch, which would let this pass without the gate
	// running at all.
	if err == nil || !strings.Contains(err.Error(), "state is") {
		t.Fatalf("Compile() error = %v, want admission state failure", err)
	}
}

// A shadow candidate is the first thing the compiler emits for an existing
// resource, so requiring admission to compile could never be satisfied: the
// receipt admission wants comes from a campaign that diffs the very binary
// compiling produces.
func TestCompileAcceptsGeneratedShadowLedgerEntry(t *testing.T) {
	if _, err := Compile(admissionGateInput(t, catalogparity.GeneratedShadow)); err != nil {
		t.Fatalf("Compile() rejected a generated shadow surface: %v", err)
	}
}

// The gate keys on the specific state, not on "anything the ledger calls a
// shadow implementation". ShadowOnly and AdapterParity both map to
// implementation "shadow" via implementationForState, so without this a future
// refactor could widen the gate to every shadow state without a test noticing.
func TestCompileRejectsStatesBelowGeneratedShadow(t *testing.T) {
	for name, state := range map[string]catalogparity.AdmissionState{
		"baseline":             catalogparity.BaselineState,
		"cataloged":            catalogparity.Cataloged,
		"policy complete":      catalogparity.PolicyComplete,
		"legacy authoritative": catalogparity.LegacyAuthoritative,
		"shadow only":          catalogparity.ShadowOnly,
		"adapter parity":       catalogparity.AdapterParity,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Compile(admissionGateInput(t, state))
			if err == nil || !strings.Contains(err.Error(), "state is") {
				t.Fatalf("Compile() error = %v, want admission state failure for %q", err, state)
			}
		})
	}
}

func TestCompileRejectsMissingOrMismatchedLedger(t *testing.T) {
	tests := map[string]func(*CompileInput){
		"missing": func(input *CompileInput) {
			input.Ledger = nil
		},
		"shadow": func(input *CompileInput) {
			input.Ledger = testLedger(t, catalogparity.ShadowOnly)
		},
		"baseline digest": func(input *CompileInput) {
			var ledger catalogparity.Ledger
			if err := json.Unmarshal(input.Ledger, &ledger); err != nil {
				t.Fatal(err)
			}
			for index := range ledger.Entries {
				if ledger.Entries[index].Kind == catalogparity.ManagedResource && ledger.Entries[index].Name == "unifi_dns_record" {
					ledger.Entries[index].BaselineSchemaSHA256 = strings.Repeat("0", 64)
				}
			}
			input.Ledger = mustJSON(t, ledger)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := pinnedDNSInput(t)
			mutate(&input)
			if _, err := Compile(input); err == nil {
				t.Fatal("Compile() succeeded")
			}
		})
	}
}

func TestCompileReportsManagedResourceKind(t *testing.T) {
	result, err := Compile(pinnedDNSInput(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, report := range map[string][]byte{
		"impact":  result.ImpactReport,
		"mapping": result.MappingReport,
	} {
		if !bytes.Contains(report, []byte(`"surface_kind": "managed_resource"`)) || !bytes.Contains(report, []byte(`"surface_name": "unifi_dns_record"`)) {
			t.Fatalf("%s report lacks surface identity: %s", name, report)
		}
	}
}

func pinnedDNSInput(t *testing.T) CompileInput {
	t.Helper()
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	return CompileInput{
		Catalog:         read("../../provider-codegen/catalog/go-unifi-v1.102.0-dns-record.catalog.json"),
		Policy:          read("../../provider-codegen/policy/dns_record.json"),
		BaselineDigests: read("../../build/m0/provider-schema-digests.json"),
		Ledger:          read("../../provider-codegen/generated/catalog-parity-ledger.json"),
	}
}

// testMigrationReason stands in for the declaration a real in-flight surface
// carries. Its content does not matter to the compiler, only that the ledger
// is valid without it having been made valid by loosening the rule.
const testMigrationReason = "fixture surface held at a migration waypoint"

func testLedger(t *testing.T, state catalogparity.AdmissionState) []byte {
	t.Helper()
	data, err := os.ReadFile("../../provider-codegen/generated/catalog-parity-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	var ledger catalogparity.Ledger
	if err := json.Unmarshal(data, &ledger); err != nil {
		t.Fatal(err)
	}
	ledger.BaselineSHA256 = byteDigest(testBaseline(t))
	for index := range ledger.Entries {
		entry := &ledger.Entries[index]
		if entry.Kind != catalogparity.ManagedResource || entry.Name != "unifi_dns_record" {
			continue
		}
		entry.State = state
		entry.Migration = ""
		switch state {
		case catalogparity.Admitted, catalogparity.ContractParity, catalogparity.ReleaseReady:
			entry.ReceiptSHA256 = strings.Repeat("a", 64)
			entry.Implementation = "candidate"
		case catalogparity.GeneratedShadow, catalogparity.AdapterParity, catalogparity.ShadowOnly:
			entry.ReceiptSHA256 = ""
			entry.Implementation = "shadow"
		default:
			entry.ReceiptSHA256 = ""
			entry.Implementation = "legacy"
		}
		// An in-flight state is only valid when it says why, so a fixture that
		// wants to reach the compiler's own gate has to declare too. Without
		// this the ledger is rejected first and the admission tests below pass
		// on the wrong error.
		if state == catalogparity.GeneratedShadow || state == catalogparity.AdapterParity {
			entry.Migration = testMigrationReason
		}
	}
	return mustJSON(t, ledger)
}

func testCatalog(t *testing.T, fieldNames []string) []byte {
	t.Helper()
	structural := make([]any, 0, len(fieldNames))
	observed := make([]any, 0, len(fieldNames))
	coverage := make([]any, 0, len(fieldNames))
	for _, name := range fieldNames {
		fieldType := "int64"
		switch name {
		case "enabled":
			fieldType = "bool"
		case "key", "record_type", "value":
			fieldType = "string"
		}
		id := "unifi.network.dns_record.field." + name
		structural = append(structural, map[string]any{
			"id":                id,
			"field":             name,
			"type":              fieldType,
			"definition_sha256": expectedCatalogDefinitionDigest(t, name, fieldType, false),
			"secret_candidate":  false,
		})
		observed = append(observed, map[string]any{
			"id":             id,
			"field":          name,
			"json_type":      fieldType,
			"present_count":  1,
			"non_null_count": 1,
		})
		coverage = append(coverage, map[string]any{"id": id, "state": "observed"})
	}
	return mustJSON(t, map[string]any{
		"format_version": 1,
		"catalog_id":     "unifi.network.dns_record@10.4.57",
		"target":         testCatalogTarget(),
		"sources": map[string]any{
			"capture_lock_sha256":          strings.Repeat("4", 64),
			"specification_sha256":         testSpecificationDigest,
			"structural_projection_sha256": strings.Repeat("5", 64),
			"semantic_predecessor_sha256":  strings.Repeat("6", 64),
			"semantic_ids_sha256":          strings.Repeat("7", 64),
		},
		"structural_records": structural,
		"observed_records":   observed,
		"conflicts":          []any{},
		"coverage":           coverage,
		"admission": map[string]any{
			"state":            "admitted",
			"operation_digest": "operation-digest",
		},
	})
}

func expectedCatalogDefinitionDigest(t *testing.T, field, fieldType string, secretCandidate bool) string {
	t.Helper()
	definition, err := json.Marshal(struct {
		WireName        string `json:"wire_name"`
		JSONType        string `json:"json_type"`
		SecretCandidate bool   `json:"secret_candidate"`
	}{field, fieldType, secretCandidate})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(definition)
	return fmt.Sprintf("%x", digest)
}

func rehashCatalogDefinition(t *testing.T, record map[string]any) {
	t.Helper()
	record["definition_sha256"] = expectedCatalogDefinitionDigest(
		t,
		record["field"].(string),
		record["type"].(string),
		record["secret_candidate"].(bool),
	)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
