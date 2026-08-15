package catalogparity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestResolvePragmaticReferencesResolvesOnlyDeclaredIdenticalRuntimeGap(t *testing.T) {
	target := SurfaceKey{Kind: ManagedResource, Name: "unifi_firewall_zone"}
	source := SurfaceKey{Kind: ListResource, Name: "unifi_firewall_zone"}
	inventoryDigest := strings.Repeat("a", 64)
	fleetDigest := strings.Repeat("b", 64)
	shapeDigest := strings.Repeat("c", 64)
	inventory := EvidenceInventory{
		FormatVersion:   1,
		ProviderAddress: CanonicalProviderAddress,
		Surfaces: []SurfaceEvidenceInventory{
			{
				SurfaceKey:     target,
				Runtime:        FileComparison{Status: FileIdentical},
				MissingSignals: []string{"acceptance"},
			},
			{
				SurfaceKey:     source,
				Runtime:        FileComparison{Status: FileIdentical},
				MissingSignals: []string{},
				// A lender must have an acceptance scenario the released
				// provider also ran; identical runtime is no longer the test.
				Scenarios: []ScenarioComparison{{
					Name: "TestAccSourceLendable", Status: ScenarioIdentical,
					ReleasedSHA256: strings.Repeat("1", 64), CandidateSHA256: strings.Repeat("1", 64),
				}},
			},
		},
	}
	references := PragmaticReferenceSet{
		FormatVersion:      1,
		ProviderAddress:    CanonicalProviderAddress,
		InventorySHA256:    inventoryDigest,
		FleetSummarySHA256: fleetDigest,
		References: []PragmaticReference{{
			SurfaceKey:       target,
			Signal:           "acceptance",
			ReferenceKind:    FleetShapeReference,
			Source:           source,
			FleetCount:       4,
			ShapeSHA256:      shapeDigest,
			RequiredEvidence: []string{"controller_list", "mapping", "plan_shape"},
		}},
	}
	fleet := FleetReferenceSummary{
		FormatVersion:   1,
		ProviderAddress: CanonicalProviderAddress,
		Surfaces: []FleetSurfaceReference{{
			SurfaceKey:           target,
			Count:                4,
			ConfiguredAttributes: []string{"name", "network_ids", "site"},
			ShapeSHA256:          shapeDigest,
		}},
	}

	resolution, err := ResolvePragmaticReferences(inventory, inventoryDigest, fleet, fleetDigest, references)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Result != "references_complete" || resolution.ResolvedSignalCount != 1 || resolution.RemainingSignalCount != 0 {
		t.Fatalf("resolution = %+v", resolution)
	}
	want := []ResolvedPragmaticSignal{{
		SurfaceKey:    target,
		Signal:        "acceptance",
		ReferenceKind: FleetShapeReference,
		Source:        source,
		FleetCount:    4,
		ShapeSHA256:   shapeDigest,
	}}
	if !reflect.DeepEqual(resolution.Resolved, want) {
		t.Fatalf("resolved = %+v, want %+v", resolution.Resolved, want)
	}
}

func TestResolvePragmaticReferencesRejectsUnsafeOrUnmeasuredSubstitutions(t *testing.T) {
	target := SurfaceKey{Kind: ManagedResource, Name: "unifi_firewall_zone"}
	source := SurfaceKey{Kind: ListResource, Name: "unifi_firewall_zone"}
	inventoryDigest := strings.Repeat("a", 64)
	validInventory := EvidenceInventory{
		FormatVersion:   1,
		ProviderAddress: CanonicalProviderAddress,
		Surfaces: []SurfaceEvidenceInventory{
			{SurfaceKey: target, Runtime: FileComparison{Status: FileIdentical}, MissingSignals: []string{"acceptance"}},
			{SurfaceKey: source, Runtime: FileComparison{Status: FileIdentical}, Scenarios: []ScenarioComparison{{
				Name: "TestAccSourceLendable", Status: ScenarioIdentical,
				ReleasedSHA256: strings.Repeat("1", 64), CandidateSHA256: strings.Repeat("1", 64),
			}}},
		},
	}
	validReferences := PragmaticReferenceSet{
		FormatVersion:      1,
		ProviderAddress:    CanonicalProviderAddress,
		InventorySHA256:    inventoryDigest,
		FleetSummarySHA256: strings.Repeat("b", 64),
		References: []PragmaticReference{{
			SurfaceKey:       target,
			Signal:           "acceptance",
			ReferenceKind:    FleetShapeReference,
			Source:           source,
			FleetCount:       4,
			ShapeSHA256:      strings.Repeat("c", 64),
			RequiredEvidence: []string{"controller_list", "mapping", "plan_shape"},
		}},
	}
	validFleet := FleetReferenceSummary{
		FormatVersion:   1,
		ProviderAddress: CanonicalProviderAddress,
		Surfaces: []FleetSurfaceReference{{
			SurfaceKey:           target,
			Count:                4,
			ConfiguredAttributes: []string{"name", "network_ids", "site"},
			ShapeSHA256:          strings.Repeat("c", 64),
		}},
	}

	tests := map[string]struct {
		mutate func(*EvidenceInventory, *FleetReferenceSummary, *PragmaticReferenceSet)
		want   string
	}{
		// The rule these three replace -- that the surface CARRYING the gap must
		// have an unchanged runtime -- was removed deliberately, so mutating it
		// proves nothing any more. What must fail now is a SOURCE that cannot
		// lend: its scenario moved, the released provider never ran it, or it
		// has none at all.
		"source scenario changed": {
			mutate: func(inventory *EvidenceInventory, _ *FleetReferenceSummary, _ *PragmaticReferenceSet) {
				inventory.Surfaces[1].Scenarios[0].Status = ScenarioChanged
			},
			want: "not identical to the released provider's",
		},
		"source scenario added only": {
			mutate: func(inventory *EvidenceInventory, _ *FleetReferenceSummary, _ *PragmaticReferenceSet) {
				inventory.Surfaces[1].Scenarios[0].Status = ScenarioAdded
			},
			want: "not identical to the released provider's",
		},
		"source declares no scenario": {
			mutate: func(inventory *EvidenceInventory, _ *FleetReferenceSummary, _ *PragmaticReferenceSet) {
				inventory.Surfaces[1].Scenarios = nil
			},
			want: "declares no acceptance scenario",
		},
		"stale inventory digest": {
			mutate: func(_ *EvidenceInventory, _ *FleetReferenceSummary, references *PragmaticReferenceSet) {
				references.InventorySHA256 = strings.Repeat("d", 64)
			},
			want: "inventory SHA-256",
		},
		"missing source evidence": {
			mutate: func(inventory *EvidenceInventory, _ *FleetReferenceSummary, _ *PragmaticReferenceSet) {
				inventory.Surfaces[1].MissingSignals = []string{"list_acceptance"}
			},
			want: "source surface",
		},
		"unmeasured fleet shape": {
			mutate: func(_ *EvidenceInventory, _ *FleetReferenceSummary, references *PragmaticReferenceSet) {
				references.References[0].ShapeSHA256 = ""
			},
			want: "shape SHA-256",
		},
		"undeclared signal": {
			mutate: func(_ *EvidenceInventory, _ *FleetReferenceSummary, references *PragmaticReferenceSet) {
				references.References[0].Signal = "import"
			},
			want: "is not missing",
		},
		"fleet count mismatch": {
			mutate: func(_ *EvidenceInventory, fleet *FleetReferenceSummary, _ *PragmaticReferenceSet) {
				fleet.Surfaces[0].Count = 3
			},
			want: "fleet count",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			inventory := validInventory
			inventory.Surfaces = append([]SurfaceEvidenceInventory(nil), validInventory.Surfaces...)
			fleet := validFleet
			fleet.Surfaces = append([]FleetSurfaceReference(nil), validFleet.Surfaces...)
			references := validReferences
			references.References = append([]PragmaticReference(nil), validReferences.References...)
			test.mutate(&inventory, &fleet, &references)
			_, err := ResolvePragmaticReferences(inventory, inventoryDigest, fleet, strings.Repeat("b", 64), references)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ResolvePragmaticReferences() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestPragmaticReferencePolicyResolvesExactlyEightCatalogGaps(t *testing.T) {
	inventoryData, err := os.ReadFile("../../build/release-ready/catalog-evidence-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory EvidenceInventory
	if err := json.Unmarshal(inventoryData, &inventory); err != nil {
		t.Fatal(err)
	}
	fleetData, err := os.ReadFile("../../build/restricted/catalog-fleet-gap-summary.json")
	if err != nil {
		t.Fatal(err)
	}
	var fleet FleetReferenceSummary
	decodeStrict(t, fleetData, &fleet)
	referenceData, err := os.ReadFile("../../provider-codegen/policy/catalog-pragmatic-references.json")
	if err != nil {
		t.Fatal(err)
	}
	var references PragmaticReferenceSet
	decodeStrict(t, referenceData, &references)

	resolution, err := ResolvePragmaticReferences(
		inventory,
		testSHA256(inventoryData),
		fleet,
		testSHA256(fleetData),
		references,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Result != "blocked_evidence" || resolution.ResolvedSignalCount != 4 || resolution.RemainingSignalCount != 4 {
		t.Fatalf("resolution = %+v", resolution)
	}
	// Three references were withdrawn because their sources' only acceptance
	// scenario is `added` -- the released provider never ran it, so there is no
	// before-and-after to borrow. Those gaps are now release blockers, which is
	// the honest state rather than a worse one.
	wantRemaining := []EvidenceGap{
		{SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_firewall_policy"}, Signal: "acceptance"},
		{SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_power_supervisor"}, Signal: "acceptance"},
		{SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_site_to_site_vpn"}, Signal: "acceptance"},
		{SurfaceKey: SurfaceKey{Kind: Action, Name: "unifi_port"}, Signal: "hardware_claim"},
	}
	if !reflect.DeepEqual(resolution.Remaining, wantRemaining) {
		t.Fatalf("remaining = %+v, want %+v", resolution.Remaining, wantRemaining)
	}
	wantKinds := map[PragmaticReferenceKind]int{
		AliasReference:       3,
		SiblingReadReference: 1,
	}
	gotKinds := make(map[PragmaticReferenceKind]int)
	for _, signal := range resolution.Resolved {
		gotKinds[signal.ReferenceKind]++
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("reference kinds = %v, want %v", gotKinds, wantKinds)
	}
}

func testSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
