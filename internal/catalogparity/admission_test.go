package catalogparity

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
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
	// Two blockers. It was four until firewall_policy and site_to_site_vpn
	// gained acceptance files of their own, which is what the multi-file
	// scenario owner change made visible: a surface with an acceptance test in
	// a companion file used to report no acceptance evidence with the test
	// sitting next to it.
	//
	// Written out rather than derived from the policy on purpose. Admission
	// already checks the blockers against accepted_evidence_gaps in both
	// directions, so deriving this would restate that check instead of being an
	// independent statement of what we expect the receipt to say.
	wantBlockers := []EvidenceGap{
		{SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_power_supervisor"}, Signal: "acceptance"},
		{SurfaceKey: SurfaceKey{Kind: Action, Name: "unifi_port"}, Signal: "hardware_claim"},
	}
	if receipt.ReleaseBlockerCount != len(wantBlockers) {
		t.Fatalf("release blockers = %d, want %d", receipt.ReleaseBlockerCount, len(wantBlockers))
	}
	if !reflect.DeepEqual(receipt.ReleaseBlockers, wantBlockers) {
		t.Fatalf("release blockers = %+v, want %+v", receipt.ReleaseBlockers, wantBlockers)
	}
	blocked := map[SurfaceKey][]string{}
	for _, gap := range wantBlockers {
		blocked[gap.SurfaceKey] = append(blocked[gap.SurfaceKey], gap.Signal)
	}
	for _, surface := range receipt.Surfaces {
		if surface.State != Admitted {
			t.Fatalf("surface %s/%s state = %q", surface.Kind, surface.Name, surface.State)
		}
		if !validSHA256(surface.AdapterParitySHA256) || !validSHA256(surface.ReceiptSHA256) {
			t.Fatalf("surface %s/%s has invalid evidence digests", surface.Kind, surface.Name)
		}
		want := blocked[surface.SurfaceKey]
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
			want: "is not a declared accepted evidence gap",
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

func TestBuildAdmissionAcceptsExactReleasedLimitation(t *testing.T) {
	input := validAdmissionInput(t)
	failures := input.Policy.ReleasedAllowedFailures
	missing := input.Policy.ReleasedAllowedMissing
	input.Controller.Released.ExitCode = 1
	input.Controller.Released.Result = "accepted_limitation"
	for _, name := range append(append([]string(nil), failures...), missing...) {
		input.Controller.Released.Passed = removeString(input.Controller.Released.Passed, name)
	}
	input.Controller.Released.Failed = failures
	input.Controller.Released.AcceptedFailures = failures
	input.Controller.Released.Missing = missing
	if _, err := BuildAdmission(input); err != nil {
		t.Fatalf("BuildAdmission() error = %v", err)
	}
}

func TestBuildAdmissionAcceptsAllowedReleasedFailureThatPasses(t *testing.T) {
	input := validAdmissionInput(t)
	missing := input.Policy.ReleasedAllowedMissing
	input.Controller.Released.Result = "accepted_limitation"
	for _, name := range missing {
		input.Controller.Released.Passed = removeString(input.Controller.Released.Passed, name)
	}
	input.Controller.Released.Missing = missing
	if _, err := BuildAdmission(input); err != nil {
		t.Fatalf("BuildAdmission() error = %v", err)
	}
}

func TestBuildAdmissionRejectsBroaderReleasedMissing(t *testing.T) {
	input := validAdmissionInput(t)
	missing := input.Policy.ReleasedAllowedMissing
	input.Controller.Released.Result = "accepted_limitation"
	for _, name := range missing {
		input.Controller.Released.Passed = removeString(input.Controller.Released.Passed, name)
	}
	input.Controller.Released.Missing = append(append([]string(nil), missing...), "TestAccUnexpected")
	if _, err := BuildAdmission(input); err == nil {
		t.Fatal("BuildAdmission() accepted a broader released missing set")
	}
}

func TestValidateControllerSuiteKeepsCleanReleasedPass(t *testing.T) {
	planned := []string{"TestAccA", "TestAccDeviceFramework_basic"}
	suite := ControllerSuiteReceipt{
		ExitCode: 0,
		Result:   "pass",
		Passed:   planned,
	}
	if err := validateControllerSuite(
		"released",
		suite,
		planned,
		nil,
		[]string{"TestAccDeviceFramework_basic"},
		nil,
	); err != nil {
		t.Fatalf("validateControllerSuite() error = %v", err)
	}
}

func TestBuildAdmissionRejectsBroaderReleasedLimitation(t *testing.T) {
	input := validAdmissionInput(t)
	failures := []string{"TestAccDeviceFramework_basic", "TestAccUnexpected"}
	input.Controller.Plan.TestNames = append(input.Controller.Plan.TestNames, "TestAccUnexpected")
	input.Controller.Plan.Surfaces[0].TestNames = append(
		input.Controller.Plan.Surfaces[0].TestNames,
		"TestAccUnexpected",
	)
	input.Controller.Released.ExitCode = 1
	input.Controller.Released.Result = "accepted_limitation"
	for _, failure := range failures {
		input.Controller.Released.Passed = removeString(input.Controller.Released.Passed, failure)
	}
	input.Controller.Released.Failed = failures
	input.Controller.Released.AcceptedFailures = failures
	if _, err := BuildAdmission(input); err == nil {
		t.Fatal("BuildAdmission() accepted a broader released limitation")
	}
}

func removeString(values []string, remove string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != remove {
			result = append(result, value)
		}
	}
	return result
}

func testCampaignPolicy(t *testing.T) CampaignPolicy {
	t.Helper()
	data, err := os.ReadFile("../../provider-codegen/policy/catalog-campaign.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy CampaignPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	return policy
}

func validAdmissionInput(t *testing.T) AdmissionInput {
	t.Helper()
	policy := testCampaignPolicy(t)
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
		// The surfaces named by the campaign policy carry their real test
		// names so the policy's allowed failures and allowed missing tests
		// resolve against this plan the way they do against a real one.
		tests := []string{"TestAcc" + strings.TrimPrefix(surface.Name, "unifi_")}
		switch {
		case surface.Kind == ManagedResource && surface.Name == "unifi_device":
			tests = []string{"TestAccDeviceFramework_basic"}
		case surface.Kind == ListResource && surface.Name == "unifi_device":
			tests = []string{"TestAccDeviceList_basic"}
		case surface.Kind == ManagedResource && surface.Name == "unifi_firewall_zone":
			tests = []string{"TestAccFirewallZoneFramework_basic"}
		case surface.Kind == ListResource && surface.Name == "unifi_firewall_zone":
			tests = []string{"TestAccFirewallZoneList_emptyOrSeeded"}
		}
		planSurfaces = append(planSurfaces, ControllerPlanSurface{
			SurfaceKey:     surface.SurfaceKey,
			Wave:           surface.Wave,
			MissingSignals: append([]string{}, surface.MissingSignals...),
			TestNames:      tests,
		})
		allTests = append(allTests, tests...)
	}
	// controllerSuiteComplete rejects a suite whose plan does not carry every
	// declared disposition by name, so this fixture has to contain them. Derive
	// that from the policy rather than listing names: the switch above named
	// exactly the four dispositions that existed when it was written, and
	// widening released_allowed_missing from three names to thirteen broke nine
	// tests across two packages purely because the fixture had its own copy of
	// the list. A declaration with a second home in a fixture goes stale the
	// first time the declaration is right.
	for _, names := range [][]string{
		policy.AllowedSkips,
		policy.ReleasedAllowedFailures,
		policy.ReleasedAllowedMissing,
	} {
		for _, name := range names {
			if slices.Contains(allTests, name) {
				continue
			}
			planSurfaces[0].TestNames = append(planSurfaces[0].TestNames, name)
			allTests = append(allTests, name)
		}
	}
	for len(allTests) < policy.TestNameCount {
		name := fmt.Sprintf("TestAccSynthetic%03d", len(allTests))
		planSurfaces[0].TestNames = append(planSurfaces[0].TestNames, name)
		allTests = append(allTests, name)
	}
	// Admission re-derives the shared scenario owner set from the inventory and
	// compares SETS, so a synthetic list of the right length is no longer a
	// valid fixture. The rule is written out here rather than calling the
	// production helper: a fixture built by the code under test would agree
	// with it by construction.
	excepted := make(map[SurfaceKey]struct{}, len(policy.SharedScenarioExceptions))
	for _, exception := range policy.SharedScenarioExceptions {
		excepted[exception.SurfaceKey] = struct{}{}
	}
	ownerSet := map[string]struct{}{}
	for _, surface := range inventory.Surfaces {
		if _, exception := excepted[surface.SurfaceKey]; exception || surface.Runtime.Status == FileIdentical {
			for _, owner := range surface.ScenarioOwners {
				ownerSet[owner] = struct{}{}
			}
		}
	}
	sharedScenarioOwners := make([]string, 0, len(ownerSet))
	for owner := range ownerSet {
		sharedScenarioOwners = append(sharedScenarioOwners, owner)
	}
	sort.Strings(sharedScenarioOwners)
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
			FormatVersion:           1,
			Gate:                    "catalog controller differential",
			Waves:                   []int{1, 2, 3, 4, 5},
			Surfaces:                planSurfaces,
			SurfaceCount:            policy.SurfaceCount,
			EvidenceGapCount:        policy.EvidenceGapCount,
			SharedScenarioOwners:    sharedScenarioOwners,
			TestNames:               allTests,
			ReleasedAllowedFailures: policy.ReleasedAllowedFailures,
			ReleasedAllowedMissing:  policy.ReleasedAllowedMissing,
		},
		Released:  controllerSuite,
		Candidate: controllerSuite,
	}
	controllerSHA256 := strings.Repeat("c", 64)
	// The pragmatic resolution is RESOLVED here from committed inputs rather
	// than read from build/restricted/catalog-pragmatic-resolution.json.
	//
	// That file is a campaign OUTPUT, not a committed input: producing it needs
	// a passing controller differential receipt, which only exists during a
	// run. Reading it as a fixture made this test depend on a snapshot that
	// only a campaign can refresh -- and since `go test ./...` is itself the
	// gate the campaign must pass before it starts, a stale snapshot deadlocked
	// the two. Resolving from the inventory, the fleet summary and the
	// reference policy, all of which ARE committed inputs, breaks that.
	//
	// It does mean this fixture agrees with the resolver by construction. That
	// is correct here -- this test is about admission, and the resolver has its
	// own tests with the inventory held fixed -- but it is the reason a
	// resolver bug would not surface in this file.
	pragmatic := resolveCommittedPragmatic(t, inventory, inventorySHA256)
	pragmatic.ControllerReceiptSHA256 = controllerSHA256

	return AdmissionInput{
		Policy:            policy,
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

// resolveCommittedPragmatic runs the reference resolver over the committed
// inventory, fleet summary and reference policy.
func resolveCommittedPragmatic(t *testing.T, inventory EvidenceInventory, inventorySHA256 string) PragmaticResolution {
	t.Helper()
	fleetData, err := os.ReadFile("../../build/restricted/catalog-fleet-gap-summary.json")
	if err != nil {
		t.Fatal(err)
	}
	var fleet FleetReferenceSummary
	if err := json.Unmarshal(fleetData, &fleet); err != nil {
		t.Fatal(err)
	}
	referenceData, err := os.ReadFile("../../provider-codegen/policy/catalog-pragmatic-references.json")
	if err != nil {
		t.Fatal(err)
	}
	var references PragmaticReferenceSet
	if err := json.Unmarshal(referenceData, &references); err != nil {
		t.Fatal(err)
	}
	resolution, err := ResolvePragmaticReferences(
		inventory, inventorySHA256, fleet, byteSHA256(fleetData), references)
	if err != nil {
		t.Fatalf("resolving the committed pragmatic references: %v", err)
	}
	return resolution
}
