package managementcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/paritydiff"
)

const releasedSurfaceCount = 67

type CatalogContract struct {
	FormatVersion           int               `json:"format_version"`
	Provider                Provider          `json:"provider"`
	LedgerSHA256            string            `json:"ledger_sha256"`
	MigrationManifestSHA256 string            `json:"migration_manifest_sha256"`
	Surfaces                []SurfaceContract `json:"surfaces"`
}

type SurfaceContract struct {
	catalogparity.SurfaceKey
	State          catalogparity.AdmissionState `json:"state"`
	EvidenceSHA256 string                       `json:"evidence_sha256,omitempty"`
	AttemptResult  paritydiff.AttemptResult     `json:"attempt_result,omitempty"`
}

type CatalogEvidence struct {
	ProviderBinarySHA256    string
	SourceCommit            string
	LedgerSHA256            string
	MigrationManifestSHA256 string
	SchemaToolchains        map[string]SchemaToolchain
	SurfaceEvidence         map[catalogparity.SurfaceKey]string
}

func VerifyCatalog(contract CatalogContract, evidence CatalogEvidence, ledger catalogparity.Ledger) error {
	if contract.FormatVersion != 1 {
		return fmt.Errorf("unsupported catalog contract format version %d", contract.FormatVersion)
	}
	if err := verifyCatalogProvider(contract.Provider, evidence); err != nil {
		return err
	}
	if err := requireDigest("catalog ledger", contract.LedgerSHA256); err != nil {
		return err
	}
	if err := requireEqual("catalog ledger", contract.LedgerSHA256, evidence.LedgerSHA256); err != nil {
		return err
	}
	measuredLedger, err := catalogLedgerDigest(ledger)
	if err != nil {
		return err
	}
	if err := requireEqual("catalog ledger artifact", contract.LedgerSHA256, measuredLedger); err != nil {
		return err
	}
	if err := requireDigest("migration manifest", contract.MigrationManifestSHA256); err != nil {
		return err
	}
	if err := requireEqual("migration manifest", contract.MigrationManifestSHA256, evidence.MigrationManifestSHA256); err != nil {
		return err
	}
	if err := verifyReleasedLedgerShape(ledger); err != nil {
		return err
	}
	if len(contract.Surfaces) != releasedSurfaceCount {
		return fmt.Errorf("catalog contract has %d surfaces, want 67 surfaces", len(contract.Surfaces))
	}

	contracts := make(map[catalogparity.SurfaceKey]SurfaceContract, len(contract.Surfaces))
	for _, surface := range contract.Surfaces {
		if _, duplicate := contracts[surface.SurfaceKey]; duplicate {
			return fmt.Errorf("duplicate catalog contract surface %s/%s", surface.Kind, surface.Name)
		}
		contracts[surface.SurfaceKey] = surface
	}
	ledgerKeys := make(map[catalogparity.SurfaceKey]struct{}, len(ledger.Entries))
	for _, entry := range ledger.Entries {
		ledgerKeys[entry.SurfaceKey] = struct{}{}
		surface, ok := contracts[entry.SurfaceKey]
		if !ok {
			return fmt.Errorf("catalog contract is missing surface %s/%s", entry.Kind, entry.Name)
		}
		if surface.State != entry.State {
			return fmt.Errorf("surface %s/%s ledger state is %q, contract has %q", entry.Kind, entry.Name, entry.State, surface.State)
		}
		if err := verifySurfaceEvidence(surface, entry, evidence.SurfaceEvidence); err != nil {
			return err
		}
	}
	for key := range evidence.SurfaceEvidence {
		if _, exists := ledgerKeys[key]; !exists {
			return fmt.Errorf("surface evidence references unknown surface %s/%s", key.Kind, key.Name)
		}
	}
	return nil
}

func VerifyCatalogPromotion(contract CatalogContract, evidence CatalogEvidence, ledger catalogparity.Ledger) error {
	if err := VerifyCatalog(contract, evidence, ledger); err != nil {
		return err
	}
	for _, surface := range contract.Surfaces {
		if surface.State != catalogparity.ReleaseReady {
			return fmt.Errorf("surface %s/%s is %q, not release_ready", surface.Kind, surface.Name, surface.State)
		}
		if surface.AttemptResult != paritydiff.Pass {
			return fmt.Errorf("surface %s/%s attempt result is %q, want pass", surface.Kind, surface.Name, surface.AttemptResult)
		}
	}
	return nil
}

func verifyCatalogProvider(provider Provider, evidence CatalogEvidence) error {
	if err := requireDigest("provider binary", provider.Binary.SHA256); err != nil {
		return err
	}
	if err := requireEqual("provider binary", provider.Binary.SHA256, evidence.ProviderBinarySHA256); err != nil {
		return err
	}
	if !validHex(provider.SourceCommit, 40) {
		return fmt.Errorf("source commit is incomplete")
	}
	if err := requireEqual("source commit", provider.SourceCommit, evidence.SourceCommit); err != nil {
		return err
	}
	for _, name := range []string{"terraform", "tofu"} {
		want, ok := provider.Schema.Toolchains[name]
		if !ok || want.Version == "" {
			return fmt.Errorf("%s schema toolchain is incomplete", name)
		}
		if err := requireDigest(name+" schema toolchain binary", want.BinarySHA256); err != nil {
			return err
		}
		if err := requireDigest(name+" canonical schema", want.CanonicalSchemaSHA256); err != nil {
			return err
		}
		got, ok := evidence.SchemaToolchains[name]
		if !ok {
			return fmt.Errorf("%s schema toolchain evidence is missing", name)
		}
		if err := requireEqual(name+" schema toolchain version", want.Version, got.Version); err != nil {
			return err
		}
		if err := requireEqual(name+" schema toolchain binary", want.BinarySHA256, got.BinarySHA256); err != nil {
			return err
		}
		if err := requireEqual(name+" canonical schema", want.CanonicalSchemaSHA256, got.CanonicalSchemaSHA256); err != nil {
			return err
		}
	}
	return nil
}

func verifySurfaceEvidence(surface SurfaceContract, entry catalogparity.LedgerEntry, measured map[catalogparity.SurfaceKey]string) error {
	requiresEvidence := surface.State == catalogparity.Admitted || surface.State == catalogparity.ContractParity || surface.State == catalogparity.ReleaseReady
	if requiresEvidence && !validHex(surface.EvidenceSHA256, 64) {
		return fmt.Errorf("surface %s/%s state %q requires evidence SHA-256", surface.Kind, surface.Name, surface.State)
	}
	if surface.EvidenceSHA256 != "" {
		if err := requireDigest("surface evidence", surface.EvidenceSHA256); err != nil {
			return fmt.Errorf("surface %s/%s: %w", surface.Kind, surface.Name, err)
		}
		if entry.ReceiptSHA256 != "" {
			if err := requireEqual("surface ledger receipt", entry.ReceiptSHA256, surface.EvidenceSHA256); err != nil {
				return fmt.Errorf("surface %s/%s: %w", surface.Kind, surface.Name, err)
			}
		}
		got, ok := measured[surface.SurfaceKey]
		if !ok {
			return fmt.Errorf("surface %s/%s measured surface evidence is missing", surface.Kind, surface.Name)
		}
		if err := requireEqual("surface evidence", surface.EvidenceSHA256, got); err != nil {
			return fmt.Errorf("surface %s/%s: %w", surface.Kind, surface.Name, err)
		}
	}
	if surface.State == catalogparity.ReleaseReady && surface.AttemptResult != paritydiff.Pass {
		return fmt.Errorf("surface %s/%s attempt result is %q, want pass", surface.Kind, surface.Name, surface.AttemptResult)
	}
	return nil
}

func verifyReleasedLedgerShape(ledger catalogparity.Ledger) error {
	if len(ledger.Entries) != releasedSurfaceCount {
		return fmt.Errorf("catalog ledger has %d surfaces, want 67 surfaces", len(ledger.Entries))
	}
	want := map[catalogparity.SurfaceKind]int{
		catalogparity.ManagedResource: 28,
		catalogparity.DataSource:      13,
		catalogparity.ListResource:    25,
		catalogparity.Action:          1,
	}
	got := make(map[catalogparity.SurfaceKind]int, len(want))
	for _, entry := range ledger.Entries {
		got[entry.Kind]++
	}
	for kind, count := range want {
		if got[kind] != count {
			return fmt.Errorf("catalog ledger has %d %s surfaces, want %d", got[kind], kind, count)
		}
	}
	return nil
}

func catalogLedgerDigest(ledger catalogparity.Ledger) (string, error) {
	data, err := json.Marshal(ledger)
	if err != nil {
		return "", fmt.Errorf("encode catalog ledger: %w", err)
	}
	data = append(data, '\n')
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
