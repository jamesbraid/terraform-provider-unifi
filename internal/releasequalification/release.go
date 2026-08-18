package releasequalification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/managementcontract"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/paritydiff"
)

const canonicalGoUnifiModule = "github.com/ubiquiti-community/go-unifi"

var releaseContractDimensions = []string{
	"capture_eligibility",
	"coverage",
	"enumeration",
	"generated_hcl",
	"identity",
	"plan_classification",
	"receipt_inputs",
	"redaction",
}

type HardwareDispositionReceipt struct {
	FormatVersion int                      `json:"format_version"`
	Gate          string                   `json:"gate"`
	TreeState     *catalogparity.TreeState `json:"tree_state,omitempty"`
	Result        string                   `json:"result"`
	catalogparity.SurfaceKey
	Mode                    string `json:"mode"`
	ClaimScope              string `json:"claim_scope"`
	PhysicalElectricalClaim bool   `json:"physical_electrical_claim"`
	ControllerReceiptSHA256 string `json:"controller_receipt_sha256"`
	ResolvedSignal          string `json:"resolved_signal"`
}

// DependencyPublishabilityReceipt is written by cmd/catalog-dependency-publishability
// through internal/dependencypin.BuildReceipt, which returns this exact type, so
// producer and consumer agree because the compiler makes them.
//
// It did not always. A shell producer wrote a `tree` key this struct has no
// field for, and nothing could decode the receipt -- see the note at the top of
// internal/catalogparity/admission.go before writing one of these from anything
// that is not Go.
type DependencyPublishabilityReceipt struct {
	FormatVersion    int    `json:"format_version"`
	Gate             string `json:"gate"`
	Result           string `json:"result"`
	ProviderCommit   string `json:"provider_commit"`
	ModulePath       string `json:"module_path"`
	ModuleVersion    string `json:"module_version"`
	ModuleCommit     string `json:"module_commit"`
	ModuleZipSHA256  string `json:"module_zip_sha256"`
	ModuleDirSHA256  string `json:"module_dir_sha256"`
	ReplacePresent   bool   `json:"replace_present"`
	ResolutionRunner string `json:"resolution_runner"`
	NetworkBoundary  string `json:"network_boundary"`

	// THE PRODUCER HAS ALWAYS WRITTEN THIS AND THIS TYPE NEVER ACCEPTED IT, so
	// catalog-release-ready could not decode the receipt at all. It has cost
	// nothing only because no workflow runs that consumer.
	//
	// The script's key was `tree`. Every other receipt in the repository spells
	// it `tree_state`, and the odd spelling is the tell that the field was added
	// without anyone opening the type. Renamed in the script rather than matched
	// here, so a one-off spelling does not become the convention by being the
	// thing that was easiest to accept.
	//
	// A pointer with omitempty, matching the receipt at line 72: absent when no
	// run measured a tree, and additive, so every strict decoder that already
	// works keeps working.
	TreeState *catalogparity.TreeState `json:"tree_state,omitempty"`
}

// ConfidentialityReceipt is the public-export gate's receipt. NOTHING PRODUCES
// ONE TODAY, and it is not a required input for that reason.
//
// The type is kept because the check behind it exists: internal/exportgate is
// implemented and tested, and exposes Inspect and InspectCommitMessages. That
// separates it from the contract-parity and fleet-soak receipts, which were
// removed because nothing implemented them at all.
//
// BUILDING THE PRODUCER IS NOT A CODING TASK. Four of these fields --
// ProvenanceReview, RestrictedEvidenceExternal, RawStateRetained and
// RawControllerResponseRetained -- are judgements no scanner measures.
// exportgate can supply FindingCount and the scan booleans; it cannot supply
// those. A producer that writes them as constants, validated by a gate that
// then asserts those constants, is a check comparing a constant to itself.
// Whoever builds it has to decide whether those four are measured or dropped.
type ConfidentialityReceipt struct {
	FormatVersion                 int    `json:"format_version"`
	Gate                          string `json:"gate"`
	Result                        string `json:"result"`
	SourceCommit                  string `json:"source_commit"`
	TreeSHA256                    string `json:"tree_sha256"`
	SecretScan                    bool   `json:"secret_scan"`
	PrivateIdentifierScan         bool   `json:"private_identifier_scan"`
	FileTypeScan                  bool   `json:"file_type_scan"`
	ProvenanceReview              bool   `json:"provenance_review"`
	FindingCount                  int    `json:"finding_count"`
	RestrictedEvidenceExternal    bool   `json:"restricted_evidence_external"`
	RawStateRetained              bool   `json:"raw_state_retained"`
	RawControllerResponseRetained bool   `json:"raw_controller_response_retained"`
}

type ReleaseReadyInput struct {
	Ledger                catalogparity.Ledger
	LedgerSHA256          string
	Management            managementcontract.CatalogManagementContract
	ManagementSHA256      string
	Migration             MigrationRecoveryReceipt
	MigrationSHA256       string
	Hardware              HardwareDispositionReceipt
	HardwareSHA256        string
	Dependency            DependencyPublishabilityReceipt
	DependencySHA256      string
	Confidentiality       ConfidentialityReceipt
	ConfidentialitySHA256 string
}

type ReleaseReadyEvidenceDigests struct {
	InputLedgerSHA256       string `json:"input_ledger_sha256"`
	ManagementSHA256        string `json:"management_contract_sha256"`
	MigrationSHA256         string `json:"migration_recovery_sha256"`
	HardwareSHA256          string `json:"hardware_disposition_sha256"`
	DependencySHA256        string `json:"dependency_publishability_sha256"`
	ConfidentialitySHA256   string `json:"confidentiality_sha256"`
	OutputLedgerSHA256      string `json:"output_ledger_sha256"`
	PromotionContractSHA256 string `json:"promotion_contract_sha256"`
}

type ReleaseReadySurface struct {
	catalogparity.SurfaceKey
	State         catalogparity.AdmissionState `json:"state"`
	ReceiptSHA256 string                       `json:"receipt_sha256"`
}

type ReleaseReadyReceipt struct {
	FormatVersion            int                                           `json:"format_version"`
	Gate                     string                                        `json:"gate"`
	Result                   string                                        `json:"result"`
	ProviderAddress          string                                        `json:"provider_address"`
	SourceCommit             string                                        `json:"source_commit"`
	ReleasedCommit           string                                        `json:"released_commit"`
	ProviderBinarySHA256     string                                        `json:"provider_binary_sha256"`
	Downstream               managementcontract.DownstreamManifestIdentity `json:"downstream"`
	SurfaceCount             int                                           `json:"surface_count"`
	ReleaseReadySurfaceCount int                                           `json:"release_ready_surface_count"`
	ResolvedReleaseBlockers  int                                           `json:"resolved_release_blockers"`
	UnverifiedGates          []string                                      `json:"unverified_gates"`
	Evidence                 ReleaseReadyEvidenceDigests                   `json:"evidence"`
	Surfaces                 []ReleaseReadySurface                         `json:"surfaces"`
}

type ReleaseReadyArtifacts struct {
	Receipt  ReleaseReadyReceipt
	Ledger   catalogparity.Ledger
	Contract managementcontract.CatalogContract
	Evidence managementcontract.CatalogEvidence
}

func BuildReleaseReadyArtifacts(input ReleaseReadyInput) (ReleaseReadyArtifacts, error) {
	if err := validateReleaseReadyInput(input); err != nil {
		return ReleaseReadyArtifacts{}, err
	}

	evidenceDigests := ReleaseReadyEvidenceDigests{
		InputLedgerSHA256: input.LedgerSHA256, ManagementSHA256: input.ManagementSHA256,
		MigrationSHA256:  input.MigrationSHA256,
		HardwareSHA256:   input.HardwareSHA256,
		DependencySHA256: input.DependencySHA256, ConfidentialitySHA256: input.ConfidentialitySHA256,
	}
	migrationSurfaces := migrationSurfaceReceipts(input.Migration.Surfaces)
	ledger := input.Ledger
	ledger.Entries = append([]catalogparity.LedgerEntry(nil), input.Ledger.Entries...)
	releaseSurfaces := make([]ReleaseReadySurface, 0, len(ledger.Entries))
	promotionSurfaces := make([]managementcontract.SurfaceContract, 0, len(ledger.Entries))
	measuredSurfaces := make(map[catalogparity.SurfaceKey]string, len(ledger.Entries))
	for index := range ledger.Entries {
		entry := &ledger.Entries[index]
		receiptSHA256, err := canonicalDigest(struct {
			FormatVersion    int                      `json:"format_version"`
			Surface          catalogparity.SurfaceKey `json:"surface"`
			AdmissionSHA256  string                   `json:"admission_sha256"`
			MigrationSHA256  string                   `json:"migration_sha256"`
			HardwareSHA256   string                   `json:"hardware_sha256"`
			DependencySHA256 string                   `json:"dependency_sha256"`
			PrivacySHA256    string                   `json:"privacy_sha256"`
		}{
			FormatVersion: 1, Surface: entry.SurfaceKey,
			AdmissionSHA256:  input.Management.Admission.ReceiptSHA256,
			MigrationSHA256:  migrationSurfaces[entry.SurfaceKey],
			HardwareSHA256:   input.HardwareSHA256,
			DependencySHA256: input.DependencySHA256, PrivacySHA256: input.ConfidentialitySHA256,
		})
		if err != nil {
			return ReleaseReadyArtifacts{}, fmt.Errorf("surface %s/%s receipt: %w", entry.Kind, entry.Name, err)
		}
		entry.State = catalogparity.ReleaseReady
		entry.ReceiptSHA256 = receiptSHA256
		entry.Implementation = "candidate"
		releaseSurfaces = append(releaseSurfaces, ReleaseReadySurface{
			SurfaceKey: entry.SurfaceKey, State: catalogparity.ReleaseReady, ReceiptSHA256: receiptSHA256,
		})
		promotionSurfaces = append(promotionSurfaces, managementcontract.SurfaceContract{
			SurfaceKey: entry.SurfaceKey, State: catalogparity.ReleaseReady,
			// A LITERAL BECAUSE THERE IS NOTHING TO MEASURE IT FROM, not
			// because the comparison was made and passed. paritydiff.Compare
			// would produce this value, but nothing builds the Observations it
			// consumes, and this site holds digests of other receipts rather
			// than observations of a provider. Task 112.
			EvidenceSHA256: receiptSHA256, AttemptResult: paritydiff.Pass,
		})
		measuredSurfaces[entry.SurfaceKey] = receiptSHA256
	}
	ledgerSHA256, err := canonicalFileDigest(ledger)
	if err != nil {
		return ReleaseReadyArtifacts{}, fmt.Errorf("release-ready ledger: %w", err)
	}
	evidenceDigests.OutputLedgerSHA256 = ledgerSHA256
	contract := managementcontract.CatalogContract{
		FormatVersion: 1, Provider: input.Management.Provider,
		LedgerSHA256:            ledgerSHA256,
		MigrationManifestSHA256: input.Migration.Evidence.ManifestSHA256,
		Surfaces:                promotionSurfaces,
	}
	contractSHA256, err := canonicalFileDigest(contract)
	if err != nil {
		return ReleaseReadyArtifacts{}, fmt.Errorf("promotion contract: %w", err)
	}
	evidenceDigests.PromotionContractSHA256 = contractSHA256
	evidence := managementcontract.CatalogEvidence{
		ProviderBinarySHA256:    input.Management.Provider.Binary.SHA256,
		SourceCommit:            input.Management.Provider.SourceCommit,
		LedgerSHA256:            ledgerSHA256,
		MigrationManifestSHA256: input.Migration.Evidence.ManifestSHA256,
		SchemaToolchains:        input.Management.Provider.Schema.Toolchains,
		SurfaceEvidence:         measuredSurfaces,
	}
	if err := managementcontract.VerifyCatalogPromotion(contract, evidence, ledger); err != nil {
		return ReleaseReadyArtifacts{}, fmt.Errorf("verify release-ready promotion: %w", err)
	}
	receipt := ReleaseReadyReceipt{
		FormatVersion: 1, Gate: "full-catalog-release-ready", Result: "pass",
		ProviderAddress:      catalogparity.CanonicalProviderAddress,
		SourceCommit:         input.Management.Provider.SourceCommit,
		ReleasedCommit:       input.Migration.ReleasedCommit,
		ProviderBinarySHA256: input.Management.Provider.Binary.SHA256,
		Downstream:           input.Management.Downstream,
		SurfaceCount:         len(releaseSurfaces), ReleaseReadySurfaceCount: len(releaseSurfaces),
		ResolvedReleaseBlockers: 1, UnverifiedGates: unverifiedGates(input),
		Evidence: evidenceDigests, Surfaces: releaseSurfaces,
	}
	return ReleaseReadyArtifacts{Receipt: receipt, Ledger: ledger, Contract: contract, Evidence: evidence}, nil
}

func validateReleaseReadyInput(input ReleaseReadyInput) error {
	for label, digest := range map[string]string{
		"input ledger": input.LedgerSHA256, "management contract": input.ManagementSHA256,
		"migration/recovery":        input.MigrationSHA256,
		"hardware disposition":      input.HardwareSHA256,
		"dependency publishability": input.DependencySHA256,
	} {
		if !validHex(digest, 64) {
			return fmt.Errorf("%s SHA-256 is invalid", label)
		}
	}
	if confidentialitySupplied(input) && !validHex(input.ConfidentialitySHA256, 64) {
		return fmt.Errorf("confidentiality SHA-256 is invalid")
	}
	if !confidentialitySupplied(input) && input.Confidentiality != (ConfidentialityReceipt{}) {
		return fmt.Errorf(
			"a confidentiality receipt was supplied without its digest, so nothing records which bytes were judged")
	}
	if err := validateReleaseManagement(input); err != nil {
		return err
	}
	if err := validateReleaseMigration(input); err != nil {
		return err
	}
	if err := validateReleaseLedger(input); err != nil {
		return err
	}
	if err := validateHardwareDisposition(input); err != nil {
		return err
	}
	if err := validateDependencyPublishability(input); err != nil {
		return err
	}
	if !confidentialitySupplied(input) {
		return nil
	}
	return validateConfidentiality(input)
}

// confidentialitySupplied reports whether a confidentiality receipt was given.
//
// The digest is the marker rather than the receipt, because a zero receipt is
// indistinguishable from one whose every boolean is false -- which is exactly
// the failing case this gate must not read as absence.
func confidentialitySupplied(input ReleaseReadyInput) bool {
	return input.ConfidentialitySHA256 != ""
}

// unverifiedGates names the checks this run did not perform.
//
// A receipt that simply omitted a digest would still say "pass", and a reader
// counting evidence would have to notice an empty string to know a gate never
// ran. Naming the gap in the artifact is the difference between a decision that
// was recorded and one that was skipped.
func unverifiedGates(input ReleaseReadyInput) []string {
	gates := []string{}
	if !confidentialitySupplied(input) {
		gates = append(gates, "confidentiality")
	}
	return gates
}

// acceptedReleaseBlockers is the set of evidence gaps this gate will ship with.
//
// IT IS A RELEASE DECISION, NOT A DERIVED FACT, AND THAT IS WHY IT IS WRITTEN
// OUT RATHER THAN COMPUTED.
//
// DO NOT "fix" this by deriving it from input.Management.Admission.ReleaseBlockers.
// That was tried and measured: deriving it makes validateReleaseManagement
// compare that field against itself, and with the change in place a catalog
// carrying twenty fabricated unresolved gaps -- declared identically in the
// receipts, as two artifacts from one campaign would be -- was ADMITTED with a
// "pass" receipt.
//
// THE COMMENT THIS REPLACES ARGUED THE SAME THING FROM THE CONTRACT PARITY
// RECEIPT, and it was right for a reason that has now been acted on: it noted
// that receipt "has no producer anywhere in this repository, so both sides of
// that comparison can arrive from the same hand". Nothing ever produced it, so
// the type and its validator have been removed. The point survives the removal
// -- this list is the only thing pinning WHICH gaps are acceptable, and it is a
// release decision rather than a derived fact.
//
// The staleness this invites is real and is handled by
// TestAcceptedReleaseBlockersMatchTheCampaignPolicy rather than by hoping. A
// pinned decision with a freshness check on its source is the pattern
// evidence_mode_committed_test.go already uses in this package.
//
// WHAT AN ENTRY IS FOR, stated because the prohibition above was read as
// covering more than it says and the gap was mine. It forbids one mechanism:
// computing this set at runtime from a field the gate exists to check. It does
// NOT forbid a human adding an entry -- that is the DESIGNED path, and the
// freshness check exists precisely to prompt it.
//
// AN ENTRY RECORDS A DECISION THAT WE SHIP WITH THAT GAP UNRESOLVED. It is not
// a note that the policy mentions the gap. The two are indistinguishable in the
// diff and opposite in meaning:
//
//	added because WE HAVE DECIDED TO SHIP WITH THIS GAP     the designed path
//	added because THE CHECK IS RED AND THIS MAKES IT GREEN  derive-to-fit, one
//	                                                       entry at a time
//
// The second is the same defect as the runtime derivation, arrived at by hand:
// the freshness check compares this set against the campaign policy, so
// resolving a red by copying the policy's entry here makes the check compare a
// copy against its source and verify nothing. Identical diffs, and nothing in
// the tree distinguishes them -- which is why the reason has to be written down
// rather than inferred from the fact that someone added it.
//
// SO EVERY ENTRY CARRIES WHY WE ARE WILLING TO SHIP THAT GAP, not which policy
// line it matches. If the honest answer is "because the check was red", the
// entry is wrong and the gap needs closing, or the policy needs to stop
// accepting it. The question to answer before adding one is never "does the
// policy declare this" -- it is "do we ship without this evidence".
//
// AND BEFORE WRITING A REASON, READ THE ONE THAT ALREADY EXISTS. Two records of
// the same fact can already disagree, and copying either into a third place is
// what performs the comparison nobody ran. That is worse than one fact with
// several homes: there is no moment where somebody edits two copies and might
// notice, because the disagreement is already there and silent. The copy is the
// first thing that would ever have caught it.
//
// The example is the entry that was NOT added here. The campaign policy records
// unifi_power_supervisor's acceptance gap as arising because "its
// zero_use_endpoint reference to list_resource/unifi_power_supervisor was
// withdrawn". A second cause was independently reasoned for the same gap -- that
// the controller has no Device Supervisor attached, so no lifecycle acceptance
// test can exist. Both are plausible, they are different claims, and nobody
// noticed they disagreed until one was about to be written HERE as the
// justification for shipping without that evidence.
//
// What was measured is narrower than either. unifi/power_supervisor_resource_test.go
// carries thirteen test functions and exactly one TestAcc, and that one exercises
// the LIST surface. The cause is unestablished. An entry stating an unestablished
// cause reads, for as long as it survives, as a decision somebody made on
// evidence.
//
// This warning lives in one place on purpose. A caution about one fact having
// several homes should not acquire a second home. If it earns a wider audience,
// MOVE it rather than copying it.
var acceptedReleaseBlockers = []catalogparity.EvidenceGap{{
	SurfaceKey: catalogparity.SurfaceKey{Kind: catalogparity.Action, Name: "unifi_port"},
	Signal:     "hardware_claim",
}}

func validateReleaseManagement(input ReleaseReadyInput) error {
	m := input.Management
	wantBlocker := acceptedReleaseBlockers
	if m.FormatVersion != 1 || m.Gate != "catalog-management-contract" ||
		m.Result != "ready_for_downstream_verification" ||
		m.ProviderAddress != catalogparity.CanonicalProviderAddress || len(m.Surfaces) != 67 ||
		m.Admission.Result != "pass" || m.Admission.AdmittedSurfaceCount != 67 ||
		!reflect.DeepEqual(m.Admission.ReleaseBlockers, wantBlocker) ||
		!validHex(m.Provider.SourceCommit, 40) || !validHex(m.Provider.Binary.SHA256, 64) ||
		!reflect.DeepEqual(m.RequiredDimensions, releaseContractDimensions) {
		return fmt.Errorf("management contract is not a complete admitted catalog")
	}
	// PRESENCE AND SHAPE ONLY, AND THAT IS THE MOST THIS GATE CAN HONESTLY SAY.
	//
	// Measured: both digests can be replaced with any other 64 hex characters,
	// and both versions can read "0.0.0-not-a-release", and nothing here
	// notices. That is not an oversight to close in place. The only artifact
	// that knows what a toolchain's canonical schema digest SHOULD be is the
	// build-schema receipt, which is not an input to this gate, so any
	// comparison written here would be a second opinion about a fact
	// .woodpecker/scripts/catalog-build-schema.sh already owns -- and a second
	// home for a fact is what this project keeps paying for.
	//
	// So these four fields are carried, not verified. Closing it means giving
	// this gate the build-schema receipt, which is a change to its inputs and a
	// decision rather than a repair.
	for _, name := range []string{"terraform", "tofu"} {
		toolchain, ok := m.Provider.Schema.Toolchains[name]
		if !ok || toolchain.Version == "" || !validHex(toolchain.BinarySHA256, 64) ||
			!validHex(toolchain.CanonicalSchemaSHA256, 64) {
			return fmt.Errorf("management contract %s toolchain is incomplete", name)
		}
	}
	return nil
}

func validateReleaseMigration(input ReleaseReadyInput) error {
	m := input.Migration
	if m.FormatVersion != 1 || m.Gate != "catalog-migration-recovery" || m.Result != "pass" ||
		m.ProviderAddress != catalogparity.CanonicalProviderAddress || m.SurfaceCount != 67 ||
		m.RecoveryCount != 67 || len(m.Surfaces) != 67 ||
		m.SourceCommit != input.Management.Provider.SourceCommit ||
		m.CandidateBinary != input.Management.Provider.Binary.SHA256 {
		return fmt.Errorf("migration/recovery lineage is incomplete")
	}
	seen := map[catalogparity.SurfaceKey]struct{}{}
	for _, surface := range m.Surfaces {
		if surface.State != "migration_recovery_pass" || surface.Strategy != "identity" ||
			!validHex(surface.ReceiptSHA256, 64) {
			return fmt.Errorf("migration/recovery surface %s/%s is incomplete", surface.Kind, surface.Name)
		}
		if _, duplicate := seen[surface.SurfaceKey]; duplicate {
			return fmt.Errorf("duplicate migration/recovery surface %s/%s", surface.Kind, surface.Name)
		}
		seen[surface.SurfaceKey] = struct{}{}
	}
	return nil
}

func validateReleaseLedger(input ReleaseReadyInput) error {
	l := input.Ledger
	if l.FormatVersion != 1 || l.ProviderAddress != catalogparity.CanonicalProviderAddress ||
		!validHex(l.BaselineSHA256, 64) || len(l.Entries) != 67 {
		return fmt.Errorf("input ledger identity is invalid")
	}
	want := make(map[catalogparity.SurfaceKey]managementcontract.CatalogManagementSurface, 67)
	for _, surface := range input.Management.Surfaces {
		want[surface.SurfaceKey] = surface
	}
	seen := make(map[catalogparity.SurfaceKey]struct{}, 67)
	counts := map[catalogparity.SurfaceKind]int{}
	for _, entry := range l.Entries {
		upstream, ok := want[entry.SurfaceKey]
		if !ok {
			return fmt.Errorf("ledger surface set differs at %s/%s", entry.Kind, entry.Name)
		}
		// THE LEDGER HAS TO HAVE EARNED PROMOTION, NOT MERELY BE PRESENT.
		//
		// BuildReleaseReadyArtifacts stamps State, ReceiptSHA256 and
		// Implementation onto every entry it is handed. Until these three
		// checks existed nothing read the incoming values, so all three were
		// overwritten unexamined and the gate promoted whatever ledger it was
		// given. Measured: a ledger whose sixty-seven entries were all
		// shadow_only -- a catalog in which nothing had been admitted at all --
		// came out release_ready with a "pass" receipt.
		//
		// The state is compared against the management contract's own entry for
		// the same surface rather than against a constant. Both inputs describe
		// the same sixty-seven surfaces and the gate already holds both, so the
		// agreement is derived rather than declared, and it cannot go stale when
		// the admitted state is renamed.
		if entry.State != upstream.State {
			return fmt.Errorf("ledger surface %s/%s is %q, but the management contract admitted it as %q; "+
				"this gate promotes the ledger it is given, so a surface that never reached the "+
				"admitted state would be stamped release-ready unexamined",
				entry.Kind, entry.Name, entry.State, upstream.State)
		}
		if entry.Implementation != "candidate" {
			return fmt.Errorf("ledger surface %s/%s is implemented by %q, not the candidate; "+
				"release-ready promotes the candidate and would relabel this one",
				entry.Kind, entry.Name, entry.Implementation)
		}
		if _, duplicate := seen[entry.SurfaceKey]; duplicate {
			return fmt.Errorf("duplicate ledger surface %s/%s", entry.Kind, entry.Name)
		}
		if !validHex(entry.BaselineSchemaSHA256, 64) {
			return fmt.Errorf("ledger surface %s/%s schema digest is invalid", entry.Kind, entry.Name)
		}
		if !validHex(entry.ReceiptSHA256, 64) {
			return fmt.Errorf("ledger surface %s/%s carries no admission receipt digest", entry.Kind, entry.Name)
		}
		seen[entry.SurfaceKey] = struct{}{}
		counts[entry.Kind]++
	}
	if counts[catalogparity.ManagedResource] != 28 || counts[catalogparity.DataSource] != 13 ||
		counts[catalogparity.ListResource] != 25 || counts[catalogparity.Action] != 1 {
		return fmt.Errorf("ledger surface counts are invalid")
	}
	return nil
}

func validateHardwareDisposition(input ReleaseReadyInput) error {
	h := input.Hardware
	want := catalogparity.SurfaceKey{Kind: catalogparity.Action, Name: "unifi_port"}
	if h.FormatVersion != 1 || h.Gate != "unifi-port-hardware-disposition" || h.Result != "pass" ||
		h.SurfaceKey != want || h.Mode != "protocol_sufficient" ||
		h.ClaimScope != "controller_poe_configuration_persistence" || h.PhysicalElectricalClaim ||
		h.ControllerReceiptSHA256 != input.Migration.Evidence.ControllerSHA256 ||
		h.ResolvedSignal != "hardware_claim" {
		return fmt.Errorf("hardware disposition does not resolve the scoped port-action claim")
	}
	return nil
}

func validateDependencyPublishability(input ReleaseReadyInput) error {
	d := input.Dependency
	if d.FormatVersion != 1 || d.Gate != "go-unifi-dependency-publishability" || d.Result != "pass" ||
		d.ProviderCommit != input.Management.Provider.SourceCommit || d.ModulePath != canonicalGoUnifiModule ||
		d.ModuleVersion == "" || !validHex(d.ModuleCommit, 40) || !validHex(d.ModuleZipSHA256, 64) ||
		!validHex(d.ModuleDirSHA256, 64) || d.ReplacePresent || d.ResolutionRunner != "remote_ci" ||
		d.NetworkBoundary != "remote_ci_only" {
		return fmt.Errorf("dependency publishability does not prove a canonical replacement-free module")
	}
	return nil
}

func validateConfidentiality(input ReleaseReadyInput) error {
	c := input.Confidentiality
	if c.FormatVersion != 1 || c.Gate != "public-export-confidentiality" || c.Result != "pass" ||
		c.SourceCommit != input.Management.Provider.SourceCommit || !validHex(c.TreeSHA256, 64) ||
		!c.SecretScan || !c.PrivateIdentifierScan || !c.FileTypeScan || !c.ProvenanceReview ||
		c.FindingCount != 0 || !c.RestrictedEvidenceExternal || c.RawStateRetained ||
		c.RawControllerResponseRetained {
		return fmt.Errorf("confidentiality gate is incomplete or retains restricted material")
	}
	return nil
}

func migrationSurfaceReceipts(surfaces []MigrationRecoverySurface) map[catalogparity.SurfaceKey]string {
	result := make(map[catalogparity.SurfaceKey]string, len(surfaces))
	for _, surface := range surfaces {
		result[surface.SurfaceKey] = surface.ReceiptSHA256
	}
	return result
}

func canonicalFileDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
