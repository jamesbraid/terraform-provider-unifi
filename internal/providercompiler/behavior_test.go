package providercompiler

import (
	"encoding/json"
	"strings"
	"testing"
)

// testBehavior builds the wrapper cmd/sdk-bootstrap writes, carrying one
// writes entry per named struct.
func testBehavior(t *testing.T, writes map[string][]string) []byte {
	t.Helper()
	entries := map[string]any{}
	for structName, wires := range writes {
		entries[structName] = map[string]any{
			"create_verb":        "POST",
			"required_on_create": wires,
		}
	}
	return mustJSON(t, map[string]any{
		"format_version": 1,
		"source": map[string]any{
			"repository":           "github.com/jamesbraid/go-unifi",
			"version":              "v1.113.1",
			"commit":               "7d5c1431100a8b9de18858a6258e06e82c1ac175",
			"specification_sha256": strings.Repeat("a", 64),
		},
		"behavior": map[string]any{
			"controller_version": "10.6.101",
			"ownership":          map[string]any{},
			"writes":             entries,
		},
	})
}

// dnsBootstrapWithLeadStruct is testBootstrap plus the recorded lead struct
// name ("DNSRecord") the behaviour derivation matches writes entries against.
func dnsBootstrapWithLeadStruct(t *testing.T, fieldNames []string) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(testBootstrap(t, fieldNames), &document); err != nil {
		t.Fatal(err)
	}
	jsonObject(document["resource"])["struct"] = "DNSRecord"
	return mustJSON(t, document)
}

// attributeRequiredness reads computed_optional_required off one emitted
// top-level attribute.
func attributeRequiredness(t *testing.T, spec []byte, name string) string {
	t.Helper()
	attribute := collectionAttribute(t, spec, name)
	for member, raw := range attribute {
		if member == "name" {
			continue
		}
		var body struct {
			ComputedOptionalRequired string `json:"computed_optional_required"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("attribute %q definition: %v", name, err)
		}
		return body.ComputedOptionalRequired
	}
	t.Fatalf("attribute %q carries no definition member", name)
	return ""
}

func TestCompileDerivesRequiredOnCreateFromTheBehaviourArtifact(t *testing.T) {
	result, err := Compile(CompileInput{
		Bootstrap: dnsBootstrapWithLeadStruct(t, dnsFieldNames()),
		Policy:    testPolicy(t, dnsFieldNames(), testSpecificationDigest),
		Behavior:  testBehavior(t, map[string][]string{"DNSRecord": {"key", "priority"}}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	// "key" is served as "name"; the derivation must follow the wire name,
	// not the Terraform one.
	for _, required := range []string{"name", "priority"} {
		if got := attributeRequiredness(t, result.ProviderCodeSpec, required); got != "required" {
			t.Errorf("attribute %q = %q, want required: the artifact measured its wire as required on create", required, got)
		}
	}
	if got := attributeRequiredness(t, result.ProviderCodeSpec, "ttl"); got != "optional" {
		t.Errorf("attribute ttl = %q, want the policy's own optional: its wire is not in required_on_create", got)
	}
}

// A writes entry for a struct this surface does not project is a fact about
// some other resource; nothing here may move.
func TestCompileIgnoresWritesForAStructTheSurfaceDoesNotProject(t *testing.T) {
	result, err := Compile(CompileInput{
		Bootstrap: dnsBootstrapWithLeadStruct(t, dnsFieldNames()),
		Policy:    testPolicy(t, dnsFieldNames(), testSpecificationDigest),
		Behavior:  testBehavior(t, map[string][]string{"Nat": {"protocol"}}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for _, name := range []string{"name", "priority", "ttl"} {
		if got := attributeRequiredness(t, result.ProviderCodeSpec, name); got != "optional" {
			t.Errorf("attribute %q = %q, want optional untouched", name, got)
		}
	}
}

// Omission stays a policy decision: a required-on-create wire the policy
// omits is not resurrected, and not an error.
func TestCompileHonoursAnOmittedRequiredOnCreateWire(t *testing.T) {
	input := omittedInput(t, "weight", nil)
	input.Bootstrap = dnsBootstrapWithLeadStruct(t, dnsFieldNames())
	input.Behavior = testBehavior(t, map[string][]string{"DNSRecord": {"weight"}})
	result, err := Compile(input)
	if err != nil {
		t.Fatalf("Compile() rejected an omitted required-on-create wire: %v", err)
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
		if name == "weight" {
			t.Fatal("weight is omitted by policy and must stay unemitted, required on create or not")
		}
	}
}

// The artifact and the bootstrap resolve from one module; a required wire
// the catalog does not observe means one of them is stale.
func TestCompileRefusesAnUnobservedRequiredOnCreateWire(t *testing.T) {
	_, err := Compile(CompileInput{
		Bootstrap: dnsBootstrapWithLeadStruct(t, dnsFieldNames()),
		Policy:    testPolicy(t, dnsFieldNames(), testSpecificationDigest),
		Behavior:  testBehavior(t, map[string][]string{"DNSRecord": {"nonexistent"}}),
	})
	if err == nil || !strings.Contains(err.Error(), `"nonexistent"`) ||
		!strings.Contains(err.Error(), "DNSRecord") {
		t.Fatalf("Compile() error = %v, want the unobserved wire and its struct named", err)
	}
}

// A bootstrap from before the lead struct was recorded cannot be matched
// against writes; guessing from the resource name would bind the wrong facts.
func TestCompileRefusesABootstrapWithoutALeadStructWhenWritesArePresent(t *testing.T) {
	_, err := Compile(CompileInput{
		Bootstrap: testBootstrap(t, dnsFieldNames()),
		Policy:    testPolicy(t, dnsFieldNames(), testSpecificationDigest),
		Behavior:  testBehavior(t, map[string][]string{"Anything": {"whatever"}}),
	})
	if err == nil || !strings.Contains(err.Error(), "records no lead struct") {
		t.Fatalf("Compile() error = %v, want a refusal naming the missing lead struct", err)
	}
}

// A grouping member consumes a flat wire, so the measurement lands on the
// nested attribute exactly as it would at the top level.
func TestCompileDerivesRequiredOnCreateForAGroupingMember(t *testing.T) {
	bootstrap := mustJSON(t, map[string]any{
		"format_version": 1,
		"source": map[string]any{
			"repository":           "github.com/ubiquiti-community/go-unifi",
			"commit":               "e255518385e0104eb838be56c2a491de158f3194",
			"specification_sha256": testSpecificationDigest,
		},
		"resource": map[string]any{
			"name":   "unifi_grouping_probe",
			"struct": "Probe",
			"fields": []any{
				map[string]any{"name": "alpha", "type": "string"},
				map[string]any{"name": "beta", "type": "string"},
			},
		},
	})
	policy := mustJSON(t, map[string]any{
		"format_version":              1,
		"surface_kind":                "managed_resource",
		"resource":                    "unifi_grouping_probe",
		"source_specification_sha256": testSpecificationDigest,
		"description":                 "",
		"fields":                      []any{},
		"groupings": []any{
			map[string]any{
				"terraform_name": "pair",
				"terraform_type": "single_nested",
				"attribute":      map[string]any{"computed_optional_required": "optional"},
				"members": []any{
					map[string]any{
						"structural_name": "alpha", "terraform_name": "alpha", "disposition": "managed",
						"attribute": map[string]any{"computed_optional_required": "optional"},
					},
					map[string]any{
						"structural_name": "beta", "terraform_name": "beta", "disposition": "managed",
						"attribute": map[string]any{"computed_optional_required": "optional"},
					},
				},
			},
		},
		"provider_owned": []any{},
	})
	result, err := Compile(CompileInput{
		Bootstrap: bootstrap,
		Policy:    policy,
		Behavior:  testBehavior(t, map[string][]string{"Probe": {"alpha"}}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	grouping := collectionAttribute(t, result.ProviderCodeSpec, "pair")
	var body struct {
		Attributes []map[string]json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(grouping["single_nested"], &body); err != nil {
		t.Fatal(err)
	}
	requiredness := map[string]string{}
	for _, member := range body.Attributes {
		var name string
		if err := json.Unmarshal(member["name"], &name); err != nil {
			t.Fatal(err)
		}
		var definition struct {
			ComputedOptionalRequired string `json:"computed_optional_required"`
		}
		if err := json.Unmarshal(member["string"], &definition); err != nil {
			t.Fatal(err)
		}
		requiredness[name] = definition.ComputedOptionalRequired
	}
	if requiredness["alpha"] != "required" {
		t.Errorf("pair.alpha = %q, want required", requiredness["alpha"])
	}
	if requiredness["beta"] != "optional" {
		t.Errorf("pair.beta = %q, want the policy's own optional", requiredness["beta"])
	}
}

// Only a managed resource creates; a data source mirroring the same struct
// keeps its own dispositions.
func TestCompileLeavesADataSourceAloneUnderTheBehaviourArtifact(t *testing.T) {
	rules := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	rules["surface_kind"] = "data_source"
	result, err := Compile(CompileInput{
		Bootstrap: dnsBootstrapWithLeadStruct(t, dnsFieldNames()),
		Policy:    mustJSON(t, rules),
		Behavior:  testBehavior(t, map[string][]string{"DNSRecord": {"key"}}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	var document struct {
		DataSources []struct {
			Schema struct {
				Attributes []map[string]json.RawMessage `json:"attributes"`
			} `json:"schema"`
		} `json:"datasources"`
	}
	if err := json.Unmarshal(result.ProviderCodeSpec, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.DataSources) != 1 {
		t.Fatalf("datasources = %d, want 1", len(document.DataSources))
	}
	for _, attribute := range document.DataSources[0].Schema.Attributes {
		var name string
		if err := json.Unmarshal(attribute["name"], &name); err != nil {
			t.Fatal(err)
		}
		if name != "name" {
			continue
		}
		var definition struct {
			ComputedOptionalRequired string `json:"computed_optional_required"`
		}
		if err := json.Unmarshal(attribute["string"], &definition); err != nil {
			t.Fatal(err)
		}
		if definition.ComputedOptionalRequired != "optional" {
			t.Fatalf("data-source attribute name = %q, want optional: a data source never creates", definition.ComputedOptionalRequired)
		}
		return
	}
	t.Fatal("data source emitted no name attribute to check")
}

// A required wire behind a claim has no single attribute to mark; the
// compile must say so rather than silently covering less than the artifact.
func TestCompileNoticesARequiredWireConsumedByAClaim(t *testing.T) {
	rules := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	kept := []any{}
	for _, raw := range jsonArray(rules["fields"]) {
		name := jsonObject(raw)["structural_name"]
		if name == "key" || name == "value" {
			continue
		}
		kept = append(kept, raw)
	}
	kept = append(kept, map[string]any{
		"terraform_name": "record",
		"terraform_type": "string",
		"disposition":    "managed",
		"attribute":      map[string]any{"computed_optional_required": "optional"},
	}, map[string]any{
		"terraform_name": "record_value",
		"terraform_type": "string",
		"disposition":    "managed",
		"attribute":      map[string]any{"computed_optional_required": "optional"},
	})
	rules["fields"] = kept
	rules["claims"] = []any{map[string]any{
		"terraform_members": []any{"record", "record_value"},
		"structural_names":  []any{"key", "value"},
		"mapping": map[string]any{
			"to_api":   "recordToAPI",
			"from_api": "recordFromAPI",
			"kind":     "dedicated",
		},
		"reason": "key and value travel as one record string on this fixture",
	}}
	result, err := Compile(CompileInput{
		Bootstrap: dnsBootstrapWithLeadStruct(t, dnsFieldNames()),
		Policy:    mustJSON(t, rules),
		Behavior:  testBehavior(t, map[string][]string{"DNSRecord": {"key"}}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	for _, notice := range result.Notices {
		if strings.Contains(notice, `"key"`) && strings.Contains(notice, "claim") {
			return
		}
	}
	t.Fatalf("Notices = %v, want one naming the claimed required-on-create wire", result.Notices)
}

// Requiredness is an attribute fact; a wire served as a block has nowhere to
// carry it, and silently not carrying it would ship the measurement dropped.
func TestCompileRefusesARequiredOnCreateWireDeclaredAsABlock(t *testing.T) {
	bootstrap := mustJSON(t, map[string]any{
		"format_version": 1,
		"source": map[string]any{
			"repository":           "github.com/ubiquiti-community/go-unifi",
			"commit":               "e255518385e0104eb838be56c2a491de158f3194",
			"specification_sha256": testSpecificationDigest,
		},
		"resource": map[string]any{
			"name":   "unifi_block_probe",
			"struct": "Probe",
			"fields": []any{
				map[string]any{
					"name": "rules", "type": "array<object>",
					"fields": []any{map[string]any{"name": "target", "type": "string"}},
				},
			},
		},
	})
	policy := mustJSON(t, map[string]any{
		"format_version":              1,
		"surface_kind":                "managed_resource",
		"resource":                    "unifi_block_probe",
		"source_specification_sha256": testSpecificationDigest,
		"description":                 "",
		"fields": []any{
			map[string]any{
				"structural_name": "rules",
				"terraform_name":  "rules",
				"terraform_type":  "list_nested_block",
				"disposition":     "managed",
				"fields": []any{
					map[string]any{
						"structural_name": "target", "terraform_name": "target", "disposition": "managed",
						"attribute": map[string]any{"computed_optional_required": "optional"},
					},
				},
			},
		},
		"provider_owned": []any{},
	})
	_, err := Compile(CompileInput{
		Bootstrap: bootstrap,
		Policy:    policy,
		Behavior:  testBehavior(t, map[string][]string{"Probe": {"rules"}}),
	})
	if err == nil || !strings.Contains(err.Error(), "block") || !strings.Contains(err.Error(), `"rules"`) {
		t.Fatalf("Compile() error = %v, want a refusal naming the block-declared required wire", err)
	}
}
