package schemaparity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The fixtures below are not invented. They are the six differences measured on
// 2026-08-16 between a provider built from tag v0.101.2 and one built from
// forgejo/verify-main, both dumped through the same OpenTofu and canonicalised
// by cmd/schema-baseline. The old gate reported that entire result as
// "differ: char 139633, line 1".
//
// Keeping the real six matters more than covering more shapes: they include the
// two cases a synthetic fixture would not have thought of -- a nested attribute
// (dhcp_v6_server.start) and a change that is neither a flag nor a type
// (lte_lan's description).

func mustParse(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	return v
}

const releasedFixture = `{
  "resource_schemas": {
    "unifi_network": {"block": {"attributes": {
      "lte_lan":            {"optional": true, "type": "bool", "description": "Defaults to ` + "`true`" + `."},
      "ipv6_static_subnet": {"optional": true, "type": "string"},
      "dhcp_v6_server":     {"optional": true, "nested_type": {"attributes": {
          "start": {"optional": true, "type": "string"},
          "stop":  {"optional": true, "type": "string"},
          "lease": {"optional": true, "type": "number"}}}}}}},
    "unifi_wlan": {"block": {"attributes": {
      "ap_group_ids": {"optional": true, "type": ["set","string"]},
      "network_id":   {"optional": true, "type": "string"},
      "name":         {"required": true, "type": "string"}}}}
  }
}`

const candidateFixture = `{
  "resource_schemas": {
    "unifi_network": {"block": {"attributes": {
      "lte_lan":            {"optional": true, "type": "bool", "description": "Read back from the controller."},
      "ipv6_static_subnet": {"optional": true, "computed": true, "type": "string"},
      "dhcp_v6_server":     {"optional": true, "nested_type": {"attributes": {
          "start": {"optional": true, "computed": true, "type": "string"},
          "stop":  {"optional": true, "computed": true, "type": "string"},
          "lease": {"optional": true, "type": "number"}}}}}}},
    "unifi_wlan": {"block": {"attributes": {
      "ap_group_ids": {"optional": true, "computed": true, "type": ["set","string"]},
      "network_id":   {"optional": true, "computed": true, "type": "string"},
      "name":         {"required": true, "type": "string"}}}}
  }
}`

func realLedger() Ledger {
	entry := func(surface, attribute, field, old, new string) Entry {
		return Entry{
			Surface: surface, Attribute: attribute, Field: field, Old: old, New: new,
			WhyNotBreaking: "still Optional, so a configuration that sets it is unaffected",
			Evidence:       []Evidence{{Claim: "measured on the released provider", Kind: Measured, Source: "released contract"}},
		}
	}
	return Ledger{FormatVersion: 1, FromVersion: "0.101.2", ToVersion: "next", Entries: []Entry{
		entry("unifi_network", "lte_lan", "description", "Defaults to `true`.", "Read back from the controller."),
		entry("unifi_network", "ipv6_static_subnet", "computed", "false", "true"),
		entry("unifi_network", "dhcp_v6_server.start", "computed", "false", "true"),
		entry("unifi_network", "dhcp_v6_server.stop", "computed", "false", "true"),
		entry("unifi_wlan", "ap_group_ids", "computed", "false", "true"),
		entry("unifi_wlan", "network_id", "computed", "false", "true"),
	}}
}

// TestDiffReportsEveryDifferenceNotTheFirst is the assertion the old gate could
// not make. cmp stops at the first difference; this must find all six, with the
// nested one addressed the way a practitioner spells it.
func TestDiffReportsEveryDifferenceNotTheFirst(t *testing.T) {
	diffs := Diff(mustParse(t, releasedFixture), mustParse(t, candidateFixture))
	if len(diffs) != 6 {
		for _, d := range diffs {
			t.Logf("  %s", d)
		}
		t.Fatalf("found %d differences, want 6", len(diffs))
	}
	want := map[string]string{
		"unifi_network.lte_lan description":           "Defaults to `true`. -> Read back from the controller.",
		"unifi_network.ipv6_static_subnet computed":   " -> true",
		"unifi_network.dhcp_v6_server.start computed": " -> true",
		"unifi_network.dhcp_v6_server.stop computed":  " -> true",
		"unifi_wlan.ap_group_ids computed":            " -> true",
		"unifi_wlan.network_id computed":              " -> true",
	}
	for _, d := range diffs {
		key := d.Surface + "." + d.Attribute + " " + d.Field
		got := d.Old + " -> " + d.New
		if want[key] != got {
			t.Errorf("%s: got %q, want %q", key, got, want[key])
		}
		delete(want, key)
	}
	for key := range want {
		t.Errorf("never reported: %s", key)
	}
}

// TestDeclaredChangesPass is the NON-TRIVIAL control, and it is the primary one.
//
// A clean tree passing proves little: a gate that ignored the ledger entirely
// would also pass it. This one only passes if the ledger is genuinely consulted
// and every observed difference is matched to a declaration.
func TestDeclaredChangesPass(t *testing.T) {
	diffs := Diff(mustParse(t, releasedFixture), mustParse(t, candidateFixture))
	declared, undeclared, unmatched := Classify(diffs, realLedger())
	if len(declared) != 6 || len(undeclared) != 0 || len(unmatched) != 0 {
		t.Fatalf("declared=%d undeclared=%d unmatched=%d, want 6/0/0\n  undeclared: %v\n  unmatched: %v",
			len(declared), len(undeclared), len(unmatched), undeclared, unmatched)
	}
}

// TestIdenticalSchemasReportNothing is the other real control: sweep/merge-verify
// measured byte-identical to v0.101.2 on the same day, from two provider
// binaries with different SHA-256. A gate reporting differences there would be
// broken in the direction nobody notices, because the release would be blocked
// rather than wrongly admitted.
func TestIdenticalSchemasReportNothing(t *testing.T) {
	if diffs := Diff(mustParse(t, releasedFixture), mustParse(t, releasedFixture)); len(diffs) != 0 {
		t.Fatalf("identical schemas produced %d differences: %v", len(diffs), diffs)
	}
}

// TestUndeclaredChangeIsNamed is the mutation the lead asked for: delete one
// entry and require exactly one undeclared difference, reported BY NAME.
//
// Asserting the count alone would pass if the wrong difference were reported,
// which is the failure the whole evening turned on -- a check that fires for a
// reason other than the one it claims.
func TestUndeclaredChangeIsNamed(t *testing.T) {
	ledger := realLedger()
	ledger.Entries = append(ledger.Entries[:4], ledger.Entries[5:]...) // drop ap_group_ids

	_, undeclared, unmatched := Classify(Diff(mustParse(t, releasedFixture), mustParse(t, candidateFixture)), ledger)
	if len(undeclared) != 1 {
		t.Fatalf("got %d undeclared, want 1: %v", len(undeclared), undeclared)
	}
	if undeclared[0].Surface != "unifi_wlan" || undeclared[0].Attribute != "ap_group_ids" ||
		undeclared[0].Field != "computed" {
		t.Fatalf("undeclared difference is %v, want unifi_wlan.ap_group_ids computed", undeclared[0])
	}
	if len(unmatched) != 0 {
		t.Fatalf("unmatched entries = %v, want none", unmatched)
	}
}

// TestStaleDeclarationIsCaught closes the other direction. An entry matching no
// observed difference means a declared change was reverted or never made, and a
// gate that only asked "is every difference declared" would pass.
func TestStaleDeclarationIsCaught(t *testing.T) {
	ledger := realLedger()
	ledger.Entries = append(ledger.Entries, Entry{
		Surface: "unifi_wlan", Attribute: "name", Field: "required", Old: "true", New: "",
		WhyNotBreaking: "declared but never made",
		Evidence:       []Evidence{{Claim: "none", Kind: Inferred}},
	})
	_, undeclared, unmatched := Classify(Diff(mustParse(t, releasedFixture), mustParse(t, candidateFixture)), ledger)
	if len(undeclared) != 0 {
		t.Fatalf("undeclared = %v, want none", undeclared)
	}
	if len(unmatched) != 1 || unmatched[0].Attribute != "name" {
		t.Fatalf("unmatched = %v, want exactly the unifi_wlan.name entry", unmatched)
	}
}

// TestDriftToAThirdValueStopsMatching pins the property the ledger's own
// comment claims: an entry licenses one transition, not an attribute. If the
// built schema moves to some third value the declaration must stop applying.
func TestDriftToAThirdValueStopsMatching(t *testing.T) {
	drifted := mustParse(t, candidateFixture).(map[string]any)
	attrs := drifted["resource_schemas"].(map[string]any)["unifi_wlan"].(map[string]any)["block"].(map[string]any)["attributes"].(map[string]any)
	attrs["ap_group_ids"].(map[string]any)["computed"] = "yes-please"

	_, undeclared, _ := Classify(Diff(mustParse(t, releasedFixture), drifted), realLedger())
	if len(undeclared) != 1 || undeclared[0].New != "yes-please" {
		t.Fatalf("undeclared = %v, want the drifted ap_group_ids value", undeclared)
	}
}

// TestLedgerValidationRefusesRubberStamps proves each validation rule fires,
// one at a time, so a rule that stopped working could not hide behind another.
func TestLedgerValidationRefusesRubberStamps(t *testing.T) {
	valid := Entry{Surface: "s", Attribute: "a", Field: "f", Old: "x", New: "y",
		WhyNotBreaking: "because", Evidence: []Evidence{{Claim: "c", Kind: Measured}}}
	tests := map[string]struct {
		mutate func(*Entry)
		want   string
	}{
		"missing field":    {func(e *Entry) { e.Field = "" }, "surface, attribute and field are all required"},
		"licenses nothing": {func(e *Entry) { e.New = e.Old }, "licenses nothing"},
		"no argument":      {func(e *Entry) { e.WhyNotBreaking = "" }, "rubber stamp"},
		"no evidence":      {func(e *Entry) { e.Evidence = nil }, "no evidence"},
		"unknown kind":     {func(e *Entry) { e.Evidence[0].Kind = "vibes" }, `want "measured" or "inferred"`},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			entry := valid
			entry.Evidence = append([]Evidence(nil), valid.Evidence...)
			test.mutate(&entry)
			err := Ledger{Entries: []Entry{entry}}.Validate("fixture")
			if err == nil {
				t.Fatalf("entry was accepted, want refusal containing %q", test.want)
			}
			if !contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to contain %q", err, test.want)
			}
		})
	}
	if err := (Ledger{Entries: []Entry{valid}}).Validate("fixture"); err != nil {
		t.Fatalf("the unmutated control was refused: %v", err)
	}
}

// TestMissingLedgerIsNotAnError pins the deliberate asymmetry: absent is empty,
// present-but-broken is fatal.
func TestMissingLedgerIsNotAnError(t *testing.T) {
	ledger, err := LoadLedger(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || len(ledger.Entries) != 0 {
		t.Fatalf("absent ledger gave (%v, %v), want empty and no error", ledger, err)
	}
	broken := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLedger(broken); err == nil {
		t.Fatal("a ledger that exists and cannot be parsed was treated as empty")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}())
}

// TestNestedAndBlockTypeAttributesResolve pins the shapes the first version of
// coordinates could not name. Measured over the committed contract's 6318 leaf
// paths, that version resolved 5151 and left 1167 undeclarable -- 448 of them
// under block_types alone. A difference with no coordinates cannot be matched
// to a ledger entry, so those would have been reported and then impossible to
// declare: a gate with no way to go green.
func TestNestedAndBlockTypeAttributesResolve(t *testing.T) {
	tests := map[string]struct{ surface, attribute, field string }{
		"/resource_schemas/unifi_network/block/attributes/lte_lan/computed":                                          {"unifi_network", "lte_lan", "computed"},
		"/resource_schemas/unifi_network/block/attributes/dhcp_v6_server/nested_type/attributes/start/computed":      {"unifi_network", "dhcp_v6_server.start", "computed"},
		"/resource_schemas/unifi_wlan/block/attributes/a/nested_type/attributes/b/nested_type/attributes/c/optional": {"unifi_wlan", "a.b.c", "optional"},
		"/resource_schemas/unifi_device/block/block_types/port_override/block/attributes/port_idx/required":          {"unifi_device", "port_override.port_idx", "required"},
		"/list_resource_schemas/unifi_network/block/block_types/filter/block/attributes/name/optional":               {"unifi_network", "filter.name", "optional"},
		"/provider/block/attributes/api_key/sensitive":                                                               {"provider", "api_key", "sensitive"},
	}
	for path, want := range tests {
		s, a, f := coordinates(path)
		if s != want.surface || a != want.attribute || f != want.field {
			t.Errorf("%s\n  got  %q %q %q\n  want %q %q %q", path, s, a, f, want.surface, want.attribute, want.field)
		}
	}
}

// TestContainerLevelChangesAreReportedButNotDeclarable states the limit rather
// than hiding it. 467 of the contract's leaves are container-level -- a
// surface's version among them, which is what a state upgrader moves. Those
// have no ledger coordinates, so they surface by Path and cannot be licensed.
// Inventing coordinates would let an entry naming an attribute license a change
// to its container, which is worse than a red gate.
func TestContainerLevelChangesAreReportedButNotDeclarable(t *testing.T) {
	for _, path := range []string{
		"/resource_schemas/unifi_network/block/version",
		"/resource_schemas/unifi_network/block/block_types/x/nesting_mode",
		"/format_version",
	} {
		if s, a, f := coordinates(path); s != "" || a != "" || f != "" {
			t.Errorf("%s resolved to %q %q %q, want no coordinates", path, s, a, f)
		}
	}
	released := mustParse(t, `{"resource_schemas":{"unifi_network":{"block":{"version":0,"attributes":{}}}}}`)
	candidate := mustParse(t, `{"resource_schemas":{"unifi_network":{"block":{"version":1,"attributes":{}}}}}`)
	diffs := Diff(released, candidate)
	if len(diffs) != 1 {
		t.Fatalf("got %d differences, want 1: %v", len(diffs), diffs)
	}
	if diffs[0].Surface != "" || diffs[0].Path != "/resource_schemas/unifi_network/block/version" {
		t.Fatalf("difference = %+v, want it reported by path with no coordinates", diffs[0])
	}
	_, undeclared, _ := Classify(diffs, realLedger())
	if len(undeclared) != 1 {
		t.Fatalf("a container-level change was not reported as undeclared: %v", undeclared)
	}
}
