package releasequalification

import (
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/managementcontract"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/paritydiff"
)

func TestBuildReleaseReadyArtifactsPromotesEverySurface(t *testing.T) {
	input := validReleaseReadyInput(t)

	artifacts, err := BuildReleaseReadyArtifacts(input)
	if err != nil {
		t.Fatalf("BuildReleaseReadyArtifacts() error = %v", err)
	}
	if artifacts.Receipt.Result != "pass" || artifacts.Receipt.SurfaceCount != 67 ||
		artifacts.Receipt.ReleaseReadySurfaceCount != 67 {
		t.Fatalf("release receipt result/counts = %q/%d/%d", artifacts.Receipt.Result,
			artifacts.Receipt.SurfaceCount, artifacts.Receipt.ReleaseReadySurfaceCount)
	}
	if artifacts.Receipt.ResolvedReleaseBlockers != 1 {
		t.Fatalf("resolved release blockers = %d, want 1", artifacts.Receipt.ResolvedReleaseBlockers)
	}
	if len(artifacts.Ledger.Entries) != 67 || len(artifacts.Contract.Surfaces) != 67 {
		t.Fatalf("ledger/contract surface counts = %d/%d", len(artifacts.Ledger.Entries), len(artifacts.Contract.Surfaces))
	}
	for index, entry := range artifacts.Ledger.Entries {
		if entry.State != catalogparity.ReleaseReady || entry.Implementation != "candidate" ||
			len(entry.ReceiptSHA256) != 64 {
			t.Fatalf("ledger entry %d = %#v", index, entry)
		}
		surface := artifacts.Contract.Surfaces[index]
		if surface.State != catalogparity.ReleaseReady || surface.AttemptResult != paritydiff.Pass ||
			surface.EvidenceSHA256 != entry.ReceiptSHA256 {
			t.Fatalf("contract surface %d = %#v", index, surface)
		}
	}
	if err := managementcontract.VerifyCatalogPromotion(
		artifacts.Contract,
		artifacts.Evidence,
		artifacts.Ledger,
	); err != nil {
		t.Fatalf("VerifyCatalogPromotion() error = %v", err)
	}
}

func TestBuildReleaseReadyArtifactsFailsClosed(t *testing.T) {
	tests := map[string]struct {
		mutate func(*ReleaseReadyInput)
		want   string
	}{
		"migration source": {
			mutate: func(input *ReleaseReadyInput) {
				input.Migration.SourceCommit = strings.Repeat("f", 40)
			},
			want: "migration/recovery lineage",
		},
		"physical hardware overclaim": {
			mutate: func(input *ReleaseReadyInput) {
				input.Hardware.PhysicalElectricalClaim = true
			},
			want: "hardware disposition",
		},
		"private module replacement": {
			mutate: func(input *ReleaseReadyInput) {
				input.Dependency.ReplacePresent = true
			},
			want: "dependency publishability",
		},
		"noncanonical module": {
			mutate: func(input *ReleaseReadyInput) {
				input.Dependency.ModulePath = "example.invalid/go-unifi"
			},
			want: "dependency publishability",
		},
		"confidentiality finding": {
			mutate: func(input *ReleaseReadyInput) {
				input.Confidentiality.FindingCount = 1
			},
			want: "confidentiality",
		},
		"retained raw state": {
			mutate: func(input *ReleaseReadyInput) {
				input.Confidentiality.RawStateRetained = true
			},
			want: "confidentiality",
		},
		"ledger substitution": {
			mutate: func(input *ReleaseReadyInput) {
				input.Ledger.Entries[2].Name = "unifi_unknown"
			},
			want: "ledger surface set",
		},
		// The four below were all measured BLIND before validateReleaseLedger
		// read the incoming state. BuildReleaseReadyArtifacts overwrites State,
		// Implementation and ReceiptSHA256 on every entry, so until these ran
		// the gate promoted whatever ledger it was handed.
		"one ledger surface never reached admitted": {
			mutate: func(input *ReleaseReadyInput) {
				input.Ledger.Entries[0].State = catalogparity.ShadowOnly
			},
			want: "the management contract admitted it as",
		},
		// The whole-catalog case is kept separately from the single-surface one.
		// A loop that stops at the first mismatch passes both; a check keyed on
		// some aggregate -- a count, a majority -- passes this one and fails the
		// other, and the difference between "one surface slipped" and "nothing
		// was ever admitted" is the difference worth having.
		"NO ledger surface reached admitted": {
			mutate: func(input *ReleaseReadyInput) {
				for index := range input.Ledger.Entries {
					input.Ledger.Entries[index].State = catalogparity.ShadowOnly
				}
			},
			want: "the management contract admitted it as",
		},
		"ledger describes the released tree": {
			mutate: func(input *ReleaseReadyInput) {
				input.Ledger.Entries[4].Implementation = "released"
			},
			want: "is implemented by",
		},
		"ledger surface carries no admission receipt": {
			mutate: func(input *ReleaseReadyInput) {
				input.Ledger.Entries[1].ReceiptSHA256 = ""
			},
			want: "carries no admission receipt digest",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := validReleaseReadyInput(t)
			test.mutate(&input)
			_, err := BuildReleaseReadyArtifacts(input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func validReleaseReadyInput(t *testing.T) ReleaseReadyInput {
	t.Helper()
	digest := strings.Repeat("a", 64)
	migrationInput := validMigrationInput(t)
	migration, err := BuildMigrationRecoveryReceipt(migrationInput)
	if err != nil {
		t.Fatal(err)
	}
	downstream := managementcontract.DownstreamManifestIdentity{
		Repository: "ubitofu", Commit: strings.Repeat("e", 40),
		ManifestPath: "src/ubitofu/manifest.py", ManifestSHA256: digest,
	}
	management := managementcontract.CatalogManagementContract{
		FormatVersion: 1, Gate: "catalog-management-contract",
		Mode: "provider_catalog_projection_required", Result: "ready_for_downstream_verification",
		ProviderAddress: catalogparity.CanonicalProviderAddress,
		Provider: managementcontract.Provider{
			SourceCommit: migration.SourceCommit,
			Binary:       managementcontract.Binary{SHA256: migration.CandidateBinary},
			Schema: managementcontract.Schema{Toolchains: map[string]managementcontract.SchemaToolchain{
				"terraform": {Version: "1.15.8", BinarySHA256: digest, CanonicalSchemaSHA256: digest},
				"tofu":      {Version: "1.12.1", BinarySHA256: digest, CanonicalSchemaSHA256: digest},
			}},
		},
		Admission: managementcontract.CatalogAdmissionIdentity{
			ReceiptSHA256: digest, Result: "pass", AdmittedSurfaceCount: 67,
			ReleaseBlockers: []catalogparity.EvidenceGap{{
				SurfaceKey: catalogparity.SurfaceKey{Kind: catalogparity.Action, Name: "unifi_port"},
				Signal:     "hardware_claim",
			}},
		},
		Downstream: downstream, PolicySHA256: digest,
		RequiredDimensions: []string{"capture_eligibility", "coverage", "enumeration", "generated_hcl", "identity", "plan_classification", "receipt_inputs", "redaction"},
	}
	ledger := catalogparity.Ledger{
		FormatVersion: 1, ProviderAddress: catalogparity.CanonicalProviderAddress,
		BaselineSHA256: digest, Entries: make([]catalogparity.LedgerEntry, 0, 67),
	}
	for _, admitted := range migrationInput.Admission.Surfaces {
		management.Surfaces = append(management.Surfaces, managementcontract.CatalogManagementSurface{
			SurfaceKey: admitted.SurfaceKey, State: catalogparity.Admitted,
			EvidenceSHA256: admitted.ReceiptSHA256,
			CaptureMode: func() managementcontract.CaptureMode {
				if admitted.Kind == catalogparity.ManagedResource {
					return managementcontract.CaptureManaged
				}
				return managementcontract.CaptureNotApplicable
			}(),
		})
		ledger.Entries = append(ledger.Entries, catalogparity.LedgerEntry{
			Surface: catalogparity.Surface{SurfaceKey: admitted.SurfaceKey, BaselineSchemaSHA256: digest},
			State:   catalogparity.Admitted, ReceiptSHA256: admitted.ReceiptSHA256,
			Implementation: "candidate",
		})
	}
	return ReleaseReadyInput{
		Ledger: ledger, LedgerSHA256: digest,
		Management: management, ManagementSHA256: digest,
		Migration: migration, MigrationSHA256: digest,
		Hardware: HardwareDispositionReceipt{
			FormatVersion: 1, Gate: "unifi-port-hardware-disposition", Result: "pass",
			SurfaceKey: catalogparity.SurfaceKey{Kind: catalogparity.Action, Name: "unifi_port"},
			Mode:       "protocol_sufficient", ClaimScope: "controller_poe_configuration_persistence",
			PhysicalElectricalClaim: false, ControllerReceiptSHA256: digest,
			ResolvedSignal: "hardware_claim",
		}, HardwareSHA256: digest,
		Dependency: DependencyPublishabilityReceipt{
			FormatVersion: 1, Gate: "go-unifi-dependency-publishability", Result: "pass",
			ProviderCommit: migration.SourceCommit,
			ModulePath:     "github.com/ubiquiti-community/go-unifi", ModuleVersion: "v1.102.0",
			ModuleCommit: strings.Repeat("d", 40), ModuleZipSHA256: digest, ModuleDirSHA256: digest,
			ReplacePresent: false, ResolutionRunner: "remote_ci", NetworkBoundary: "remote_ci_only",
		}, DependencySHA256: digest,
		Confidentiality: ConfidentialityReceipt{
			FormatVersion: 1, Gate: "public-export-confidentiality", Result: "pass",
			SourceCommit: migration.SourceCommit, TreeSHA256: digest,
			SecretScan: true, PrivateIdentifierScan: true, FileTypeScan: true,
			ProvenanceReview: true, FindingCount: 0, RestrictedEvidenceExternal: true,
			RawStateRetained: false, RawControllerResponseRetained: false,
		}, ConfidentialitySHA256: digest,
	}
}
