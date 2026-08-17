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

// PROVEN TO FAIL, recorded here rather than only in the commit that proved it.
// The exactly-once accounting catches a field ADDED to the SDK and a field
// REMOVED from it -- both confirmed by mutation -- because either leaves a field
// unclassified or a policy entry stale. (11bd12cb)
//
// THE LIMIT, from the same commit and worth as much as the proof: a RETYPED
// field is still present and still classified, so it passes. The source digest
// does not reach it either. That hole was closed separately by refusing an
// override that changes cardinality, after a field declared as a list over a
// field the SDK observes as a scalar compiled clean.
//
// WHAT THESE TESTS DO NOT COVER: 44 of the 47 are fixture-driven, and only
// dns_record is compiled from a real committed policy. The other 61 surfaces are
// compiled solely by `go generate ./...` under CI's git-diff gate -- and that
// job's paths filter does not fire on a JSON-only policy edit.

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
				fields := jsonArray(policy["fields"])
				jsonObject(fields[0])["terraform_name"] = "name"
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
				jsonObject(catalog["target"])["name"] = "different-target"
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
				jsonObject(catalog["sources"])["capture_lock_sha256"] = strings.Repeat("8", 64)
			},
			want: "catalog source digests do not match provider policy",
		},
		"incomplete coverage": {
			mutate: func(catalog map[string]any) {
				catalog["coverage"] = jsonArray(catalog["coverage"])[1:]
			},
			want: "incomplete catalog coverage",
		},
		"missing observed record": {
			mutate: func(catalog map[string]any) {
				catalog["observed_records"] = jsonArray(catalog["observed_records"])[1:]
			},
			want: "missing observed record",
		},
		"duplicate observed semantic ID": {
			mutate: func(catalog map[string]any) {
				records := jsonArray(catalog["observed_records"])
				jsonObject(records[1])["id"] = jsonObject(records[0])["id"]
			},
			want: "duplicate observed semantic ID",
		},
		"duplicate policy semantic ID": {
			mutate: func(catalog map[string]any) {},
			want:   "duplicate policy semantic ID",
		},
		"unknown observed semantic ID": {
			mutate: func(catalog map[string]any) {
				records := jsonArray(catalog["observed_records"])
				jsonObject(records[0])["id"] = "unifi.network.dns_record.field.unknown"
			},
			want: "unknown observed semantic ID",
		},
		"definition digest disagreement": {
			mutate: func(catalog map[string]any) {
				jsonObject(jsonArray(catalog["structural_records"])[0])["definition_sha256"] = strings.Repeat("0", 64)
			},
			want: "definition digest mismatch",
		},
		"observed type disagreement": {
			mutate: func(catalog map[string]any) {
				jsonObject(jsonArray(catalog["observed_records"])[0])["json_type"] = "string"
			},
			want: "observed type mismatch",
		},
		"uncovered field": {
			mutate: func(catalog map[string]any) {
				jsonObject(jsonArray(catalog["coverage"])[0])["state"] = "not_observed"
			},
			want: "incomplete catalog coverage",
		},
		"unsafe secret candidate": {
			mutate: func(catalog map[string]any) {
				jsonObject(jsonArray(catalog["structural_records"])[0])["secret_candidate"] = true
				rehashCatalogDefinition(t, jsonObject(jsonArray(catalog["structural_records"])[0]))
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
				jsonObject(jsonArray(catalog["structural_records"])[0])["id"] = "dns.enabled"
			},
			want: "unstable catalog ID",
		},
		"unpinned operation": {
			mutate: func(catalog map[string]any) {
				jsonObject(catalog["admission"])["operation_digest"] = "different-operation"
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
				fields := jsonArray(policy["fields"])
				jsonObject(fields[1])["semantic_id"] = jsonObject(fields[0])["semantic_id"]
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
	for _, raw := range jsonArray(rules["fields"]) {
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
	for _, raw := range jsonArray(rules["fields"]) {
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
	for _, raw := range jsonArray(jsonObject(source["resource"])["fields"]) {
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
			want:           "must declare terraform_type as list_nested, set_nested, list_nested_block or set_nested_block",
		},
		"single object declared as a collection": {
			structuralType: "object",
			mutate:         func(field map[string]any) { field["terraform_type"] = "list_nested" },
			want:           `declares terraform_type "list_nested", want single_nested`,
		},
		"member the catalog does not observe": {
			structuralType: "object",
			mutate: func(field map[string]any) {
				field["fields"] = append(jsonArray(field["fields"]), map[string]any{
					"structural_name": "ghost", "terraform_name": "ghost", "disposition": "managed",
					"attribute": map[string]any{"computed_optional_required": "optional"},
				})
			},
			want: `policy for member "ghost" that the catalog does not observe`,
		},
		"unclassified member": {
			structuralType: "object",
			mutate: func(field map[string]any) {
				field["fields"] = []any{jsonArray(field["fields"])[0]}
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
	for _, raw := range jsonArray(rules["fields"]) {
		field := jsonObject(raw)
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
	return jsonObject(jsonArray(rules["groupings"])[0])
}

func groupingMembers(rules map[string]any) []any {
	return jsonArray(firstGrouping(rules)["members"])
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
				jsonObject(groupingMembers(rules)[0])["structural_name"] = "nonexistent"
			},
			want: `consumes "nonexistent", which the catalog does not observe`,
		},
		"member claims a field also classified at the top level": {
			mutate: func(rules map[string]any) {
				rules["fields"] = append(jsonArray(rules["fields"]), map[string]any{
					"structural_name": "port", "semantic_id": "unifi.network.dns_record.field.port",
					"terraform_name": "port_again", "disposition": "managed",
					"attribute": map[string]any{"computed_optional_required": "optional"},
				})
			},
			want: "consumed by grouping \"endpoint\" and also classified at the top level",
		},
		"two groupings consume the same field": {
			mutate: func(rules map[string]any) {
				rules["groupings"] = append(jsonArray(rules["groupings"]), map[string]any{
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
		// The same conflict WITHIN one grouping. Told apart from the case
		// above because "consumed by groupings "endpoint" and "endpoint""
		// sends the reader looking for a second grouping that is not there,
		// and because the two have different fixes: one is a duplicated
		// member, the other is two groupings disagreeing about who owns a
		// field.
		"one grouping consumes the same field in two members": {
			mutate: func(rules map[string]any) {
				members := groupingMembers(rules)
				jsonObject(members[1])["structural_name"] = "port"
				firstGrouping(rules)["members"] = members
			},
			want: `grouping "endpoint" consumes structural field "port" twice, in members "port" and "priority"`,
		},
		"member names nothing and is not declared invented": {
			mutate: func(rules map[string]any) {
				delete(jsonObject(groupingMembers(rules)[0]), "structural_name")
			},
			want: "names no structural field, is not declared invented, and is not named by any claim",
		},
		"invented member also claims an observed field": {
			mutate: func(rules map[string]any) {
				member := jsonObject(groupingMembers(rules)[0])
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
		delete(jsonObject(members[len(members)-1]), "terraform_type")
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

// oneToManyInput folds both grouped fields into ONE member through a claim,
// which is the shape traffic_route's destination.ip has over ip_addresses and
// ip_ranges.
func oneToManyInput(t *testing.T, mutate func(rules map[string]any)) CompileInput {
	t.Helper()
	return groupingInput(t, func(rules map[string]any) {
		member := jsonObject(groupingMembers(rules)[0])
		delete(member, "structural_name")
		member["terraform_type"] = "string"
		firstGrouping(rules)["members"] = []any{member}
		rules["claims"] = []any{testClaim([]any{"endpoint.port"}, []any{"port", "priority"})}
		if mutate != nil {
			mutate(rules)
		}
	})
}

// manyToOneInput keeps BOTH members and relates them to ONE field, which is the
// shape traffic_route's source.{clients,networks} has over target_devices and
// vpn_server's {wireguard.port, openvpn.port} has over local_port.
func manyToOneInput(t *testing.T, mutate func(rules map[string]any)) CompileInput {
	t.Helper()
	return groupingInput(t, func(rules map[string]any) {
		for _, raw := range groupingMembers(rules) {
			member := jsonObject(raw)
			delete(member, "structural_name")
			member["terraform_type"] = "string"
		}
		rules["fields"] = append(jsonArray(rules["fields"]), map[string]any{
			"structural_name": "priority", "semantic_id": "unifi.network.dns_record.field.priority",
			"terraform_name": "priority_at_top", "disposition": "managed",
			"attribute": map[string]any{"computed_optional_required": "optional"},
		})
		rules["claims"] = []any{
			testClaim([]any{"endpoint.port", "endpoint.priority"}, []any{"port"}),
		}
		if mutate != nil {
			mutate(rules)
		}
	})
}

func testClaim(members, fields []any) map[string]any {
	return map[string]any{
		"terraform_members": members,
		"structural_names":  fields,
		"mapping": map[string]any{
			"to_api": "splitEndpoint", "from_api": "joinEndpoint", "kind": "dedicated",
		},
		"reason": "the released attribute set does not correspond one to one with the wire",
	}
}

func firstClaim(rules map[string]any) map[string]any {
	return jsonObject(jsonArray(rules["claims"])[0])
}

// dropTopLevelField frees a structural field for a second claim to consume.
func dropTopLevelField(rules map[string]any, names ...string) {
	drop := map[string]bool{}
	for _, name := range names {
		drop[name] = true
	}
	kept := []any{}
	for _, raw := range jsonArray(rules["fields"]) {
		if field, ok := raw.(map[string]any); ok && drop[fmt.Sprint(field["structural_name"])] {
			continue
		}
		kept = append(kept, raw)
	}
	rules["fields"] = kept
}

// bothMembersInput restores endpoint's two members, each claimed, so a test can
// exercise a conflict BETWEEN claims rather than within one.
func bothMembersInput(t *testing.T, mutate func(rules map[string]any)) CompileInput {
	t.Helper()
	return groupingInput(t, func(rules map[string]any) {
		for _, raw := range groupingMembers(rules) {
			member := jsonObject(raw)
			delete(member, "structural_name")
			member["terraform_type"] = "string"
		}
		mutate(rules)
	})
}

// A claimed member has no one observed field behind it, so the policy declares
// its type and the compiler does not compare one -- there is no comparison that
// holds across the estate's three cases: a PARTITION (traffic_route's ip over
// ip_addresses and ip_ranges), a BROADCAST (vpn_server's wan.ip over three
// *_local_wan_ip fields), and a SPLIT ACROSS MEMBERS (source.clients and
// source.networks over the one target_devices).
func TestCompileConstructsAClaimedMember(t *testing.T) {
	result, err := Compile(oneToManyInput(t, nil))
	if err != nil {
		t.Fatalf("Compile() rejected a claimed member: %v", err)
	}
	attribute := collectionAttribute(t, result.ProviderCodeSpec, "endpoint")
	var definition struct {
		Attributes []map[string]json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(attribute["single_nested"], &definition); err != nil {
		t.Fatal(err)
	}
	if len(definition.Attributes) != 1 {
		t.Fatalf("grouping has %d members, want 1", len(definition.Attributes))
	}
	// The DECLARED type, not either observed one: both fields are int64 and the
	// member is a string. Taking a type from the catalog here would have to pick
	// one of the two fields, and neither is more right than the other.
	if _, declared := definition.Attributes[0]["string"]; !declared {
		t.Fatalf("member emitted as %v, want the declared string type",
			attributeMembers(definition.Attributes[0]))
	}
}

// TWO members over ONE field, which no member-scoped declaration reaches: under
// one the two members would each assert the field and collide, and the policy
// would have no place to say they belong together.
func TestCompileConstructsTwoMembersOverOneField(t *testing.T) {
	result, err := Compile(manyToOneInput(t, nil))
	if err != nil {
		t.Fatalf("Compile() rejected two members over one field: %v", err)
	}
	attribute := collectionAttribute(t, result.ProviderCodeSpec, "endpoint")
	var definition struct {
		Attributes []map[string]json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(attribute["single_nested"], &definition); err != nil {
		t.Fatal(err)
	}
	if len(definition.Attributes) != 2 {
		t.Fatalf("grouping has %d members, want 2", len(definition.Attributes))
	}
}

// The failure that used to reach requireScalarOverride and report
// `field "" is observed as ""` -- naming neither the member, nor the grouping,
// nor any field. A refusal that names nothing is treated as a defect here.
func TestCompileRefusesAClaimedMemberWithNoDeclaredType(t *testing.T) {
	_, err := Compile(oneToManyInput(t, func(rules map[string]any) {
		delete(jsonObject(groupingMembers(rules)[0]), "terraform_type")
	}))
	if err == nil {
		t.Fatal("Compile() accepted a claimed member with no terraform_type")
	}
	for _, want := range []string{"endpoint.port", "must declare terraform_type", "port, priority"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Compile() error = %v, want it to name %q", err, want)
		}
	}
}

// The mapping report is where the exactly-once accounting is reviewed, so a
// claim consuming two fields owes it two rows. One row naming one field would
// leave the other invisible in the artifact -- which is a field nobody
// classified, as far as any reader can tell. The terraform side names every
// member of the claim together, because that is the fact: the field relates to
// the SET.
func TestMappingReportsEveryFieldAClaimConsumes(t *testing.T) {
	for name, test := range map[string]struct {
		input CompileInput
		want  map[string]string
	}{
		"one member over two fields": {
			input: oneToManyInput(t, nil),
			want:  map[string]string{"port": "endpoint.port", "priority": "endpoint.port"},
		},
		"two members over one field": {
			input: manyToOneInput(t, nil),
			want:  map[string]string{"port": "endpoint.port, endpoint.priority"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Compile(test.input)
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
			for structural, terraform := range test.want {
				found := false
				for _, field := range mapping.Fields {
					if field.StructuralName != structural {
						continue
					}
					found = true
					if field.TerraformName != terraform {
						t.Fatalf("%s maps to %q, want %q", structural, field.TerraformName, terraform)
					}
					if field.StructuralType != "int64" {
						t.Fatalf("%s reports observed type %q, want int64", structural, field.StructuralType)
					}
				}
				if !found {
					t.Fatalf("the mapping report has no row for %q", structural)
				}
			}
		})
	}
}

// Every way a claim can be ambiguous about what it relates, refused rather than
// defaulted. The accounting is what makes a claim a migration rather than
// hand-authoring, and it runs in BOTH directions: a field named twice and a
// member named twice are equally conflicts.
func TestCompileRejectsClaimsThatAreNotDerivations(t *testing.T) {
	tests := map[string]struct {
		mutate func(rules map[string]any)
		want   string
	}{
		"a claim with no mapping": {
			mutate: func(rules map[string]any) { delete(firstClaim(rules), "mapping") },
			want:   "declares no mapping",
		},
		"a mapping naming only the write direction": {
			mutate: func(rules map[string]any) {
				firstClaim(rules)["mapping"] = map[string]any{
					"to_api": "splitEndpoint", "kind": "dedicated",
				}
			},
			want: "declares a mapping with no from_api function",
		},
		"a mapping naming only the read direction": {
			mutate: func(rules map[string]any) {
				firstClaim(rules)["mapping"] = map[string]any{
					"from_api": "joinEndpoint", "kind": "dedicated",
				}
			},
			want: "declares a mapping with no to_api function",
		},
		// A name cannot say whether it IS the relation or merely contains it,
		// and the two are different strengths of claim. Every claim in this
		// provider is the weaker kind; leaving that unsaid is how it read as
		// the stronger one for as long as it did.
		"a mapping that does not say whether the name is the transform": {
			mutate: func(rules map[string]any) {
				delete(jsonObject(firstClaim(rules)["mapping"]), "kind")
			},
			want: "declares a mapping with no kind",
		},
		"a mapping claiming a kind that does not exist": {
			mutate: func(rules map[string]any) {
				jsonObject(firstClaim(rules)["mapping"])["kind"] = "inline"
			},
			want: `declares mapping kind "inline"`,
		},
		// NOT COVERED HERE: a to_api on a surface that never writes.
		//
		// The rule is in claimedStructuralFields and fires -- it rejects the
		// nine that client_ds, client_list_ds and network_ds carried, naming
		// each one. It has no case in this table because every input here is a
		// managed_resource and the baseline digest is keyed by surface kind:
		// flipping rules["surface_kind"] to data_source fails on "baseline
		// data_source digest mismatch" before reaching the claim at all, so
		// the case would pass for the wrong reason. Covering it needs a
		// claim-bearing data-source fixture, which surfaceKindInput does not
		// build today. Named rather than omitted, because a silently absent
		// case and a covered one look identical in a green run.
		"a claim with no reason": {
			mutate: func(rules map[string]any) { delete(firstClaim(rules), "reason") },
			want:   "declares no reason",
		},
		// A one-to-one claim is an ordinary member wearing a costume, and
		// admitting it would give the estate two ways to say the same thing.
		"a claim relating one member to one field": {
			mutate: func(rules map[string]any) {
				firstClaim(rules)["structural_names"] = []any{"port"}
				rules["fields"] = append(jsonArray(rules["fields"]), map[string]any{
					"structural_name": "priority", "semantic_id": "unifi.network.dns_record.field.priority",
					"terraform_name": "priority_at_top", "disposition": "managed",
					"attribute": map[string]any{"computed_optional_required": "optional"},
				})
			},
			want: "declare structural_name on the member instead",
		},
		"a claim listing one field twice": {
			mutate: func(rules map[string]any) {
				firstClaim(rules)["structural_names"] = []any{"port", "port"}
			},
			want: "lists structural field \"port\" twice",
		},
		"a claim naming a field the catalog does not observe": {
			mutate: func(rules map[string]any) {
				firstClaim(rules)["structural_names"] = []any{"port", "nonexistent"}
			},
			want: `consumes "nonexistent", which the catalog does not observe`,
		},
		"a claim naming a member no grouping declares": {
			mutate: func(rules map[string]any) {
				dropTopLevelField(rules, "ttl", "weight")
				rules["claims"] = append(jsonArray(rules["claims"]),
					testClaim([]any{"endpoint.nonexistent"}, []any{"ttl", "weight"}))
			},
			want: "which is neither a top-level field nor a member of any grouping",
		},
		"a claimed member that also names a field": {
			mutate: func(rules map[string]any) {
				jsonObject(groupingMembers(rules)[0])["structural_name"] = "port"
			},
			want: "the claim already says which fields it relates to",
		},
		"a claimed member that is also declared invented": {
			mutate: func(rules map[string]any) {
				jsonObject(groupingMembers(rules)[0])["invented"] = "computed by the provider"
			},
			want: "is declared invented and is also named by",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Compile(oneToManyInput(t, test.mutate))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want %q", err, test.want)
			}
		})
	}

	// The two cross-claim conflicts, which need both members present and are
	// the halves of exactly-once that a per-member declaration cannot express.
	for name, test := range map[string]struct {
		claims func(rules map[string]any)
		want   string
	}{
		// Disjoint members, overlapping fields.
		"two claims consuming the same field": {
			claims: func(rules map[string]any) {
				rules["claims"] = []any{
					testClaim([]any{"endpoint.port"}, []any{"port", "priority"}),
					testClaim([]any{"endpoint.priority"}, []any{"priority", "port"}),
				}
			},
			want: `structural field "priority" is consumed by two claims`,
		},
		// Overlapping members, disjoint fields. A member relating to the wire
		// two ways leaves the schema unable to say which.
		"two claims naming the same member": {
			claims: func(rules map[string]any) {
				dropTopLevelField(rules, "ttl", "weight")
				rules["claims"] = []any{
					testClaim([]any{"endpoint.port"}, []any{"port", "priority"}),
					testClaim([]any{"endpoint.port", "endpoint.priority"}, []any{"ttl", "weight"}),
				}
			},
			want: `terraform member "endpoint.port" is named by two claims`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Compile(bothMembersInput(t, test.claims))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want %q", err, test.want)
			}
		})
	}
}

// elementMemberInput models traffic_route's destination.domain: an observed
// array<object> presented as a list of the element's one live member.
func elementMemberInput(t *testing.T, mutate func(member map[string]any)) CompileInput {
	t.Helper()
	names := append(dnsFieldNames(), "options")
	rules := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	kept := []any{}
	for _, raw := range jsonArray(rules["fields"]) {
		field := jsonObject(raw)
		if field["structural_name"] == "port" || field["structural_name"] == "priority" {
			continue
		}
		kept = append(kept, field)
	}
	rules["fields"] = kept
	member := map[string]any{
		"structural_name": "options", "terraform_name": "options", "disposition": "managed",
		"terraform_type": "list", "element_member": "value",
		"attribute": map[string]any{
			"computed_optional_required": "optional",
			"element_type":               map[string]any{"string": map[string]any{}},
		},
		"fields": []any{
			map[string]any{"structural_name": "optionNumber", "terraform_name": "option_number",
				"disposition": "omitted"},
			map[string]any{"structural_name": "value", "terraform_name": "value",
				"disposition": "managed",
				"attribute":   map[string]any{"computed_optional_required": "optional"}},
		},
	}
	if mutate != nil {
		mutate(member)
	}
	rules["groupings"] = []any{map[string]any{
		"terraform_name": "endpoint", "terraform_type": "single_nested",
		"attribute": map[string]any{"computed_optional_required": "optional"},
		"members": []any{member, map[string]any{
			"structural_name": "port", "terraform_name": "port", "disposition": "managed",
			"attribute": map[string]any{"computed_optional_required": "optional"},
		}},
	}}
	rules["fields"] = append(jsonArray(rules["fields"]), map[string]any{
		"structural_name": "priority", "semantic_id": "unifi.network.dns_record.field.priority",
		"terraform_name": "priority", "disposition": "managed",
		"attribute": map[string]any{"computed_optional_required": "optional"},
	})
	_ = names
	return CompileInput{
		Bootstrap:       bootstrapWithObjectMember(t),
		Policy:          mustJSON(t, rules),
		BaselineDigests: testBaseline(t),
		Ledger:          testLedger(t, catalogparity.Admitted),
	}
}

// An observed array<object> presented as a list of SCALARS. Emitting it as
// list_nested instead compiles and is a different schema -- practitioners write
// domain = ["a.com"] and would have to write domain = [{domain = "a.com"}] --
// so the collapse is declared, and unlike a mapping the compiler CHECKS it.
func TestCompileCollapsesAnObjectArrayToItsElementMember(t *testing.T) {
	result, err := Compile(elementMemberInput(t, nil))
	if err != nil {
		t.Fatalf("Compile() rejected a collapsed element: %v", err)
	}
	attribute := collectionAttribute(t, result.ProviderCodeSpec, "endpoint")
	var definition struct {
		Attributes []map[string]json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(attribute["single_nested"], &definition); err != nil {
		t.Fatal(err)
	}
	for _, member := range definition.Attributes {
		if member["name"] == nil {
			continue
		}
		var name string
		if err := json.Unmarshal(member["name"], &name); err != nil {
			t.Fatal(err)
		}
		if name != "options" {
			continue
		}
		if _, ok := member["list"]; !ok {
			t.Fatalf("options emitted as %v, want a plain list", attributeMembers(member))
		}
		return
	}
	t.Fatal("the grouping does not carry the collapsed member")
}

// The three checks that separate this from a hand-written attribute, each
// refused by NAME. The redundancy between element_member and the field list is
// deliberate: derived from "whichever is not omitted", omitting a second member
// would silently change the attribute's element type.
func TestCompileRejectsACollapseTheCatalogContradicts(t *testing.T) {
	tests := map[string]struct {
		mutate func(member map[string]any)
		want   string
	}{
		"a member the element does not carry": {
			mutate: func(member map[string]any) { member["element_member"] = "nonexistent" },
			want:   `declares element_member "nonexistent", which "options" does not carry`,
		},
		"a second member left live": {
			mutate: func(member map[string]any) {
				jsonObject(jsonArray(member["fields"])[0])["disposition"] = "managed"
				jsonObject(jsonArray(member["fields"])[0])["attribute"] =
					map[string]any{"computed_optional_required": "optional"}
			},
			want: "leaves 2 member(s) not omitted (optionNumber, value); a list of scalars carries exactly one",
		},
		// The list stays declared as string while the member it now carries is
		// observed as int64. Nothing else in the policy is wrong, and only the
		// catalog can say so.
		"an element type the catalog contradicts": {
			mutate: func(member map[string]any) {
				member["element_member"] = "optionNumber"
				fields := jsonArray(member["fields"])
				jsonObject(fields[0])["disposition"] = "managed"
				jsonObject(fields[0])["attribute"] =
					map[string]any{"computed_optional_required": "optional"}
				jsonObject(fields[1])["disposition"] = "omitted"
			},
			want: `declares element type "string" but the catalog observes "options"."optionNumber" as "int64"`,
		},
		"a member of the element left undecided": {
			mutate: func(member map[string]any) {
				member["fields"] = []any{jsonArray(member["fields"])[1]}
			},
			want: `leaves member "optionNumber" undecided`,
		},
		"a collapse declared as a nested type": {
			mutate: func(member map[string]any) { member["terraform_type"] = "list_nested" },
			want:   `terraform_type "list_nested", want list or set`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Compile(elementMemberInput(t, test.mutate))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want %q", err, test.want)
			}
		})
	}
}

// A claim reaching TOP-LEVEL fields, which is network's shape: `purpose` and
// `third_party_gateway` are both computed from the one observed `purpose`, and
// neither can borrow its type -- one is a string and the other a bool.
func TestCompileConstructsClaimedTopLevelFields(t *testing.T) {
	input := groupingInput(t, func(rules map[string]any) {
		dropTopLevelField(rules, "ttl")
		rules["fields"] = append(jsonArray(rules["fields"]),
			map[string]any{
				"terraform_name": "ttl", "terraform_type": "string", "disposition": "managed",
				"attribute": map[string]any{"computed_optional_required": "optional"},
			},
			map[string]any{
				"terraform_name": "ttl_is_set", "terraform_type": "bool", "disposition": "managed",
				"attribute": map[string]any{"computed_optional_required": "computed"},
			})
		rules["claims"] = []any{testClaim([]any{"ttl", "ttl_is_set"}, []any{"ttl"})}
	})
	result, err := Compile(input)
	if err != nil {
		t.Fatalf("Compile() rejected a claim over top-level fields: %v", err)
	}
	for name, want := range map[string]string{"ttl": "string", "ttl_is_set": "bool"} {
		attribute := collectionAttribute(t, result.ProviderCodeSpec, name)
		if _, ok := attribute[want]; !ok {
			t.Fatalf("%s emitted as %v, want the declared %s", name, attributeMembers(attribute), want)
		}
	}

	// The report shows the one observed field related to BOTH attributes, in one
	// row. Two rows would double-count it against the one-row-per-field rule.
	var mapping struct {
		Fields []struct {
			TerraformName  string `json:"terraform_name"`
			StructuralName string `json:"structural_name"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(result.MappingReport, &mapping); err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, field := range mapping.Fields {
		if field.StructuralName != "ttl" {
			continue
		}
		rows++
		if field.TerraformName != "ttl, ttl_is_set" {
			t.Fatalf("ttl maps to %q, want both members named together", field.TerraformName)
		}
	}
	if rows != 1 {
		t.Fatalf("the mapping report has %d rows for ttl, want 1", rows)
	}

	// A top-level field naming nothing and claimed by nobody is a typo, not a
	// shape: it would occupy an attribute name and account for no field.
	_, err = Compile(groupingInput(t, func(rules map[string]any) {
		rules["fields"] = append(jsonArray(rules["fields"]), map[string]any{
			"terraform_name": "orphan", "terraform_type": "string", "disposition": "managed",
			"attribute": map[string]any{"computed_optional_required": "optional"},
		})
	}))
	if err == nil || !strings.Contains(err.Error(),
		`top-level field "orphan" names no structural field and is not named by any claim`) {
		t.Fatalf("Compile() error = %v, want an unclaimed top-level field to be refused", err)
	}
}

// The referee on the artifact itself. The exactly-once rule is enforced while
// the policy is read and the report is written afterwards from the same policy;
// nothing compared the two until this existed, so a reporting mistake could
// hide a field the accounting had already counted.
func TestMappingCoverageRefereeNamesWhatIsWrong(t *testing.T) {
	sourceFields := map[string]bootstrapField{
		"port":     {Name: "port", Type: "int64"},
		"priority": {Name: "priority", Type: "int64"},
	}
	complete := mappingReport{Fields: []mappingField{
		{StructuralName: "port"}, {StructuralName: "priority"},
	}}
	if err := mappingCoversEveryObservedField(complete, sourceFields, nil, nil); err != nil {
		t.Fatalf("a complete report was refused: %v", err)
	}

	tests := map[string]struct {
		report mappingReport
		want   string
	}{
		"a field with no row": {
			report: mappingReport{Fields: []mappingField{{StructuralName: "port"}}},
			want:   `no row for structural field "priority"`,
		},
		"a field reported twice": {
			report: mappingReport{Fields: []mappingField{
				{StructuralName: "port"}, {StructuralName: "port"}, {StructuralName: "priority"},
			}},
			want: `2 rows for structural field "port", want 1`,
		},
		"a row for a field the catalog does not observe": {
			report: mappingReport{Fields: []mappingField{
				{StructuralName: "port"}, {StructuralName: "priority"}, {StructuralName: "invented_by_a_typo"},
			}},
			want: `row for "invented_by_a_typo", which is neither an observed field`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := mappingCoversEveryObservedField(test.report, sourceFields, nil, nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("mappingCoversEveryObservedField() error = %v, want %q", err, test.want)
			}
		})
	}

	// A flattened field is spread rather than emitted, so its members carry the
	// rows and the parent carries none. Counting rows would refuse
	// power_supervisor, which is why this compares against a constructed
	// expectation instead.
	spread := map[string]bootstrapField{"settings": {Name: "settings", Type: "object"}}
	flattened := map[string]string{"settings": "settings"}
	flattenings := []flatteningPolicy{{
		StructuralName: "settings",
		Members:        []flattenedMember{{StructuralName: "heartbeat_interval"}},
	}}
	report := mappingReport{Fields: []mappingField{{StructuralName: "settings.heartbeat_interval"}}}
	if err := mappingCoversEveryObservedField(report, spread, flattened, flattenings); err != nil {
		t.Fatalf("a flattened surface was refused: %v", err)
	}
	if err := mappingCoversEveryObservedField(
		mappingReport{}, spread, flattened, flattenings,
	); err == nil {
		t.Fatal("a flattening whose member lost its row was accepted")
	}
}

// flatteningInput models power_supervisor's real shape: an observed nested
// struct whose members the schema presents as top-level attributes.
func flatteningInput(t *testing.T, mutate func(rules map[string]any)) CompileInput {
	t.Helper()
	names := append(dnsFieldNames(), "settings")
	rules := testPolicyObject(names, testSpecificationDigest)

	kept := []any{}
	for _, raw := range jsonArray(rules["fields"]) {
		field := jsonObject(raw)
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
	for _, raw := range jsonArray(jsonObject(source["resource"])["fields"]) {
		field := jsonObject(raw)
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
	return jsonObject(jsonArray(rules["flattenings"])[0])
}

func flattenedMembers(rules map[string]any) []any {
	return jsonArray(firstFlattening(rules)["members"])
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
				jsonObject(flattenedMembers(rules)[0])["structural_name"] = "nonexistent"
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
				rules["fields"] = append(jsonArray(rules["fields"]), map[string]any{
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
		jsonObject(flattenedMembers(rules)[1])["disposition"] = "omitted"
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
	return surfaceKindBaseline(t, catalogparity.DataSource)
}

// surfaceKindBaseline names the manifest key the compiler reads for this kind,
// so a fixture cannot pass by declaring a digest under a key the code never
// looks at.
func surfaceKindBaseline(t *testing.T, kind catalogparity.SurfaceKind) []byte {
	t.Helper()
	prefixes := map[catalogparity.SurfaceKind]string{
		catalogparity.ManagedResource: "resource_schemas.",
		catalogparity.DataSource:      "data_source_schemas.",
		catalogparity.ListResource:    "list_resource_schemas.",
		catalogparity.Action:          "action_schemas.",
	}
	prefix, known := prefixes[kind]
	if !known {
		prefix = "data_source_schemas."
	}
	return mustJSON(t, map[string]any{
		"schema_sha256": map[string]any{prefix + surfaceKindSubject(kind): dnsDataSourceBaselineDigest},
	})
}

// surfaceKindSubject names a surface that actually exists for the kind. The
// estate has exactly one action and it is not called dns_record, so a fixture
// reusing that name fails on a missing ledger entry rather than on the code
// under test.
func surfaceKindSubject(kind catalogparity.SurfaceKind) string {
	if kind == catalogparity.Action {
		return "unifi_port"
	}
	return "unifi_dns_record"
}

// surfaceKindDigestMember is the policy member each kind declares its baseline
// under. They differ, and a fixture that guessed would fail for the wrong reason.
func surfaceKindDigestMember(kind catalogparity.SurfaceKind) string {
	switch kind {
	case catalogparity.DataSource:
		return "data_source"
	case catalogparity.ListResource:
		return "list_resource"
	case catalogparity.Action:
		return "action"
	default:
		return "resource"
	}
}

// surfaceKindInput builds a self-consistent compile input for one surface
// kind: the policy, the baseline manifest and the ledger all agree, so a
// failure is attributable to the code under test rather than to the fixture.
func surfaceKindInput(t *testing.T, kind catalogparity.SurfaceKind) CompileInput {
	t.Helper()
	baseline := surfaceKindBaseline(t, kind)

	rules := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	rules["surface_kind"] = string(kind)
	rules["resource"] = surfaceKindSubject(kind)
	rules["generator_name"] = "dns_record"
	rules["baseline_digests"] = map[string]any{
		surfaceKindDigestMember(kind): dnsDataSourceBaselineDigest,
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
		if entry.Kind != kind || entry.Name != surfaceKindSubject(kind) {
			continue
		}
		entry.State = catalogparity.GeneratedShadow
		entry.ReceiptSHA256 = ""
		entry.Migration = testMigrationReason
		entry.Implementation = "shadow"
		entry.BaselineSchemaSHA256 = dnsDataSourceBaselineDigest
	}
	var source map[string]any
	if err := json.Unmarshal(testBootstrap(t, dnsFieldNames()), &source); err != nil {
		t.Fatal(err)
	}
	jsonObject(source["resource"])["name"] = surfaceKindSubject(kind)

	return CompileInput{
		Bootstrap:       mustJSON(t, source),
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
// whichever member happens to exist is the silent-wrong-answer case, and it is
// the reason every kind is named explicitly rather than defaulted.
//
// list_resource and action used to be the examples here, because the code
// specification had no member for either. They have members now, so the
// guarantee is asserted with a kind that genuinely has none — otherwise this
// test would have been deleted along with the limitation it described, and the
// property would have gone with it.
func TestCompileRejectsSurfaceKindsWithoutAnEmissionPath(t *testing.T) {
	_, err := Compile(surfaceKindInput(t, catalogparity.SurfaceKind("provider_meta")))
	if err == nil || !strings.Contains(err.Error(), "no code specification member") {
		t.Fatalf("Compile() error = %v, want a missing emission path failure", err)
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
		jsonString(record["field"]),
		jsonString(record["type"]),
		jsonBool(record["secret_candidate"]),
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

// An SDK field that changes from a slice to a scalar used to compile clean.
// The policy kept declaring a list with a string element type, the compiler
// took the declaration at its word, and the generated schema described a
// collection the controller no longer sends. Neither the exactly-once
// accounting nor the source digest catches it: the field is still present and
// still classified, and the digest is only ever compared against the policy's
// own copy of itself.
func TestCompileRejectsAnOverrideThatChangesCardinality(t *testing.T) {
	tests := map[string]struct {
		structural, declared string
		wants                []string
	}{
		"list declared over a scalar": {
			structural: "string", declared: "list",
			wants: []string{"observed as \"string\"", "terraform_type \"list\"", "not how many values there are"},
		},
		"set declared over a scalar": {
			structural: "string", declared: "set",
			wants: []string{"terraform_type \"set\"", "not how many values there are"},
		},
		"nested declared over a scalar": {
			structural: "int64", declared: "single_nested",
			wants: []string{"terraform_type \"single_nested\"", "not how many values there are"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := scalarOverrideInput(t, test.structural, test.declared)
			_, err := Compile(input)
			if err == nil {
				t.Fatal("Compile() accepted an override that changes cardinality")
			}
			for _, want := range test.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not say %q", err, want)
				}
			}
		})
	}

	// The control. Representing one value differently is established practice —
	// dns_record's ttl is a number presented as a duration string — so the rule
	// must not reach it.
	if _, err := Compile(scalarOverrideInput(t, "number", "string")); err != nil {
		t.Fatalf("a scalar-for-scalar override was rejected: %v", err)
	}
}

// A bootstrap and a policy that both omit the source they were derived from
// used to satisfy the binding, because two empty strings compare equal.
func TestCompileRequiresBothSidesToNameTheirSource(t *testing.T) {
	input := scalarOverrideInput(t, "string", "")
	var source map[string]any
	if err := json.Unmarshal(input.Bootstrap, &source); err != nil {
		t.Fatal(err)
	}
	jsonObject(source["source"])["specification_sha256"] = ""
	input.Bootstrap = mustJSON(t, source)

	var rules map[string]any
	if err := json.Unmarshal(input.Policy, &rules); err != nil {
		t.Fatal(err)
	}
	rules["source_specification_sha256"] = ""
	input.Policy = mustJSON(t, rules)

	_, err := Compile(input)
	if err == nil {
		t.Fatal("Compile() accepted a bootstrap and policy that name no source at all")
	}
	if !strings.Contains(err.Error(), "must both record the source specification") {
		t.Fatalf("error %q does not name the missing binding", err)
	}
}

// scalarOverrideInput builds a surface whose "value" field is observed as
// structural and whose policy declares terraform_type declared. An empty
// declared leaves the policy silent, which is the ordinary case.
func scalarOverrideInput(t *testing.T, structural, declared string) CompileInput {
	t.Helper()
	names := dnsFieldNames()
	rules := testPolicyObject(names, testSpecificationDigest)
	for _, raw := range jsonArray(rules["fields"]) {
		field := jsonObject(raw)
		if field["structural_name"] != "value" {
			continue
		}
		if declared == "" {
			delete(field, "terraform_type")
			continue
		}
		field["terraform_type"] = declared
	}

	var source map[string]any
	if err := json.Unmarshal(testBootstrap(t, names), &source); err != nil {
		t.Fatal(err)
	}
	for _, raw := range jsonArray(jsonObject(source["resource"])["fields"]) {
		field := jsonObject(raw)
		if field["name"] == "value" {
			field["type"] = structural
		}
	}
	return CompileInput{
		Bootstrap:       mustJSON(t, source),
		Policy:          mustJSON(t, rules),
		BaselineDigests: testBaseline(t),
		Ledger:          testLedger(t, catalogparity.Admitted),
	}
}

// A repeated object can be written as a block or as a nested attribute, and the
// SDK says []T either way. Three surfaces in the estate use blocks, and until
// the compiler could emit them a migration would have dropped them entirely —
// the generated schema simply omits what the compiler cannot describe.
func TestCompileEmitsBlocksUnderTheBlocksMember(t *testing.T) {
	for declared, want := range map[string]string{
		"list_nested_block":   "list_nested",
		"set_nested_block":    "set_nested",
		"single_nested_block": "single_nested",
	} {
		t.Run(declared, func(t *testing.T) {
			result, err := Compile(blockInput(t, declared))
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			var specification struct {
				Resources []struct {
					Schema struct {
						Attributes []struct {
							Name string `json:"name"`
						} `json:"attributes"`
						Blocks []map[string]json.RawMessage `json:"blocks"`
					} `json:"schema"`
				} `json:"resources"`
			}
			if err := json.Unmarshal(result.ProviderCodeSpec, &specification); err != nil {
				t.Fatal(err)
			}
			schema := specification.Resources[0].Schema
			if len(schema.Blocks) != 1 {
				t.Fatalf("blocks = %d, want 1", len(schema.Blocks))
			}
			if _, ok := schema.Blocks[0][want]; !ok {
				t.Fatalf("block does not use the %q member; got keys %v", want, keysOf(schema.Blocks[0]))
			}
			for _, attribute := range schema.Attributes {
				if attribute.Name == "settings" {
					t.Fatal("the block was also emitted as an attribute; it must appear once, as a block")
				}
			}
		})
	}

	// The same field declared as a nested attribute must stay an attribute.
	// Blocks and nested attributes are different syntax, so routing on the
	// declaration is the whole point.
	result, err := Compile(blockInput(t, "single_nested"))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if strings.Contains(string(result.ProviderCodeSpec), `"blocks"`) {
		t.Fatal("a nested attribute was emitted under blocks")
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// blockInput reuses the flattening fixture's observed settings struct, since it
// is the one object in the fixtures, and declares it as the given type.
func blockInput(t *testing.T, declared string) CompileInput {
	t.Helper()
	input := flatteningInput(t, nil)

	var rules map[string]any
	if err := json.Unmarshal(input.Policy, &rules); err != nil {
		t.Fatal(err)
	}
	delete(rules, "flattenings")
	structural := "settings"
	if declared == "list_nested_block" || declared == "set_nested_block" {
		structural = "settings"
	}
	rules["fields"] = append(jsonArray(rules["fields"]), map[string]any{
		"structural_name": structural,
		"terraform_name":  "settings",
		"terraform_type":  declared,
		"disposition":     "managed",
		"attribute":       map[string]any{},
		"fields": []any{
			map[string]any{"structural_name": "heartbeat_interval", "terraform_name": "heartbeat_interval",
				"disposition": "managed", "attribute": map[string]any{"computed_optional_required": "optional"}},
			map[string]any{"structural_name": "silence_threshold", "terraform_name": "silence_threshold",
				"disposition": "managed", "attribute": map[string]any{"computed_optional_required": "optional"}},
		},
	})
	input.Policy = mustJSON(t, rules)

	if declared == "list_nested_block" || declared == "set_nested_block" {
		var source map[string]any
		if err := json.Unmarshal(input.Bootstrap, &source); err != nil {
			t.Fatal(err)
		}
		for _, raw := range jsonArray(jsonObject(source["resource"])["fields"]) {
			if field := jsonObject(raw); field["name"] == "settings" {
				field["type"] = "array<object>"
			}
		}
		input.Bootstrap = mustJSON(t, source)
	}
	return input
}

// The specification's Go type carries ComputedOptionalRequired on a block, and
// the specification's JSON schema forbids it. So a policy giving a block a
// disposition produces a document that type-checks, marshals, satisfies every
// assertion this package makes about its own output, and is then rejected by
// the generator with a message naming a JSON path rather than a field.
//
// It is refused here instead, by field name, because the compiler is where the
// policy author's mistake is still legible.
func TestCompileRejectsADispositionOnABlock(t *testing.T) {
	input := blockInput(t, "list_nested_block")
	var rules map[string]any
	if err := json.Unmarshal(input.Policy, &rules); err != nil {
		t.Fatal(err)
	}
	for _, raw := range jsonArray(rules["fields"]) {
		field := jsonObject(raw)
		if field["terraform_name"] == "settings" {
			field["attribute"] = map[string]any{"computed_optional_required": "optional"}
		}
	}
	input.Policy = mustJSON(t, rules)

	_, err := Compile(input)
	if err == nil {
		t.Fatal("Compile() accepted a block with a disposition")
	}
	for _, want := range []string{"settings", "computed_optional_required", "how many times it is written"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not say %q", err, want)
		}
	}
}

// A nested shape can present a member no observed field supplies. wlan's
// schedule block shows a single day_of_week string over an SDK array of them —
// the provider chooses one, and no derivation produces that. The declaration is
// the same one a grouped member already carries, because the risk is the same:
// without it, an invented member is indistinguishable from a policy naming a
// field that does not exist.
func TestCompileAdmitsAnInventedNestedMemberOnlyWhenDeclared(t *testing.T) {
	withMember := func(member map[string]any) CompileInput {
		input := blockInput(t, "list_nested_block")
		var rules map[string]any
		if err := json.Unmarshal(input.Policy, &rules); err != nil {
			t.Fatal(err)
		}
		for _, raw := range jsonArray(rules["fields"]) {
			field := jsonObject(raw)
			if field["terraform_name"] == "settings" {
				field["fields"] = append(jsonArray(field["fields"]), member)
			}
		}
		input.Policy = mustJSON(t, rules)
		return input
	}

	// Declared, with a type of its own: emitted.
	result, err := Compile(withMember(map[string]any{
		"structural_name": "day_of_week", "terraform_name": "day_of_week",
		"terraform_type": "string", "disposition": "managed",
		"invented":  "the controller sends an array of days; the provider presents the first",
		"attribute": map[string]any{"computed_optional_required": "optional"},
	}))
	if err != nil {
		t.Fatalf("a declared invented member was rejected: %v", err)
	}
	if !strings.Contains(string(result.ProviderCodeSpec), `"day_of_week"`) {
		t.Fatal("the invented member was not emitted")
	}

	tests := map[string]struct {
		member map[string]any
		wants  []string
	}{
		// Undeclared, it is just a policy naming something the catalog does not
		// have — and the error should say the declaration exists.
		"undeclared": {
			member: map[string]any{
				"structural_name": "day_of_week", "terraform_name": "day_of_week",
				"terraform_type": "string", "disposition": "managed",
				"attribute": map[string]any{"computed_optional_required": "optional"},
			},
			wants: []string{"day_of_week", "does not observe", "invented and a reason"},
		},
		// Declared but with no type. There is no observed field to take one
		// from, so the policy must supply it.
		"declared with no type": {
			member: map[string]any{
				"structural_name": "day_of_week", "terraform_name": "day_of_week",
				"disposition": "managed", "invented": "the provider picks one",
				"attribute": map[string]any{"computed_optional_required": "optional"},
			},
			wants: []string{"day_of_week", "must declare terraform_type"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Compile(withMember(test.member))
			if err == nil {
				t.Fatal("Compile() succeeded")
			}
			for _, want := range test.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not say %q", err, want)
				}
			}
		})
	}
}

// A list resource and an action each emit under their own member. Emitting a
// list surface under "resources" would generate a managed resource with the
// wrong schema and nothing would say so — that is the case emittableSurfaceKind
// was built to prevent, and it stays prevented by naming every kind.
//
// listresources and actions are ours rather than HashiCorp's: the specification
// format has carried the same four members since September 2024 while the
// framework grew list and action packages, and offers no extension point. They
// are siblings of resources, not a new grammar, so the divergence is a key
// rename away from being undone if the concept is ever defined upstream.
func TestCompileEmitsEachSurfaceKindUnderItsOwnMember(t *testing.T) {
	for kind, member := range map[catalogparity.SurfaceKind]string{
		catalogparity.ManagedResource: "resources",
		catalogparity.DataSource:      "datasources",
		catalogparity.ListResource:    "listresources",
		catalogparity.Action:          "actions",
	} {
		t.Run(string(kind), func(t *testing.T) {
			result, err := Compile(surfaceKindInput(t, kind))
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			var document map[string]json.RawMessage
			if err := json.Unmarshal(result.ProviderCodeSpec, &document); err != nil {
				t.Fatal(err)
			}
			if _, present := document[member]; !present {
				t.Fatalf("a %s surface emitted no %q member; document has %v",
					kind, member, sortedKeysOf(document))
			}
			for _, other := range []string{"resources", "datasources", "listresources", "actions"} {
				if other == member {
					continue
				}
				if _, present := document[other]; present {
					t.Fatalf("a %s surface also emitted %q, so one document describes two kinds",
						kind, other)
				}
			}
		})
	}
}

func sortedKeysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestCompileEmitsAGroupedMemberBackedByAnObject covers the shape wan needs and
// nothing before it did: a grouping whose member consumes an observed
// array<object> rather than a flat field.
//
// wan.dhcp is a grouping the SDK does not have, and one of its members is
// wan_dhcp_options -- an array of NetworkWANDHCPOptions. The descent already
// worked for a top-level object field, because buildCodeAttribute reaches
// nestedDefinition either way; groupedMember simply had no Fields to carry the
// per-member decisions to it, and the compiler refused with "member optionNumber
// is unclassified".
func TestCompileEmitsAGroupedMemberBackedByAnObject(t *testing.T) {
	input := groupingInput(t, func(rules map[string]any) {
		members := groupingMembers(rules)
		firstGrouping(rules)["members"] = append(members, map[string]any{
			"structural_name": "options",
			"terraform_name":  "options",
			"terraform_type":  "list_nested",
			"disposition":     "managed",
			"attribute":       map[string]any{"computed_optional_required": "optional"},
			"fields": []any{
				map[string]any{
					"structural_name": "optionNumber", "terraform_name": "option_number",
					"disposition": "managed",
					"attribute":   map[string]any{"computed_optional_required": "optional"},
				},
				map[string]any{
					"structural_name": "value", "terraform_name": "value",
					"disposition": "managed",
					"attribute":   map[string]any{"computed_optional_required": "optional"},
				},
			},
		})
	})
	input.Bootstrap = bootstrapWithObjectMember(t)

	result, err := Compile(input)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	attribute := collectionAttribute(t, result.ProviderCodeSpec, "endpoint")
	var definition struct {
		Attributes []map[string]json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(attribute["single_nested"], &definition); err != nil {
		t.Fatal(err)
	}

	var options map[string]json.RawMessage
	for _, member := range definition.Attributes {
		var named struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(mustJSON(t, member), &named); err == nil && named.Name == "options" {
			options = member
		}
	}
	if options == nil {
		t.Fatalf("the object-backed member was not emitted; grouping has %d members",
			len(definition.Attributes))
	}

	// Its members must come from the catalog, exactly as a top-level object
	// field's do -- that is what keeps this a migration rather than a hand
	// written attribute that happens to live in a policy.
	var nested struct {
		NestedObject struct {
			Attributes []map[string]json.RawMessage `json:"attributes"`
		} `json:"nested_object"`
	}
	if err := json.Unmarshal(options["list_nested"], &nested); err != nil {
		t.Fatalf("object-backed member emitted as %v, want list_nested: %v",
			attributeMembers(options), err)
	}
	if len(nested.NestedObject.Attributes) != 2 {
		t.Fatalf("object-backed member has %d attributes, want 2 from the catalog",
			len(nested.NestedObject.Attributes))
	}
}

// TestCompileRejectsAnUnclassifiedGroupedObjectMember is the other half: the
// accounting that applies to a top-level object field applies inside a grouping
// too. Every member of the observed struct is classified or omitted, so nothing
// is dropped without someone deciding to drop it.
func TestCompileRejectsAnUnclassifiedGroupedObjectMember(t *testing.T) {
	input := groupingInput(t, func(rules map[string]any) {
		members := groupingMembers(rules)
		firstGrouping(rules)["members"] = append(members, map[string]any{
			"structural_name": "options",
			"terraform_name":  "options",
			"terraform_type":  "list_nested",
			"disposition":     "managed",
			"attribute":       map[string]any{"computed_optional_required": "optional"},
			// value is left undecided.
			"fields": []any{
				map[string]any{
					"structural_name": "optionNumber", "terraform_name": "option_number",
					"disposition": "managed",
					"attribute":   map[string]any{"computed_optional_required": "optional"},
				},
			},
		})
	})
	input.Bootstrap = bootstrapWithObjectMember(t)

	_, err := Compile(input)
	if err == nil {
		t.Fatal("a grouped object member with an undecided field was accepted")
	}
	if !strings.Contains(err.Error(), "value") {
		t.Errorf("the refusal does not name the undecided member: %v", err)
	}
}

// bootstrapWithObjectMember adds an observed array<object> the grouping can
// consume, since the shared fixture is all flat fields.
func bootstrapWithObjectMember(t *testing.T) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(testBootstrap(t, dnsFieldNames()), &document); err != nil {
		t.Fatal(err)
	}
	resource := jsonObject(document["resource"])
	resource["fields"] = append(jsonArray(resource["fields"]), map[string]any{
		"name": "options",
		"type": "array<object>",
		"fields": []any{
			map[string]any{"name": "optionNumber", "type": "int64"},
			map[string]any{"name": "value", "type": "string"},
		},
	})
	return mustJSON(t, document)
}

// companionInput models unifi_client_list's shape: a surface whose released
// attributes come from TWO SDK structs, joined by the conversion.
//
// The companion deliberately repeats `port`, because that is the whole reason
// observed fields are keyed by source and name: Client and ClientInfo both carry
// `name` and `mac`, and Client and ClientGroup both carry `name`.
func companionInput(t *testing.T, mutate func(bootstrap, rules map[string]any)) CompileInput {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(testBootstrap(t, dnsFieldNames()), &document); err != nil {
		t.Fatal(err)
	}
	document["companions"] = []any{map[string]any{
		"struct": "Sidecar",
		"fields": []any{
			map[string]any{"name": "port", "type": "int64"},
			map[string]any{"name": "label", "type": "string"},
		},
	}}

	rules := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	rules["fields"] = append(jsonArray(rules["fields"]),
		map[string]any{
			"structural_name": "label", "structural_source": "Sidecar",
			"terraform_name": "sidecar_label", "disposition": "managed",
			"attribute": map[string]any{"computed_optional_required": "computed"},
		},
		map[string]any{
			"structural_name": "port", "structural_source": "Sidecar",
			"terraform_name": "sidecar_port", "disposition": "omitted",
		})
	if mutate != nil {
		mutate(document, rules)
	}
	return CompileInput{
		Bootstrap:       mustJSON(t, document),
		Policy:          mustJSON(t, rules),
		BaselineDigests: testBaseline(t),
		Ledger:          testLedger(t, catalogparity.Admitted),
	}
}

// A surface may project several SDK structs, and the accounting covers ALL of
// them. Declaring a second struct's fields `invented` compiles and is a lie:
// they come off the wire, from a different call, and the exactly-once rule would
// then say nothing at all about that struct.
func TestCompileConsumesACompanionStructsFields(t *testing.T) {
	result, err := Compile(companionInput(t, nil))
	if err != nil {
		t.Fatalf("Compile() rejected a companion struct: %v", err)
	}
	attribute := collectionAttribute(t, result.ProviderCodeSpec, "sidecar_label")
	if _, ok := attribute["string"]; !ok {
		t.Fatalf("sidecar_label emitted as %v, want its companion's observed string type",
			attributeMembers(attribute))
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
	rows := map[string]string{}
	for _, field := range mapping.Fields {
		rows[field.StructuralName] = field.TerraformName
	}
	// The report qualifies a companion's field and leaves the lead's bare, so a
	// reviewer can tell Sidecar.port from the lead's own port -- which is the
	// distinction that makes the accounting mean anything on this surface.
	for name, want := range map[string]string{
		"Sidecar.label": "sidecar_label",
		"Sidecar.port":  "sidecar_port",
		"port":          "port",
	} {
		if rows[name] != want {
			t.Errorf("mapping row %q maps to %q, want %q", name, rows[name], want)
		}
	}
}

// The return on this capability: a companion's fields are accounted for, so one
// nobody classified is refused exactly as the lead struct's are.
func TestCompileRefusesAnUnclassifiedCompanionField(t *testing.T) {
	_, err := Compile(companionInput(t, func(_, rules map[string]any) {
		kept := []any{}
		for _, raw := range jsonArray(rules["fields"]) {
			field := jsonObject(raw)
			if field["terraform_name"] == "sidecar_port" {
				continue
			}
			kept = append(kept, raw)
		}
		rules["fields"] = kept
	}))
	if err == nil || !strings.Contains(err.Error(), `unclassified structural field "Sidecar.port"`) {
		t.Fatalf("Compile() error = %v, want the unclassified companion field named", err)
	}
}

// Every way a bootstrap could make a qualified name ambiguous, refused.
func TestCompileRejectsAmbiguousCompanions(t *testing.T) {
	tests := map[string]struct {
		mutate func(bootstrap, rules map[string]any)
		want   string
	}{
		"the same struct named twice": {
			mutate: func(document, _ map[string]any) {
				document["companions"] = append(jsonArray(document["companions"]),
					map[string]any{"struct": "Sidecar", "fields": []any{}})
			},
			want: `names companion struct "Sidecar" twice`,
		},
		// "port.label" would then be ambiguous between a companion's field and
		// a flattening of the lead's own `port`, and the mapping report writes
		// both in that form.
		"a struct named like a field of the lead": {
			mutate: func(document, _ map[string]any) {
				jsonObject(jsonArray(document["companions"])[0])["struct"] = "port"
			},
			want: "same name as an observed field of the lead struct",
		},
		"a companion with no struct name": {
			mutate: func(document, _ map[string]any) {
				delete(jsonObject(jsonArray(document["companions"])[0]), "struct")
			},
			want: "companion has no struct name",
		},
		"a policy naming a struct the bootstrap does not carry": {
			mutate: func(_, rules map[string]any) {
				for _, raw := range jsonArray(rules["fields"]) {
					field := jsonObject(raw)
					if field["terraform_name"] == "sidecar_label" {
						field["structural_source"] = "Nonexistent"
					}
				}
			},
			want: `declares structural_source "Nonexistent", which the bootstrap does not carry; it names only Sidecar`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Compile(companionInput(t, test.mutate))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want %q", err, test.want)
			}
		})
	}
}
