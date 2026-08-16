package releasequalification

import (
	"crypto/sha256"
	"encoding/hex"
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
	// pragmatic_reference is 1 and it is now EARNED rather than optimistic. It
	// used to rest on the committed resolution artifact, which was produced
	// before the conversion and still claims seven signals resolved; this
	// harness resolves from committed inputs instead, under the same rule the
	// gate applies. data_source/unifi_client_info keeps it because its lender,
	// data_source/unifi_client_info_list, has an acceptance scenario identical
	// to the released provider's.
	//
	// Withdrawing three references for lending from `added`-only sources did
	// NOT move this census, which is worth stating because it is surprising.
	// managed firewall_policy, power_supervisor and site_to_site_vpn were
	// already unjustified here for an independent reason -- their scenario
	// files changed, so pragmatic_reference refused them whether or not their
	// references resolved. What that change moved was the release blocker
	// count, from one to four.
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

	// The release blockers are RESOLVED from committed inputs rather than read
	// from build/restricted/catalog-pragmatic-resolution.json.
	//
	// That file is a campaign output built from the PRE-conversion inventory:
	// it still reports seven signals resolved and one remaining, when the
	// current rules resolve four and leave four. Reading it made this census
	// optimistic about pragmatic_reference in a way a reader could not see.
	// Resolving here means the count is earned by the same rules the gate
	// applies, against the same committed artifacts.
	resolution := resolveCommittedPragmatic(t)

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
		// Read from the committed campaign policy rather than listed here. A
		// census carrying its own copy of the declaration would agree with
		// itself instead of with the policy the gate reads.
		declaredMissing: committedDeclaredMissing(t),
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

// resolveCommittedPragmatic runs the reference resolver over the committed
// inventory, fleet summary and reference policy -- all committed INPUTS, unlike
// the resolution artifact, which only a campaign can produce.
func resolveCommittedPragmatic(t *testing.T) catalogparity.PragmaticResolution {
	t.Helper()
	inventoryData, err := os.ReadFile("../../build/release-ready/catalog-evidence-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory catalogparity.EvidenceInventory
	if err := json.Unmarshal(inventoryData, &inventory); err != nil {
		t.Fatal(err)
	}
	fleetData, err := os.ReadFile("../../build/restricted/catalog-fleet-gap-summary.json")
	if err != nil {
		t.Fatal(err)
	}
	var fleet catalogparity.FleetReferenceSummary
	if err := json.Unmarshal(fleetData, &fleet); err != nil {
		t.Fatal(err)
	}
	referenceData, err := os.ReadFile("../../provider-codegen/policy/catalog-pragmatic-references.json")
	if err != nil {
		t.Fatal(err)
	}
	var references catalogparity.PragmaticReferenceSet
	if err := json.Unmarshal(referenceData, &references); err != nil {
		t.Fatal(err)
	}
	resolution, err := catalogparity.ResolvePragmaticReferences(
		inventory, sha256Hex(inventoryData), fleet, sha256Hex(fleetData), references)
	if err != nil {
		t.Fatalf("resolving the committed pragmatic references: %v", err)
	}
	return resolution
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// committedDeclaredMissing reads the tests the campaign policy states the
// released provider cannot run.
//
// The census must learn the declaration from the same file the gate does.
// Hard-coding the names here would make this test agree with a copy rather
// than with the policy, which is the second-home failure this project keeps
// paying for.
func committedDeclaredMissing(t *testing.T) map[string]struct{} {
	t.Helper()
	data, err := os.ReadFile("../../provider-codegen/policy/catalog-campaign.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy catalogparity.CampaignPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if len(policy.ReleasedAllowedMissing) == 0 {
		t.Fatal("the campaign policy declares no released_allowed_missing, so this " +
			"census cannot tell an excused test from an unmeasured one")
	}
	declared := make(map[string]struct{}, len(policy.ReleasedAllowedMissing))
	for _, name := range policy.ReleasedAllowedMissing {
		declared[name] = struct{}{}
	}
	return declared
}
