package catalogparity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type AdmissionState string

const (
	BaselineState       AdmissionState = "baseline"
	Cataloged           AdmissionState = "cataloged"
	PolicyComplete      AdmissionState = "policy_complete"
	GeneratedShadow     AdmissionState = "generated_shadow"
	AdapterParity       AdmissionState = "adapter_parity"
	Admitted            AdmissionState = "admitted"
	ContractParity      AdmissionState = "contract_parity"
	ReleaseReady        AdmissionState = "release_ready"
	Unknown             AdmissionState = "unknown"
	ShadowOnly          AdmissionState = "shadow_only"
	Uncovered           AdmissionState = "uncovered"
	Divergent           AdmissionState = "divergent"
	Inconclusive        AdmissionState = "inconclusive"
	Invalid             AdmissionState = "invalid"
	LegacyAuthoritative AdmissionState = "legacy_authoritative"
)

type LedgerEntry struct {
	Surface
	State          AdmissionState `json:"state"`
	ReceiptSHA256  string         `json:"receipt_sha256,omitempty"`
	Migration      string         `json:"migration,omitempty"`
	Implementation string         `json:"implementation"`
}

type StatusOverride struct {
	SurfaceKey
	State         AdmissionState `json:"state"`
	ReceiptSHA256 string         `json:"receipt_sha256,omitempty"`
	// Migration says why this surface rests in an in-flight state. A surface
	// being migrated must pass through generated_shadow: the compiler admits a
	// shadow at that state, and admission needs a campaign run it cannot have
	// yet. So the state is a waypoint on the designed path, not limbo — but
	// only when someone says so. The reason is required rather than a flag,
	// for the same cause an invented grouping member states its reason: a flag
	// is something set to pass a gate, and a surface left in limbo by accident
	// is exactly what the wave checkpoint exists to catch.
	Migration string `json:"migration,omitempty"`
}

type StatusOverlay struct {
	FormatVersion int              `json:"format_version"`
	DefaultState  AdmissionState   `json:"default_state"`
	Overrides     []StatusOverride `json:"overrides"`
}

type Ledger struct {
	FormatVersion   int           `json:"format_version"`
	ProviderAddress string        `json:"provider_address"`
	BaselineSHA256  string        `json:"baseline_sha256"`
	Entries         []LedgerEntry `json:"entries"`
}

func ParseStatusOverlay(data []byte) (StatusOverlay, error) {
	var overlay StatusOverlay
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&overlay); err != nil {
		return StatusOverlay{}, fmt.Errorf("decode status overlay: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return StatusOverlay{}, fmt.Errorf("decode status overlay: %w", err)
	}
	return overlay, nil
}

func BuildLedger(baseline Baseline, overlay StatusOverlay) (Ledger, error) {
	if err := validateBaselineForLedger(baseline); err != nil {
		return Ledger{}, err
	}
	if overlay.FormatVersion != 1 {
		return Ledger{}, fmt.Errorf("unsupported status overlay format version %d", overlay.FormatVersion)
	}
	if !validAdmissionState(overlay.DefaultState) {
		return Ledger{}, fmt.Errorf("invalid default admission state %q", overlay.DefaultState)
	}
	if stateRequiresReceipt(overlay.DefaultState) {
		return Ledger{}, fmt.Errorf("default admission state %q requires per-surface receipts", overlay.DefaultState)
	}
	if stateIsInFlight(overlay.DefaultState) {
		return Ledger{}, fmt.Errorf("default admission state %q is in flight and must be declared per surface", overlay.DefaultState)
	}

	overrides := make(map[SurfaceKey]StatusOverride, len(overlay.Overrides))
	for _, override := range overlay.Overrides {
		if _, exists := overrides[override.SurfaceKey]; exists {
			return Ledger{}, fmt.Errorf("duplicate status override for %s/%s", override.Kind, override.Name)
		}
		if _, exists := baseline.Surface(override.SurfaceKey); !exists {
			return Ledger{}, fmt.Errorf("status override references missing surface %s/%s", override.Kind, override.Name)
		}
		if !validAdmissionState(override.State) {
			return Ledger{}, fmt.Errorf("invalid admission state %q for %s/%s", override.State, override.Kind, override.Name)
		}
		if err := validateReceipt(override.State, override.ReceiptSHA256); err != nil {
			return Ledger{}, fmt.Errorf("surface %s/%s: %w", override.Kind, override.Name, err)
		}
		if err := validateMigration(override.State, override.Migration); err != nil {
			return Ledger{}, fmt.Errorf("surface %s/%s: %w", override.Kind, override.Name, err)
		}
		overrides[override.SurfaceKey] = override
	}

	entries := make([]LedgerEntry, 0, len(baseline.Surfaces))
	for _, surface := range baseline.Surfaces {
		state := overlay.DefaultState
		receipt := ""
		migration := ""
		if override, exists := overrides[surface.SurfaceKey]; exists {
			state = override.State
			receipt = override.ReceiptSHA256
			migration = override.Migration
		}
		entries = append(entries, LedgerEntry{
			Surface:        surface,
			State:          state,
			ReceiptSHA256:  receipt,
			Migration:      migration,
			Implementation: implementationForState(state),
		})
	}
	return Ledger{
		FormatVersion:   1,
		ProviderAddress: baseline.ProviderAddress,
		BaselineSHA256:  baseline.SourceSHA256,
		Entries:         entries,
	}, nil
}

func ParseLedger(data []byte) (Ledger, error) {
	var ledger Ledger
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ledger); err != nil {
		return Ledger{}, fmt.Errorf("decode ledger: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Ledger{}, fmt.Errorf("decode ledger: %w", err)
	}
	if err := validateLedger(ledger); err != nil {
		return Ledger{}, err
	}
	return ledger, nil
}

func (l Ledger) Require(key SurfaceKey, states ...AdmissionState) error {
	for _, entry := range l.Entries {
		if entry.SurfaceKey != key {
			continue
		}
		for _, state := range states {
			if entry.State == state {
				return nil
			}
		}
		return fmt.Errorf("surface %s/%s state is %q, want one of %v", key.Kind, key.Name, entry.State, states)
	}
	return fmt.Errorf("surface %s/%s is missing from ledger", key.Kind, key.Name)
}

func validateBaselineForLedger(baseline Baseline) error {
	if baseline.FormatVersion != 1 || baseline.ProviderAddress != CanonicalProviderAddress {
		return fmt.Errorf("baseline identity is invalid")
	}
	if !validSHA256(baseline.SourceSHA256) || !validSHA256(baseline.CanonicalSchemaSHA256) {
		return fmt.Errorf("baseline digests are invalid")
	}
	if err := validateReleasedCounts(baseline.Counts()); err != nil {
		return err
	}
	for _, surface := range baseline.Surfaces {
		if !validSurfaceKind(surface.Kind) || !surfaceNamePattern.MatchString(surface.Name) || !validSHA256(surface.BaselineSchemaSHA256) {
			return fmt.Errorf("baseline surface %s/%s is invalid", surface.Kind, surface.Name)
		}
	}
	return nil
}

func validateLedger(ledger Ledger) error {
	if ledger.FormatVersion != 1 {
		return fmt.Errorf("unsupported ledger format version %d", ledger.FormatVersion)
	}
	if ledger.ProviderAddress != CanonicalProviderAddress {
		return fmt.Errorf("ledger provider address %q is not canonical", ledger.ProviderAddress)
	}
	if !validSHA256(ledger.BaselineSHA256) {
		return fmt.Errorf("ledger baseline SHA-256 is invalid")
	}
	counts := map[SurfaceKind]int{}
	seen := make(map[SurfaceKey]struct{}, len(ledger.Entries))
	for index, entry := range ledger.Entries {
		if !validSurfaceKind(entry.Kind) || !surfaceNamePattern.MatchString(entry.Name) || !validSHA256(entry.BaselineSchemaSHA256) {
			return fmt.Errorf("ledger surface %s/%s is invalid", entry.Kind, entry.Name)
		}
		if _, duplicate := seen[entry.SurfaceKey]; duplicate {
			return fmt.Errorf("duplicate ledger surface %s/%s", entry.Kind, entry.Name)
		}
		seen[entry.SurfaceKey] = struct{}{}
		counts[entry.Kind]++
		if !validAdmissionState(entry.State) {
			return fmt.Errorf("invalid ledger state %q for %s/%s", entry.State, entry.Kind, entry.Name)
		}
		if err := validateReceipt(entry.State, entry.ReceiptSHA256); err != nil {
			return fmt.Errorf("surface %s/%s: %w", entry.Kind, entry.Name, err)
		}
		if err := validateMigration(entry.State, entry.Migration); err != nil {
			return fmt.Errorf("surface %s/%s: %w", entry.Kind, entry.Name, err)
		}
		if entry.Implementation != implementationForState(entry.State) {
			return fmt.Errorf("surface %s/%s implementation %q does not match state %q", entry.Kind, entry.Name, entry.Implementation, entry.State)
		}
		if index > 0 && !surfaceLess(ledger.Entries[index-1].SurfaceKey, entry.SurfaceKey) {
			return fmt.Errorf("ledger entries are not strictly sorted")
		}
	}
	if err := validateReleasedCounts(counts); err != nil {
		return err
	}
	return nil
}

func validateReceipt(state AdmissionState, receipt string) error {
	if stateRequiresReceipt(state) && !validSHA256(receipt) {
		return fmt.Errorf("state %q requires a receipt SHA-256", state)
	}
	if receipt != "" && !validSHA256(receipt) {
		return fmt.Errorf("receipt SHA-256 is invalid")
	}
	return nil
}

func stateRequiresReceipt(state AdmissionState) bool {
	return state == Admitted || state == ContractParity || state == ReleaseReady
}

// validateMigration enforces that an in-flight state is a declared one.
//
// Both directions matter. Without a reason the surface is indistinguishable
// from one abandoned mid-migration, which is the condition the wave checkpoint
// protects against. With a reason on a state that is not in flight the
// declaration is stale, and a stale declaration is worse than none: it reads
// as current and nothing would contradict it.
func validateMigration(state AdmissionState, migration string) error {
	declared := strings.TrimSpace(migration) != ""
	if stateIsInFlight(state) && !declared {
		return fmt.Errorf("state %q is in flight and requires a migration reason saying why the surface rests there", state)
	}
	if declared && !stateIsInFlight(state) {
		return fmt.Errorf("state %q is not in flight, so it must not carry a migration reason", state)
	}
	return nil
}

// stateIsInFlight reports whether a surface at this state is partway through
// migration rather than settled.
//
// generated_shadow is the motivating case: the compiler emits a schema there,
// and admission cannot follow until a campaign run says the runtime agrees.
// adapter_parity is the same shape — neither terminal nor legacy, an
// implementation that exists but does not yet serve — so it carries the same
// obligation. shadow_only is deliberately absent: a shadow built to study a
// surface that stays legacy is a settled position, not a waypoint.
func stateIsInFlight(state AdmissionState) bool {
	return state == GeneratedShadow || state == AdapterParity
}

func implementationForState(state AdmissionState) string {
	switch state {
	case Admitted, ContractParity, ReleaseReady:
		return "candidate"
	case GeneratedShadow, AdapterParity, ShadowOnly:
		return "shadow"
	default:
		return "legacy"
	}
}

func validAdmissionState(state AdmissionState) bool {
	switch state {
	case BaselineState, Cataloged, PolicyComplete, GeneratedShadow, AdapterParity,
		Admitted, ContractParity, ReleaseReady, Unknown, ShadowOnly, Uncovered,
		Divergent, Inconclusive, Invalid, LegacyAuthoritative:
		return true
	default:
		return false
	}
}

func validSurfaceKind(kind SurfaceKind) bool {
	return kind == ManagedResource || kind == DataSource || kind == ListResource || kind == Action
}

func surfaceLess(left, right SurfaceKey) bool {
	leftOrder := surfaceKindOrder(left.Kind)
	rightOrder := surfaceKindOrder(right.Kind)
	if leftOrder != rightOrder {
		return leftOrder < rightOrder
	}
	return left.Name < right.Name
}
