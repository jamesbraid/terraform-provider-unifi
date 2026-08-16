package managementcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/paritydiff"
)

const testMigrationManifest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

func TestVerifyCatalogAcceptsCompleteDevelopmentContract(t *testing.T) {
	ledger := loadCatalogLedger(t)
	contract, evidence := testCatalogContract(t, ledger)

	if err := VerifyCatalog(contract, evidence, ledger); err != nil {
		t.Fatalf("VerifyCatalog(valid) error = %v", err)
	}
}

func TestVerifyCatalogRejectsUnboundOrIncompleteCatalog(t *testing.T) {
	ledger := loadCatalogLedger(t)

	tests := map[string]struct {
		mutate func(*CatalogContract, *CatalogEvidence)
		want   string
	}{
		"provider binary": {
			mutate: func(_ *CatalogContract, evidence *CatalogEvidence) {
				evidence.ProviderBinarySHA256 = strings.Repeat("d", 64)
			},
			want: "provider binary",
		},
		"terraform schema": {
			mutate: func(_ *CatalogContract, evidence *CatalogEvidence) {
				toolchain := evidence.SchemaToolchains["terraform"]
				toolchain.CanonicalSchemaSHA256 = strings.Repeat("d", 64)
				evidence.SchemaToolchains["terraform"] = toolchain
			},
			want: "terraform canonical schema",
		},
		"ledger artifact": {
			mutate: func(_ *CatalogContract, evidence *CatalogEvidence) {
				evidence.LedgerSHA256 = strings.Repeat("d", 64)
			},
			want: "catalog ledger",
		},
		"migration manifest": {
			mutate: func(_ *CatalogContract, evidence *CatalogEvidence) {
				evidence.MigrationManifestSHA256 = strings.Repeat("d", 64)
			},
			want: "migration manifest",
		},
		"missing surface": {
			mutate: func(contract *CatalogContract, _ *CatalogEvidence) {
				contract.Surfaces = contract.Surfaces[:len(contract.Surfaces)-1]
			},
			want: "67 surfaces",
		},
		"duplicate surface": {
			mutate: func(contract *CatalogContract, _ *CatalogEvidence) {
				contract.Surfaces[1].SurfaceKey = contract.Surfaces[0].SurfaceKey
			},
			want: "duplicate",
		},
		"state mismatch": {
			mutate: func(contract *CatalogContract, _ *CatalogEvidence) {
				contract.Surfaces[0].State = catalogparity.ReleaseReady
				contract.Surfaces[0].AttemptResult = paritydiff.Pass
			},
			want: "ledger state",
		},
		"admitted evidence": {
			mutate: func(contract *CatalogContract, _ *CatalogEvidence) {
				for index := range contract.Surfaces {
					if contract.Surfaces[index].State == catalogparity.Admitted {
						contract.Surfaces[index].EvidenceSHA256 = ""
						return
					}
				}
			},
			want: "requires evidence",
		},
		"measured surface evidence": {
			mutate: func(contract *CatalogContract, evidence *CatalogEvidence) {
				for _, surface := range contract.Surfaces {
					if surface.State == catalogparity.Admitted {
						evidence.SurfaceEvidence[surface.SurfaceKey] = strings.Repeat("d", 64)
						return
					}
				}
			},
			want: "surface evidence",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			contract, evidence := testCatalogContract(t, ledger)
			test.mutate(&contract, &evidence)
			err := VerifyCatalog(contract, evidence, ledger)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("VerifyCatalog() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestVerifyCatalogPromotionRejectsDevelopmentStates(t *testing.T) {
	ledger := loadCatalogLedger(t)
	contract, evidence := testCatalogContract(t, ledger)

	err := VerifyCatalogPromotion(contract, evidence, ledger)
	if err == nil || !strings.Contains(err.Error(), "not release_ready") {
		t.Fatalf("VerifyCatalogPromotion() error = %v, want non-release-ready surface", err)
	}
}

func TestVerifyCatalogPromotionRequiresPassingAttempts(t *testing.T) {
	ledger := loadCatalogLedger(t)
	contract, evidence := testCatalogContract(t, ledger)
	for index := range ledger.Entries {
		ledger.Entries[index].State = catalogparity.ReleaseReady
		ledger.Entries[index].Implementation = "candidate"
		ledger.Entries[index].ReceiptSHA256 = digestForSurface(ledger.Entries[index].SurfaceKey)
		contract.Surfaces[index].State = catalogparity.ReleaseReady
		contract.Surfaces[index].EvidenceSHA256 = ledger.Entries[index].ReceiptSHA256
		contract.Surfaces[index].AttemptResult = paritydiff.Pass
		evidence.SurfaceEvidence[ledger.Entries[index].SurfaceKey] = ledger.Entries[index].ReceiptSHA256
	}
	contract.LedgerSHA256 = digestJSON(t, ledger)
	evidence.LedgerSHA256 = contract.LedgerSHA256

	contract.Surfaces[0].AttemptResult = paritydiff.Inconclusive
	if err := VerifyCatalogPromotion(contract, evidence, ledger); err == nil || !strings.Contains(err.Error(), "attempt result") {
		t.Fatalf("VerifyCatalogPromotion(inconclusive) error = %v, want attempt result", err)
	}
	contract.Surfaces[0].AttemptResult = paritydiff.Pass
	if err := VerifyCatalogPromotion(contract, evidence, ledger); err != nil {
		t.Fatalf("VerifyCatalogPromotion(valid) error = %v", err)
	}
}

func loadCatalogLedger(t *testing.T) catalogparity.Ledger {
	t.Helper()
	data, err := os.ReadFile("../../provider-codegen/generated/catalog-parity-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := catalogparity.ParseLedger(data)
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func testCatalogContract(t *testing.T, ledger catalogparity.Ledger) (CatalogContract, CatalogEvidence) {
	t.Helper()
	contract := CatalogContract{
		FormatVersion:           1,
		Provider:                testContract().Provider,
		LedgerSHA256:            digestJSON(t, ledger),
		MigrationManifestSHA256: testMigrationManifest,
		Surfaces:                make([]SurfaceContract, 0, len(ledger.Entries)),
	}
	evidence := CatalogEvidence{
		ProviderBinarySHA256:    testProviderBinary,
		SourceCommit:            testSourceCommit,
		LedgerSHA256:            contract.LedgerSHA256,
		MigrationManifestSHA256: testMigrationManifest,
		SchemaToolchains:        testEvidence().SchemaToolchains,
		SurfaceEvidence:         make(map[catalogparity.SurfaceKey]string),
	}
	for _, entry := range ledger.Entries {
		surface := SurfaceContract{SurfaceKey: entry.SurfaceKey, State: entry.State}
		if entry.State == catalogparity.Admitted || entry.State == catalogparity.ContractParity || entry.State == catalogparity.ReleaseReady {
			surface.EvidenceSHA256 = entry.ReceiptSHA256
			evidence.SurfaceEvidence[entry.SurfaceKey] = entry.ReceiptSHA256
		}
		contract.Surfaces = append(contract.Surfaces, surface)
	}
	return contract, evidence
}

func digestJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func digestForSurface(key catalogparity.SurfaceKey) string {
	sum := sha256.Sum256([]byte(string(key.Kind) + "/" + key.Name))
	return hex.EncodeToString(sum[:])
}
