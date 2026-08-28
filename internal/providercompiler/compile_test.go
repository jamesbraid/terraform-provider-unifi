package providercompiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Exactly-once accounting catches a field added to the SDK or removed from
// it, since either leaves a field unclassified or a policy entry stale. A
// retyped field still compiles and passes; that's caught separately, by
// requireScalarOverride refusing a cardinality-changing override.

const testSpecificationDigest = "3ddcc597a631259089c823553f3bf696725ad0bbf7d78d2f412b111e8e3427ad"

func TestCompileResolvesCompletePolicy(t *testing.T) {
	result, err := Compile(CompileInput{
		Bootstrap: testBootstrap(t, dnsFieldNames()),
		Policy:    testPolicy(t, dnsFieldNames(), testSpecificationDigest),
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	for name, output := range map[string][]byte{
		"provider code specification": result.ProviderCodeSpec,
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
				Bootstrap: testBootstrap(t, test.bootstrapFields),
				Policy:    mustJSON(t, policy),
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
			"version":              "v1.103.0",
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
	}
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
		Bootstrap: testBootstrap(t, names),
		Policy:    mustJSON(t, rules),
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

// The specification member and the element type both have to survive to
// the emitted document; a collection under the wrong member generates
// nothing.
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

// nestedInput adds one object field to the dns_record fixture: the member
// list comes from the bootstrap (the catalog side), the per-member
// decisions from the policy.
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
		Bootstrap: mustJSON(t, source),
		Policy:    mustJSON(t, rules),
	}
}

// The member list must be derived from the catalog; a policy that
// hand-authors it would look like a migration while carrying the same
// hand-written schema in a different file.
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

// groupingInput models a real shape: two observed flat fields grouped into
// a nested attribute the SDK does not have.
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
		Bootstrap: testBootstrap(t, names),
		Policy:    mustJSON(t, rules),
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
		// Same conflict within one grouping, told apart from the case above
		// since the fix differs: a duplicated member, not two groupings
		// disagreeing about ownership.
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

// An invented attribute corresponds to no observed field and must say so:
// it's admitted by declaration rather than by weakening the observed-fields
// guard.
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

// oneToManyInput folds both grouped fields into one member through a claim.
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

// manyToOneInput keeps both members and relates them to one field through a
// claim.
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

// A claimed member has no one observed field behind it, so the policy
// declares its type and the compiler does not compare it against the wire.
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
	// The declared type, not either observed one: both fields are int64 and
	// the member is a string, and neither field is more right than the other.
	if _, declared := definition.Attributes[0]["string"]; !declared {
		t.Fatalf("member emitted as %v, want the declared string type",
			attributeMembers(definition.Attributes[0]))
	}
}

// Two members over one field, which no member-scoped declaration reaches:
// each would assert the field and collide, with no place to say they
// belong together.
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

// A refusal that names nothing -- neither the member, the grouping, nor any
// field -- is treated as a defect here.
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
// claim consuming two fields owes it two rows, and the terraform side names
// every member of the claim together.
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

// Every way a claim can be ambiguous about what it relates, refused rather
// than defaulted. The accounting runs in both directions: a field named
// twice and a member named twice are equally conflicts.
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
		// A name alone can't say whether it is the relation or merely
		// contains it -- two different strengths of claim.
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
		// Not covered here: a to_api on a surface that never writes. The rule
		// is in claimedStructuralFields, but every input in this table is a
		// managed_resource, and flipping surface_kind to data_source here
		// fails on a digest mismatch before reaching the claim check -- so
		// the case would pass for the wrong reason. Named here rather than
		// silently absent, since the two look identical in a green run.
		"a claim with no reason": {
			mutate: func(rules map[string]any) { delete(firstClaim(rules), "reason") },
			want:   "declares no reason",
		},
		// A one-to-one claim is an ordinary member wearing a costume, and
		// admitting it would give two ways to say the same thing.
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
			map[string]any{
				"structural_name": "optionNumber", "terraform_name": "option_number",
				"disposition": "omitted",
			},
			map[string]any{
				"structural_name": "value", "terraform_name": "value",
				"disposition": "managed",
				"attribute":   map[string]any{"computed_optional_required": "optional"},
			},
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
		Bootstrap: bootstrapWithObjectMember(t),
		Policy:    mustJSON(t, rules),
	}
}

// An observed array<object> presented as a list of scalars. Emitting it as
// list_nested instead compiles but describes a different schema, so the
// collapse is declared and the compiler checks it.
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
				jsonObject(jsonArray(member["fields"])[0])["attribute"] = map[string]any{"computed_optional_required": "optional"}
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
				jsonObject(fields[0])["attribute"] = map[string]any{"computed_optional_required": "optional"}
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

// A claim reaching top-level fields, both computed from one observed field
// and each borrowing a different declared type.
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

// The exactly-once rule is enforced while the policy is read; the mapping
// report is written afterwards from the same policy, so this is an
// independent check that a reporting mistake can't hide a miscounted field.
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

	// A flattened field is spread rather than emitted, so its members carry
	// the rows and the parent carries none -- this compares against a
	// constructed expectation rather than counting rows.
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

// flatteningInput models a real shape: an observed nested struct whose
// members the schema presents as top-level attributes.
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
		Bootstrap: mustJSON(t, source),
		Policy:    mustJSON(t, rules),
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

// surfaceKindSubject names a surface that actually exists for the kind:
// reusing dns_record for an action would fail on a missing ledger entry
// rather than on the code under test.
func surfaceKindSubject(kind SurfaceKind) string {
	if kind == Action {
		return "unifi_port"
	}
	return "unifi_dns_record"
}

// surfaceKindInput builds a self-consistent compile input for one surface
// kind, so a failure is attributable to the code under test rather than to
// the fixture.
func surfaceKindInput(t *testing.T, kind SurfaceKind) CompileInput {
	t.Helper()

	rules := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	rules["surface_kind"] = string(kind)
	rules["resource"] = surfaceKindSubject(kind)
	rules["generator_name"] = "dns_record"

	var source map[string]any
	if err := json.Unmarshal(testBootstrap(t, dnsFieldNames()), &source); err != nil {
		t.Fatal(err)
	}
	jsonObject(source["resource"])["name"] = surfaceKindSubject(kind)

	return CompileInput{
		Bootstrap: mustJSON(t, source),
		Policy:    mustJSON(t, rules),
	}
}

// A data source must be emitted under the specification's "datasources"
// member; under "resources" the generator accepts the file and silently
// generates nothing.
func TestCompileEmitsDataSourceUnderDataSourcesMember(t *testing.T) {
	result, err := Compile(surfaceKindInput(t, DataSource))
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

// A surface kind with no emission path must fail loudly rather than be
// emitted under whichever member happens to exist. This must always use a
// kind that genuinely has no member, or the guarantee goes untested.
func TestCompileRejectsSurfaceKindsWithoutAnEmissionPath(t *testing.T) {
	_, err := Compile(surfaceKindInput(t, SurfaceKind("provider_meta")))
	if err == nil || !strings.Contains(err.Error(), "no code specification member") {
		t.Fatalf("Compile() error = %v, want a missing emission path failure", err)
	}
}

// The mapping report is keyed by the surface's own kind and name, not by
// whichever surface happened to compile most recently.
func TestCompileReportsManagedResourceKind(t *testing.T) {
	result, err := Compile(CompileInput{
		Bootstrap: testBootstrap(t, dnsFieldNames()),
		Policy:    testPolicy(t, dnsFieldNames(), testSpecificationDigest),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.MappingReport, []byte(`"surface_kind": "managed_resource"`)) ||
		!bytes.Contains(result.MappingReport, []byte(`"surface_name": "unifi_dns_record"`)) {
		t.Fatalf("mapping report lacks surface identity: %s", result.MappingReport)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// An SDK field that changes from a slice to a scalar is neither caught by
// exactly-once accounting (still present, still classified) nor by the
// source digest (only ever compared against the policy's own copy).
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

	// The control: representing one value differently (a number as a
	// duration string) is established practice, so the rule must not reach it.
	if _, err := Compile(scalarOverrideInput(t, "number", "string")); err != nil {
		t.Fatalf("a scalar-for-scalar override was rejected: %v", err)
	}
}

// A bootstrap and a policy that both omit the source they were derived from
// must not satisfy the binding just because two empty strings compare equal.
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
		Bootstrap: mustJSON(t, source),
		Policy:    mustJSON(t, rules),
	}
}

// A repeated object can be written as a block or as a nested attribute, and
// the SDK says []T either way.
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

	// The same field declared as a nested attribute must stay an attribute:
	// routing on the declaration is the whole point.
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
			map[string]any{
				"structural_name": "heartbeat_interval", "terraform_name": "heartbeat_interval",
				"disposition": "managed", "attribute": map[string]any{"computed_optional_required": "optional"},
			},
			map[string]any{
				"structural_name": "silence_threshold", "terraform_name": "silence_threshold",
				"disposition": "managed", "attribute": map[string]any{"computed_optional_required": "optional"},
			},
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

// The specification's Go type carries ComputedOptionalRequired on a block,
// but the JSON schema forbids it -- refused here by field name rather than
// left for the generator to reject by an unattributed JSON path.
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

// A nested shape can present a member no observed field supplies, declared
// the same way a grouped member already is: without it, an invented member
// is indistinguishable from a policy naming a field that doesn't exist.
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
	for kind, member := range map[SurfaceKind]string{
		ManagedResource: "resources",
		DataSource:      "datasources",
		ListResource:    "listresources",
		Action:          "actions",
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

// A grouping whose member consumes an observed array<object> rather than a
// flat field.
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
	// field's do.
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

// companionInput models a surface whose released attributes come from two
// SDK structs, joined by the conversion. The companion deliberately repeats
// `port`, since colliding names across structs is the whole reason observed
// fields are keyed by source and name.
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
		Bootstrap: mustJSON(t, document),
		Policy:    mustJSON(t, rules),
	}
}

// A surface may project several SDK structs, and the accounting covers all
// of them -- declaring a second struct's fields "invented" would be a lie,
// since they come off the wire too.
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
	// The report qualifies a companion's field and leaves the lead's bare, so
	// a reviewer can tell Sidecar.label from the lead's own port.
	for name, want := range map[string]string{
		"Sidecar.label": "sidecar_label",
		"port":          "port",
	} {
		if rows[name] != want {
			t.Errorf("mapping row %q maps to %q, want %q", name, rows[name], want)
		}
	}
	// Sidecar.port is the fixture's omitted companion field: withoutOmittedRows
	// drops it, so its qualification is proven by Compile() accepting the
	// fixture at all (an unqualified duplicate of the lead's "port" would have
	// collided) rather than by a row here.
	if _, present := rows["Sidecar.port"]; present {
		t.Error("Sidecar.port is omitted and should carry no mapping row")
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
		// "port.label" would then be ambiguous between a companion's field
		// and a flattening of the lead's own `port`.
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

// A list resource's fields are the lead struct's, entirely omitted, plus
// invented filter attributes with no structural name at all: nothing a
// mapping report would say survives, so Compile emits none rather than a
// technically-valid, permanently-empty artifact.
func TestCompileEmitsNoMappingReportForAListResource(t *testing.T) {
	result, err := Compile(surfaceKindInput(t, ListResource))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(result.MappingReport) != 0 {
		t.Fatalf("MappingReport = %s, want none for a list resource", result.MappingReport)
	}
	if len(result.ProviderCodeSpec) == 0 {
		t.Fatal("ProviderCodeSpec is empty; suppressing the mapping report must have discarded the wrong artifact")
	}
}

// Every other surface kind keeps its mapping report; only ListResource is
// special-cased, and this fails if that gate is ever accidentally widened.
func TestCompileKeepsTheMappingReportForNonListSurfaceKinds(t *testing.T) {
	for _, kind := range []SurfaceKind{ManagedResource, DataSource, Action} {
		t.Run(string(kind), func(t *testing.T) {
			result, err := Compile(surfaceKindInput(t, kind))
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			if len(result.MappingReport) == 0 {
				t.Fatalf("%s surface has no MappingReport, want one", kind)
			}
		})
	}
}

// omittedInput builds a dns_record-shaped policy with one field moved from
// "fields" to the compact "omitted" list, leaving every other field managed.
func omittedInput(t *testing.T, structuralName string, mutateRules func(rules map[string]any)) CompileInput {
	t.Helper()
	rules := testPolicyObject(dnsFieldNames(), testSpecificationDigest)
	kept := []any{}
	for _, raw := range jsonArray(rules["fields"]) {
		if jsonObject(raw)["structural_name"] == structuralName {
			continue
		}
		kept = append(kept, raw)
	}
	rules["fields"] = kept
	rules["omitted"] = []any{structuralName}
	if mutateRules != nil {
		mutateRules(rules)
	}
	return CompileInput{
		Bootstrap: testBootstrap(t, dnsFieldNames()),
		Policy:    mustJSON(t, rules),
	}
}

// The compact form must classify a field identically to writing the object
// out by hand: accepted, and its row dropped from the report the same way
// withoutOmittedRows drops an object-form omitted row.
func TestCompileAcceptsACompactOmittedField(t *testing.T) {
	result, err := Compile(omittedInput(t, "weight", nil))
	if err != nil {
		t.Fatalf("Compile() rejected a compact omitted field: %v", err)
	}
	var mapping struct {
		Fields []struct {
			StructuralName string `json:"structural_name"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(result.MappingReport, &mapping); err != nil {
		t.Fatal(err)
	}
	for _, field := range mapping.Fields {
		if field.StructuralName == "weight" {
			t.Fatal("weight is omitted and should carry no mapping row")
		}
	}
}

// A compact entry naming a field the bootstrap no longer carries is the
// same authoring mistake "fields" catches today -- a field renamed or
// removed from the SDK with a stale policy entry left behind.
func TestCompileRefusesAStaleCompactOmittedField(t *testing.T) {
	input := omittedInput(t, "weight", func(rules map[string]any) {
		rules["omitted"] = []any{"weight", "removed_field"}
	})
	_, err := Compile(input)
	if err == nil || !strings.Contains(err.Error(), `stale policy field "removed_field"`) {
		t.Fatalf("Compile() error = %v, want a stale policy field failure", err)
	}
}

// A field named by both forms at once is disposed twice, exactly as two
// object-form entries for the same field would be.
func TestCompileRefusesAFieldInBothOmittedFormsAtOnce(t *testing.T) {
	input := omittedInput(t, "weight", func(rules map[string]any) {
		rules["fields"] = append(jsonArray(rules["fields"]), map[string]any{
			"structural_name": "weight", "terraform_name": "weight", "disposition": "omitted",
		})
	})
	_, err := Compile(input)
	if err == nil || !strings.Contains(err.Error(), `duplicate policy field "weight"`) {
		t.Fatalf("Compile() error = %v, want a duplicate policy field failure", err)
	}
}

// An empty entry names nothing to omit; refusing it by name rather than
// letting it fall through to some other field's error keeps the mistake
// attributable to the "omitted" list itself.
func TestCompileRefusesAnEmptyCompactOmittedEntry(t *testing.T) {
	input := omittedInput(t, "weight", func(rules map[string]any) {
		rules["omitted"] = []any{"weight", ""}
	})
	_, err := Compile(input)
	if err == nil || !strings.Contains(err.Error(), `omitted field "" names no structural field`) {
		t.Fatalf("Compile() error = %v, want the empty-entry failure", err)
	}
}

// expandOmittedFields must run before verifyBootstrapSecretCandidates: a
// secret candidate disposed through the compact form is exactly as safe as
// one written out in "fields", but only if the compact entry is visible to
// the check by the time it runs.
func TestCompileAcceptsASecretCandidateOmittedByTheCompactForm(t *testing.T) {
	var document map[string]any
	if err := json.Unmarshal(testBootstrap(t, dnsFieldNames()), &document); err != nil {
		t.Fatal(err)
	}
	for _, raw := range jsonArray(jsonObject(document["resource"])["fields"]) {
		if jsonObject(raw)["name"] == "key" {
			jsonObject(raw)["secret_candidate"] = true
		}
	}
	input := omittedInput(t, "key", nil)
	input.Bootstrap = mustJSON(t, document)
	if _, err := Compile(input); err != nil {
		t.Fatalf("Compile() rejected a secret candidate omitted by the compact form: %v", err)
	}
}
