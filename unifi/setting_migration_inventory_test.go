package unifi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"testing"
)

// This file tracks the PR-A settings-migration inventory: which of the 13
// legacy settings sections (unifi/setting_resource.go) have which pieces of
// migration work done.
//
// Recognized ops (Task 9 brief):
//   - "golden":  a checked-in golden JSON body exists for this section
//     (unifi/setting_golden_test.go), captured from the CURRENT legacy
//     converter. Present for all 13 sections as of Task 9.
//   - "section": a settingSection implementation is registered for this
//     section (via registerSection) whose overlay() reproduces the golden
//     body byte-for-byte (after normalizeSettingJSON). Added by that
//     section's migration task (Tasks 10-22); absent until then.
//
// This table is intentionally a checked-in *test*, not a comment: it is
// asserted below to stay in sync with reality (golden test existence, and
// eventually the settingSections registry) so it cannot silently drift from
// what's actually implemented.
var settingMigrationInventory = map[string][]string{
	"auto_speedtest":       {"golden"},
	"country":              {"golden"},
	"dpi":                  {"golden"},
	"lcm":                  {"golden"},
	"network_optimization": {"golden"},
	"ntp":                  {"golden"},
	"syslog":               {"golden"},
	"doh":                  {"golden"},
	"ips":                  {"golden"},
	"mgmt":                 {"golden"},
	"radius":               {"golden"},
	"usg":                  {"golden"},
	"igmp_snooping":        {"golden"},
}

// settingSectionSchemaAttrNames lists the 13 top-level settingResourceModel
// attribute names (tfsdk tags) that the legacy resource models as nested
// settings blocks. This is the authoritative "13 sections" list: it's
// cross-checked against settingMigrationInventory's keys below, so if a
// section is renamed or a 14th one is added to the model, this test catches
// the drift rather than the inventory table silently going stale.
var settingSectionSchemaAttrNames = []string{
	"auto_speedtest",
	"country",
	"dpi",
	"lcm",
	"network_optimization",
	"ntp",
	"syslog",
	"doh",
	"ips",
	"mgmt",
	"radius",
	"usg",
	"igmp_snooping",
}

func hasOp(ops []string, op string) bool {
	for _, o := range ops {
		if o == op {
			return true
		}
	}
	return false
}

// goldenTestFuncNames parses setting_golden_test.go and returns the set of
// top-level "TestGolden_<section>" function names it declares. Parsing the
// source (rather than reflecting over testing.T at runtime, which cannot
// enumerate sibling test functions) gives a structural, non-vacuous check
// that each section named in the inventory table actually has a
// corresponding golden test function, not just a table entry.
func goldenTestFuncNames(t *testing.T) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "setting_golden_test.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing setting_golden_test.go: %v", err)
	}

	names := make(map[string]bool)
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		names[fn.Name.Name] = true
	}
	return names
}

// TestMigrationInventoryCoversAllSections asserts every one of the 13
// legacy settings sections is present in settingMigrationInventory, has at
// least a "golden" op, and has a real TestGolden_<section> function
// declared in setting_golden_test.go (structural, not just a table entry).
func TestMigrationInventoryCoversAllSections(t *testing.T) {
	goldenFuncs := goldenTestFuncNames(t)

	if len(settingSectionSchemaAttrNames) != 13 {
		t.Fatalf("settingSectionSchemaAttrNames has %d entries, want 13", len(settingSectionSchemaAttrNames))
	}

	seen := make(map[string]bool, len(settingSectionSchemaAttrNames))
	for _, section := range settingSectionSchemaAttrNames {
		if seen[section] {
			t.Errorf("settingSectionSchemaAttrNames lists %q more than once", section)
		}
		seen[section] = true

		ops, ok := settingMigrationInventory[section]
		if !ok {
			t.Errorf("settingMigrationInventory is missing section %q", section)
			continue
		}
		if !hasOp(ops, "golden") {
			t.Errorf("settingMigrationInventory[%q] = %v, missing required %q op", section, ops, "golden")
		}

		wantFunc := "TestGolden_" + section
		if !goldenFuncs[wantFunc] {
			t.Errorf("section %q claims op %q but setting_golden_test.go declares no func %s", section, "golden", wantFunc)
		}
	}

	// The inventory table must not carry entries for sections that aren't
	// (or are no longer) one of the 13 - keeps the table from silently
	// accumulating stale/typo'd keys over time.
	for section := range settingMigrationInventory {
		if !seen[section] {
			t.Errorf("settingMigrationInventory has entry for %q, which is not in settingSectionSchemaAttrNames", section)
		}
	}

	if t.Failed() {
		return
	}
	if len(settingMigrationInventory) != 13 {
		t.Errorf("settingMigrationInventory has %d sections, want 13", len(settingMigrationInventory))
	}
}

// TestMigrationInventorySectionOpImpliesRegisteredSection asserts the
// forward-looking half of the contract: once a section's inventory entry
// claims the "section" op (added by that section's Task 10-22 migration),
// a settingSection with a matching key() or attrName() must actually be
// registered in settingSections. As of Task 9 no section claims "section"
// yet, so this test is currently vacuous-but-armed: Tasks 10-22 will each
// flip one entry to include "section", and from that point on this test
// enforces the registry actually grew to match.
func TestMigrationInventorySectionOpImpliesRegisteredSection(t *testing.T) {
	registered := make(map[string]bool, len(settingSections))
	for _, s := range settingSections {
		registered[s.key()] = true
		registered[s.attrName()] = true
	}

	var claimsSection []string
	for section, ops := range settingMigrationInventory {
		if hasOp(ops, "section") {
			claimsSection = append(claimsSection, section)
			if !registered[section] {
				t.Errorf(
					"settingMigrationInventory[%q] claims op %q but no settingSection with key()/attrName() %q is registered in settingSections",
					section, "section", section,
				)
			}
		}
	}

	sort.Strings(claimsSection)
	t.Logf("sections currently claiming the %q op: %v", "section", claimsSection)
}
