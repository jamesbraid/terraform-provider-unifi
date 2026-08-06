package catalogparity

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestBuildAdmissionAdmitsCatalogAndRetainsHardwareReleaseBlocker(t *testing.T) {
	input := validAdmissionInput(t)

	receipt, err := BuildAdmission(input)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Result != "pass" || receipt.AdmittedSurfaceCount != 67 {
		t.Fatalf("admission result = %q with %d surfaces", receipt.Result, receipt.AdmittedSurfaceCount)
	}
	if receipt.ReleaseBlockerCount != 1 {
		t.Fatalf("release blockers = %d, want 1", receipt.ReleaseBlockerCount)
	}
	wantBlocker := EvidenceGap{
		SurfaceKey: SurfaceKey{Kind: Action, Name: "unifi_port"},
		Signal:     "hardware_claim",
	}
	if !reflect.DeepEqual(receipt.ReleaseBlockers, []EvidenceGap{wantBlocker}) {
		t.Fatalf("release blockers = %+v", receipt.ReleaseBlockers)
	}
	for _, surface := range receipt.Surfaces {
		if surface.State != Admitted {
			t.Fatalf("surface %s/%s state = %q", surface.Kind, surface.Name, surface.State)
		}
		if !validSHA256(surface.AdapterParitySHA256) || !validSHA256(surface.ReceiptSHA256) {
			t.Fatalf("surface %s/%s has invalid evidence digests", surface.Kind, surface.Name)
		}
		want := []string(nil)
		if surface.SurfaceKey == wantBlocker.SurfaceKey {
			want = []string{"hardware_claim"}
		}
		if !reflect.DeepEqual(surface.ReleaseBlockers, want) {
			t.Fatalf("surface %s/%s release blockers = %v, want %v", surface.Kind, surface.Name, surface.ReleaseBlockers, want)
		}
	}
}

func TestBuildAdmissionIsDeterministic(t *testing.T) {
	input := validAdmissionInput(t)
	first, err := BuildAdmission(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildAdmission(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("BuildAdmission() is nondeterministic")
	}
}

func TestBuildAdmissionAcceptsDeclaredControllerTargetSkips(t *testing.T) {
	input := validAdmissionInput(t)
	firstAllowed := len(input.Controller.Plan.TestNames) - 3
	allowed := append([]string(nil), input.Controller.Plan.TestNames[firstAllowed:]...)
	input.Controller.Plan.AllowedSkips = allowed
	for _, suite := range []*ControllerSuiteReceipt{
		&input.Controller.Released,
		&input.Controller.Candidate,
	} {
		suite.Passed = append([]string(nil), input.Controller.Plan.TestNames[:firstAllowed]...)
		suite.Skipped = append([]string(nil), allowed...)
	}

	if _, err := BuildAdmission(input); err != nil {
		t.Fatalf("BuildAdmission() rejected declared target skips: %v", err)
	}
}

func TestBuildAdmissionRejectsUnboundOrIncompleteEvidence(t *testing.T) {
	tests := map[string]struct {
		mutate func(*AdmissionInput)
		want   string
	}{
		"build result": {
			mutate: func(input *AdmissionInput) { input.BuildSchema.Result = "diagnostic_pass" },
			want:   "build/schema result",
		},
		"source commit": {
			mutate: func(input *AdmissionInput) { input.Unit.SourceCommit = strings.Repeat("d", 40) },
			want:   "source commit",
		},
		"inventory digest": {
			mutate: func(input *AdmissionInput) { input.Unit.InventorySHA256 = strings.Repeat("d", 64) },
			want:   "inventory SHA-256",
		},
		"unit failure": {
			mutate: func(input *AdmissionInput) { input.Unit.Candidate.FailedTestCount = 1 },
			want:   "candidate unit suite",
		},
		"controller missing test": {
			mutate: func(input *AdmissionInput) {
				input.Controller.Candidate.Missing = append(input.Controller.Candidate.Missing, "TestAccMissing")
			},
			want: "candidate controller suite",
		},
		"undeclared controller skip": {
			mutate: func(input *AdmissionInput) {
				name := input.Controller.Candidate.Passed[0]
				input.Controller.Candidate.Passed = input.Controller.Candidate.Passed[1:]
				input.Controller.Candidate.Skipped = []string{name}
			},
			want: "candidate controller suite",
		},
		"declared controller skip was not skipped": {
			mutate: func(input *AdmissionInput) {
				input.Controller.Plan.AllowedSkips = []string{input.Controller.Plan.TestNames[0]}
			},
			want: "released controller suite",
		},
		"allowed skip outside plan": {
			mutate: func(input *AdmissionInput) {
				input.Controller.Plan.AllowedSkips = []string{"TestAccNotInPlan"}
			},
			want: "released controller suite",
		},
		"controller receipt digest": {
			mutate: func(input *AdmissionInput) {
				input.Pragmatic.ControllerReceiptSHA256 = strings.Repeat("d", 64)
			},
			want: "controller receipt SHA-256",
		},
		"unresolved lifecycle gap": {
			mutate: func(input *AdmissionInput) {
				input.Pragmatic.Remaining = append(input.Pragmatic.Remaining, EvidenceGap{
					SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_network"},
					Signal:     "import",
				})
				input.Pragmatic.RemainingSignalCount++
			},
			want: "release-only blocker",
		},
		"surface set": {
			mutate: func(input *AdmissionInput) {
				input.Controller.Plan.Surfaces = input.Controller.Plan.Surfaces[:66]
			},
			want: "controller plan surfaces",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := validAdmissionInput(t)
			test.mutate(&input)
			_, err := BuildAdmission(input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildAdmission() error = %v, want %q", err, test.want)
			}
		})
	}
}

func validAdmissionInput(t *testing.T) AdmissionInput {
	t.Helper()
	data, err := os.ReadFile("../../build/release-ready/catalog-evidence-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory EvidenceInventory
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	inventorySHA256 := byteSHA256(data)
	sourceCommit := strings.Repeat("a", 40)
	releasedCommit := inventory.ReleasedProvider.Commit
	digest := strings.Repeat("b", 64)

	build := BuildSchemaReceipt{
		FormatVersion:  1,
		Gate:           "catalog-build-schema",
		Result:         "pass",
		SourceCommit:   sourceCommit,
		ReleasedCommit: releasedCommit,
		Platform:       "linux/amd64",
		GoVersion:      "go1.25.8",
		BuildNetwork:   "none",
		CleanBuilds:    BuildCounts{Released: 2, Candidate: 2},
		ProviderBinaries: ProviderBinaryEvidence{
			ReleasedSourceRebuildSHA256: digest,
			ReleasedAuthority:           "published_archive",
			ReleasedAuthoritySHA256:     digest,
			CandidateSHA256:             digest,
		},
		InventorySHA256: inventorySHA256,
		SchemaEvidence: SchemaDifferentialEvidence{
			Terraform:                   SchemaCLIReceipt{Version: "1.15.8", BinarySHA256: digest, ReleasedRawSHA256: digest, CandidateRawSHA256: digest, CanonicalSHA256: digest},
			Tofu:                        SchemaCLIReceipt{Version: "1.12.1", BinarySHA256: digest, ReleasedRawSHA256: digest, CandidateRawSHA256: digest, CanonicalSHA256: digest},
			ReleaseToCandidateWithinCLI: true,
			SharedCLIProjectionEqual:    true,
			FullCLIProjectionEqual:      false,
			SharedSchemaSHA256:          digest,
			TerraformOnlyCategories:     []string{"action_schemas", "list_resource_schemas"},
		},
	}
	suite := UnitSuiteReceipt{
		ExitCode:                0,
		Result:                  "pass",
		PackagePassCount:        1,
		RawLogSHA256:            digest,
		NormalizedSummarySHA256: digest,
	}
	unit := UnitDifferentialReceipt{
		FormatVersion:   1,
		Gate:            "catalog-unit-http-differential",
		Result:          "pass",
		Network:         "none",
		SourceCommit:    sourceCommit,
		ReleasedCommit:  releasedCommit,
		Platform:        "linux/amd64",
		GoVersion:       "go1.25.8",
		InventorySHA256: inventorySHA256,
		Released:        suite,
		Candidate:       suite,
	}

	planSurfaces := make([]ControllerPlanSurface, 0, len(inventory.Surfaces))
	allTests := make([]string, 0)
	for _, surface := range inventory.Surfaces {
		tests := []string{"TestAcc" + strings.TrimPrefix(surface.Name, "unifi_")}
		planSurfaces = append(planSurfaces, ControllerPlanSurface{
			SurfaceKey:     surface.SurfaceKey,
			Wave:           surface.Wave,
			MissingSignals: append([]string{}, surface.MissingSignals...),
			TestNames:      tests,
		})
		allTests = append(allTests, tests...)
	}
	for len(allTests) < 150 {
		name := fmt.Sprintf("TestAccSynthetic%03d", len(allTests))
		planSurfaces[0].TestNames = append(planSurfaces[0].TestNames, name)
		allTests = append(allTests, name)
	}
	sharedScenarioOwners := make([]string, 40)
	for index := range sharedScenarioOwners {
		sharedScenarioOwners[index] = fmt.Sprintf("unifi/scenario_%02d_test.go", index)
	}
	controllerSuite := ControllerSuiteReceipt{ExitCode: 0, Result: "pass", Passed: allTests}
	controller := ControllerDifferentialReceipt{
		FormatVersion:         1,
		Gate:                  "catalog controller differential",
		Result:                "blocked_evidence",
		PlanSHA256:            digest,
		ReleasedCommit:        releasedCommit,
		CandidateCommit:       sourceCommit,
		Target:                ControllerImageReceipt{Image: "controller@sha256:" + digest, ImageID: "sha256:" + digest, PullPolicy: "never"},
		Fleet:                 ControllerFleetReceipt{Image: "fleet:" + digest, ImageID: "sha256:" + digest, HerderSHA256: digest},
		Testcontainers:        ControllerRyukReceipt{RyukImage: "ryuk@sha256:" + digest, RyukImageID: "sha256:" + digest},
		TerraformBinarySHA256: digest,
		Plan: ControllerPlanReceipt{
			FormatVersion:        1,
			Gate:                 "catalog controller differential",
			Waves:                []int{1, 2, 3, 4, 5},
			Surfaces:             planSurfaces,
			SurfaceCount:         67,
			EvidenceGapCount:     10,
			SharedScenarioOwners: sharedScenarioOwners,
			TestNames:            allTests,
		},
		Released:  controllerSuite,
		Candidate: controllerSuite,
	}
	controllerSHA256 := strings.Repeat("c", 64)
	pragmaticData, err := os.ReadFile("../../build/restricted/catalog-pragmatic-resolution.json")
	if err != nil {
		t.Fatal(err)
	}
	var pragmatic PragmaticResolution
	if err := json.Unmarshal(pragmaticData, &pragmatic); err != nil {
		t.Fatal(err)
	}
	pragmatic.ControllerReceiptSHA256 = controllerSHA256

	return AdmissionInput{
		Inventory:         inventory,
		InventorySHA256:   inventorySHA256,
		BuildSchema:       build,
		BuildSchemaSHA256: digest,
		Unit:              unit,
		UnitSHA256:        digest,
		Controller:        controller,
		ControllerSHA256:  controllerSHA256,
		Pragmatic:         pragmatic,
		PragmaticSHA256:   digest,
	}
}
