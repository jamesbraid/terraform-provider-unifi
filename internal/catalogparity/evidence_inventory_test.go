package catalogparity

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildEvidenceInventoryAccountsForEverySurfaceAndNamesGaps(t *testing.T) {
	baseline := releasedBaseline(t)
	contracts, err := BuildSurfaceContracts(
		baseline,
		releasedSchemaVersions(t, baseline),
		releasedWavePolicy(t),
	)
	if err != nil {
		t.Fatal(err)
	}

	inventory, err := BuildEvidenceInventory(EvidenceInventoryInput{
		Baseline:         baseline,
		Contracts:        contracts,
		ReleasedRoot:     filepath.Join("..", ".."),
		CandidateRoot:    filepath.Join("..", ".."),
		ReleasedProvider: ReleasedProvider{Version: "0.101.2", Commit: strings.Repeat("f", 40)},
		SDK: SDKComparison{
			ModulePath:             "github.com/jamesbraid/go-unifi",
			ReleasedVersion:        "v1.101.0",
			ReleasedArchiveSHA256:  strings.Repeat("a", 64),
			CandidateVersion:       "v1.102.0",
			CandidateArchiveSHA256: strings.Repeat("b", 64),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Surfaces) != 67 {
		t.Fatalf("surfaces = %d, want 67", len(inventory.Surfaces))
	}
	if got := inventory.CoverageCounts["scenario_owner"]; got != 67 {
		t.Fatalf("scenario owners = %d, want 67", got)
	}
	wantCoverage := map[string]int{
		"scenario_owner":    67,
		"constructor":       67,
		"acceptance":        33,
		"import":            27,
		"list_acceptance":   25,
		"action_acceptance": 0,
	}
	if !reflect.DeepEqual(inventory.CoverageCounts, wantCoverage) {
		t.Fatalf("coverage counts = %v, want %v", inventory.CoverageCounts, wantCoverage)
	}

	network := inventory.Surface(SurfaceKey{Kind: ManagedResource, Name: "unifi_network"})
	if network == nil {
		t.Fatal("managed unifi_network is missing")
	}
	if network.Runtime.Status != FileIdentical || network.Tests.Status != FileIdentical {
		t.Fatalf("network comparisons = runtime:%q tests:%q, want identical", network.Runtime.Status, network.Tests.Status)
	}
	if !network.Signals.Acceptance || !network.Signals.Import {
		t.Fatalf("network signals = %+v, want acceptance and import", network.Signals)
	}
	if network.Signals.ListAcceptance || network.Signals.ActionAcceptance {
		t.Fatalf("network signals = %+v, want non-applicable signals false", network.Signals)
	}
	if len(network.MissingSignals) != 0 {
		t.Fatalf("network missing signals = %v, want none", network.MissingSignals)
	}

	portAction := inventory.Surface(SurfaceKey{Kind: Action, Name: "unifi_port"})
	if portAction == nil {
		t.Fatal("unifi_port action is missing")
	}
	if !reflect.DeepEqual(portAction.MissingSignals, []string{"action_acceptance", "hardware_claim"}) {
		t.Fatalf("port action missing signals = %v", portAction.MissingSignals)
	}
}

func TestBuildEvidenceInventoryReportsChangedRuntimeAndRejectsMissingOwner(t *testing.T) {
	released := t.TempDir()
	candidate := t.TempDir()
	writeEvidenceFixture(t, released, "unifi/dns_record_resource.go", "package unifi\n")
	writeEvidenceFixture(t, candidate, "unifi/dns_record_resource.go", "package unifi\n\nvar changed = true\n")
	writeEvidenceFixture(t, released, "unifi/dns_record_resource_test.go", "package unifi\n\nfunc TestNewDNSRecordResource() {}\n")
	writeEvidenceFixture(t, candidate, "unifi/dns_record_resource_test.go", "package unifi\n\nfunc TestNewDNSRecordResource() {}\nfunc TestAccDNSRecord() {}\nfunc TestDNSRecordImportState() {}\n")

	key := SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"}
	baseline := Baseline{
		FormatVersion:         1,
		ProviderAddress:       CanonicalProviderAddress,
		SourceSHA256:          strings.Repeat("c", 64),
		CanonicalSchemaSHA256: strings.Repeat("d", 64),
		Surfaces: []Surface{{
			SurfaceKey:           key,
			BaselineSchemaSHA256: strings.Repeat("e", 64),
		}},
	}
	contracts := SurfaceContractCorpus{
		FormatVersion:   1,
		ProviderAddress: CanonicalProviderAddress,
		Contracts: []SurfaceContract{{
			SurfaceKey: key,
			Wave:       2,
		}},
	}
	input := EvidenceInventoryInput{
		Baseline:         baseline,
		Contracts:        contracts,
		ReleasedRoot:     released,
		CandidateRoot:    candidate,
		ReleasedProvider: ReleasedProvider{Version: "0.101.2", Commit: strings.Repeat("f", 40)},
		SDK: SDKComparison{
			ModulePath:             "github.com/jamesbraid/go-unifi",
			ReleasedVersion:        "v1.101.0",
			ReleasedArchiveSHA256:  strings.Repeat("a", 64),
			CandidateVersion:       "v1.102.0",
			CandidateArchiveSHA256: strings.Repeat("b", 64),
		},
	}
	inventory, err := BuildEvidenceInventory(input)
	if err != nil {
		t.Fatal(err)
	}
	got := inventory.Surface(key)
	if got == nil || got.Runtime.Status != FileChanged || !got.Signals.Acceptance || !got.Signals.Import {
		t.Fatalf("inventory surface = %+v", got)
	}

	if err := os.Remove(filepath.Join(candidate, "unifi", "dns_record_resource_test.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildEvidenceInventory(input); err == nil || !strings.Contains(err.Error(), "scenario owner") {
		t.Fatalf("BuildEvidenceInventory() error = %v, want missing scenario owner", err)
	}
}

func writeEvidenceFixture(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
