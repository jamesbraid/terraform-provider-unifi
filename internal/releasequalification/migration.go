package releasequalification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

type ToolArtifact struct {
	Version       string `json:"version"`
	ArchiveSHA256 string `json:"archive_sha256,omitempty"`
	BinarySHA256  string `json:"binary_sha256"`
}

type StateUpgradeArtifact struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	BinarySHA256 string `json:"binary_sha256"`
}

type DNSTargetReceipt struct {
	Product                string `json:"product"`
	Version                string `json:"version"`
	IndexSHA256            string `json:"index_sha256"`
	PlatformManifestSHA256 string `json:"platform_manifest_sha256"`
	ConfigSHA256           string `json:"config_sha256"`
}

type DNSLifecycleChecks struct {
	FreshTargetPerCLIAndAdapter        bool `json:"fresh_target_per_cli_and_adapter"`
	Create                             bool `json:"create"`
	Update                             bool `json:"update"`
	OmittedOptionalFields              bool `json:"omitted_optional_fields"`
	ConfiguredOptionalFields           bool `json:"configured_optional_fields"`
	ReplacementPlan                    bool `json:"replacement_plan"`
	RestartRefresh                     bool `json:"restart_refresh"`
	Import                             bool `json:"import"`
	V0IntegerTTLStateUpgrade           bool `json:"v0_integer_ttl_state_upgrade"`
	NoOpPlan                           bool `json:"no_op_plan"`
	Delete                             bool `json:"delete"`
	Cleanup                            bool `json:"cleanup"`
	BidirectionalAdapterStateRoundTrip bool `json:"bidirectional_adapter_state_round_trip"`
}

type DNSLifecycleReceipt struct {
	FormatVersion             int                  `json:"format_version"`
	Gate                      string               `json:"gate"`
	Result                    string               `json:"result"`
	SourceCommit              string               `json:"source_commit"`
	Platform                  string               `json:"platform"`
	ProviderVersion           string               `json:"provider_version"`
	ProviderBinarySHA256      string               `json:"provider_binary_sha256"`
	LegacyProvider            ToolArtifact         `json:"legacy_provider"`
	StateUpgradeSource        StateUpgradeArtifact `json:"state_upgrade_source"`
	Terraform                 ToolArtifact         `json:"terraform"`
	Tofu                      ToolArtifact         `json:"tofu"`
	Target                    DNSTargetReceipt     `json:"target"`
	Lifecycle                 DNSLifecycleChecks   `json:"lifecycle"`
	NormalizedStateSHA256     string               `json:"normalized_state_sha256"`
	CLIOutcomesEquivalent     bool                 `json:"cli_outcomes_equivalent"`
	AdapterOutcomesEquivalent bool                 `json:"adapter_outcomes_equivalent"`
}

type MigrationRecoveryInput struct {
	Admission          catalogparity.AdmissionReceipt
	AdmissionSHA256    string
	BuildSchema        catalogparity.BuildSchemaReceipt
	BuildSchemaSHA256  string
	Controller         catalogparity.ControllerDifferentialReceipt
	ControllerSHA256   string
	Inventory          catalogparity.EvidenceInventory
	InventorySHA256    string
	Manifest           catalogparity.MigrationManifest
	ManifestSHA256     string
	DNSLifecycle       DNSLifecycleReceipt
	DNSLifecycleSHA256 string
}

type MigrationEvidenceDigests struct {
	AdmissionSHA256    string `json:"admission_sha256"`
	BuildSchemaSHA256  string `json:"build_schema_sha256"`
	ControllerSHA256   string `json:"controller_sha256"`
	InventorySHA256    string `json:"inventory_sha256"`
	ManifestSHA256     string `json:"migration_manifest_sha256"`
	DNSLifecycleSHA256 string `json:"dns_lifecycle_sha256"`
}

type MigrationRecoverySurface struct {
	catalogparity.SurfaceKey
	State         string `json:"state"`
	Strategy      string `json:"strategy"`
	EvidenceMode  string `json:"evidence_mode"`
	RecoveryMode  string `json:"recovery_mode"`
	ReceiptSHA256 string `json:"receipt_sha256"`
}

type MigrationRecoveryReceipt struct {
	FormatVersion   int                        `json:"format_version"`
	Gate            string                     `json:"gate"`
	Result          string                     `json:"result"`
	ProviderAddress string                     `json:"provider_address"`
	SourceCommit    string                     `json:"source_commit"`
	ReleasedCommit  string                     `json:"released_commit"`
	CandidateBinary string                     `json:"candidate_binary_sha256"`
	SurfaceCount    int                        `json:"surface_count"`
	RecoveryCount   int                        `json:"recovery_count"`
	StrategyCounts  map[string]int             `json:"strategy_counts"`
	EvidenceModes   map[string]int             `json:"evidence_modes"`
	Evidence        MigrationEvidenceDigests   `json:"evidence"`
	Surfaces        []MigrationRecoverySurface `json:"surfaces"`
}

func BuildMigrationRecoveryReceipt(input MigrationRecoveryInput) (MigrationRecoveryReceipt, error) {
	if err := validateMigrationInputs(input); err != nil {
		return MigrationRecoveryReceipt{}, err
	}
	evidence := MigrationEvidenceDigests{
		AdmissionSHA256: input.AdmissionSHA256, BuildSchemaSHA256: input.BuildSchemaSHA256,
		ControllerSHA256: input.ControllerSHA256, InventorySHA256: input.InventorySHA256,
		ManifestSHA256: input.ManifestSHA256, DNSLifecycleSHA256: input.DNSLifecycleSHA256,
	}
	inventory := make(map[catalogparity.SurfaceKey]catalogparity.SurfaceEvidenceInventory, len(input.Inventory.Surfaces))
	for _, surface := range input.Inventory.Surfaces {
		inventory[surface.SurfaceKey] = surface
	}
	surfaces := make([]MigrationRecoverySurface, 0, len(input.Manifest.Entries))
	modes := map[string]int{}
	for _, entry := range input.Manifest.Entries {
		mode := "source_identity"
		if entry.Kind == catalogparity.ManagedResource && entry.Name == "unifi_dns_record" {
			mode = "dns_bidirectional_state"
		} else if entry.Kind == catalogparity.ListResource && entry.Name == "unifi_dns_record" {
			mode = "dns_list_controller"
		}
		modes[mode]++
		digest, err := canonicalDigest(struct {
			FormatVersion int                          `json:"format_version"`
			State         string                       `json:"state"`
			Migration     catalogparity.MigrationEntry `json:"migration"`
			Runtime       catalogparity.FileComparison `json:"runtime"`
			EvidenceMode  string                       `json:"evidence_mode"`
			Evidence      MigrationEvidenceDigests     `json:"evidence"`
		}{1, "migration_recovery_pass", entry, inventory[entry.SurfaceKey].Runtime, mode, evidence})
		if err != nil {
			return MigrationRecoveryReceipt{}, fmt.Errorf("surface %s/%s receipt: %w", entry.Kind, entry.Name, err)
		}
		surfaces = append(surfaces, MigrationRecoverySurface{
			SurfaceKey: entry.SurfaceKey, State: "migration_recovery_pass",
			Strategy: string(entry.Strategy), EvidenceMode: mode,
			RecoveryMode: entry.Recovery.Mode, ReceiptSHA256: digest,
		})
	}
	return MigrationRecoveryReceipt{
		FormatVersion: 1, Gate: "catalog-migration-recovery", Result: "pass",
		ProviderAddress: catalogparity.CanonicalProviderAddress,
		SourceCommit:    input.Admission.SourceCommit, ReleasedCommit: input.Admission.ReleasedCommit,
		CandidateBinary: input.Admission.CandidateBinarySHA256,
		SurfaceCount:    len(surfaces), RecoveryCount: len(surfaces),
		StrategyCounts: map[string]int{"identity": len(surfaces)}, EvidenceModes: modes,
		Evidence: evidence, Surfaces: surfaces,
	}, nil
}

func validateMigrationInputs(input MigrationRecoveryInput) error {
	for label, digest := range map[string]string{
		"admission": input.AdmissionSHA256, "build/schema": input.BuildSchemaSHA256,
		"controller": input.ControllerSHA256, "inventory": input.InventorySHA256,
		"migration manifest": input.ManifestSHA256, "DNS lifecycle": input.DNSLifecycleSHA256,
	} {
		if !validHex(digest, 64) {
			return fmt.Errorf("%s SHA-256 is invalid", label)
		}
	}
	if err := validateMigrationAdmission(input); err != nil {
		return err
	}
	if err := validateMigrationBuild(input); err != nil {
		return err
	}
	if err := validateMigrationController(input); err != nil {
		return err
	}
	if err := validateMigrationInventory(input); err != nil {
		return err
	}
	if err := validateMigrationManifest(input); err != nil {
		return err
	}
	return validateDNSLifecycle(input)
}

func validateMigrationAdmission(input MigrationRecoveryInput) error {
	a := input.Admission
	if a.FormatVersion != 1 || a.Gate != "catalog-admission" || a.Result != "pass" ||
		a.ProviderAddress != catalogparity.CanonicalProviderAddress || a.AdmittedSurfaceCount != 67 ||
		len(a.Surfaces) != 67 {
		return fmt.Errorf("catalog admission is not a complete pass")
	}
	want := catalogparity.EvidenceGap{
		SurfaceKey: catalogparity.SurfaceKey{Kind: catalogparity.Action, Name: "unifi_port"},
		Signal:     "hardware_claim",
	}
	if a.ReleaseBlockerCount != 1 || len(a.ReleaseBlockers) != 1 || a.ReleaseBlockers[0] != want {
		return fmt.Errorf("catalog admission release blocker is invalid")
	}
	if a.Evidence.InventorySHA256 != input.InventorySHA256 ||
		a.Evidence.BuildSchemaSHA256 != input.BuildSchemaSHA256 ||
		a.Evidence.ControllerSHA256 != input.ControllerSHA256 {
		return fmt.Errorf("catalog admission evidence digests do not match migration inputs")
	}
	seen := map[catalogparity.SurfaceKey]struct{}{}
	for _, surface := range a.Surfaces {
		if surface.State != catalogparity.Admitted || surface.AdapterState != catalogparity.AdapterParity ||
			!validHex(surface.ReceiptSHA256, 64) || !validHex(surface.AdapterParitySHA256, 64) {
			return fmt.Errorf("surface %s/%s is not admitted", surface.Kind, surface.Name)
		}
		if _, duplicate := seen[surface.SurfaceKey]; duplicate {
			return fmt.Errorf("duplicate admitted surface %s/%s", surface.Kind, surface.Name)
		}
		seen[surface.SurfaceKey] = struct{}{}
	}
	return nil
}

func validateMigrationBuild(input MigrationRecoveryInput) error {
	b := input.BuildSchema
	if b.FormatVersion != 1 || b.Gate != "catalog-build-schema" || b.Result != "pass" ||
		len(b.PromotionBlockers) != 0 || b.Platform != "linux/amd64" || b.GoVersion != "go1.25.8" ||
		b.BuildNetwork != "none" || b.CleanBuilds.Released != 2 || b.CleanBuilds.Candidate != 2 {
		return fmt.Errorf("build/schema receipt is not promotable")
	}
	if b.SourceCommit != input.Admission.SourceCommit || b.ReleasedCommit != input.Admission.ReleasedCommit ||
		b.ProviderBinaries.CandidateSHA256 != input.Admission.CandidateBinarySHA256 ||
		b.InventorySHA256 != input.InventorySHA256 {
		return fmt.Errorf("build/schema lineage differs from admission")
	}
	if !b.SchemaEvidence.ReleaseToCandidateWithinCLI || !b.SchemaEvidence.SharedCLIProjectionEqual ||
		b.SchemaEvidence.FullCLIProjectionEqual || !reflect.DeepEqual(
		b.SchemaEvidence.TerraformOnlyCategories, []string{"action_schemas", "list_resource_schemas"}) {
		return fmt.Errorf("build/schema projection is not release-identical")
	}
	for name, cli := range map[string]catalogparity.SchemaCLIReceipt{
		"terraform": b.SchemaEvidence.Terraform, "tofu": b.SchemaEvidence.Tofu,
	} {
		if cli.Version == "" || !validHex(cli.BinarySHA256, 64) || !validHex(cli.CanonicalSHA256, 64) {
			return fmt.Errorf("%s schema evidence is incomplete", name)
		}
	}
	return nil
}

func validateMigrationController(input MigrationRecoveryInput) error {
	c := input.Controller
	if c.FormatVersion != 1 || c.Gate != "catalog controller differential" ||
		c.Result != "blocked_evidence" || c.CandidateCommit != input.Admission.SourceCommit ||
		c.ReleasedCommit != input.Admission.ReleasedCommit || c.Plan.SurfaceCount != 67 ||
		len(c.Plan.Surfaces) != 67 || len(c.Plan.TestNames) == 0 {
		return fmt.Errorf("controller differential identity is invalid")
	}
	admitted := admittedSurfaceSet(input.Admission)
	planned := make(map[catalogparity.SurfaceKey]struct{}, len(c.Plan.Surfaces))
	for _, surface := range c.Plan.Surfaces {
		if _, ok := admitted[surface.SurfaceKey]; !ok ||
			(len(surface.TestNames) == 0 && len(surface.MissingSignals) == 0) {
			return fmt.Errorf("controller surface set differs from admission at %s/%s", surface.Kind, surface.Name)
		}
		if _, duplicate := planned[surface.SurfaceKey]; duplicate {
			return fmt.Errorf("controller surface set contains duplicate %s/%s", surface.Kind, surface.Name)
		}
		planned[surface.SurfaceKey] = struct{}{}
	}
	if !controllerSuiteComplete(c.Released, c.Plan, c.Plan.ReleasedAllowedFailures, c.Plan.ReleasedAllowedMissing) {
		return fmt.Errorf("controller differential released suite is incomplete")
	}
	if !controllerSuiteComplete(c.Candidate, c.Plan, nil, nil) {
		return fmt.Errorf("controller differential candidate suite is incomplete")
	}
	return nil
}

func controllerSuiteComplete(
	suite catalogparity.ControllerSuiteReceipt,
	plan catalogparity.ControllerPlanReceipt,
	allowedFailures, allowedMissing []string,
) bool {
	if len(suite.UnexpectedFailures) != 0 ||
		len(plan.AllowedSkips) > len(plan.TestNames) ||
		len(allowedFailures) > len(plan.TestNames) ||
		len(allowedMissing) > len(plan.TestNames) {
		return false
	}
	seenAllowed := make(map[string]struct{}, len(plan.AllowedSkips))
	for _, testName := range plan.AllowedSkips {
		if !containsString(plan.TestNames, testName) {
			return false
		}
		if _, duplicate := seenAllowed[testName]; duplicate {
			return false
		}
		seenAllowed[testName] = struct{}{}
	}
	seenFailures := make(map[string]struct{}, len(allowedFailures))
	for _, testName := range allowedFailures {
		if !containsString(plan.TestNames, testName) || containsString(plan.AllowedSkips, testName) {
			return false
		}
		if _, duplicate := seenFailures[testName]; duplicate {
			return false
		}
		seenFailures[testName] = struct{}{}
	}
	seenMissing := make(map[string]struct{}, len(allowedMissing))
	for _, testName := range allowedMissing {
		if !containsString(plan.TestNames, testName) ||
			containsString(plan.AllowedSkips, testName) ||
			containsString(allowedFailures, testName) {
			return false
		}
		if _, duplicate := seenMissing[testName]; duplicate {
			return false
		}
		seenMissing[testName] = struct{}{}
	}
	if suite.Result == "accepted_limitation" {
		wantPassed := make([]string, 0, len(plan.TestNames)-len(plan.AllowedSkips)-len(suite.Failed)-len(allowedMissing))
		for _, testName := range plan.TestNames {
			if !containsString(plan.AllowedSkips, testName) &&
				!containsString(suite.Failed, testName) &&
				!containsString(allowedMissing, testName) {
				wantPassed = append(wantPassed, testName)
			}
		}
		exitMatches := (len(suite.Failed) > 0 && suite.ExitCode != 0) ||
			(len(suite.Failed) == 0 && suite.ExitCode == 0)
		return len(allowedFailures)+len(allowedMissing) != 0 && exitMatches &&
			allControllerStringsAllowed(suite.Failed, allowedFailures) &&
			sameControllerStrings(suite.AcceptedFailures, suite.Failed) &&
			sameControllerStrings(suite.Missing, allowedMissing) &&
			sameControllerStrings(suite.Skipped, plan.AllowedSkips) &&
			sameControllerStrings(suite.Passed, wantPassed)
	}
	wantPassed := make([]string, 0, len(plan.TestNames)-len(plan.AllowedSkips))
	for _, testName := range plan.TestNames {
		if !containsString(plan.AllowedSkips, testName) {
			wantPassed = append(wantPassed, testName)
		}
	}
	if suite.Result != "pass" || suite.ExitCode != 0 ||
		len(suite.Failed) != 0 || len(suite.AcceptedFailures) != 0 ||
		len(suite.Missing) != 0 {
		return false
	}
	return sameControllerStrings(suite.Passed, wantPassed) &&
		sameControllerStrings(suite.Skipped, plan.AllowedSkips)
}

func allControllerStringsAllowed(values, allowed []string) bool {
	if len(values) > len(allowed) {
		return false
	}
	for _, value := range values {
		if !containsString(allowed, value) {
			return false
		}
	}
	return true
}

func sameControllerStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	return reflect.DeepEqual(left, right)
}

func validateMigrationInventory(input MigrationRecoveryInput) error {
	i := input.Inventory
	if i.FormatVersion != 1 || i.ProviderAddress != catalogparity.CanonicalProviderAddress ||
		len(i.Surfaces) != 67 || i.ReleasedProvider.Commit != input.Admission.ReleasedCommit {
		return fmt.Errorf("catalog inventory identity is invalid")
	}
	changed := []catalogparity.SurfaceKey{}
	seen := map[catalogparity.SurfaceKey]struct{}{}
	admitted := admittedSurfaceSet(input.Admission)
	for _, surface := range i.Surfaces {
		if _, ok := admitted[surface.SurfaceKey]; !ok {
			return fmt.Errorf("inventory surface set differs from admission at %s/%s", surface.Kind, surface.Name)
		}
		if _, duplicate := seen[surface.SurfaceKey]; duplicate {
			return fmt.Errorf("duplicate inventory surface %s/%s", surface.Kind, surface.Name)
		}
		seen[surface.SurfaceKey] = struct{}{}
		if surface.Runtime.Status == catalogparity.FileChanged {
			changed = append(changed, surface.SurfaceKey)
		} else if surface.Runtime.Status != catalogparity.FileIdentical {
			return fmt.Errorf("surface %s/%s runtime status is invalid", surface.Kind, surface.Name)
		}
	}
	sort.Slice(changed, func(a, b int) bool {
		if changed[a].Kind != changed[b].Kind {
			return changed[a].Kind < changed[b].Kind
		}
		return changed[a].Name < changed[b].Name
	})
	want := []catalogparity.SurfaceKey{
		{Kind: catalogparity.ListResource, Name: "unifi_device"},
		{Kind: catalogparity.ListResource, Name: "unifi_dns_record"},
		{Kind: catalogparity.ManagedResource, Name: "unifi_device"},
		{Kind: catalogparity.ManagedResource, Name: "unifi_dns_record"},
	}
	if !reflect.DeepEqual(changed, want) {
		return fmt.Errorf("catalog runtime change set is %v, want %v", changed, want)
	}
	return nil
}

func admittedSurfaceSet(admission catalogparity.AdmissionReceipt) map[catalogparity.SurfaceKey]struct{} {
	result := make(map[catalogparity.SurfaceKey]struct{}, len(admission.Surfaces))
	for _, surface := range admission.Surfaces {
		result[surface.SurfaceKey] = struct{}{}
	}
	return result
}

func validateMigrationManifest(input MigrationRecoveryInput) error {
	m := input.Manifest
	if m.FormatVersion != 1 || m.FromVersion != "0.101.2" || m.ToVersion == "" ||
		m.ProviderAddress != catalogparity.CanonicalProviderAddress || len(m.Entries) != 67 {
		return fmt.Errorf("migration manifest identity is invalid")
	}
	admitted := map[catalogparity.SurfaceKey]struct{}{}
	for _, surface := range input.Admission.Surfaces {
		admitted[surface.SurfaceKey] = struct{}{}
	}
	seen := map[catalogparity.SurfaceKey]struct{}{}
	for _, entry := range m.Entries {
		if _, ok := admitted[entry.SurfaceKey]; !ok {
			return fmt.Errorf("migration manifest references unknown surface %s/%s", entry.Kind, entry.Name)
		}
		if _, duplicate := seen[entry.SurfaceKey]; duplicate {
			return fmt.Errorf("duplicate migration surface %s/%s", entry.Kind, entry.Name)
		}
		seen[entry.SurfaceKey] = struct{}{}
		if entry.Strategy != catalogparity.IdentityTransform || entry.OldName != entry.Name ||
			entry.NewName != entry.Name || entry.ConfigurationEdit != "none" ||
			entry.DestructiveRisk != catalogparity.RiskNone || entry.Recovery.Mode != "snapshot_restore" ||
			len(entry.ForwardAssertions) == 0 || len(entry.Recovery.Assertions) == 0 {
			return fmt.Errorf("surface %s/%s is not a complete identity migration", entry.Kind, entry.Name)
		}
	}
	return nil
}

func validateDNSLifecycle(input MigrationRecoveryInput) error {
	d := input.DNSLifecycle
	if d.FormatVersion != 1 || d.Gate != "M3 DNS managed-operation qualification" ||
		d.Result != "pass" || d.SourceCommit != input.Admission.SourceCommit ||
		d.Platform != "linux/amd64" || d.ProviderVersion != "0.101.2" {
		return fmt.Errorf("DNS lifecycle identity is invalid")
	}
	if d.ProviderBinarySHA256 != input.Admission.CandidateBinarySHA256 ||
		d.LegacyProvider.Version != "0.101.2" || !validHex(d.LegacyProvider.BinarySHA256, 64) {
		return fmt.Errorf("DNS lifecycle candidate binary or release authority differs")
	}
	if d.StateUpgradeSource.Version != "0.41.11" || !validHex(d.StateUpgradeSource.Commit, 40) ||
		!validHex(d.StateUpgradeSource.BinarySHA256, 64) {
		return fmt.Errorf("DNS lifecycle state upgrade authority is invalid")
	}
	for name, pair := range map[string]struct {
		actual ToolArtifact
		want   catalogparity.SchemaCLIReceipt
	}{
		"terraform": {d.Terraform, input.BuildSchema.SchemaEvidence.Terraform},
		"tofu":      {d.Tofu, input.BuildSchema.SchemaEvidence.Tofu},
	} {
		if pair.actual.Version != pair.want.Version || pair.actual.BinarySHA256 != pair.want.BinarySHA256 {
			return fmt.Errorf("DNS lifecycle %s toolchain differs from build/schema", name)
		}
	}
	checks := d.Lifecycle
	if !checks.FreshTargetPerCLIAndAdapter || !checks.Create || !checks.Update ||
		!checks.OmittedOptionalFields || !checks.ConfiguredOptionalFields || !checks.ReplacementPlan ||
		!checks.RestartRefresh || !checks.Import || !checks.V0IntegerTTLStateUpgrade ||
		!checks.NoOpPlan || !checks.Delete || !checks.Cleanup ||
		!checks.BidirectionalAdapterStateRoundTrip || !d.CLIOutcomesEquivalent ||
		!d.AdapterOutcomesEquivalent || !validHex(d.NormalizedStateSHA256, 64) {
		return fmt.Errorf("DNS lifecycle checks are incomplete")
	}
	if d.Target.Product != "UniFi Network" || d.Target.Version != "10.4.57" ||
		!strings.Contains(input.Controller.Target.Image, d.Target.PlatformManifestSHA256) {
		return fmt.Errorf("DNS lifecycle target differs from controller differential")
	}
	return nil
}

func canonicalDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
