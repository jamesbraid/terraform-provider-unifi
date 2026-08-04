package providercompiler

import (
	"encoding/json"
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
		Bootstrap:       read("../../provider-codegen/bootstrap/go-unifi-v1.102.0-dns-record.json"),
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

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
