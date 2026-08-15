package releasequalification

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// TestEvidenceModesCoverTheCommittedInventory runs the mode rules over the
// COMMITTED evidence artifacts rather than a fixture.
//
// This exists because migration_test.go builds its own policy, its own
// inventory and its own controller receipt, so every assertion in it is true by
// construction. That file proved the rules behave; it cannot prove they say
// anything about this repository. A mode that has never been pointed at the
// real catalog is a mode nobody has tested.
//
// WHAT IT ASSUMES, and why the assumption is safe: no controller receipt is
// committed, so this grants the most favourable campaign imaginable -- every
// planned test passes on both providers. That makes the result an UPPER BOUND.
// A surface with no mode here can never acquire one from a better campaign,
// because there is no better campaign. Surfaces that do get a mode here are not
// thereby proven recovered; they are proven not to be blocked by their own
// evidence shape.
//
// It is deliberately sensitive to build/release-ready/catalog-evidence-inventory.json.
// That file currently records seven changed runtimes while sixty-four surfaces
// have actually been converted. When it is regenerated to tell the truth, this
// test moves, loudly, in a Go package -- which is the whole point. Discovering
// it an hour into a controller campaign is what the numbers below are here to
// prevent.
func TestEvidenceModesCoverTheCommittedInventory(t *testing.T) {
	inventory := committedInventory(t)
	view := bestCaseEvidenceView(t, inventory)

	census := map[string]int{}
	unjustified := make([]string, 0)
	for _, surface := range inventory.Surfaces {
		mode, err := view.evidenceMode(catalogparity.MigrationEntry{SurfaceKey: surface.SurfaceKey})
		if err != nil {
			if !strings.Contains(err.Error(), "has no evidence mode") {
				t.Fatalf("%s/%s: %v", surface.Kind, surface.Name, err)
			}
			unjustified = append(unjustified, string(surface.Kind)+"/"+surface.Name)
			continue
		}
		census[mode]++
	}
	sort.Strings(unjustified)

	// These numbers describe the committed inventory. They are recorded rather
	// than computed so that regenerating it produces a diff a human has to read
	// and agree with.
	//
	// They have already moved twice, and both moves were the point. Against the
	// pre-conversion inventory only source_identity was reachable and seven
	// surfaces had nothing behind them, because that inventory carried file
	// digests and no per-scenario comparisons. Against the regenerated one, 64
	// surfaces are converted, scenarios are measured, and 45 of them earn
	// differential_scenario. The tree got worse before it got better and this
	// test said so at both steps.
	//
	// CAVEAT ON pragmatic_reference, which is OPTIMISTIC here. This harness
	// takes its release blockers from the committed
	// catalog-pragmatic-resolution.json, and that artifact was produced from
	// the pre-conversion inventory. ResolvePragmaticReferences requires both a
	// gap's own surface and the surface it references to have an identical
	// runtime (pragmatic_reference.go:150 and :167), and on the regenerated
	// inventory all seven references fail that. So the 1 below is reachable
	// only while a resolution built from a stale inventory is still on disk.
	// Regenerating it turns that 1 into a 0 and this surface into an
	// eighteenth unjustified one.
	want := map[string]int{
		EvidenceSourceIdentity:       3,
		EvidenceDifferentialScenario: 45,
		EvidencePragmaticReference:   1,
	}
	wantUnjustified := []string{
		"action/unifi_port",
		"data_source/unifi_ap_group",
		"data_source/unifi_client_qos_rate",
		"data_source/unifi_dns_record",
		"data_source/unifi_firewall_zone",
		"data_source/unifi_port_profile",
		"list_resource/unifi_ap_group",
		"list_resource/unifi_device",
		"list_resource/unifi_firewall_policy",
		"list_resource/unifi_firewall_zone",
		"list_resource/unifi_power_supervisor",
		"list_resource/unifi_site_to_site_vpn",
		"list_resource/unifi_wan",
		"managed_resource/unifi_device",
		"managed_resource/unifi_firewall_policy",
		"managed_resource/unifi_firewall_zone",
		"managed_resource/unifi_power_supervisor",
		"managed_resource/unifi_site_to_site_vpn",
	}

	for mode, count := range want {
		if census[mode] != count {
			t.Errorf("evidence mode %s covers %d surfaces, expected %d", mode, census[mode], count)
		}
	}
	for mode, count := range census {
		if _, expected := want[mode]; !expected {
			t.Errorf("evidence mode %s covers %d surfaces and was not expected at all", mode, count)
		}
	}
	if strings.Join(unjustified, ",") != strings.Join(wantUnjustified, ",") {
		t.Errorf("surfaces with no evidence mode are\n  %s\nexpected\n  %s",
			strings.Join(unjustified, "\n  "), strings.Join(wantUnjustified, "\n  "))
	}

	// The census must account for every surface. Without this a rule that
	// silently dropped surfaces would shrink both sides of the comparison
	// together and still look right.
	total := len(unjustified)
	for _, count := range census {
		total += count
	}
	if total != len(inventory.Surfaces) {
		t.Errorf("census covers %d surfaces, the inventory has %d", total, len(inventory.Surfaces))
	}
}

func committedInventory(t *testing.T) catalogparity.EvidenceInventory {
	t.Helper()
	data, err := os.ReadFile("../../build/release-ready/catalog-evidence-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory catalogparity.EvidenceInventory
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Surfaces) == 0 {
		t.Fatal("the committed inventory has no surfaces, so every count below would be vacuous")
	}
	return inventory
}

// bestCaseEvidenceView grants every campaign-shaped input its most favourable
// value, so the only thing that can deny a surface a mode is the surface's own
// evidence shape.
func bestCaseEvidenceView(
	t *testing.T,
	inventory catalogparity.EvidenceInventory,
) evidenceView {
	t.Helper()

	data, err := os.ReadFile("../../build/restricted/catalog-pragmatic-resolution.json")
	if err != nil {
		t.Fatal(err)
	}
	var resolution catalogparity.PragmaticResolution
	if err := json.Unmarshal(data, &resolution); err != nil {
		t.Fatal(err)
	}
	if len(resolution.Resolved) == 0 {
		t.Fatal("the committed pragmatic resolution resolves nothing, so pragmatic_reference would be untested")
	}
	blockers := map[catalogparity.SurfaceKey][]string{}
	for _, gap := range resolution.Remaining {
		blockers[gap.SurfaceKey] = append(blockers[gap.SurfaceKey], gap.Signal)
	}

	view := evidenceView{
		inventory: make(map[catalogparity.SurfaceKey]catalogparity.SurfaceEvidenceInventory, len(inventory.Surfaces)),
		admission: make(map[catalogparity.SurfaceKey]catalogparity.SurfaceAdmission, len(inventory.Surfaces)),
		planned:   make(map[catalogparity.SurfaceKey][]string, len(inventory.Surfaces)),
		released:  map[string]struct{}{},
		candidate: map[string]struct{}{},
	}
	for _, surface := range inventory.Surfaces {
		view.inventory[surface.SurfaceKey] = surface
		view.admission[surface.SurfaceKey] = catalogparity.SurfaceAdmission{
			SurfaceKey:      surface.SurfaceKey,
			ReleaseBlockers: blockers[surface.SurfaceKey],
		}
		planned := plannedAcceptanceNames(surface)
		view.planned[surface.SurfaceKey] = planned
		for _, name := range planned {
			view.released[name] = struct{}{}
			view.candidate[name] = struct{}{}
		}
	}
	return view
}

// plannedAcceptanceNames applies the jq plan builder's kind filter from
// .woodpecker/scripts/catalog-controller-differential.sh:22-29. It is written
// out rather than imported because the plan builder is a shell script; keeping
// the rule in one Go place would mean pretending otherwise.
func plannedAcceptanceNames(surface catalogparity.SurfaceEvidenceInventory) []string {
	names := make([]string, 0, len(surface.TestFunctions))
	for _, name := range surface.TestFunctions {
		if !strings.HasPrefix(name, "TestAcc") {
			continue
		}
		isList := strings.Contains(name, "List")
		switch surface.Kind {
		case catalogparity.ManagedResource:
			if isList {
				continue
			}
		case catalogparity.ListResource:
			if !isList {
				continue
			}
		}
		names = append(names, name)
	}
	return names
}
