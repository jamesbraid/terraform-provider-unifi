package providercompiler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

const testSpecificationDigest = "3ddcc597a631259089c823553f3bf696725ad0bbf7d78d2f412b111e8e3427ad"

func TestCompileResolvesCompletePolicy(t *testing.T) {
	result, err := Compile(CompileInput{
		Bootstrap:       testBootstrap(t, dnsFieldNames()),
		Policy:          testPolicy(t, dnsFieldNames(), testSpecificationDigest),
		BaselineDigests: testBaseline(t),
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
	})
	if err != nil {
		t.Fatalf("bootstrap Compile() error = %v", err)
	}
	catalogResult, err := Compile(CompileInput{
		Catalog:         catalog,
		Policy:          policy,
		BaselineDigests: baseline,
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
			"resource":      "resource-digest",
			"identity":      "identity-digest",
			"list_resource": "list-digest",
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
			"resource_schemas.unifi_dns_record":          "resource-digest",
			"resource_identity_schemas.unifi_dns_record": "identity-digest",
			"list_resource_schemas.unifi_dns_record":     "list-digest",
		},
	})
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
