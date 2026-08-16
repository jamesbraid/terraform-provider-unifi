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

type ContractParitySurface struct {
	catalogparity.SurfaceKey
	State         catalogparity.AdmissionState   `json:"state"`
	CaptureMode   managementcontract.CaptureMode `json:"capture_mode"`
	ReceiptSHA256 string                         `json:"receipt_sha256"`
}

type ContractParityReceipt struct {
	FormatVersion        int                                           `json:"format_version"`
	Gate                 string                                        `json:"gate"`
	Result               string                                        `json:"result"`
	ContractSHA256       string                                        `json:"contract_sha256"`
	ProviderBinarySHA256 string                                        `json:"provider_binary_sha256"`
	Downstream           managementcontract.DownstreamManifestIdentity `json:"downstream"`
	SurfaceCount         int                                           `json:"surface_count"`
	CaptureCounts        map[string]int                                `json:"capture_counts"`
	Dimensions           map[string]bool                               `json:"dimensions"`
	ReleaseBlockers      []catalogparity.EvidenceGap                   `json:"release_blockers"`
	Surfaces             []ContractParitySurface                       `json:"surfaces"`
}

type FleetSoakReceipt struct {
	FormatVersion          int    `json:"format_version"`
	Gate                   string `json:"gate"`
	Result                 string `json:"result"`
	ProviderAddress        string `json:"provider_address"`
	SourceCommit           string `json:"source_commit"`
	ProviderBinarySHA256   string `json:"provider_binary_sha256"`
	DownstreamCommit       string `json:"downstream_commit"`
	Platform               string `json:"platform"`
	Mode                   string `json:"mode"`
	ConsecutivePasses      int    `json:"consecutive_passes"`
	ObservedResourceCount  int    `json:"observed_resource_count"`
	UnexplainedDriftCount  int    `json:"unexplained_drift_count"`
	DestructiveChangeCount int    `json:"destructive_change_count"`
	RefreshOnly            bool   `json:"refresh_only"`
	SecretsRedacted        bool   `json:"secrets_redacted"`
	StateSnapshotSHA256    string `json:"state_snapshot_sha256"`
	NormalizedPlanSHA256   string `json:"normalized_plan_sha256"`
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
}

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
	ContractParity        ContractParityReceipt
	ContractParitySHA256  string
	FleetSoak             FleetSoakReceipt
	FleetSoakSHA256       string
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
	ContractParitySHA256    string `json:"contract_parity_sha256"`
	FleetSoakSHA256         string `json:"fleet_soak_sha256"`
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
		MigrationSHA256: input.MigrationSHA256, ContractParitySHA256: input.ContractParitySHA256,
		FleetSoakSHA256: input.FleetSoakSHA256, HardwareSHA256: input.HardwareSHA256,
		DependencySHA256: input.DependencySHA256, ConfidentialitySHA256: input.ConfidentialitySHA256,
	}
	migrationSurfaces := migrationSurfaceReceipts(input.Migration.Surfaces)
	contractSurfaces := contractParitySurfaceReceipts(input.ContractParity.Surfaces)
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
			ContractSHA256   string                   `json:"contract_sha256"`
			FleetSHA256      string                   `json:"fleet_sha256"`
			HardwareSHA256   string                   `json:"hardware_sha256"`
			DependencySHA256 string                   `json:"dependency_sha256"`
			PrivacySHA256    string                   `json:"privacy_sha256"`
		}{
			FormatVersion: 1, Surface: entry.SurfaceKey,
			AdmissionSHA256: input.Management.Admission.ReceiptSHA256,
			MigrationSHA256: migrationSurfaces[entry.SurfaceKey],
			ContractSHA256:  contractSurfaces[entry.SurfaceKey],
			FleetSHA256:     input.FleetSoakSHA256, HardwareSHA256: input.HardwareSHA256,
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
		ResolvedReleaseBlockers: 1, Evidence: evidenceDigests, Surfaces: releaseSurfaces,
	}
	return ReleaseReadyArtifacts{Receipt: receipt, Ledger: ledger, Contract: contract, Evidence: evidence}, nil
}

func validateReleaseReadyInput(input ReleaseReadyInput) error {
	for label, digest := range map[string]string{
		"input ledger": input.LedgerSHA256, "management contract": input.ManagementSHA256,
		"migration/recovery": input.MigrationSHA256, "contract parity": input.ContractParitySHA256,
		"fleet soak": input.FleetSoakSHA256, "hardware disposition": input.HardwareSHA256,
		"dependency publishability": input.DependencySHA256, "confidentiality": input.ConfidentialitySHA256,
	} {
		if !validHex(digest, 64) {
			return fmt.Errorf("%s SHA-256 is invalid", label)
		}
	}
	if err := validateReleaseManagement(input); err != nil {
		return err
	}
	if err := validateReleaseMigration(input); err != nil {
		return err
	}
	if err := validateReleaseContractParity(input); err != nil {
		return err
	}
	if err := validateReleaseLedger(input); err != nil {
		return err
	}
	if err := validateFleetSoak(input); err != nil {
		return err
	}
	if err := validateHardwareDisposition(input); err != nil {
		return err
	}
	if err := validateDependencyPublishability(input); err != nil {
		return err
	}
	return validateConfidentiality(input)
}

func validateReleaseManagement(input ReleaseReadyInput) error {
	m := input.Management
	wantBlocker := []catalogparity.EvidenceGap{{
		SurfaceKey: catalogparity.SurfaceKey{Kind: catalogparity.Action, Name: "unifi_port"},
		Signal:     "hardware_claim",
	}}
	if m.FormatVersion != 1 || m.Gate != "catalog-management-contract" ||
		m.Result != "ready_for_downstream_verification" ||
		m.ProviderAddress != catalogparity.CanonicalProviderAddress || len(m.Surfaces) != 67 ||
		m.Admission.Result != "pass" || m.Admission.AdmittedSurfaceCount != 67 ||
		!reflect.DeepEqual(m.Admission.ReleaseBlockers, wantBlocker) ||
		!validHex(m.Provider.SourceCommit, 40) || !validHex(m.Provider.Binary.SHA256, 64) ||
		!reflect.DeepEqual(m.RequiredDimensions, releaseContractDimensions) {
		return fmt.Errorf("management contract is not a complete admitted catalog")
	}
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

func validateReleaseContractParity(input ReleaseReadyInput) error {
	c := input.ContractParity
	wantBlocker := input.Management.Admission.ReleaseBlockers
	if c.FormatVersion != 1 || c.Gate != "ubitofu-catalog-contract-parity" || c.Result != "pass" ||
		c.ContractSHA256 != input.ManagementSHA256 || c.ProviderBinarySHA256 != input.Management.Provider.Binary.SHA256 ||
		c.Downstream != input.Management.Downstream || c.SurfaceCount != 67 || len(c.Surfaces) != 67 ||
		!reflect.DeepEqual(c.CaptureCounts, map[string]int{"managed": 28, "not_applicable": 39}) ||
		!reflect.DeepEqual(c.ReleaseBlockers, wantBlocker) {
		return fmt.Errorf("contract parity release blocker or lineage is invalid")
	}
	for _, dimension := range releaseContractDimensions {
		if len(c.Dimensions) != len(releaseContractDimensions) || !c.Dimensions[dimension] {
			return fmt.Errorf("contract parity dimensions are incomplete")
		}
	}
	management := make(map[catalogparity.SurfaceKey]managementcontract.CatalogManagementSurface, 67)
	for _, surface := range input.Management.Surfaces {
		management[surface.SurfaceKey] = surface
	}
	seen := map[catalogparity.SurfaceKey]struct{}{}
	for _, surface := range c.Surfaces {
		upstream, ok := management[surface.SurfaceKey]
		if !ok {
			return fmt.Errorf("contract parity surface set differs at %s/%s", surface.Kind, surface.Name)
		}
		if surface.State != catalogparity.ContractParity || surface.CaptureMode != upstream.CaptureMode ||
			!validHex(surface.ReceiptSHA256, 64) {
			return fmt.Errorf("contract parity surface %s/%s is incomplete", surface.Kind, surface.Name)
		}
		if _, duplicate := seen[surface.SurfaceKey]; duplicate {
			return fmt.Errorf("duplicate contract parity surface %s/%s", surface.Kind, surface.Name)
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
	want := make(map[catalogparity.SurfaceKey]struct{}, 67)
	for _, surface := range input.Management.Surfaces {
		want[surface.SurfaceKey] = struct{}{}
	}
	seen := make(map[catalogparity.SurfaceKey]struct{}, 67)
	counts := map[catalogparity.SurfaceKind]int{}
	for _, entry := range l.Entries {
		if _, ok := want[entry.SurfaceKey]; !ok {
			return fmt.Errorf("ledger surface set differs at %s/%s", entry.Kind, entry.Name)
		}
		if _, duplicate := seen[entry.SurfaceKey]; duplicate {
			return fmt.Errorf("duplicate ledger surface %s/%s", entry.Kind, entry.Name)
		}
		if !validHex(entry.BaselineSchemaSHA256, 64) {
			return fmt.Errorf("ledger surface %s/%s schema digest is invalid", entry.Kind, entry.Name)
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

func validateFleetSoak(input ReleaseReadyInput) error {
	f := input.FleetSoak
	if f.FormatVersion != 1 || f.Gate != "unifi-private-fleet-soak" || f.Result != "pass" ||
		f.ProviderAddress != catalogparity.CanonicalProviderAddress ||
		f.SourceCommit != input.Management.Provider.SourceCommit ||
		f.ProviderBinarySHA256 != input.Management.Provider.Binary.SHA256 ||
		f.DownstreamCommit != input.Management.Downstream.Commit || f.Platform != "linux/amd64" ||
		f.Mode != "plan_only" || f.ConsecutivePasses < 2 || f.ObservedResourceCount < 1 ||
		f.UnexplainedDriftCount != 0 || f.DestructiveChangeCount != 0 || !f.RefreshOnly ||
		!f.SecretsRedacted || !validHex(f.StateSnapshotSHA256, 64) || !validHex(f.NormalizedPlanSHA256, 64) {
		return fmt.Errorf("fleet soak is not a safe drift-free pass")
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

func contractParitySurfaceReceipts(surfaces []ContractParitySurface) map[catalogparity.SurfaceKey]string {
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
