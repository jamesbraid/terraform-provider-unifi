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
		"acceptance":        35,
		"import":            27,
		"list_acceptance":   25,
		"action_acceptance": 1,
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
	if !reflect.DeepEqual(portAction.MissingSignals, []string{"hardware_claim"}) {
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

// TestScenarioOwnersFindsACompanionFileAndTagsItsScenarios covers the whole
// point of the plural change, and it exists because the manual probe that first
// demonstrated it was a throwaway.
//
// A surface whose acceptance tests do not exist on the released side has to put
// them somewhere. Putting them in the conventional test file makes that file
// differ, which disqualified every scenario in it at once; putting them in a
// companion file was not possible while a surface could own only one.
//
// The companion is found BY THE BASE, which includes the kind suffix. That is
// load-bearing rather than incidental: the stem alone would be ambiguous,
// because "client" is a prefix of the real surfaces client_info, client_list
// and client_qos_rate, and a stem rule would need a longest-match tiebreak to
// tell them apart. The kind suffix terminates the stem, so firewall_policy_resource
// can never prefix another surface's base.
func TestScenarioOwnersFindsACompanionFileAndTagsItsScenarios(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "unifi"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "unifi", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("firewall_policy_resource_test.go", "package unifi\n\nfunc TestAccExisting(t *testing.T) {}\n")
	write("firewall_policy_resource_acc_test.go", "package unifi\n\nfunc TestAccCompanion(t *testing.T) {}\n")
	// A file belonging to a DIFFERENT surface, to prove the match is not a
	// loose prefix over the whole directory.
	write("firewall_zone_resource_test.go", "package unifi\n\nfunc TestAccOther(t *testing.T) {}\n")
	// A companion that matches the name and carries NO acceptance test. It must
	// NOT become an owner. Not hypothetical: unifi/port_action_merge_test.go is
	// exactly this shape, and action/unifi_port carries the one declared
	// shared-scenario exception, so enrolling it would have made the graft copy a
	// unit-test file bound to a candidate-side function into the released tree --
	// no scenario gained, a compile failure risked, on the one piece of grafting
	// machinery with evidence behind it.
	write("firewall_policy_resource_merge_test.go", "package unifi\n\nfunc Test_unitOnly(t *testing.T) {}\n")

	key := SurfaceKey{Kind: ManagedResource, Name: "unifi_firewall_policy"}
	owners, err := scenarioOwners(root, key)
	if err != nil {
		t.Fatalf("scenarioOwners() error = %v", err)
	}
	want := []string{
		"unifi/firewall_policy_resource_acc_test.go",
		"unifi/firewall_policy_resource_test.go",
	}
	if !reflect.DeepEqual(owners, want) {
		t.Fatalf("owners = %v, want %v", owners, want)
	}

	// The released tree has neither file, so both scenarios are added, and each
	// must name the file it came from -- a reader holding only the function
	// name cannot say which file to graft.
	scenarios, err := compareScenarios(t.TempDir(), root, owners)
	if err != nil {
		t.Fatalf("compareScenarios() error = %v", err)
	}
	got := map[string]string{}
	for _, scenario := range scenarios {
		if scenario.Status != ScenarioAdded {
			t.Errorf("%s status = %q, want added; the released tree has no such file", scenario.Name, scenario.Status)
		}
		got[scenario.Name] = scenario.File
	}
	wantFiles := map[string]string{
		"TestAccExisting":  "unifi/firewall_policy_resource_test.go",
		"TestAccCompanion": "unifi/firewall_policy_resource_acc_test.go",
	}
	if !reflect.DeepEqual(got, wantFiles) {
		t.Errorf("scenario files = %v, want %v", got, wantFiles)
	}
}

// TestUnclaimedScenarioFilesNamesAnAcceptanceFileNoSurfaceOwns is the freshness
// check on deriving owners by name. Deriving is only safe while every
// acceptance file matches the convention, and nothing enforces the convention.
//
// It deliberately does not report a file claimed by SEVERAL surfaces: a managed
// resource and its list companion share a base by design and legitimately name
// the same file. A symmetric guard would be red on the existing tree.
func TestUnclaimedScenarioFilesNamesAnAcceptanceFileNoSurfaceOwns(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "unifi"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "unifi", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("firewall_policy_resource_test.go", "package unifi\n\nfunc TestAccOwned(t *testing.T) {}\n")
	write("firewall_policy_extra_test.go", "package unifi\n\nfunc TestAccStranded(t *testing.T) {}\n")
	write("helpers_test.go", "package unifi\n\nfunc TestUnitOnly(t *testing.T) {}\n")

	keys := []SurfaceKey{
		{Kind: ManagedResource, Name: "unifi_firewall_policy"},
		{Kind: ListResource, Name: "unifi_firewall_policy"},
	}
	unclaimed, err := UnclaimedScenarioFiles(root, keys)
	if err != nil {
		t.Fatalf("UnclaimedScenarioFiles() error = %v", err)
	}
	// helpers_test.go declares no TestAcc, so it is not evidence and not a
	// finding. firewall_policy_extra_test.go does, and no surface claims it.
	if !reflect.DeepEqual(unclaimed, []string{"unifi/firewall_policy_extra_test.go"}) {
		t.Fatalf("unclaimed = %v, want only the stranded acceptance file", unclaimed)
	}
}
