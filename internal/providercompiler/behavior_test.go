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

// A required-on-create wire the provider supplies is opted out per wire: the
// opted-out wire keeps its own optional disposition while every other
// required wire is still forced, and the exception is recorded on the
// mapping row for the agreement suite to read.
func TestCompileProviderFilledWireStaysOptionalWhileOthersAreForced(t *testing.T) {
	rules := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	rules["provider_filled_wires"] = []any{map[string]any{
		"structural_name": "key",
		"reason":          "the provider fills key from a default before send on this fixture",
	}}
	result, err := Compile(CompileInput{
		Bootstrap: dnsBootstrapWithLeadStruct(t, dnsFieldNames()),
		Policy:    mustJSON(t, rules),
		Behavior:  testBehavior(t, map[string][]string{"DNSRecord": {"key", "priority"}}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	// "key" is served as "name"; opted out, it keeps the policy's optional.
	if got := attributeRequiredness(t, result.ProviderCodeSpec, "name"); got != "optional" {
		t.Errorf("attribute name = %q, want optional: its wire is provider-filled and opted out", got)
	}
	// "priority" is not opted out, so the force still fires.
	if got := attributeRequiredness(t, result.ProviderCodeSpec, "priority"); got != "required" {
		t.Errorf("attribute priority = %q, want required: only the declared wire is lifted", got)
	}
	var mapping struct {
		Fields []struct {
			StructuralName            string `json:"structural_name"`
			ProviderFillsRequiredWire bool   `json:"provider_fills_required_wire"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(result.MappingReport, &mapping); err != nil {
		t.Fatal(err)
	}
	flagged := map[string]bool{}
	for _, field := range mapping.Fields {
		flagged[field.StructuralName] = field.ProviderFillsRequiredWire
	}
	if !flagged["key"] {
		t.Error("mapping report does not flag key as a provider-filled required wire")
	}
	if flagged["priority"] {
		t.Error("mapping report flags priority, which is forced Required rather than provider-filled")
	}
}

// An opt-out naming a wire the artifact does not mark required on create is
// stale: it would lift a force that never fires, so the compile refuses it
// rather than let it linger once the SDK stops requiring the wire.
func TestCompileRefusesAStaleProviderFilledWire(t *testing.T) {
	rules := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	rules["provider_filled_wires"] = []any{map[string]any{
		"structural_name": "ttl",
		"reason":          "ttl is not required on create, so this opt-out is stale",
	}}
	_, err := Compile(CompileInput{
		Bootstrap: dnsBootstrapWithLeadStruct(t, dnsFieldNames()),
		Policy:    mustJSON(t, rules),
		Behavior:  testBehavior(t, map[string][]string{"DNSRecord": {"key"}}),
	})
	if err == nil || !strings.Contains(err.Error(), `"ttl"`) || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("Compile() error = %v, want a refusal naming the stale opt-out wire", err)
	}
}

// The compiler cannot check the fill an opt-out stands in for, so the reason
// is required: a bare entry would assert the exception with nothing behind it.
func TestCompileRefusesAProviderFilledWireWithoutAReason(t *testing.T) {
	rules := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	rules["provider_filled_wires"] = []any{map[string]any{"structural_name": "key"}}
	_, err := Compile(CompileInput{
		Bootstrap: dnsBootstrapWithLeadStruct(t, dnsFieldNames()),
		Policy:    mustJSON(t, rules),
		Behavior:  testBehavior(t, map[string][]string{"DNSRecord": {"key"}}),
	})
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("Compile() error = %v, want a refusal demanding a reason", err)
	}
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

// TestRequiredWireObservedResolvesNestedPaths covers the artifact's dotted
// required_on_create paths against a struct with observed nested Fields: an
// object member (source.zone_id) and an array<object> element member
// (areas[].network_ids), the OSPFRouter shape the flat sourceFields lookup
// cannot see. A path whose leaf is not observed is refused, so a stale
// artifact still fails the compile rather than silently forcing nothing.
func TestRequiredWireObservedResolvesNestedPaths(t *testing.T) {
	sourceFields := map[string]bootstrapField{
		"router_id": {Name: "router_id", Type: "string"},
		"areas": {Name: "areas", Type: "array<object>", Fields: []bootstrapField{
			{Name: "area_id", Type: "string"},
			{Name: "network_ids", Type: "array<string>"},
		}},
		"source": {Name: "source", Type: "object", Fields: []bootstrapField{
			{Name: "zone_id", Type: "string"},
		}},
		// A companion field is stored under its qualified key.
		"Companion.flag": {Name: "flag", Type: "bool"},
	}
	cases := []struct {
		qualifier, wire string
		want            bool
	}{
		{"", "router_id", true},              // flat lead field
		{"", "areas", true},                  // the array field itself
		{"", "areas[].network_ids", true},    // array<object> element member
		{"", "source.zone_id", true},         // object member
		{"", "areas[].nonexistent", false},   // observed parent, absent leaf
		{"", "missing.zone_id", false},       // absent parent
		{"", "source.zone_id.deeper", false}, // scalar has no members
		{"Companion", "flag", true},          // flat companion field
		{"Companion", "flag.nested", false},  // companions carry no nested behaviour
	}
	for _, tc := range cases {
		if got := requiredWireObserved(sourceFields, tc.qualifier, tc.wire); got != tc.want {
			t.Errorf("requiredWireObserved(%q, %q) = %v, want %v", tc.qualifier, tc.wire, got, tc.want)
		}
	}
}

// testBehaviorEmpty builds the artifact wrapper carrying a writes family (for
// its update paths) and an empty family, the two the empty-write suppression
// derivation reads.
func testBehaviorEmpty(t *testing.T, writes, empty map[string]any) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"format_version": 1,
		"source": map[string]any{
			"repository":           "github.com/jamesbraid/go-unifi",
			"version":              "v1.113.1",
			"commit":               strings.Repeat("a", 40),
			"specification_sha256": strings.Repeat("a", 64),
		},
		"behavior": map[string]any{
			"controller_version": "10.6.101",
			"writes":             writes,
			"empty":              empty,
		},
	})
}

func TestPathCollectionExtractsTheCollectionSegment(t *testing.T) {
	cases := []struct{ path, want string }{
		{"v2/api/site/{site}/nat/{id}", "nat"},                // v2 API, id-terminated
		{"api/s/{site}/rest/networkconf/{id}", "networkconf"}, // rest API
		{"v2/api/site/{site}/nat", "nat"},                     // no trailing id
		{"", ""},
		{"{id}", ""}, // only placeholders
	}
	for _, tc := range cases {
		if got := pathCollection(tc.path); got != tc.want {
			t.Errorf("pathCollection(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// The bootstrap collection is preferred; a v2-API surface without one (nat)
// falls back to the collection segment of its measured update path.
func TestEmptyFamilyCollectionPrefersBootstrapThenPath(t *testing.T) {
	rest := bootstrap{Resource: bootstrapSchema{Struct: "Network", Collection: "networkconf"}}
	if got := emptyFamilyCollection(rest, nil); got != "networkconf" {
		t.Errorf("emptyFamilyCollection(rest) = %q, want networkconf", got)
	}
	v2 := bootstrap{Resource: bootstrapSchema{Struct: "Nat"}}
	writes := map[string]behaviorWrites{"Nat": {UpdatePath: "v2/api/site/{site}/nat/{id}"}}
	if got := emptyFamilyCollection(v2, writes); got != "nat" {
		t.Errorf("emptyFamilyCollection(v2) = %q, want nat", got)
	}
	if got := emptyFamilyCollection(v2, nil); got != "" {
		t.Errorf("emptyFamilyCollection(no writes) = %q, want empty", got)
	}
}

// Only EMPTY-REJECTED with OMIT-CLEARS is suppressed: an EMPTY-CLEARS field
// takes an empty write happily, and an OMIT-REJECTED one cannot be safely
// omitted, so neither is a suppression to derive.
func TestBehaviorEmptySuppressedWiresAppliesTheRule(t *testing.T) {
	behavior := testBehaviorEmpty(t,
		map[string]any{"Nat": map[string]any{"update_path": "v2/api/site/{site}/nat/{id}"}},
		map[string]any{"nat": map[string]any{
			"in_interface": map[string]any{"empty": "EMPTY-REJECTED", "omit": "OMIT-CLEARS"},
			"ip_address":   map[string]any{"empty": "EMPTY-REJECTED", "omit": "OMIT-CLEARS"},
			"description":  map[string]any{"empty": "EMPTY-CLEARS", "omit": "OMIT-CLEARS"},
			"dhcpd_ip_1":   map[string]any{"empty": "EMPTY-REJECTED", "omit": "OMIT-REJECTED"},
		}},
	)
	source := bootstrap{Resource: bootstrapSchema{Struct: "Nat"}}
	got, err := behaviorEmptySuppressedWires(behavior, ManagedResource, source)
	if err != nil {
		t.Fatalf("behaviorEmptySuppressedWires() error = %v", err)
	}
	want := map[string]struct{}{"in_interface": {}, "ip_address": {}}
	if len(got) != len(want) {
		t.Fatalf("suppressed = %v, want %v", got, want)
	}
	for wire := range want {
		if _, ok := got[wire]; !ok {
			t.Errorf("suppressed set missing %q", wire)
		}
	}
}

// A data source never writes, so it derives no empty-write suppression even
// when the artifact measures its collection.
func TestBehaviorEmptySuppressedWiresLeavesADataSourceAlone(t *testing.T) {
	behavior := testBehaviorEmpty(t,
		map[string]any{"Nat": map[string]any{"update_path": "v2/api/site/{site}/nat/{id}"}},
		map[string]any{"nat": map[string]any{
			"in_interface": map[string]any{"empty": "EMPTY-REJECTED", "omit": "OMIT-CLEARS"},
		}},
	)
	source := bootstrap{Resource: bootstrapSchema{Struct: "Nat"}}
	got, err := behaviorEmptySuppressedWires(behavior, DataSource, source)
	if err != nil {
		t.Fatalf("behaviorEmptySuppressedWires() error = %v", err)
	}
	if got != nil {
		t.Fatalf("suppressed = %v, want nil for a data source", got)
	}
}

// The derived verdict lands on the mapping report the descriptor emitter
// reads: a managed field the artifact refuses an empty write on is flagged,
// and one it does not is left alone.
func TestCompileMarksEmptyWriteSuppressionInTheMappingReport(t *testing.T) {
	behavior := testBehaviorEmpty(t,
		map[string]any{"DNSRecord": map[string]any{"update_path": "api/s/{site}/rest/dns/{id}"}},
		map[string]any{"dns": map[string]any{
			"key":   map[string]any{"empty": "EMPTY-REJECTED", "omit": "OMIT-CLEARS"},
			"value": map[string]any{"empty": "EMPTY-CLEARS", "omit": "OMIT-CLEARS"},
		}},
	)
	result, err := Compile(CompileInput{
		Bootstrap: dnsBootstrapWithLeadStruct(t, dnsFieldNames()),
		Policy:    testPolicy(t, dnsFieldNames(), testSpecificationDigest),
		Behavior:  behavior,
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	var mapping struct {
		Fields []struct {
			StructuralName     string `json:"structural_name"`
			SuppressEmptyWrite bool   `json:"suppress_empty_write"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(result.MappingReport, &mapping); err != nil {
		t.Fatal(err)
	}
	suppressed := map[string]bool{}
	for _, field := range mapping.Fields {
		suppressed[field.StructuralName] = field.SuppressEmptyWrite
	}
	if !suppressed["key"] {
		t.Error("mapping report does not flag key for empty-write suppression")
	}
	if suppressed["value"] {
		t.Error("mapping report flags value, whose empty the controller clears; only EMPTY-REJECTED wants suppression")
	}
}

// testBehaviorMinItems builds the artifact wrapper carrying a writes family
// whose entries include min_items maps.
func testBehaviorMinItems(t *testing.T, writes map[string]any) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"format_version": 1,
		"source": map[string]any{
			"repository":           "github.com/jamesbraid/go-unifi",
			"version":              "v1.113.1",
			"commit":               strings.Repeat("a", 40),
			"specification_sha256": strings.Repeat("a", 64),
		},
		"behavior": map[string]any{
			"controller_version": "10.6.101",
			"writes":             writes,
		},
	})
}

// The artifact's dotted min_items path is canonicalised to the structural
// path the build uses -- the array marker dropped.
func TestBehaviorMinItemsResolvesAndCanonicalisesNestedPaths(t *testing.T) {
	sourceFields := map[string]bootstrapField{
		"areas": {Name: "areas", Type: "array<object>", Fields: []bootstrapField{
			{Name: "network_ids", Type: "array<string>"},
		}},
	}
	source := bootstrap{Resource: bootstrapSchema{Struct: "OSPFRouter"}}
	behavior := testBehaviorMinItems(t, map[string]any{
		"OSPFRouter": map[string]any{
			"min_items": map[string]any{"areas[].network_ids": 1},
		},
	})
	got, err := behaviorMinItems(behavior, ManagedResource, source, sourceFields)
	if err != nil {
		t.Fatalf("behaviorMinItems() error = %v", err)
	}
	if len(got) != 1 || got["areas.network_ids"] != 1 {
		t.Fatalf("min items = %v, want {areas.network_ids: 1}", got)
	}
}

// A min_items path the catalog does not observe is a stale artifact, refused
// exactly as an unobserved required-on-create wire is.
func TestBehaviorMinItemsRefusesAnUnobservedPath(t *testing.T) {
	sourceFields := map[string]bootstrapField{
		"areas": {Name: "areas", Type: "array<object>", Fields: []bootstrapField{
			{Name: "area_id", Type: "string"},
		}},
	}
	source := bootstrap{Resource: bootstrapSchema{Struct: "OSPFRouter"}}
	behavior := testBehaviorMinItems(t, map[string]any{
		"OSPFRouter": map[string]any{
			"min_items": map[string]any{"areas[].network_ids": 1},
		},
	})
	_, err := behaviorMinItems(behavior, ManagedResource, source, sourceFields)
	if err == nil || !strings.Contains(err.Error(), "network_ids") ||
		!strings.Contains(err.Error(), "OSPFRouter") {
		t.Fatalf("behaviorMinItems() error = %v, want the unobserved path and struct named", err)
	}
}

// The measured minimum lands as a plan-time SizeAtLeast validator on the
// nested list member the path names.
func TestCompileDerivesAListSizeValidatorFromMinItems(t *testing.T) {
	bootstrap := mustJSON(t, map[string]any{
		"format_version": 1,
		"source": map[string]any{
			"repository":           "github.com/ubiquiti-community/go-unifi",
			"commit":               "e255518385e0104eb838be56c2a491de158f3194",
			"specification_sha256": testSpecificationDigest,
		},
		"resource": map[string]any{
			"name":   "unifi_area_probe",
			"struct": "AreaProbe",
			"fields": []any{
				map[string]any{
					"name": "areas", "type": "array<object>",
					"fields": []any{map[string]any{"name": "network_ids", "type": "array<string>"}},
				},
			},
		},
	})
	policy := mustJSON(t, map[string]any{
		"format_version":              1,
		"surface_kind":                "managed_resource",
		"resource":                    "unifi_area_probe",
		"source_specification_sha256": testSpecificationDigest,
		"description":                 "",
		"fields": []any{
			map[string]any{
				"structural_name": "areas",
				"terraform_name":  "areas",
				"terraform_type":  "list_nested",
				"disposition":     "managed",
				"attribute":       map[string]any{"computed_optional_required": "required"},
				"fields": []any{
					map[string]any{
						"structural_name": "network_ids", "terraform_name": "network_ids",
						"terraform_type": "list", "disposition": "managed",
						"attribute": map[string]any{
							"computed_optional_required": "required",
							"element_type":               map[string]any{"string": map[string]any{}},
						},
					},
				},
			},
		},
		"provider_owned": []any{},
	})
	result, err := Compile(CompileInput{
		Bootstrap: bootstrap,
		Policy:    policy,
		Behavior: testBehaviorMinItems(t, map[string]any{
			"AreaProbe": map[string]any{"min_items": map[string]any{"areas[].network_ids": 1}},
		}),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	areas := collectionAttribute(t, result.ProviderCodeSpec, "areas")
	var nested struct {
		NestedObject struct {
			Attributes []map[string]json.RawMessage `json:"attributes"`
		} `json:"nested_object"`
	}
	if err := json.Unmarshal(areas["list_nested"], &nested); err != nil {
		t.Fatal(err)
	}
	for _, member := range nested.NestedObject.Attributes {
		var name string
		if err := json.Unmarshal(member["name"], &name); err != nil {
			t.Fatal(err)
		}
		if name != "network_ids" {
			continue
		}
		var definition struct {
			Validators []struct {
				Custom struct {
					SchemaDefinition string `json:"schema_definition"`
				} `json:"custom"`
			} `json:"validators"`
		}
		if err := json.Unmarshal(member["list"], &definition); err != nil {
			t.Fatal(err)
		}
		for _, validator := range definition.Validators {
			if validator.Custom.SchemaDefinition == "listvalidator.SizeAtLeast(1)" {
				return
			}
		}
		t.Fatalf("network_ids validators = %+v, want a listvalidator.SizeAtLeast(1)", definition.Validators)
	}
	t.Fatal("spec has no network_ids member to check")
}
