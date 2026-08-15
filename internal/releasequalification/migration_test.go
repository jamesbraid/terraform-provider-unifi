package releasequalification

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func TestBuildMigrationRecoveryReceipt(t *testing.T) {
	input := validMigrationInput(t)

	receipt, err := BuildMigrationRecoveryReceipt(input)
	if err != nil {
		t.Fatalf("BuildMigrationRecoveryReceipt() error = %v", err)
	}
	if receipt.Result != "pass" || receipt.SurfaceCount != 67 || receipt.RecoveryCount != 67 {
		t.Fatalf("receipt result/counts = %q/%d/%d", receipt.Result, receipt.SurfaceCount, receipt.RecoveryCount)
	}
	// The modes are now selected from the fixture's MEASURED state, not from
	// surface names: 63 surfaces whose runtime is identical, the two device
	// surfaces whose runtime changed but whose scenario did not, and the two
	// dns_record surfaces whose scenario changed as well.
	if receipt.EvidenceModes["source_identity"] != 63 ||
		receipt.EvidenceModes["differential_scenario"] != 2 ||
		receipt.EvidenceModes["dns_bidirectional_state"] != 1 ||
		receipt.EvidenceModes["dns_list_controller"] != 1 {
		t.Fatalf("evidence modes = %#v", receipt.EvidenceModes)
	}
	for _, surface := range receipt.Surfaces {
		if surface.State != "migration_recovery_pass" || len(surface.ReceiptSHA256) != 64 {
			t.Fatalf("surface = %#v", surface)
		}
	}
}

func TestBuildMigrationRecoveryReceiptAllowsReleasedLimitation(t *testing.T) {
	input := validMigrationInput(t)
	allowReleasedControllerLimitation(t, &input.Controller)

	receipt, err := BuildMigrationRecoveryReceipt(input)
	if err != nil {
		t.Fatalf("BuildMigrationRecoveryReceipt() error = %v", err)
	}
	if receipt.Result != "pass" || receipt.RecoveryCount != 67 {
		t.Fatalf("receipt result/count = %q/%d", receipt.Result, receipt.RecoveryCount)
	}
}

func TestBuildMigrationRecoveryReceiptAllowsReleasedMissingTest(t *testing.T) {
	input := validMigrationInput(t)
	allowReleasedControllerMissing(t, &input.Controller)

	receipt, err := BuildMigrationRecoveryReceipt(input)
	if err != nil {
		t.Fatalf("BuildMigrationRecoveryReceipt() error = %v", err)
	}
	if receipt.Result != "pass" || receipt.RecoveryCount != 67 {
		t.Fatalf("receipt result/count = %q/%d", receipt.Result, receipt.RecoveryCount)
	}
}

func TestBuildMigrationRecoveryReceiptAllowsReleasedFailureThatPasses(t *testing.T) {
	input := validMigrationInput(t)
	// Indices 2 and 3: both belong to surfaces whose runtime is identical, so
	// neither surface's recovery depends on the controller result being clean.
	allowedFailure := input.Controller.Plan.TestNames[2]
	missing := input.Controller.Plan.TestNames[3]
	input.Controller.Plan.ReleasedAllowedFailures = []string{allowedFailure}
	input.Controller.Plan.ReleasedAllowedMissing = []string{missing}
	input.Controller.Released.Result = "accepted_limitation"
	input.Controller.Released.Passed = removeControllerTest(input.Controller.Released.Passed, missing)
	input.Controller.Released.Missing = []string{missing}

	receipt, err := BuildMigrationRecoveryReceipt(input)
	if err != nil {
		t.Fatalf("BuildMigrationRecoveryReceipt() error = %v", err)
	}
	if receipt.Result != "pass" || receipt.RecoveryCount != 67 {
		t.Fatalf("receipt result/count = %q/%d", receipt.Result, receipt.RecoveryCount)
	}
}

func TestBuildMigrationRecoveryReceiptFailsClosed(t *testing.T) {
	tests := map[string]struct {
		mutate func(*MigrationRecoveryInput)
		want   string
	}{
		"admission blocker": {
			mutate: func(input *MigrationRecoveryInput) {
				input.Admission.ReleaseBlockers[0].Signal = "unknown"
			},
			want: "release blocker",
		},
		"extra runtime change": {
			mutate: func(input *MigrationRecoveryInput) {
				input.Inventory.Surfaces[2].Runtime.Status = catalogparity.FileChanged
			},
			want: "runtime change set",
		},
		"non identity migration": {
			mutate: func(input *MigrationRecoveryInput) {
				input.Manifest.Entries[0].Strategy = catalogparity.ImportBridge
			},
			want: "identity migration",
		},
		"dns round trip": {
			mutate: func(input *MigrationRecoveryInput) {
				input.DNSLifecycle.Lifecycle.BidirectionalAdapterStateRoundTrip = false
			},
			want: "DNS lifecycle",
		},
		"candidate binary": {
			mutate: func(input *MigrationRecoveryInput) {
				input.DNSLifecycle.ProviderBinarySHA256 = strings.Repeat("f", 64)
			},
			want: "candidate binary",
		},
		"state upgrade authority": {
			mutate: func(input *MigrationRecoveryInput) {
				input.DNSLifecycle.StateUpgradeSource.BinarySHA256 = ""
			},
			want: "state upgrade authority",
		},
		"controller incomplete": {
			mutate: func(input *MigrationRecoveryInput) {
				input.Controller.Candidate.Missing = []string{"TestAccMissing"}
			},
			want: "controller differential",
		},
		"controller surface substitution": {
			mutate: func(input *MigrationRecoveryInput) {
				input.Controller.Plan.Surfaces[2].Name = "unifi_unknown"
			},
			want: "controller surface set",
		},
		"inventory surface substitution": {
			mutate: func(input *MigrationRecoveryInput) {
				input.Inventory.Surfaces[2].Name = "unifi_unknown"
			},
			want: "inventory surface set",
		},
		// The mutations below are the point of the evidence modes. Each takes
		// away one piece of a CONVERTED surface's justification and nothing
		// else. Before this, every one of them left the surface stamped
		// source_identity and the receipt passed.
		//
		// unifi_device (managed) is the fixture's differential_scenario
		// surface: runtime changed, scenario unchanged, one acceptance test
		// that passed on both providers.
		"converted surface lost its released side": {
			mutate: func(input *MigrationRecoveryInput) {
				input.Controller.Released.Passed = removeControllerTest(
					input.Controller.Released.Passed, deviceScenario(input))
				input.Controller.Released.Result = "accepted_limitation"
				input.Controller.Released.Missing = []string{deviceScenario(input)}
				input.Controller.Plan.ReleasedAllowedMissing = []string{deviceScenario(input)}
			},
			want: "did not pass on the released provider",
		},
		// A skip is the one way a scenario can legitimately go unrun on the
		// CANDIDATE side -- the controller gate requires the candidate suite to
		// pass everything it is not allowed to skip, so nothing else gets past
		// it. Skipping a converted surface's only scenario leaves the whole
		// controller receipt valid and the surface with nothing behind it,
		// which is precisely the hole this mode exists to close.
		"converted surface scenario skipped on both sides": {
			mutate: func(input *MigrationRecoveryInput) {
				scenario := deviceScenario(input)
				input.Controller.Plan.AllowedSkips = append(input.Controller.Plan.AllowedSkips, scenario)
				for _, suite := range []*catalogparity.ControllerSuiteReceipt{
					&input.Controller.Released, &input.Controller.Candidate,
				} {
					suite.Passed = removeControllerTest(suite.Passed, scenario)
					suite.Skipped = append(append([]string{}, suite.Skipped...), scenario)
				}
			},
			want: "unifi_device has no evidence mode",
		},
		"converted surface scenario also changed": {
			mutate: func(input *MigrationRecoveryInput) {
				surface := inventorySurface(input, catalogparity.SurfaceKey{
					Kind: catalogparity.ManagedResource, Name: "unifi_device"})
				surface.Tests.Status = catalogparity.FileChanged
			},
			want: "has no evidence mode",
		},
		"converted surface has no planned scenario": {
			mutate: func(input *MigrationRecoveryInput) {
				for index := range input.Controller.Plan.Surfaces {
					if input.Controller.Plan.Surfaces[index].Kind == catalogparity.ManagedResource &&
						input.Controller.Plan.Surfaces[index].Name == "unifi_device" {
						input.Controller.Plan.Surfaces[index].TestNames = nil
						input.Controller.Plan.Surfaces[index].MissingSignals = []string{"acceptance"}
					}
				}
			},
			want: "plans no acceptance test",
		},
		// The M3 receipt is what distinguishes the dns modes from a name. Take
		// its round trip away and dns_record must not simply fall back.
		"dns record loses its lifecycle evidence": {
			mutate: func(input *MigrationRecoveryInput) {
				input.DNSLifecycle.Lifecycle.BidirectionalAdapterStateRoundTrip = false
				input.DNSLifecycle.Lifecycle.Cleanup = false
			},
			want: "DNS lifecycle",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := validMigrationInput(t)
			test.mutate(&input)
			_, err := BuildMigrationRecoveryReceipt(input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

// deviceScenario is the acceptance test the fixture's converted surface,
// managed unifi_device, depends on. Looked up rather than hardcoded as an
// index, because an index into the plan is exactly the kind of quiet coupling
// that made allowReleasedControllerLimitation excuse the wrong test.
func deviceScenario(input *MigrationRecoveryInput) string {
	for _, surface := range input.Controller.Plan.Surfaces {
		if surface.Kind == catalogparity.ManagedResource && surface.Name == "unifi_device" {
			return surface.TestNames[0]
		}
	}
	return ""
}

func inventorySurface(
	input *MigrationRecoveryInput,
	key catalogparity.SurfaceKey,
) *catalogparity.SurfaceEvidenceInventory {
	for index := range input.Inventory.Surfaces {
		if input.Inventory.Surfaces[index].SurfaceKey == key {
			return &input.Inventory.Surfaces[index]
		}
	}
	return nil
}

func validMigrationInput(t *testing.T) MigrationRecoveryInput {
	t.Helper()
	digest := strings.Repeat("a", 64)
	commit := strings.Repeat("b", 40)
	releasedCommit := strings.Repeat("c", 40)
	keys := migrationSurfaceKeys()

	admissionSurfaces := make([]catalogparity.SurfaceAdmission, 0, len(keys))
	inventorySurfaces := make([]catalogparity.SurfaceEvidenceInventory, 0, len(keys))
	planSurfaces := make([]catalogparity.ControllerPlanSurface, 0, len(keys))
	migrationEntries := make([]catalogparity.MigrationEntry, 0, len(keys))
	testNames := make([]string, 0, len(keys))
	for index, key := range keys {
		admissionSurfaces = append(admissionSurfaces, catalogparity.SurfaceAdmission{
			SurfaceKey: key, Wave: 1, AdapterState: catalogparity.AdapterParity,
			AdapterParitySHA256: digest, State: catalogparity.Admitted, ReceiptSHA256: digest,
		})
		status := catalogparity.FileIdentical
		if (key.Name == "unifi_dns_record" || key.Name == "unifi_device") &&
			(key.Kind == catalogparity.ManagedResource || key.Kind == catalogparity.ListResource) {
			status = catalogparity.FileChanged
		}
		// dns_record's scenario changed in the real tree as well as its runtime,
		// which is what puts it on the M3 lifecycle receipt instead of the
		// unchanged-scenario differential. device's scenario is unchanged, so it
		// is the fixture's differential_scenario surface. Getting this wrong
		// hides a mode overlap: with every scenario identical, list dns_record
		// satisfied both differential_scenario and dns_list_controller.
		testStatus := catalogparity.FileIdentical
		if key.Name == "unifi_dns_record" &&
			(key.Kind == catalogparity.ManagedResource || key.Kind == catalogparity.ListResource) {
			testStatus = catalogparity.FileChanged
		}
		inventorySurfaces = append(inventorySurfaces, catalogparity.SurfaceEvidenceInventory{
			SurfaceKey: key, Wave: 1,
			Runtime:       catalogparity.FileComparison{Path: "unifi/runtime.go", Status: status, ReleasedSHA256: digest, CandidateSHA256: digest},
			Tests:         catalogparity.FileComparison{Path: "unifi/runtime_test.go", Status: testStatus, ReleasedSHA256: digest, CandidateSHA256: digest},
			ScenarioOwner: "unifi/runtime_test.go", TestFunctions: []string{"TestAccSurface"},
		})
		testName := fmt.Sprintf("TestAccSurface%02d", index)
		testNames = append(testNames, testName)
		planSurfaces = append(planSurfaces, catalogparity.ControllerPlanSurface{
			SurfaceKey: key, Wave: 1, TestNames: []string{testName}, MissingSignals: []string{},
		})
		zero := int64(0)
		migrationEntries = append(migrationEntries, catalogparity.MigrationEntry{
			SurfaceKey: key, OldName: key.Name, NewName: key.Name,
			Strategy: catalogparity.IdentityTransform, OldSchemaVersion: &zero, NewSchemaVersion: &zero,
			AttributeMapping: map[string]string{}, StateMoves: []catalogparity.StateMove{},
			ConfigurationEdit: "none", DestructiveRisk: catalogparity.RiskNone,
			ForwardAssertions: []string{"identity_preserved", "first_plan_empty"},
			Recovery:          catalogparity.Recovery{Mode: "snapshot_restore", Assertions: []string{"state_snapshot_present"}},
		})
	}
	controllerSuite := catalogparity.ControllerSuiteReceipt{
		Result: "pass", Passed: append([]string(nil), testNames[1:]...), Skipped: []string{testNames[0]},
		Failed: []string{}, Missing: []string{}, PreTestDiagnostics: []string{},
	}
	return MigrationRecoveryInput{
		// Declares exactly the surfaces this fixture marks FileChanged above.
		// The policy document shipped under provider-codegen/policy is bound to
		// the real inventory by catalogparity's own test; what matters here is
		// the gate logic, so this fixture stays self-contained.
		Policy: catalogparity.CampaignPolicy{
			FormatVersion: 1, Gate: "catalog controller differential",
			SurfaceCount: 67, EvidenceGapCount: 8,
			TestNameCount: 152,
			RuntimeChangeSet: []catalogparity.RuntimeChange{
				{
					SurfaceKey: catalogparity.SurfaceKey{Kind: catalogparity.ListResource, Name: "unifi_device"},
					Reason:     catalogparity.ReasonConverted,
				},
				{
					SurfaceKey: catalogparity.SurfaceKey{Kind: catalogparity.ListResource, Name: "unifi_dns_record"},
					Reason:     catalogparity.ReasonConverted,
				},
				{
					SurfaceKey: catalogparity.SurfaceKey{Kind: catalogparity.ManagedResource, Name: "unifi_device"},
					Reason:     catalogparity.ReasonHandEdit,
				},
				{
					SurfaceKey: catalogparity.SurfaceKey{Kind: catalogparity.ManagedResource, Name: "unifi_dns_record"},
					Reason:     catalogparity.ReasonConverted,
				},
			},
		},
		Admission: catalogparity.AdmissionReceipt{
			FormatVersion: 1, Gate: "catalog-admission", Result: "pass",
			ProviderAddress: catalogparity.CanonicalProviderAddress, SourceCommit: commit,
			ReleasedCommit: releasedCommit, CandidateBinarySHA256: digest,
			Evidence: catalogparity.AdmissionEvidenceDigests{
				InventorySHA256: digest, BuildSchemaSHA256: digest, ControllerSHA256: digest,
			},
			AdmittedSurfaceCount: 67, ReleaseBlockerCount: 1,
			ReleaseBlockers: []catalogparity.EvidenceGap{{
				SurfaceKey: catalogparity.SurfaceKey{Kind: catalogparity.Action, Name: "unifi_port"},
				Signal:     "hardware_claim",
			}},
			Surfaces: admissionSurfaces,
		},
		AdmissionSHA256: digest,
		BuildSchema: catalogparity.BuildSchemaReceipt{
			FormatVersion: 1, Gate: "catalog-build-schema", Result: "pass",
			PromotionBlockers: []string{}, SourceCommit: commit, ReleasedCommit: releasedCommit,
			Platform: "linux/amd64", GoVersion: "go1.25.8", BuildNetwork: "none",
			CleanBuilds:      catalogparity.BuildCounts{Released: 2, Candidate: 2},
			ProviderBinaries: catalogparity.ProviderBinaryEvidence{CandidateSHA256: digest},
			InventorySHA256:  digest,
			SchemaEvidence: catalogparity.SchemaDifferentialEvidence{
				Terraform:                   catalogparity.SchemaCLIReceipt{Version: "1.15.8", BinarySHA256: digest, CanonicalSHA256: digest},
				Tofu:                        catalogparity.SchemaCLIReceipt{Version: "1.12.1", BinarySHA256: digest, CanonicalSHA256: digest},
				ReleaseToCandidateWithinCLI: true, SharedCLIProjectionEqual: true, FullCLIProjectionEqual: false,
				TerraformOnlyCategories: []string{"action_schemas", "list_resource_schemas"},
			},
		},
		BuildSchemaSHA256: digest,
		Controller: catalogparity.ControllerDifferentialReceipt{
			FormatVersion: 1, Gate: "catalog controller differential", Result: "blocked_evidence",
			PlanSHA256: digest, ReleasedCommit: releasedCommit, CandidateCommit: commit,
			Target:   catalogparity.ControllerImageReceipt{Image: "controller@sha256:" + digest, ImageID: "sha256:" + digest, PullPolicy: "never"},
			Plan:     catalogparity.ControllerPlanReceipt{FormatVersion: 1, Gate: "catalog controller differential", Waves: []int{1, 2, 3, 4, 5}, Surfaces: planSurfaces, SurfaceCount: 67, EvidenceGapCount: 1, TestNames: testNames, AllowedSkips: []string{testNames[0]}},
			Released: controllerSuite, Candidate: controllerSuite,
		},
		ControllerSHA256: digest,
		Inventory: catalogparity.EvidenceInventory{
			FormatVersion: 1, ProviderAddress: catalogparity.CanonicalProviderAddress,
			ReleasedProvider: catalogparity.ReleasedProvider{Version: "0.101.2", Commit: releasedCommit},
			Surfaces:         inventorySurfaces,
		},
		InventorySHA256: digest,
		Manifest: catalogparity.MigrationManifest{
			FormatVersion: 1, FromVersion: "0.101.2", ToVersion: "next",
			ProviderAddress: catalogparity.CanonicalProviderAddress, BaselineSHA256: digest,
			SchemaSHA256: digest, Entries: migrationEntries,
		},
		ManifestSHA256: digest,
		DNSLifecycle: DNSLifecycleReceipt{
			FormatVersion: 1, Gate: "M3 DNS managed-operation qualification", Result: "pass",
			SourceCommit: commit, Platform: "linux/amd64", ProviderVersion: "0.101.2",
			ProviderBinarySHA256: digest,
			LegacyProvider:       ToolArtifact{Version: "0.101.2", BinarySHA256: digest},
			StateUpgradeSource: StateUpgradeArtifact{
				Version: "0.41.11", Commit: strings.Repeat("d", 40), BinarySHA256: digest,
			},
			Terraform: ToolArtifact{Version: "1.15.8", BinarySHA256: digest},
			Tofu:      ToolArtifact{Version: "1.12.1", BinarySHA256: digest},
			Target:    DNSTargetReceipt{Product: "UniFi Network", Version: "10.4.57", PlatformManifestSHA256: "sha256:" + digest},
			Lifecycle: DNSLifecycleChecks{
				FreshTargetPerCLIAndAdapter: true, Create: true, Update: true,
				OmittedOptionalFields: true, ConfiguredOptionalFields: true,
				ReplacementPlan: true, RestartRefresh: true, Import: true,
				V0IntegerTTLStateUpgrade: true, NoOpPlan: true, Delete: true,
				Cleanup: true, BidirectionalAdapterStateRoundTrip: true,
			},
			NormalizedStateSHA256: digest,
			CLIOutcomesEquivalent: true, AdapterOutcomesEquivalent: true,
		},
		DNSLifecycleSHA256: digest,
	}
}

func allowReleasedControllerLimitation(
	t *testing.T,
	controller *catalogparity.ControllerDifferentialReceipt,
) {
	t.Helper()
	// Index 2, not 1. Index 1 belongs to unifi_device, whose runtime changed
	// and whose recovery therefore RESTS on that test passing on both sides --
	// excusing it on the released side withdraws the surface's only evidence,
	// and the gate correctly refuses it. This test is about tolerating a
	// released limitation in general, so it picks a surface whose runtime is
	// identical and whose recovery does not depend on the controller at all.
	// TestBuildMigrationRecoveryReceiptFailsClosed covers the converted case.
	failure := controller.Plan.TestNames[2]
	if failure == portPersistenceScenario {
		t.Fatalf("released limitation unexpectedly selected port persistence scenario")
	}
	controller.Plan.ReleasedAllowedFailures = []string{failure}
	controller.Released.Result = "accepted_limitation"
	controller.Released.ExitCode = 1
	controller.Released.Failed = []string{failure}
	controller.Released.AcceptedFailures = []string{failure}
	passed := make([]string, 0, len(controller.Released.Passed)-1)
	for _, testName := range controller.Released.Passed {
		if testName != failure {
			passed = append(passed, testName)
		}
	}
	controller.Released.Passed = passed
}

func allowReleasedControllerMissing(
	t *testing.T,
	controller *catalogparity.ControllerDifferentialReceipt,
) {
	t.Helper()
	// Index 2 for the same reason as allowReleasedControllerLimitation: index 1
	// is the converted surface whose evidence is that very test.
	missing := controller.Plan.TestNames[2]
	controller.Plan.ReleasedAllowedMissing = []string{missing}
	controller.Released.Result = "accepted_limitation"
	controller.Released.Missing = []string{missing}
	passed := make([]string, 0, len(controller.Released.Passed)-1)
	for _, testName := range controller.Released.Passed {
		if testName != missing {
			passed = append(passed, testName)
		}
	}
	controller.Released.Passed = passed
}

func migrationSurfaceKeys() []catalogparity.SurfaceKey {
	keys := make([]catalogparity.SurfaceKey, 0, 67)
	for index := range 28 {
		name := fmt.Sprintf("unifi_resource_%02d", index)
		if index == 0 {
			name = "unifi_dns_record"
		} else if index == 1 {
			name = "unifi_device"
		}
		keys = append(keys, catalogparity.SurfaceKey{Kind: catalogparity.ManagedResource, Name: name})
	}
	for index := range 13 {
		keys = append(keys, catalogparity.SurfaceKey{Kind: catalogparity.DataSource, Name: fmt.Sprintf("unifi_data_%02d", index)})
	}
	for index := range 25 {
		name := fmt.Sprintf("unifi_list_%02d", index)
		if index == 0 {
			name = "unifi_dns_record"
		} else if index == 1 {
			name = "unifi_device"
		}
		keys = append(keys, catalogparity.SurfaceKey{Kind: catalogparity.ListResource, Name: name})
	}
	keys = append(keys, catalogparity.SurfaceKey{Kind: catalogparity.Action, Name: "unifi_port"})
	return keys
}
