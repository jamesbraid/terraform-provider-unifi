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
			{SurfaceKey: source, Runtime: FileComparison{Status: FileIdentical}},
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
		"changed target runtime": {
			mutate: func(inventory *EvidenceInventory, _ *FleetReferenceSummary, _ *PragmaticReferenceSet) {
				inventory.Surfaces[0].Runtime.Status = FileChanged
			},
			want: "runtime is changed",
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

func TestPragmaticReferencePolicyResolvesExactlyNineCatalogGaps(t *testing.T) {
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
	if resolution.Result != "blocked_evidence" || resolution.ResolvedSignalCount != 9 || resolution.RemainingSignalCount != 2 {
		t.Fatalf("resolution = %+v", resolution)
	}
	wantRemaining := []EvidenceGap{
		{SurfaceKey: SurfaceKey{Kind: Action, Name: "unifi_port"}, Signal: "action_acceptance"},
		{SurfaceKey: SurfaceKey{Kind: Action, Name: "unifi_port"}, Signal: "hardware_claim"},
	}
	if !reflect.DeepEqual(resolution.Remaining, wantRemaining) {
		t.Fatalf("remaining = %+v, want %+v", resolution.Remaining, wantRemaining)
	}
	wantKinds := map[PragmaticReferenceKind]int{
		AliasReference:           3,
		FleetShapeReference:      3,
		SiblingReadReference:     1,
		ZeroUseEndpointReference: 2,
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
