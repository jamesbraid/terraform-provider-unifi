package schemabehaviour

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePolicy(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "thing.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing policy: %v", err)
	}
	return path
}

func readPolicy(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading policy: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("decoding policy: %v", err)
	}
	return document
}

// attributeAt walks the written policy the way a reader would, so the test
// asserts against the document rather than against the code that wrote it.
func attributeAt(t *testing.T, document map[string]any, keys []string, name string) map[string]any {
	t.Helper()
	var found map[string]any
	var walk func(any)
	walk = func(node any) {
		entries, ok := node.([]any)
		if !ok {
			return
		}
		for _, entry := range entries {
			field, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if field["terraform_name"] == name {
				if attribute, ok := field["attribute"].(map[string]any); ok {
					found = attribute
				}
			}
			walk(field["members"])
			walk(field["fields"])
		}
	}
	for _, key := range keys {
		walk(document[key])
	}
	if found == nil {
		t.Fatalf("no attribute called %q in the written policy", name)
	}
	return found
}

const mergePolicy = `{
  "format_version": 1,
  "resource": "unifi_thing",
  "fields": [
    {"structural_name": "x_enabled", "terraform_name": "enabled", "disposition": "managed",
     "attribute": {"computed_optional_required": "computed_optional", "description": "d"}},
    {"structural_name": "x_mac", "terraform_name": "mac", "disposition": "managed",
     "attribute": {"computed_optional_required": "required", "description": "d"}},
    {"structural_name": "x_rekey", "terraform_name": "rekey", "disposition": "managed",
     "attribute": {"computed_optional_required": "computed_optional", "description": "d",
                   "default": {"static": 7}}},
    {"structural_name": "x_gone", "terraform_name": "not_in_the_schema", "disposition": "managed",
     "attribute": {"computed_optional_required": "optional", "description": "d"}},
    {"structural_name": "x_source", "terraform_name": "source", "disposition": "managed",
     "attribute": {"computed_optional_required": "required", "description": "d"},
     "fields": [
       {"structural_name": "x_kind", "terraform_name": "kind", "disposition": "managed",
        "attribute": {"computed_optional_required": "computed_optional", "description": "d"}}
     ]}
  ],
  "provider_owned": []
}`

// Test_mergeWritesBehaviourIntoThePolicy checks the writer against the shapes
// that matter: a plain default, a custom type with its value type, a validator
// with its imports, and a nested attribute reached through a shared map.
func Test_mergeWritesBehaviourIntoThePolicy(t *testing.T) {
	path := writePolicy(t, mergePolicy)
	if _, err := MergeIntoPolicy(path, derived(t)); err != nil {
		t.Fatalf("MergeIntoPolicy: %v", err)
	}
	document := readPolicy(t, path)
	keys := []string{"fields"}

	enabled := attributeAt(t, document, keys, "enabled")
	if fault, ok := enabled["default"].(map[string]any); !ok || fault["static"] != true {
		t.Errorf("enabled's default was written as %v, want a static true", enabled["default"])
	}

	mac := attributeAt(t, document, keys, "mac")
	custom, ok := mac["custom_type"].(map[string]any)
	if !ok {
		t.Fatalf("mac carries no custom_type; it has %v", mac)
	}
	if custom["type"] != "hwtypes.MACAddressType{}" || custom["value_type"] != "hwtypes.MACAddress" {
		t.Errorf("mac's custom type was written as %v", custom)
	}

	// Reached only through the shared endpoint map, which is the case a policy
	// author would otherwise have to transcribe twice by hand.
	kind := attributeAt(t, document, keys, "kind")
	validators, ok := kind["validators"].([]any)
	if !ok || len(validators) != 1 {
		t.Fatalf("source.kind carries %v, want one validator", kind["validators"])
	}
	entry := validators[0].(map[string]any)["custom"].(map[string]any)
	if entry["schema_definition"] != `stringvalidator.OneOf("any", "one")` {
		t.Errorf("source.kind's validator was written as %v", entry["schema_definition"])
	}
	imports, ok := entry["imports"].([]any)
	if !ok || len(imports) != 1 ||
		imports[0].(map[string]any)["path"] != "github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator" {
		t.Errorf("source.kind's validator imports were written as %v", entry["imports"])
	}
}

// Test_mergeKeepsWhatIsAlreadyThere checks that a considered decision already
// in the policy survives. Overwriting one would make a second run quietly
// revert a deliberate divergence from the hand-written schema.
func Test_mergeKeepsWhatIsAlreadyThere(t *testing.T) {
	path := writePolicy(t, mergePolicy)
	report, err := MergeIntoPolicy(path, derived(t))
	if err != nil {
		t.Fatalf("MergeIntoPolicy: %v", err)
	}
	rekey := attributeAt(t, readPolicy(t, path), []string{"fields"}, "rekey")
	fault, ok := rekey["default"].(map[string]any)
	if !ok {
		t.Fatalf("rekey lost its default entirely: %v", rekey)
	}
	if number, ok := fault["static"].(float64); !ok || number != 7 {
		t.Errorf("rekey's existing default was replaced with %v; the schema says 3600 and "+
			"the policy said 7, and a merge must not silently pick", fault["static"])
	}
	if !strings.Contains(report, "rekey default") {
		t.Errorf("the report does not mention keeping rekey's default:\n%s", report)
	}

	// A whole number must stay whole in the file. Decoding a policy through
	// float64 and writing it back turns 7 into 7.0, which is a diff on every
	// untouched attribute and a specification the generator reads differently.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading policy: %v", err)
	}
	if !strings.Contains(string(body), `"static": 7`) {
		t.Errorf("rekey's default is not written as a whole number; the file says:\n%s", body)
	}
}

// Test_mergeReportsAPolicyAttributeTheSchemaLacks is the check that makes this
// a rename check as well as a transcription.
//
// wlan renames nine fields, two of which nobody recovers by inspection. A
// wrong rename produces a policy attribute the schema does not have, and the
// failure to want is the loud one -- a merge that matched nothing and said
// nothing would leave the wrong name in place with an empty attribute.
func Test_mergeReportsAPolicyAttributeTheSchemaLacks(t *testing.T) {
	path := writePolicy(t, mergePolicy)
	report, err := MergeIntoPolicy(path, derived(t))
	if err != nil {
		t.Fatalf("MergeIntoPolicy: %v", err)
	}
	if !strings.Contains(report, "not_in_the_schema") {
		t.Errorf("a policy attribute the schema does not have went unreported:\n%s", report)
	}
	if !strings.Contains(report, "check the rename") {
		t.Errorf("the report names the attribute but not what to suspect:\n%s", report)
	}
}

// Test_mergeAcceptsADeclaredAttributeCarryingNoBehaviour is the other half of
// the rename check, and the half that decides whether anyone still reads it.
//
// Most attributes carry no validator, plan modifier, default or custom type.
// Indexing behaviour alone made every one of those read as a policy attribute
// the schema does not have: firewall_group, client_qos_rate and wireguard_peer
// each had correct renames reported that way, and a check that is wrong three
// surfaces running is a check people learn to skip -- which is when a real
// wrong rename gets through.
//
// source is declared by the fixture schema and carries no behaviour of its own,
// only children that do. It must not be reported, while not_in_the_schema above
// still must be. A change that simply stopped reporting would pass one of these
// two tests and fail the other.
func Test_mergeAcceptsADeclaredAttributeCarryingNoBehaviour(t *testing.T) {
	surface := derived(t)

	declared := false
	for _, attribute := range surface.Attributes {
		if attribute == "source" {
			declared = true
		}
	}
	if !declared {
		t.Fatalf("the fixture no longer declares source, so this proves nothing: %v",
			surface.Attributes)
	}
	for _, behaviour := range surface.Behaviours {
		if behaviour.Path == "source" {
			t.Fatalf("source now carries behaviour (%s), so it is no longer the case "+
				"this test was written for", behaviour.Kind)
		}
	}

	path := writePolicy(t, mergePolicy)
	report, err := MergeIntoPolicy(path, surface)
	if err != nil {
		t.Fatalf("MergeIntoPolicy: %v", err)
	}
	for _, line := range strings.Split(report, "\n") {
		if strings.TrimSpace(line) == "source" {
			t.Errorf("a declared attribute carrying no behaviour was reported as a "+
				"suspect rename:\n%s", report)
		}
	}
}

// Test_mergeReportsBehaviourWithNowhereToGo is the other direction: the schema
// has behaviour and the policy has no attribute for it. Silence there means a
// validator the released provider applies is dropped and the policy looks
// finished.
func Test_mergeReportsBehaviourWithNowhereToGo(t *testing.T) {
	path := writePolicy(t, `{
      "format_version": 1, "resource": "unifi_thing",
      "fields": [
        {"structural_name": "x_enabled", "terraform_name": "enabled", "disposition": "managed",
         "attribute": {"computed_optional_required": "computed_optional", "description": "d"}}
      ],
      "provider_owned": []
    }`)
	report, err := MergeIntoPolicy(path, derived(t))
	if err != nil {
		t.Fatalf("MergeIntoPolicy: %v", err)
	}
	if !strings.Contains(report, "NOWHERE TO GO") || !strings.Contains(report, "rekey") {
		t.Errorf("schema behaviour with no policy attribute went unreported:\n%s", report)
	}
}

// Test_mergeWritesBehaviourIntoAProviderOwnedAttribute covers the half of a
// policy that has no structural field behind it. site carries two plan
// modifiers on every migrated surface and bgp's asn and router_id carry
// validators; all of them were being reported as omitted and typed in by hand.
func Test_mergeWritesBehaviourIntoAProviderOwnedAttribute(t *testing.T) {
	path := writePolicy(t, `{
      "format_version": 1, "resource": "unifi_thing",
      "fields": [],
      "provider_owned": [
        {"terraform_name": "rekey", "terraform_type": "int64", "disposition": "managed",
         "generated": true,
         "attribute": {"computed_optional_required": "computed_optional", "description": "d"}}
      ]
    }`)
	report, err := MergeIntoPolicy(path, derived(t))
	if err != nil {
		t.Fatalf("MergeIntoPolicy: %v", err)
	}
	attribute := attributeAt(t, readPolicy(t, path), []string{"provider_owned"}, "rekey")
	fault, ok := attribute["default"].(map[string]any)
	if !ok || fault["static"] == nil {
		t.Fatalf("a provider-owned attribute's derived default was not written; it has %v\n%s",
			attribute, report)
	}
	if reportSection(report, "NOWHERE TO GO")["rekey"] {
		t.Errorf("a provider-owned attribute that was written into was still reported "+
			"as one the policy omits:\n%s", report)
	}
}

// reportSection returns the entries listed under the first heading containing
// title. The report is what a policy author reads, so a test that asserts on a
// substring of the whole thing cannot tell which heading an entry appeared
// under -- which is the entire distinction these two tests exist for.
func reportSection(report, title string) map[string]bool {
	entries := map[string]bool{}
	inside := false
	for _, line := range strings.Split(report, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasSuffix(trimmed, ":") && strings.Contains(trimmed, "("):
			inside = strings.Contains(trimmed, title)
		case trimmed == "":
		case inside:
			entries[trimmed] = true
		}
	}
	return entries
}

// Test_mergeSeparatesAProviderOwnedNestedMember keeps the one case the walk
// cannot reach from reading as the case it looks like. A provider-owned
// attribute's members are written in specification form, so behaviour under one
// has to be transcribed -- but reporting it as an attribute the policy omits
// sends the reader to move it into fields, where it has no structural_name to
// give.
func Test_mergeSeparatesAProviderOwnedNestedMember(t *testing.T) {
	surface := derived(t)
	nested := ""
	for _, behaviour := range surface.Behaviours {
		if root, _, found := strings.Cut(behaviour.Path, "."); found {
			nested, _ = behaviour.Path, root
			break
		}
	}
	if nested == "" {
		t.Fatal("the fixture derives no nested behaviour, so this proves nothing")
	}
	root, _, _ := strings.Cut(nested, ".")

	path := writePolicy(t, `{
      "format_version": 1, "resource": "unifi_thing",
      "fields": [],
      "provider_owned": [
        {"terraform_name": "`+root+`", "terraform_type": "single_nested",
         "disposition": "managed", "generated": true,
         "attribute": {"computed_optional_required": "required", "description": "d"}}
      ]
    }`)
	report, err := MergeIntoPolicy(path, surface)
	if err != nil {
		t.Fatalf("MergeIntoPolicy: %v", err)
	}
	if !reportSection(report, "NESTED INSIDE A PROVIDER-OWNED ATTRIBUTE")[nested] {
		t.Errorf("%s was not reported as nested inside a provider-owned attribute:\n%s",
			nested, report)
	}
	if reportSection(report, "NOWHERE TO GO")[nested] {
		t.Errorf("%s is still reported as an attribute the policy omits:\n%s", nested, report)
	}
}

// Test_mergeRefusesAGeneratedSurface stops the merge that cannot mean anything:
// a surface already serving generated code has no hand-written schema, so
// anything read from its source is not what it serves.
func Test_mergeRefusesAGeneratedSurface(t *testing.T) {
	path := writePolicy(t, mergePolicy)
	_, err := MergeIntoPolicy(path, Surface{
		TypeName:    "unifi_thing",
		Delegated:   true,
		DelegatedTo: "resource_thing.ThingResourceSchema(ctx)",
	})
	if err == nil {
		t.Fatal("merging from a surface that serves a generated schema was accepted")
	}
	if !strings.Contains(err.Error(), "no hand-written schema") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// Test_mergeLeavesTheFileAloneWhenNothingIsWritten keeps a no-op run from
// showing up as a change. A policy rewritten byte-for-byte still moves in a
// diff if the encoder disagrees with the author about formatting, and a
// migration is hard enough to review without that.
func Test_mergeLeavesTheFileAloneWhenNothingIsWritten(t *testing.T) {
	path := writePolicy(t, `{
      "format_version": 1, "resource": "unifi_thing",
      "fields": [], "provider_owned": []
    }`)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading policy: %v", err)
	}
	if _, err := MergeIntoPolicy(path, derived(t)); err != nil {
		t.Fatalf("MergeIntoPolicy: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading policy: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("a run that wrote no behaviour still rewrote the file:\nbefore:\n%s\nafter:\n%s",
			before, after)
	}
}
