package catalogparity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

type SchemaVersions struct {
	SchemaSHA256 string
	Versions     map[SurfaceKey]*int64
}

type schemaVersionRecord struct {
	Version *int64          `json:"version,omitempty"`
	Block   json.RawMessage `json:"block"`
}

type canonicalSchemaDocument struct {
	Provider                json.RawMessage                `json:"provider"`
	ResourceSchemas         map[string]schemaVersionRecord `json:"resource_schemas"`
	ResourceIdentitySchemas map[string]json.RawMessage     `json:"resource_identity_schemas"`
	DataSourceSchemas       map[string]schemaVersionRecord `json:"data_source_schemas"`
	ListResourceSchemas     map[string]schemaVersionRecord `json:"list_resource_schemas"`
	ActionSchemas           map[string]schemaVersionRecord `json:"action_schemas"`
}

type MigrationStrategy string

const (
	IdentityTransform MigrationStrategy = "identity"
	StateUpgrader     MigrationStrategy = "state_upgrader"
	AddressRecipe     MigrationStrategy = "address_recipe"
	ImportBridge      MigrationStrategy = "import_bridge"
	Unsupported       MigrationStrategy = "unsupported"
)

type Risk string

const (
	RiskNone     Risk = "none"
	RiskDeclared Risk = "declared"
)

type StateMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Recovery struct {
	Mode       string   `json:"mode"`
	Assertions []string `json:"assertions"`
}

type MigrationOverride struct {
	SurfaceKey
	Strategy          MigrationStrategy `json:"strategy"`
	NewName           string            `json:"new_name,omitempty"`
	NewSchemaVersion  *int64            `json:"new_schema_version,omitempty"`
	AttributeMapping  map[string]string `json:"attribute_mapping,omitempty"`
	StateMoves        []StateMove       `json:"state_moves,omitempty"`
	ImportTransform   string            `json:"import_transform,omitempty"`
	ConfigurationEdit string            `json:"configuration_edit,omitempty"`
	DestructiveRisk   Risk              `json:"destructive_risk,omitempty"`
	ForwardAssertions []string          `json:"forward_assertions,omitempty"`
	Recovery          Recovery          `json:"recovery,omitempty"`
}

type MigrationPolicy struct {
	FormatVersion   int                 `json:"format_version"`
	FromVersion     string              `json:"from_version"`
	ToVersion       string              `json:"to_version"`
	ProviderAddress string              `json:"provider_address"`
	DefaultStrategy MigrationStrategy   `json:"default_strategy"`
	Overrides       []MigrationOverride `json:"overrides"`
}

type MigrationEntry struct {
	SurfaceKey
	OldName           string            `json:"old_name"`
	NewName           string            `json:"new_name"`
	Strategy          MigrationStrategy `json:"strategy"`
	OldSchemaVersion  *int64            `json:"old_schema_version"`
	NewSchemaVersion  *int64            `json:"new_schema_version"`
	AttributeMapping  map[string]string `json:"attribute_mapping"`
	StateMoves        []StateMove       `json:"state_moves"`
	ImportTransform   string            `json:"import_transform,omitempty"`
	ConfigurationEdit string            `json:"configuration_edit"`
	DestructiveRisk   Risk              `json:"destructive_risk"`
	ForwardAssertions []string          `json:"forward_assertions"`
	Recovery          Recovery          `json:"recovery"`
}

type MigrationManifest struct {
	FormatVersion   int              `json:"format_version"`
	FromVersion     string           `json:"from_version"`
	ToVersion       string           `json:"to_version"`
	ProviderAddress string           `json:"provider_address"`
	BaselineSHA256  string           `json:"baseline_sha256"`
	SchemaSHA256    string           `json:"schema_sha256"`
	Entries         []MigrationEntry `json:"entries"`
}

type MigrationReport struct {
	FormatVersion  int            `json:"format_version"`
	SurfaceCount   int            `json:"surface_count"`
	StrategyCounts map[string]int `json:"strategy_counts"`
	RiskCounts     map[string]int `json:"risk_counts"`
	StateCommands  []string       `json:"state_commands"`
}

func ParseSchemaVersions(data []byte, baseline Baseline) (SchemaVersions, error) {
	if !validSHA256(baseline.CanonicalSchemaSHA256) || byteSHA256(data) != baseline.CanonicalSchemaSHA256 {
		return SchemaVersions{}, fmt.Errorf("canonical schema digest does not match baseline")
	}
	var document canonicalSchemaDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return SchemaVersions{}, fmt.Errorf("decode canonical schema: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return SchemaVersions{}, fmt.Errorf("decode canonical schema: %w", err)
	}
	if len(document.Provider) == 0 {
		return SchemaVersions{}, fmt.Errorf("canonical provider schema is missing")
	}

	versions := make(map[SurfaceKey]*int64, len(baseline.Surfaces))
	groups := []struct {
		kind    SurfaceKind
		records map[string]schemaVersionRecord
	}{
		{kind: ManagedResource, records: document.ResourceSchemas},
		{kind: DataSource, records: document.DataSourceSchemas},
		{kind: ListResource, records: document.ListResourceSchemas},
		{kind: Action, records: document.ActionSchemas},
	}
	for _, group := range groups {
		for name, record := range group.records {
			key := SurfaceKey{Kind: group.kind, Name: name}
			if _, exists := baseline.Surface(key); !exists {
				return SchemaVersions{}, fmt.Errorf("canonical schema contains unknown surface %s/%s", group.kind, name)
			}
			if len(record.Block) == 0 {
				return SchemaVersions{}, fmt.Errorf("canonical schema block is missing for %s/%s", group.kind, name)
			}
			if group.kind != Action && record.Version == nil {
				return SchemaVersions{}, fmt.Errorf("schema version is missing for %s/%s", group.kind, name)
			}
			if record.Version != nil && *record.Version < 0 {
				return SchemaVersions{}, fmt.Errorf("schema version is negative for %s/%s", group.kind, name)
			}
			versions[key] = cloneVersion(record.Version)
		}
	}
	for _, surface := range baseline.Surfaces {
		if _, exists := versions[surface.SurfaceKey]; !exists {
			return SchemaVersions{}, fmt.Errorf("canonical schema is missing surface %s/%s", surface.Kind, surface.Name)
		}
	}
	return SchemaVersions{SchemaSHA256: byteSHA256(data), Versions: versions}, nil
}

func ParseMigrationPolicy(data []byte) (MigrationPolicy, error) {
	var policy MigrationPolicy
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return MigrationPolicy{}, fmt.Errorf("decode migration policy: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return MigrationPolicy{}, fmt.Errorf("decode migration policy: %w", err)
	}
	return policy, nil
}

func ExpandMigration(baseline Baseline, versions SchemaVersions, policy MigrationPolicy) (MigrationManifest, error) {
	if err := validateBaselineForLedger(baseline); err != nil {
		return MigrationManifest{}, err
	}
	if versions.SchemaSHA256 != baseline.CanonicalSchemaSHA256 || len(versions.Versions) != len(baseline.Surfaces) {
		return MigrationManifest{}, fmt.Errorf("schema versions do not match the released baseline")
	}
	if policy.FormatVersion != 1 {
		return MigrationManifest{}, fmt.Errorf("unsupported migration policy format version %d", policy.FormatVersion)
	}
	if policy.FromVersion != "0.101.2" {
		return MigrationManifest{}, fmt.Errorf("migration source version %q is not 0.101.2", policy.FromVersion)
	}
	if policy.ToVersion == "" || policy.ToVersion == policy.FromVersion {
		return MigrationManifest{}, fmt.Errorf("migration target version is invalid")
	}
	if policy.ProviderAddress != CanonicalProviderAddress {
		return MigrationManifest{}, fmt.Errorf("migration provider address %q is not canonical", policy.ProviderAddress)
	}
	if policy.DefaultStrategy != IdentityTransform {
		return MigrationManifest{}, fmt.Errorf("default migration strategy must be identity")
	}

	overrides := make(map[SurfaceKey]MigrationOverride, len(policy.Overrides))
	for _, override := range policy.Overrides {
		if _, exists := overrides[override.SurfaceKey]; exists {
			return MigrationManifest{}, fmt.Errorf("duplicate migration override for %s/%s", override.Kind, override.Name)
		}
		if _, exists := baseline.Surface(override.SurfaceKey); !exists {
			return MigrationManifest{}, fmt.Errorf("migration override references unknown surface %s/%s", override.Kind, override.Name)
		}
		if !validMigrationStrategy(override.Strategy) {
			return MigrationManifest{}, fmt.Errorf("invalid migration strategy %q for %s/%s", override.Strategy, override.Kind, override.Name)
		}
		overrides[override.SurfaceKey] = override
	}

	entries := make([]MigrationEntry, 0, len(baseline.Surfaces))
	for _, surface := range baseline.Surfaces {
		version, exists := versions.Versions[surface.SurfaceKey]
		if !exists {
			return MigrationManifest{}, fmt.Errorf("schema version is missing for %s/%s", surface.Kind, surface.Name)
		}
		entry := identityMigrationEntry(surface.SurfaceKey, version)
		if override, exists := overrides[surface.SurfaceKey]; exists {
			var err error
			entry, err = applyMigrationOverride(entry, override)
			if err != nil {
				return MigrationManifest{}, fmt.Errorf("surface %s/%s: %w", surface.Kind, surface.Name, err)
			}
		}
		entries = append(entries, entry)
	}
	manifest := MigrationManifest{
		FormatVersion:   1,
		FromVersion:     policy.FromVersion,
		ToVersion:       policy.ToVersion,
		ProviderAddress: policy.ProviderAddress,
		BaselineSHA256:  baseline.SourceSHA256,
		SchemaSHA256:    versions.SchemaSHA256,
		Entries:         entries,
	}
	if err := ValidateMigrationManifest(manifest); err != nil {
		return MigrationManifest{}, err
	}
	return manifest, nil
}

func BuildMigrationReport(manifest MigrationManifest) (MigrationReport, error) {
	if err := ValidateMigrationManifest(manifest); err != nil {
		return MigrationReport{}, err
	}
	report := MigrationReport{
		FormatVersion:  1,
		SurfaceCount:   len(manifest.Entries),
		StrategyCounts: map[string]int{},
		RiskCounts:     map[string]int{},
		StateCommands:  []string{},
	}
	for _, entry := range manifest.Entries {
		report.StrategyCounts[string(entry.Strategy)]++
		report.RiskCounts[string(entry.DestructiveRisk)]++
		for _, move := range entry.StateMoves {
			report.StateCommands = append(report.StateCommands, fmt.Sprintf("<terraform|tofu> state mv %q %q", move.From, move.To))
		}
	}
	sort.Strings(report.StateCommands)
	return report, nil
}

func identityMigrationEntry(key SurfaceKey, version *int64) MigrationEntry {
	return MigrationEntry{
		SurfaceKey:        key,
		OldName:           key.Name,
		NewName:           key.Name,
		Strategy:          IdentityTransform,
		OldSchemaVersion:  cloneVersion(version),
		NewSchemaVersion:  cloneVersion(version),
		AttributeMapping:  map[string]string{},
		StateMoves:        []StateMove{},
		ConfigurationEdit: "none",
		DestructiveRisk:   RiskNone,
		ForwardAssertions: defaultForwardAssertions(),
		Recovery:          defaultRecovery(),
	}
}

func applyMigrationOverride(entry MigrationEntry, override MigrationOverride) (MigrationEntry, error) {
	if override.Strategy == IdentityTransform {
		if override.NewName != "" && override.NewName != entry.OldName {
			return MigrationEntry{}, fmt.Errorf("identity migration cannot rename the surface")
		}
		if override.NewSchemaVersion != nil && !sameVersion(override.NewSchemaVersion, entry.OldSchemaVersion) {
			return MigrationEntry{}, fmt.Errorf("identity migration cannot change the schema version")
		}
		if len(override.AttributeMapping) != 0 || len(override.StateMoves) != 0 || override.ImportTransform != "" || override.ConfigurationEdit != "" || override.DestructiveRisk != "" || len(override.ForwardAssertions) != 0 || override.Recovery.Mode != "" {
			return MigrationEntry{}, fmt.Errorf("identity migration contains a non-identity transform")
		}
		return entry, nil
	}

	entry.Strategy = override.Strategy
	if override.NewName != "" {
		entry.NewName = override.NewName
	}
	if override.NewSchemaVersion != nil {
		entry.NewSchemaVersion = cloneVersion(override.NewSchemaVersion)
	}
	entry.AttributeMapping = cloneStringMap(override.AttributeMapping)
	entry.StateMoves = append([]StateMove(nil), override.StateMoves...)
	entry.ImportTransform = override.ImportTransform
	entry.ConfigurationEdit = override.ConfigurationEdit
	entry.DestructiveRisk = override.DestructiveRisk
	if entry.DestructiveRisk == "" {
		entry.DestructiveRisk = RiskNone
	}
	entry.ForwardAssertions = append([]string(nil), override.ForwardAssertions...)
	if len(entry.ForwardAssertions) == 0 {
		entry.ForwardAssertions = defaultForwardAssertions()
	}
	entry.Recovery = override.Recovery
	if !surfaceNamePattern.MatchString(entry.NewName) {
		return MigrationEntry{}, fmt.Errorf("new surface name %q is invalid", entry.NewName)
	}
	if entry.ConfigurationEdit == "" {
		return MigrationEntry{}, fmt.Errorf("configuration edit is required")
	}
	if entry.Recovery.Mode == "" || len(entry.Recovery.Assertions) == 0 {
		return MigrationEntry{}, fmt.Errorf("recovery procedure is required")
	}

	switch entry.Strategy {
	case StateUpgrader:
		if entry.Kind != ManagedResource || entry.OldSchemaVersion == nil || entry.NewSchemaVersion == nil || *entry.NewSchemaVersion <= *entry.OldSchemaVersion {
			return MigrationEntry{}, fmt.Errorf("state upgrader requires an increasing managed-resource schema version")
		}
		if len(entry.StateMoves) != 0 || entry.ImportTransform != "" || entry.NewName != entry.OldName {
			return MigrationEntry{}, fmt.Errorf("state upgrader contains an address or import transform")
		}
	case AddressRecipe:
		if entry.NewName == entry.OldName || len(entry.StateMoves) == 0 {
			return MigrationEntry{}, fmt.Errorf("address recipe requires a renamed surface and state move")
		}
	case ImportBridge:
		if entry.ImportTransform == "" {
			return MigrationEntry{}, fmt.Errorf("import bridge requires an identity transform")
		}
	case Unsupported:
		if entry.DestructiveRisk != RiskDeclared {
			return MigrationEntry{}, fmt.Errorf("unsupported migration requires declared destructive risk")
		}
	default:
		return MigrationEntry{}, fmt.Errorf("invalid migration strategy %q", entry.Strategy)
	}
	return entry, nil
}

// ValidateMigrationManifest checks a migration manifest's identity, provenance,
// ordering and per-entry consistency. It admits any of the five strategies,
// because a manifest may legitimately be non-identity.
//
// It is exported so release qualification can hold the same rules over the
// committed manifest that generation holds over the emitted one. It had a
// same-named twin there that was not a superset either way, and five defects
// this rejects passed that one -- see releasequalification.validateMigrationManifest.
func ValidateMigrationManifest(manifest MigrationManifest) error {
	if manifest.FormatVersion != 1 || manifest.FromVersion != "0.101.2" || manifest.ToVersion == "" {
		return fmt.Errorf("migration manifest identity is invalid")
	}
	if manifest.ProviderAddress != CanonicalProviderAddress || !validSHA256(manifest.BaselineSHA256) || !validSHA256(manifest.SchemaSHA256) {
		return fmt.Errorf("migration manifest provenance is invalid")
	}
	counts := map[SurfaceKind]int{}
	seen := make(map[SurfaceKey]struct{}, len(manifest.Entries))
	for index, entry := range manifest.Entries {
		if _, duplicate := seen[entry.SurfaceKey]; duplicate {
			return fmt.Errorf("duplicate migration surface %s/%s", entry.Kind, entry.Name)
		}
		seen[entry.SurfaceKey] = struct{}{}
		counts[entry.Kind]++
		if index > 0 && !surfaceLess(manifest.Entries[index-1].SurfaceKey, entry.SurfaceKey) {
			return fmt.Errorf("migration entries are not strictly sorted")
		}
		if !validMigrationStrategy(entry.Strategy) || !validRisk(entry.DestructiveRisk) || entry.OldName != entry.Name || !surfaceNamePattern.MatchString(entry.NewName) {
			return fmt.Errorf("migration entry %s/%s is invalid", entry.Kind, entry.Name)
		}
		if entry.Strategy == IdentityTransform {
			if entry.NewName != entry.OldName || !sameVersion(entry.OldSchemaVersion, entry.NewSchemaVersion) || len(entry.AttributeMapping) != 0 || len(entry.StateMoves) != 0 || entry.ImportTransform != "" || entry.ConfigurationEdit != "none" || entry.DestructiveRisk != RiskNone {
				return fmt.Errorf("identity migration for %s/%s contains a transform", entry.Kind, entry.Name)
			}
		}
		if len(entry.ForwardAssertions) == 0 || entry.Recovery.Mode == "" || len(entry.Recovery.Assertions) == 0 {
			return fmt.Errorf("migration safety assertions are incomplete for %s/%s", entry.Kind, entry.Name)
		}
	}
	return validateReleasedCounts(counts)
}

func validMigrationStrategy(strategy MigrationStrategy) bool {
	return strategy == IdentityTransform || strategy == StateUpgrader || strategy == AddressRecipe || strategy == ImportBridge || strategy == Unsupported
}

func validRisk(risk Risk) bool {
	return risk == RiskNone || risk == RiskDeclared
}

func defaultForwardAssertions() []string {
	return []string{"identity_preserved", "no_undeclared_replace", "no_undeclared_delete", "first_plan_empty"}
}

func defaultRecovery() Recovery {
	return Recovery{
		Mode:       "snapshot_restore",
		Assertions: []string{"state_snapshot_present", "previous_configuration_restored"},
	}
}

func cloneVersion(version *int64) *int64 {
	if version == nil {
		return nil
	}
	cloned := *version
	return &cloned
}

func sameVersion(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
