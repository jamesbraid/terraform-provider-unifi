package catalogparity

// IF YOU ARE ABOUT TO WRITE ONE OF THESE RECEIPTS FROM ANYTHING THAT IS NOT GO,
// READ THIS FIRST. That includes a shell script, a jq filter, a Python helper,
// or a workflow step assembling JSON inline.
//
// Every receipt below is written by a Go program from the same struct the
// consumer decodes, and every consumer decodes with DisallowUnknownFields. So
// the agreement between producer and consumer is held by the compiler: a field
// that one side adds and the other does not know about cannot be written.
//
// THAT WAS NOT ALWAYS TRUE AND IT COST TWICE. While the producers were shell,
// there was nothing between them and these types -- no import, no path, no name
// in common, nothing for a compiler to check. Two receipts were produced and
// then refused by their own consumers:
//
//   - catalog-dependency-publishability.sh wrote a `tree` key.
//     DependencyPublishabilityReceipt has no such field, so catalog-release-ready
//     could not decode the receipt at all. Latent, because no workflow ran that
//     consumer.
//   - catalog-controller-differential.sh added `diagnostic_selection` and
//     `catalog_test_count` to the plan in its diagnostic mode.
//     ControllerPlanReceipt had neither, so catalog-admission failed one step
//     later in the same workflow -- but only in the mode an operator reaches for
//     when something is already broken.
//
// Both were found by accident while porting something else. check_receipt_types_test.go
// existed to catch the class: it parsed each shell producer for the keys it
// wrote and compared them against the consumer's json tags, with a ledger of
// the pairs nobody had established yet. It was deleted when the last shell
// producer was, because a check over an empty population is a check that cannot
// fail, and its ledger of unpaired consumers went with it.
//
// A NON-GO PRODUCER BRINGS THE CLASS BACK, and nothing here will notice. If you
// are adding one, restore that check with your producer as its first pair, or
// accept that your receipt's fit with its consumer is verified by nobody.

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type BuildCounts struct {
	Released  int `json:"released"`
	Candidate int `json:"candidate"`
}

type ProviderBinaryEvidence struct {
	ReleasedSourceRebuildSHA256 string `json:"released_source_rebuild_sha256"`
	ReleasedAuthority           string `json:"released_authority"`
	ReleasedAuthoritySHA256     string `json:"released_authority_sha256"`
	CandidateSHA256             string `json:"candidate_sha256"`
}

type SchemaCLIReceipt struct {
	Version            string `json:"version"`
	BinarySHA256       string `json:"binary_sha256"`
	ReleasedRawSHA256  string `json:"released_raw_sha256"`
	CandidateRawSHA256 string `json:"candidate_raw_sha256"`
	CanonicalSHA256    string `json:"canonical_sha256"`
}

type SchemaDifferentialEvidence struct {
	Terraform                   SchemaCLIReceipt `json:"terraform"`
	Tofu                        SchemaCLIReceipt `json:"tofu"`
	ReleaseToCandidateWithinCLI bool             `json:"release_to_candidate_within_cli"`
	SharedCLIProjectionEqual    bool             `json:"shared_cli_projection_equal"`
	FullCLIProjectionEqual      bool             `json:"full_cli_projection_equal"`
	SharedSchemaSHA256          string           `json:"shared_schema_sha256"`
	TerraformOnlyCategories     []string         `json:"terraform_only_categories"`
}

// TreeState is what cmd/tree-state records in a receipt: the
// commit the receipt names, and whether the working tree it was generated from
// actually matched it.
//
// source_commit alone cannot answer that. It comes from `git rev-parse HEAD`,
// which on a dirty tree names a commit the receipt does not describe -- honest
// about what it saw and wrong about what exists, and it heals silently once the
// files land, so the window where it was wrong leaves no trace.
//
// A POINTER so a receipt without one stays without one. Every consumer here
// decodes with DisallowUnknownFields, and re-emitting an empty tree_state would
// read as "clean" to anyone scanning for the key -- a claim nothing measured.
type TreeState struct {
	Status     string   `json:"status"`
	Commit     string   `json:"commit"`
	DirtyPaths []string `json:"dirty_paths"`
}

type BuildSchemaReceipt struct {
	FormatVersion     int                        `json:"format_version"`
	Gate              string                     `json:"gate"`
	TreeState         *TreeState                 `json:"tree_state,omitempty"`
	Result            string                     `json:"result"`
	PromotionBlockers []string                   `json:"promotion_blockers"`
	SourceCommit      string                     `json:"source_commit"`
	ReleasedCommit    string                     `json:"released_commit"`
	Platform          string                     `json:"platform"`
	GoVersion         string                     `json:"go_version"`
	BuildNetwork      string                     `json:"build_network"`
	CleanBuilds       BuildCounts                `json:"clean_builds"`
	ProviderBinaries  ProviderBinaryEvidence     `json:"provider_binaries"`
	InventorySHA256   string                     `json:"catalog_evidence_inventory_sha256"`
	SchemaEvidence    SchemaDifferentialEvidence `json:"schema_evidence"`
}

type UnitSuiteReceipt struct {
	ExitCode                int    `json:"exit_code"`
	Result                  string `json:"result"`
	UnparsedLineCount       int    `json:"unparsed_line_count"`
	PackagePassCount        int    `json:"package_pass_count"`
	PackageFailCount        int    `json:"package_fail_count"`
	PassedTestCount         int    `json:"passed_test_count"`
	SkippedTestCount        int    `json:"skipped_test_count"`
	FailedTestCount         int    `json:"failed_test_count"`
	RawLogSHA256            string `json:"raw_log_sha256"`
	NormalizedSummarySHA256 string `json:"normalized_summary_sha256"`
}

type UnitDifferentialReceipt struct {
	FormatVersion     int              `json:"format_version"`
	Gate              string           `json:"gate"`
	TreeState         *TreeState       `json:"tree_state,omitempty"`
	Result            string           `json:"result"`
	PromotionBlockers []string         `json:"promotion_blockers"`
	Network           string           `json:"network"`
	SourceCommit      string           `json:"source_commit"`
	ReleasedCommit    string           `json:"released_commit"`
	Platform          string           `json:"platform"`
	GoVersion         string           `json:"go_version"`
	InventorySHA256   string           `json:"catalog_evidence_inventory_sha256"`
	Released          UnitSuiteReceipt `json:"released"`
	Candidate         UnitSuiteReceipt `json:"candidate"`
}

type ControllerPlanSurface struct {
	SurfaceKey
	Wave           int      `json:"wave"`
	MissingSignals []string `json:"missing_signals"`
	TestNames      []string `json:"test_names"`
}

type ControllerPlanReceipt struct {
	FormatVersion           int                     `json:"format_version"`
	Gate                    string                  `json:"gate"`
	Waves                   []int                   `json:"waves"`
	Surfaces                []ControllerPlanSurface `json:"surfaces"`
	SurfaceCount            int                     `json:"surface_count"`
	EvidenceGapCount        int                     `json:"evidence_gap_count"`
	SharedScenarioOwners    []string                `json:"shared_scenario_owners"`
	TestNames               []string                `json:"test_names"`
	AllowedSkips            []string                `json:"allowed_skips"`
	ReleasedAllowedFailures []string                `json:"released_allowed_failures"`
	ReleasedAllowedMissing  []string                `json:"released_allowed_missing"`

	// A DIAGNOSTIC RUN adds these two, and until now nothing here could hold
	// them: setting CATALOG_ACCEPTANCE_TEST_NAMES made the producer write
	// diagnostic_selection and catalog_test_count into the plan, and
	// cmd/catalog-admission -- run against that same file one step later in the
	// same workflow -- decodes with DisallowUnknownFields and failed on
	// "unknown field \"diagnostic_selection\"".
	//
	// The trigger is what makes it expensive: an operator narrows the run when
	// something is ALREADY broken, and the pipeline answers by failing a later
	// step with a JSON error the narrowing itself caused.
	//
	// catalog-controller-followup.sh reads both to decide whether a diagnostic
	// run counts as complete, so they are load-bearing rather than incidental:
	// one consumer needed them while another could not survive them.
	//
	// omitempty so a normal run's receipt is unchanged.
	DiagnosticSelection bool `json:"diagnostic_selection,omitempty"`
	CatalogTestCount    int  `json:"catalog_test_count,omitempty"`
}

type ControllerImageReceipt struct {
	Image      string `json:"image"`
	ImageID    string `json:"image_id"`
	PullPolicy string `json:"pull_policy"`
}

type ControllerFleetReceipt struct {
	Image        string `json:"image"`
	ImageID      string `json:"image_id"`
	HerderSHA256 string `json:"herder_sha256"`
}

type ControllerRyukReceipt struct {
	RyukImage   string `json:"ryuk_image"`
	RyukImageID string `json:"ryuk_image_id"`
}

type ControllerSuiteReceipt struct {
	ExitCode           int      `json:"exit_code"`
	Result             string   `json:"result"`
	Passed             []string `json:"passed"`
	Skipped            []string `json:"skipped"`
	Failed             []string `json:"failed"`
	AcceptedFailures   []string `json:"accepted_failures"`
	UnexpectedFailures []string `json:"unexpected_failures"`
	Missing            []string `json:"missing"`
	PreTestDiagnostics []string `json:"pre_test_diagnostics"`
}

type ControllerDifferentialReceipt struct {
	FormatVersion         int                    `json:"format_version"`
	Gate                  string                 `json:"gate"`
	TreeState             *TreeState             `json:"tree_state,omitempty"`
	Result                string                 `json:"result"`
	PlanSHA256            string                 `json:"plan_sha256"`
	ReleasedCommit        string                 `json:"released_commit"`
	CandidateCommit       string                 `json:"candidate_commit"`
	Target                ControllerImageReceipt `json:"target"`
	Fleet                 ControllerFleetReceipt `json:"fleet"`
	Testcontainers        ControllerRyukReceipt  `json:"testcontainers"`
	TerraformBinarySHA256 string                 `json:"terraform_binary_sha256"`
	Plan                  ControllerPlanReceipt  `json:"plan"`
	Released              ControllerSuiteReceipt `json:"released"`
	Candidate             ControllerSuiteReceipt `json:"candidate"`
}

type AdmissionInput struct {
	Policy            CampaignPolicy
	Inventory         EvidenceInventory
	InventorySHA256   string
	BuildSchema       BuildSchemaReceipt
	BuildSchemaSHA256 string
	Unit              UnitDifferentialReceipt
	UnitSHA256        string
	Controller        ControllerDifferentialReceipt
	ControllerSHA256  string
	Pragmatic         PragmaticResolution
	PragmaticSHA256   string
}

type AdmissionEvidenceDigests struct {
	InventorySHA256   string `json:"inventory_sha256"`
	BuildSchemaSHA256 string `json:"build_schema_sha256"`
	UnitSHA256        string `json:"unit_sha256"`
	ControllerSHA256  string `json:"controller_sha256"`
	PragmaticSHA256   string `json:"pragmatic_sha256"`
}

type SurfaceAdmission struct {
	SurfaceKey
	Wave                int            `json:"wave"`
	AdapterState        AdmissionState `json:"adapter_state"`
	AdapterParitySHA256 string         `json:"adapter_parity_sha256"`
	State               AdmissionState `json:"state"`
	ReceiptSHA256       string         `json:"receipt_sha256"`
	ReleaseBlockers     []string       `json:"release_blockers,omitempty"`
}

type AdmissionReceipt struct {
	FormatVersion         int                      `json:"format_version"`
	Gate                  string                   `json:"gate"`
	TreeState             *TreeState               `json:"tree_state,omitempty"`
	Result                string                   `json:"result"`
	ProviderAddress       string                   `json:"provider_address"`
	SourceCommit          string                   `json:"source_commit"`
	ReleasedCommit        string                   `json:"released_commit"`
	CandidateBinarySHA256 string                   `json:"candidate_binary_sha256"`
	Evidence              AdmissionEvidenceDigests `json:"evidence"`
	AdmittedSurfaceCount  int                      `json:"admitted_surface_count"`
	ReleaseBlockerCount   int                      `json:"release_blocker_count"`
	ReleaseBlockers       []EvidenceGap            `json:"release_blockers"`
	Surfaces              []SurfaceAdmission       `json:"surfaces"`
}

func BuildAdmission(input AdmissionInput) (AdmissionReceipt, error) {
	if err := validateAdmissionInput(input); err != nil {
		return AdmissionReceipt{}, err
	}

	resolved := make(map[EvidenceGap]ResolvedPragmaticSignal, len(input.Pragmatic.Resolved))
	for _, signal := range input.Pragmatic.Resolved {
		resolved[EvidenceGap{SurfaceKey: signal.SurfaceKey, Signal: signal.Signal}] = signal
	}
	blockers := make(map[SurfaceKey][]string, len(input.Pragmatic.Remaining))
	for _, gap := range input.Pragmatic.Remaining {
		blockers[gap.SurfaceKey] = append(blockers[gap.SurfaceKey], gap.Signal)
	}
	evidence := AdmissionEvidenceDigests{
		InventorySHA256:   input.InventorySHA256,
		BuildSchemaSHA256: input.BuildSchemaSHA256,
		UnitSHA256:        input.UnitSHA256,
		ControllerSHA256:  input.ControllerSHA256,
		PragmaticSHA256:   input.PragmaticSHA256,
	}

	surfaces := make([]SurfaceAdmission, 0, len(input.Inventory.Surfaces))
	for _, surface := range input.Inventory.Surfaces {
		surfaceResolved := make([]ResolvedPragmaticSignal, 0, len(surface.MissingSignals))
		for _, signal := range surface.MissingSignals {
			if value, ok := resolved[EvidenceGap{SurfaceKey: surface.SurfaceKey, Signal: signal}]; ok {
				surfaceResolved = append(surfaceResolved, value)
			}
		}
		adapterDigest, err := canonicalDigest(struct {
			FormatVersion int                       `json:"format_version"`
			State         AdmissionState            `json:"state"`
			Surface       SurfaceEvidenceInventory  `json:"surface"`
			Resolved      []ResolvedPragmaticSignal `json:"resolved"`
			Evidence      AdmissionEvidenceDigests  `json:"evidence"`
		}{1, AdapterParity, surface, surfaceResolved, evidence})
		if err != nil {
			return AdmissionReceipt{}, fmt.Errorf("surface %s/%s adapter receipt: %w", surface.Kind, surface.Name, err)
		}
		admissionDigest, err := canonicalDigest(struct {
			FormatVersion       int                        `json:"format_version"`
			State               AdmissionState             `json:"state"`
			SurfaceKey          SurfaceKey                 `json:"surface"`
			AdapterParitySHA256 string                     `json:"adapter_parity_sha256"`
			SourceCommit        string                     `json:"source_commit"`
			ReleasedCommit      string                     `json:"released_commit"`
			CandidateBinary     string                     `json:"candidate_binary_sha256"`
			SDK                 SDKComparison              `json:"sdk"`
			Platform            string                     `json:"platform"`
			GoVersion           string                     `json:"go_version"`
			Schema              SchemaDifferentialEvidence `json:"schema"`
			Target              ControllerImageReceipt     `json:"target"`
			Fleet               ControllerFleetReceipt     `json:"fleet"`
			Testcontainers      ControllerRyukReceipt      `json:"testcontainers"`
			ControllerPlan      string                     `json:"controller_plan_sha256"`
			Evidence            AdmissionEvidenceDigests   `json:"evidence"`
		}{
			1, Admitted, surface.SurfaceKey, adapterDigest,
			input.BuildSchema.SourceCommit, input.BuildSchema.ReleasedCommit,
			input.BuildSchema.ProviderBinaries.CandidateSHA256, input.Inventory.SDK,
			input.BuildSchema.Platform, input.BuildSchema.GoVersion,
			input.BuildSchema.SchemaEvidence, input.Controller.Target,
			input.Controller.Fleet, input.Controller.Testcontainers,
			input.Controller.PlanSHA256, evidence,
		})
		if err != nil {
			return AdmissionReceipt{}, fmt.Errorf("surface %s/%s admission receipt: %w", surface.Kind, surface.Name, err)
		}
		surfaces = append(surfaces, SurfaceAdmission{
			SurfaceKey:          surface.SurfaceKey,
			Wave:                surface.Wave,
			AdapterState:        AdapterParity,
			AdapterParitySHA256: adapterDigest,
			State:               Admitted,
			ReceiptSHA256:       admissionDigest,
			ReleaseBlockers:     blockers[surface.SurfaceKey],
		})
	}

	return AdmissionReceipt{
		FormatVersion:         1,
		Gate:                  "catalog-admission",
		Result:                "pass",
		ProviderAddress:       CanonicalProviderAddress,
		SourceCommit:          input.BuildSchema.SourceCommit,
		ReleasedCommit:        input.BuildSchema.ReleasedCommit,
		CandidateBinarySHA256: input.BuildSchema.ProviderBinaries.CandidateSHA256,
		Evidence:              evidence,
		AdmittedSurfaceCount:  len(surfaces),
		ReleaseBlockerCount:   len(input.Pragmatic.Remaining),
		ReleaseBlockers:       append([]EvidenceGap(nil), input.Pragmatic.Remaining...),
		Surfaces:              surfaces,
	}, nil
}

func validateAdmissionInput(input AdmissionInput) error {
	for label, digest := range map[string]string{
		"inventory": input.InventorySHA256, "build/schema": input.BuildSchemaSHA256,
		"unit": input.UnitSHA256, "controller": input.ControllerSHA256,
		"pragmatic": input.PragmaticSHA256,
	} {
		if !validSHA256(digest) {
			return fmt.Errorf("%s receipt SHA-256 is invalid", label)
		}
	}
	if err := input.Policy.Validate(); err != nil {
		return err
	}
	if err := validateAdmissionInventory(input.Inventory, input.InventorySHA256); err != nil {
		return err
	}
	if err := validateBuildSchemaAdmission(input.BuildSchema, input.InventorySHA256); err != nil {
		return err
	}
	if input.BuildSchema.ReleasedCommit != input.Inventory.ReleasedProvider.Commit {
		return fmt.Errorf("released commit differs between inventory and build/schema receipt")
	}
	if err := validateUnitAdmission(input.Unit, input.BuildSchema, input.InventorySHA256); err != nil {
		return err
	}
	if err := validateControllerAdmission(input.Controller, input.BuildSchema, input.Inventory, input.Policy); err != nil {
		return err
	}
	if input.ControllerSHA256 != input.Pragmatic.ControllerReceiptSHA256 {
		return fmt.Errorf("controller receipt SHA-256 does not match pragmatic resolution")
	}
	return validatePragmaticAdmission(input.Pragmatic, input.Inventory, input.InventorySHA256, input.Policy)
}

func validateAdmissionInventory(inventory EvidenceInventory, digest string) error {
	if inventory.FormatVersion != 1 || inventory.ProviderAddress != CanonicalProviderAddress {
		return fmt.Errorf("inventory identity is invalid")
	}
	if !validSHA256(digest) || !validSHA256(inventory.BaselineSHA256) ||
		!validSHA256(inventory.ReleasedRuntimeTreeSHA256) ||
		!validSHA256(inventory.CandidateRuntimeTreeSHA256) {
		return fmt.Errorf("inventory SHA-256 lineage is invalid")
	}
	if len(inventory.Surfaces) != 67 {
		return fmt.Errorf("inventory has %d surfaces, want 67", len(inventory.Surfaces))
	}
	measuredDigest, err := canonicalDigest(inventory)
	if err != nil {
		return fmt.Errorf("encode inventory: %w", err)
	}
	if measuredDigest != digest {
		return fmt.Errorf("inventory SHA-256 does not match its content")
	}
	if err := validateSDKComparison(inventory.SDK); err != nil {
		return err
	}
	if inventory.SDK.ModulePath != "github.com/jamesbraid/go-unifi" ||
		inventory.SDK.ReleasedVersion != "v1.101.0" || inventory.SDK.CandidateVersion != "v1.102.0" {
		return fmt.Errorf("inventory SDK lineage is invalid")
	}
	if !validCommit(inventory.ReleasedProvider.Commit) || inventory.ReleasedProvider.Version != "0.101.2" {
		return fmt.Errorf("released provider identity is invalid")
	}
	seen := make(map[SurfaceKey]struct{}, len(inventory.Surfaces))
	for index, surface := range inventory.Surfaces {
		if !validSurfaceKind(surface.Kind) || !surfaceNamePattern.MatchString(surface.Name) {
			return fmt.Errorf("inventory surface %s/%s is invalid", surface.Kind, surface.Name)
		}
		if _, duplicate := seen[surface.SurfaceKey]; duplicate {
			return fmt.Errorf("duplicate inventory surface %s/%s", surface.Kind, surface.Name)
		}
		seen[surface.SurfaceKey] = struct{}{}
		if index > 0 && !surfaceLess(inventory.Surfaces[index-1].SurfaceKey, surface.SurfaceKey) {
			return fmt.Errorf("inventory surfaces are not strictly sorted")
		}
		if surface.Wave < 1 || surface.Wave > 5 || len(surface.ScenarioOwners) == 0 ||
			len(surface.TestFunctions) == 0 {
			return fmt.Errorf("inventory surface %s/%s evidence is incomplete", surface.Kind, surface.Name)
		}
		for label, comparison := range map[string]FileComparison{"runtime": surface.Runtime, "tests": surface.Tests} {
			if comparison.Path == "" ||
				(comparison.Status != FileIdentical && comparison.Status != FileChanged) ||
				!validSHA256(comparison.ReleasedSHA256) || !validSHA256(comparison.CandidateSHA256) {
				return fmt.Errorf("inventory surface %s/%s %s comparison is invalid", surface.Kind, surface.Name, label)
			}
		}
	}
	// The gate checks that the inventory's coverage counts describe the
	// inventory's own surfaces. It does NOT pin absolute values.
	//
	// It used to pin them, with acceptance hand-typed as 35. Adding an
	// acceptance test to any surface made this gate refuse the tree, and refuse
	// it with a message naming nothing, so the person whose work broke the
	// release gate had six numbers and a boolean to work from. A count of how
	// many surfaces have acceptance tests is a number that SHOULD rise; a gate
	// that blocks on it is a ratchet against the work.
	//
	// What is expected of the tree is declared once, as the policy's evidence
	// gaps. This is the integrity half: whatever the counts claim, they must be
	// a true tally of the surfaces alongside them, so a hand-edited or truncated
	// inventory is caught.
	// EVERY disagreeing count is reported, not the first. Returning on the first
	// makes the operator fix one number, run again, and discover the next --
	// once per round trip through a gate that is not cheap to reach. The
	// superseded version of this check printed both maps in full for the same
	// reason, and that instinct was right even though the literal it defended
	// was not.
	recount := recountCoverage(inventory.Surfaces)
	disagreements := make([]string, 0)
	for _, key := range coverageKeys {
		if inventory.CoverageCounts[key] != recount[key] {
			disagreements = append(disagreements, fmt.Sprintf(
				"%q is %d but its surfaces tally %d", key, inventory.CoverageCounts[key], recount[key]))
		}
	}
	if len(disagreements) != 0 {
		return fmt.Errorf("inventory coverage counts contradict its surfaces: %s",
			strings.Join(disagreements, "; "))
	}
	if len(inventory.CoverageCounts) != len(coverageKeys) {
		return fmt.Errorf("inventory declares %d coverage counts, want %d",
			len(inventory.CoverageCounts), len(coverageKeys))
	}
	return nil
}

// repeatedTestNames returns the names appearing more than once, sorted.
//
// The block above compares len(receipt.Plan.TestNames) against the policy's
// declared count, and a LENGTH is satisfied by padding: a plan that repeats one
// name and drops another hits the same total while covering fewer distinct
// tests. Nothing else in Go would notice.
//
// This is not hypothetical. The fixture in admission_test.go carried exactly
// that shape until it was corrected alongside this check -- 156 entries, 124
// distinct, against a declared count of 156 -- because it derived one name per
// stem with the surface kind discarded. The suite that validates admission was
// itself the padded plan.
//
// Uniqueness is guaranteed in production by exactly one thing:
// catalog-controller-differential.sh:61 ends its test_names with `| unique`.
// That is the reason to check it here rather than the reason not to. A
// guarantee held in one layer and unverified in the next is how
// cmd/schema-baseline kept a refuted exemption reason and how allowed_skips sat
// unbound -- the shell being right is not the question, whether anything
// notices when it stops being right is.
//
// It names the repeats rather than reporting a count, because a gate that says
// the plan is wrong without saying which entry is wrong leaves the reader to
// diff two lists by eye.
//
// PROVEN TO FAIL, both directions, each restored:
//
//	deleting this block leaves the "controller plan repeats a test name" case
//	reporting `error = <nil>, want "repeats 1 test name"`, so the check is what
//	catches it rather than some neighbouring assertion.
//
//	reverting admission_test.go's surfaceTestStem to the kind-ignoring name
//	turns the ordinary happy-path test red with "repeats 24 test name(s)" --
//	no mutation of the plan required. That is how this was found: the check
//	fired on the fixture before it could be aimed at anything.
func repeatedTestNames(names []string) []string {
	seen := make(map[string]int, len(names))
	for _, name := range names {
		seen[name]++
	}
	repeated := make([]string, 0)
	for name, count := range seen {
		if count > 1 {
			repeated = append(repeated, fmt.Sprintf("%s (x%d)", name, count))
		}
	}
	sort.Strings(repeated)
	return repeated
}

var coverageKeys = []string{
	"scenario_owner", "constructor", "acceptance", "import", "list_acceptance", "action_acceptance",
}

// recountCoverage tallies the coverage counts from the per-surface signals.
//
// DO NOT MAKE BuildEvidenceInventory CALL THIS. It restates the accumulation
// that the generator performs inline, and the restatement is the entire point:
// the gate compares the generator's tally with an independent count of the data
// the generator emitted. Share the implementation and both sides derive from
// one source, they agree no matter what either does, and the check becomes one
// that cannot fail -- which is the exact mechanism behind a schema referee that
// passed on a broken tree, a golden regenerated during a fix, and a pre-flight
// comparing a plan to the stale artifact that produced it.
func recountCoverage(surfaces []SurfaceEvidenceInventory) map[string]int {
	counts := map[string]int{}
	for _, key := range coverageKeys {
		counts[key] = 0
	}
	for _, surface := range surfaces {
		counts["scenario_owner"]++
		if surface.Signals.Constructor {
			counts["constructor"]++
		}
		switch surface.Kind {
		case ManagedResource:
			if surface.Signals.Acceptance {
				counts["acceptance"]++
			}
			if surface.Signals.Import {
				counts["import"]++
			}
		case DataSource:
			if surface.Signals.Acceptance {
				counts["acceptance"]++
			}
		case ListResource:
			if surface.Signals.ListAcceptance {
				counts["list_acceptance"]++
			}
		case Action:
			if surface.Signals.ActionAcceptance {
				counts["action_acceptance"]++
			}
		}
	}
	return counts
}

func validateBuildSchemaAdmission(receipt BuildSchemaReceipt, inventorySHA256 string) error {
	if receipt.FormatVersion != 1 || receipt.Gate != "catalog-build-schema" {
		return fmt.Errorf("build/schema identity is invalid")
	}
	if receipt.Result != "pass" || len(receipt.PromotionBlockers) != 0 {
		return fmt.Errorf("build/schema result is %q with blockers %v", receipt.Result, receipt.PromotionBlockers)
	}
	if !validCommit(receipt.SourceCommit) || !validCommit(receipt.ReleasedCommit) {
		return fmt.Errorf("build/schema source commit identity is invalid")
	}
	if receipt.Platform != "linux/amd64" || receipt.GoVersion != "go1.25.8" || receipt.BuildNetwork != "none" {
		return fmt.Errorf("build/schema toolchain is not promotable")
	}
	if receipt.CleanBuilds != (BuildCounts{Released: 2, Candidate: 2}) {
		return fmt.Errorf("build/schema clean build counts are incomplete")
	}
	if receipt.InventorySHA256 != inventorySHA256 {
		return fmt.Errorf("build/schema inventory SHA-256 does not match")
	}
	for label, digest := range map[string]string{
		"released source binary":    receipt.ProviderBinaries.ReleasedSourceRebuildSHA256,
		"released authority binary": receipt.ProviderBinaries.ReleasedAuthoritySHA256,
		"candidate binary":          receipt.ProviderBinaries.CandidateSHA256,
		"shared schema":             receipt.SchemaEvidence.SharedSchemaSHA256,
	} {
		if !validSHA256(digest) {
			return fmt.Errorf("build/schema %s SHA-256 is invalid", label)
		}
	}
	if receipt.ProviderBinaries.ReleasedAuthority != "published_archive" {
		return fmt.Errorf("build/schema released binary authority is %q", receipt.ProviderBinaries.ReleasedAuthority)
	}
	if err := validateSchemaCLI("terraform", receipt.SchemaEvidence.Terraform, "1.15.8"); err != nil {
		return err
	}
	if err := validateSchemaCLI("tofu", receipt.SchemaEvidence.Tofu, "1.12.1"); err != nil {
		return err
	}
	if !receipt.SchemaEvidence.ReleaseToCandidateWithinCLI ||
		!receipt.SchemaEvidence.SharedCLIProjectionEqual ||
		receipt.SchemaEvidence.FullCLIProjectionEqual ||
		!reflect.DeepEqual(receipt.SchemaEvidence.TerraformOnlyCategories, []string{"action_schemas", "list_resource_schemas"}) {
		return fmt.Errorf("build/schema parity projection is invalid")
	}
	return nil
}

func validateSchemaCLI(name string, receipt SchemaCLIReceipt, version string) error {
	if receipt.Version != version {
		return fmt.Errorf("build/schema %s version is %q", name, receipt.Version)
	}
	for _, digest := range []string{
		receipt.BinarySHA256, receipt.ReleasedRawSHA256,
		receipt.CandidateRawSHA256, receipt.CanonicalSHA256,
	} {
		if !validSHA256(digest) {
			return fmt.Errorf("build/schema %s SHA-256 is invalid", name)
		}
	}
	return nil
}

func validateUnitAdmission(receipt UnitDifferentialReceipt, build BuildSchemaReceipt, inventorySHA256 string) error {
	if receipt.FormatVersion != 1 || receipt.Gate != "catalog-unit-http-differential" {
		return fmt.Errorf("unit differential identity is invalid")
	}
	if receipt.Result != "pass" || len(receipt.PromotionBlockers) != 0 || receipt.Network != "none" {
		return fmt.Errorf("unit differential result is %q with blockers %v", receipt.Result, receipt.PromotionBlockers)
	}
	if receipt.SourceCommit != build.SourceCommit {
		return fmt.Errorf("source commit differs between build/schema and unit receipts")
	}
	if receipt.ReleasedCommit != build.ReleasedCommit || receipt.InventorySHA256 != inventorySHA256 {
		return fmt.Errorf("unit inventory SHA-256 or released commit does not match")
	}
	if receipt.Platform != build.Platform || receipt.GoVersion != build.GoVersion {
		return fmt.Errorf("unit toolchain differs from build/schema")
	}
	if err := validateUnitSuite("released", receipt.Released); err != nil {
		return err
	}
	return validateUnitSuite("candidate", receipt.Candidate)
}

func validateUnitSuite(label string, suite UnitSuiteReceipt) error {
	if suite.Result != "pass" || suite.ExitCode != 0 || suite.UnparsedLineCount != 0 ||
		suite.PackagePassCount <= 0 || suite.PackageFailCount != 0 || suite.FailedTestCount != 0 ||
		!validSHA256(suite.RawLogSHA256) || !validSHA256(suite.NormalizedSummarySHA256) {
		return fmt.Errorf("%s unit suite is incomplete or failed", label)
	}
	return nil
}

func validateControllerAdmission(
	receipt ControllerDifferentialReceipt,
	build BuildSchemaReceipt,
	inventory EvidenceInventory,
	policy CampaignPolicy,
) error {
	if receipt.FormatVersion != 1 || receipt.Gate != "catalog controller differential" {
		return fmt.Errorf("controller differential identity is invalid")
	}
	if err := RequireControllerResultAgreesWithGaps(receipt); err != nil {
		return err
	}
	if !validSHA256(receipt.PlanSHA256) {
		return fmt.Errorf("controller differential records no plan digest")
	}
	if receipt.ReleasedCommit != build.ReleasedCommit || receipt.CandidateCommit != build.SourceCommit {
		return fmt.Errorf("controller source commit lineage does not match build/schema")
	}
	if receipt.TerraformBinarySHA256 != build.SchemaEvidence.Terraform.BinarySHA256 {
		return fmt.Errorf("controller Terraform binary differs from schema toolchain")
	}
	if !strings.Contains(receipt.Target.Image, "@sha256:") || !validImageID(receipt.Target.ImageID) || receipt.Target.PullPolicy != "never" ||
		receipt.Fleet.Image == "" || !validImageID(receipt.Fleet.ImageID) || !validSHA256(receipt.Fleet.HerderSHA256) ||
		!strings.Contains(receipt.Testcontainers.RyukImage, "@sha256:") || !validImageID(receipt.Testcontainers.RyukImageID) {
		return fmt.Errorf("controller target or helper identity is incomplete")
	}
	if receipt.Plan.FormatVersion != 1 || receipt.Plan.Gate != receipt.Gate ||
		!reflect.DeepEqual(receipt.Plan.Waves, []int{1, 2, 3, 4, 5}) ||
		receipt.Plan.SurfaceCount != policy.SurfaceCount ||
		len(receipt.Plan.Surfaces) != policy.SurfaceCount ||
		receipt.Plan.EvidenceGapCount != policy.EvidenceGapCount ||
		len(receipt.Plan.TestNames) != policy.TestNameCount ||
		!reflect.DeepEqual(receipt.Plan.ReleasedAllowedFailures, policy.ReleasedAllowedFailures) ||
		!reflect.DeepEqual(receipt.Plan.ReleasedAllowedMissing, policy.ReleasedAllowedMissing) ||
		// AllowedSkips sat outside this block while its two neighbours were in
		// it, and the difference was not deliberate: a skip is the one excusal
		// the receipt could grant itself.
		//
		// validateControllerSuite constrains skips only in shape -- unique, a
		// subset of the PLANNED names, no longer than the plan. Nothing tied
		// them to what the campaign actually agreed to skip, so the receipt
		// declared its own allowance and the gate checked the receipt against
		// itself. Measured against this fixture: the policy declares 3 allowed
		// skips, and a receipt declaring 106 of the 156 planned tests skipped
		// -- with only 50 actually running -- was ADMITTED. That was not a
		// ceiling either; 106 was the size of the pool the probe drew from.
		//
		// SUBSET, NOT EQUALITY, and the fixture is what settled that. Its plan
		// declares no skips at all against a policy permitting three, and it is
		// right to: a test the campaign was willing to skip that ran anyway is a
		// better run, not a broken one. The direction that matters is the other
		// one -- a receipt may never skip something the campaign did not agree
		// to. Its two neighbours above are exact because they describe the
		// released side's known limitations, where fewer is as suspect as more.
		!allStringsInSet(receipt.Plan.AllowedSkips, policy.AllowedSkips) {
		return fmt.Errorf("controller plan surfaces or counts are incomplete")
	}
	if repeated := repeatedTestNames(receipt.Plan.TestNames); len(repeated) > 0 {
		return fmt.Errorf("controller plan repeats %d test name(s), so it covers fewer distinct tests than it declares: %s",
			len(repeated), strings.Join(repeated, ", "))
	}
	if err := validateSharedScenarioOwners(receipt.Plan.SharedScenarioOwners, inventory, policy); err != nil {
		return err
	}
	want := make(map[SurfaceKey]SurfaceEvidenceInventory, len(inventory.Surfaces))
	for _, surface := range inventory.Surfaces {
		want[surface.SurfaceKey] = surface
	}
	seen := make(map[SurfaceKey]struct{}, len(receipt.Plan.Surfaces))
	for _, surface := range receipt.Plan.Surfaces {
		inventorySurface, ok := want[surface.SurfaceKey]
		if !ok {
			return fmt.Errorf("controller plan references unknown surface %s/%s", surface.Kind, surface.Name)
		}
		if _, duplicate := seen[surface.SurfaceKey]; duplicate {
			return fmt.Errorf("controller plan has duplicate surface %s/%s", surface.Kind, surface.Name)
		}
		seen[surface.SurfaceKey] = struct{}{}
		if surface.Wave != inventorySurface.Wave || !reflect.DeepEqual(surface.MissingSignals, inventorySurface.MissingSignals) {
			return fmt.Errorf("controller plan surface %s/%s differs from inventory", surface.Kind, surface.Name)
		}
	}
	if err := validateControllerSuite(
		"released", receipt.Released, receipt.Plan.TestNames, receipt.Plan.AllowedSkips,
		receipt.Plan.ReleasedAllowedFailures, receipt.Plan.ReleasedAllowedMissing,
	); err != nil {
		return err
	}
	return validateControllerSuite(
		"candidate", receipt.Candidate, receipt.Plan.TestNames, receipt.Plan.AllowedSkips,
		nil, nil,
	)
}

// validateSharedScenarioOwners re-derives the plan's shared scenario owners
// from the inventory and compares the SET, not its length.
//
// A shared scenario owner is not a statistic. It is a file the campaign copies
// out of the candidate tree over the released checkout
// (.woodpecker/scripts/catalog-controller-differential.sh:137-139) so that one
// scenario exercises both providers. Copying it is only sound when the two
// runtimes are the same file, which is what the derivation reads.
//
// This replaces a comparison against a hardcoded shared_scenario_owner_count.
// That constant was a copy of a fact the inventory already carries, so it could
// only ever go stale -- and comparing lengths meant a plan carrying the right
// number of WRONG owners passed. Deriving the set here gives admission an
// independent reading of the same rule the jq plan builder applies, which is
// the point: two implementations that must agree, rather than one blessed by a
// number nobody rechecks.
func validateSharedScenarioOwners(
	planned []string,
	inventory EvidenceInventory,
	policy CampaignPolicy,
) error {
	excepted := make(map[SurfaceKey]struct{}, len(policy.SharedScenarioExceptions))
	for _, exception := range policy.SharedScenarioExceptions {
		excepted[exception.SurfaceKey] = struct{}{}
	}
	known := make(map[SurfaceKey]struct{}, len(inventory.Surfaces))
	owners := make(map[string]struct{}, len(inventory.Surfaces))
	for _, surface := range inventory.Surfaces {
		known[surface.SurfaceKey] = struct{}{}
		_, exception := excepted[surface.SurfaceKey]
		// A declaration is checked in both directions. An exception for a
		// surface whose runtime is already identical is doing nothing, and a
		// silently redundant exception is how a list survives the reason it was
		// written for.
		if exception && surface.Runtime.Status == FileIdentical {
			return fmt.Errorf(
				"campaign policy declares a shared scenario exception for %s/%s, whose runtime is identical",
				surface.Kind, surface.Name)
		}
		if surface.Runtime.Status == FileIdentical || exception {
			// Every owner, not just the first. A surface that lends evidence
			// lends all of it, and taking one file here would re-derive a
			// smaller set than the plan grafts -- the two would disagree
			// without either being wrong on its own terms.
			for _, owner := range surface.ScenarioOwners {
				owners[owner] = struct{}{}
			}
		}
	}
	for _, exception := range policy.SharedScenarioExceptions {
		if _, ok := known[exception.SurfaceKey]; !ok {
			return fmt.Errorf(
				"campaign policy declares a shared scenario exception for unknown surface %s/%s",
				exception.Kind, exception.Name)
		}
	}
	want := make([]string, 0, len(owners))
	for owner := range owners {
		want = append(want, owner)
	}
	// jq's `unique` sorts, so the plan's list is sorted and this must be too.
	sort.Strings(want)
	if !reflect.DeepEqual(planned, want) {
		return fmt.Errorf("controller plan shared scenario owners are %v, the inventory yields %v", planned, want)
	}
	return nil
}

func validateControllerSuite(
	label string,
	suite ControllerSuiteReceipt,
	planned, allowedSkips, allowedFailures, allowedMissing []string,
) error {
	if len(allowedSkips) > len(planned) ||
		len(allowedSkips) != len(uniqueStrings(allowedSkips)) ||
		!allStringsInSet(allowedSkips, planned) ||
		len(allowedFailures) != len(uniqueStrings(allowedFailures)) ||
		!allStringsInSet(allowedFailures, planned) ||
		len(allowedMissing) != len(uniqueStrings(allowedMissing)) ||
		!allStringsInSet(allowedMissing, planned) ||
		stringsOverlap(allowedSkips, allowedMissing) ||
		stringsOverlap(allowedFailures, allowedMissing) {
		return fmt.Errorf("%s controller suite is incomplete or failed", label)
	}
	if suite.Result == "accepted_limitation" {
		wantPassed := make([]string, 0, len(planned)-len(allowedSkips)-len(suite.Failed)-len(allowedMissing))
		for _, testName := range planned {
			if !controllerContainsString(allowedSkips, testName) &&
				!controllerContainsString(suite.Failed, testName) &&
				!controllerContainsString(allowedMissing, testName) {
				wantPassed = append(wantPassed, testName)
			}
		}
		if len(allowedFailures)+len(allowedMissing) == 0 ||
			(len(suite.Failed) > 0 && suite.ExitCode == 0) ||
			(len(suite.Failed) == 0 && suite.ExitCode != 0) ||
			len(suite.UnexpectedFailures) != 0 ||
			len(suite.Failed) != len(uniqueStrings(suite.Failed)) ||
			!allStringsInSet(suite.Failed, allowedFailures) ||
			!sameStringSet(suite.AcceptedFailures, suite.Failed) ||
			!sameStringSet(suite.Missing, allowedMissing) ||
			!sameStringSet(suite.Skipped, allowedSkips) ||
			!sameStringSet(suite.Passed, wantPassed) {
			return fmt.Errorf("%s controller suite is incomplete or failed", label)
		}
		return nil
	}
	wantPassed := make([]string, 0, len(planned)-len(allowedSkips))
	for _, testName := range planned {
		if !controllerContainsString(allowedSkips, testName) {
			wantPassed = append(wantPassed, testName)
		}
	}
	if suite.Result != "pass" || suite.ExitCode != 0 ||
		len(suite.Failed) != 0 || len(suite.AcceptedFailures) != 0 ||
		len(suite.UnexpectedFailures) != 0 || len(suite.Missing) != 0 ||
		!sameStringSet(suite.Skipped, allowedSkips) ||
		!sameStringSet(suite.Passed, wantPassed) {
		return fmt.Errorf(
			"%s controller suite is incomplete or failed: result=%q exit=%d passed=%d want_passed=%d skipped=%d want_skipped=%d failed=%d accepted=%d unexpected=%d missing=%d",
			label,
			suite.Result,
			suite.ExitCode,
			len(suite.Passed),
			len(wantPassed),
			len(suite.Skipped),
			len(allowedSkips),
			len(suite.Failed),
			len(suite.AcceptedFailures),
			len(suite.UnexpectedFailures),
			len(suite.Missing),
		)
	}
	return nil
}

func stringsOverlap(left, right []string) bool {
	for _, value := range left {
		if controllerContainsString(right, value) {
			return true
		}
	}
	return false
}

func controllerContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func allStringsInSet(values, set []string) bool {
	for _, value := range values {
		if !controllerContainsString(set, value) {
			return false
		}
	}
	return true
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func validatePragmaticAdmission(resolution PragmaticResolution, inventory EvidenceInventory, inventorySHA256 string, policy CampaignPolicy) error {
	if resolution.FormatVersion != 1 || resolution.ProviderAddress != CanonicalProviderAddress ||
		resolution.Result != "blocked_evidence" || resolution.InventorySHA256 != inventorySHA256 {
		return fmt.Errorf("pragmatic resolution identity or inventory SHA-256 is invalid")
	}
	if !validSHA256(resolution.FleetSummarySHA256) || !validSHA256(resolution.ControllerReceiptSHA256) {
		return fmt.Errorf("pragmatic resolution lineage SHA-256 is invalid")
	}
	if resolution.ResolvedSignalCount != len(resolution.Resolved) ||
		resolution.RemainingSignalCount != len(resolution.Remaining) {
		return fmt.Errorf("pragmatic resolution counts are incomplete")
	}
	want := make(map[EvidenceGap]struct{})
	for _, surface := range inventory.Surfaces {
		for _, signal := range surface.MissingSignals {
			want[EvidenceGap{SurfaceKey: surface.SurfaceKey, Signal: signal}] = struct{}{}
		}
	}
	got := make(map[EvidenceGap]struct{}, len(resolution.Resolved)+len(resolution.Remaining))
	for _, signal := range resolution.Resolved {
		gap := EvidenceGap{SurfaceKey: signal.SurfaceKey, Signal: signal.Signal}
		if _, duplicate := got[gap]; duplicate {
			return fmt.Errorf("pragmatic resolution duplicates %s/%s signal %q", gap.Kind, gap.Name, gap.Signal)
		}
		got[gap] = struct{}{}
	}
	accepted := make(map[EvidenceGap]struct{}, len(policy.AcceptedEvidenceGaps))
	for _, gap := range policy.AcceptedEvidenceGaps {
		accepted[gap.Gap()] = struct{}{}
	}
	// Every gap the campaign could not resolve must have been declared, WITH a
	// reason, in the campaign policy. This used to assert that the only such
	// gap could ever be the port action's hardware claim, which was true when
	// it was written and stopped being true when three pragmatic references
	// were withdrawn for leaning on sources the released provider never ran.
	for _, gap := range resolution.Remaining {
		if _, declared := accepted[gap]; !declared {
			return fmt.Errorf(
				"remaining signal %s/%s %q is not a declared accepted evidence gap",
				gap.Kind, gap.Name, gap.Signal)
		}
		if _, duplicate := got[gap]; duplicate {
			return fmt.Errorf("pragmatic resolution duplicates %s/%s signal %q", gap.Kind, gap.Name, gap.Signal)
		}
		got[gap] = struct{}{}
	}
	// A declared acceptance the campaign actually resolved is a stale
	// declaration. Checking both directions is what stops the list growing to
	// fit whatever went wrong.
	remaining := make(map[EvidenceGap]struct{}, len(resolution.Remaining))
	for _, gap := range resolution.Remaining {
		remaining[gap] = struct{}{}
	}
	for _, gap := range policy.AcceptedEvidenceGaps {
		if _, unresolved := remaining[gap.Gap()]; !unresolved {
			return fmt.Errorf(
				"campaign policy accepts %s/%s %q, which the campaign resolved",
				gap.Kind, gap.Name, gap.Signal)
		}
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("pragmatic resolution does not cover every inventory gap")
	}
	return nil
}

func validCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validImageID(value string) bool {
	return strings.HasPrefix(value, "sha256:") && validSHA256(strings.TrimPrefix(value, "sha256:"))
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	values := make(map[string]int, len(left))
	for _, value := range left {
		values[value]++
	}
	for _, value := range right {
		values[value]--
		if values[value] < 0 {
			return false
		}
	}
	return true
}

func canonicalDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return byteSHA256(append(data, '\n')), nil
}

// ControllerRegressionReceipt is the campaign's second suite, in its own
// artifact rather than as a field of the differential.
//
// TWO REASONS IT IS SEPARATE, and only the first is about file layout.
//
// It is not half of a comparison. The differential judges a released tree
// against a candidate one; a regression guard is written for a defect found
// after the released tree shipped, so it has no released counterpart and a
// missing one there would be correct rather than a finding. Scoring it as part
// of that comparison would manufacture failures out of the passage of time.
//
// And ControllerDifferentialReceipt is compared byte for byte against what the
// shell emitted on pipeline 232 -- that comparison is the migration's
// acceptance criterion. A key the shell never wrote would be the Go port
// claiming to have invented evidence, which the frozen tests exist to catch.
// The guards postdate the shell, so they get their own receipt instead of
// growing one whose shape is load-bearing.
type ControllerRegressionReceipt struct {
	FormatVersion   int        `json:"format_version"`
	Gate            string     `json:"gate"`
	TreeState       *TreeState `json:"tree_state,omitempty"`
	Result          string     `json:"result"`
	CandidateCommit string     `json:"candidate_commit"`
	// TestNames is what ran, named rather than counted. "The campaign passed"
	// meaning something different this week from last is how a suite nothing
	// ran became possible, and a count cannot show a set that shrank.
	TestNames []string               `json:"test_names"`
	Suite     ControllerSuiteReceipt `json:"suite"`
}
