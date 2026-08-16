package managementcontract

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

func TestBuildCatalogManagementContractCoversAllAdmittedSurfaces(t *testing.T) {
	admission, build, policy := validCatalogManagementInput(t)
	digest := strings.Repeat("a", 64)
	admission.Evidence.BuildSchemaSHA256 = digest

	contract, err := BuildCatalogManagementContract(admission, digest, build, digest, policy, digest)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Result != "ready_for_downstream_verification" || len(contract.Surfaces) != 67 {
		t.Fatalf("contract result = %q with %d surfaces", contract.Result, len(contract.Surfaces))
	}
	counts := map[CaptureMode]int{}
	for _, surface := range contract.Surfaces {
		counts[surface.CaptureMode]++
		if surface.State != catalogparity.Admitted || surface.EvidenceSHA256 == "" {
			t.Fatalf("surface %s/%s is not admitted and bound", surface.Kind, surface.Name)
		}
	}
	want := map[CaptureMode]int{CaptureManaged: 28, CaptureNotApplicable: 39}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("capture counts = %v, want %v", counts, want)
	}
	if contract.Downstream.ManifestSHA256 != policy.Downstream.ManifestSHA256 {
		t.Fatal("downstream manifest digest is not bound")
	}
}

func TestBuildCatalogManagementContractRejectsUnboundCatalog(t *testing.T) {
	tests := map[string]struct {
		mutate func(*catalogparity.AdmissionReceipt, *catalogparity.BuildSchemaReceipt, *CatalogManagementPolicy)
		want   string
	}{
		"admission state": {
			mutate: func(admission *catalogparity.AdmissionReceipt, _ *catalogparity.BuildSchemaReceipt, _ *CatalogManagementPolicy) {
				admission.Surfaces[0].State = catalogparity.AdapterParity
			},
			want: "not admitted",
		},
		"binary": {
			mutate: func(admission *catalogparity.AdmissionReceipt, _ *catalogparity.BuildSchemaReceipt, _ *CatalogManagementPolicy) {
				admission.CandidateBinarySHA256 = strings.Repeat("b", 64)
			},
			want: "candidate binary",
		},
		"missing downstream managed resource": {
			mutate: func(_ *catalogparity.AdmissionReceipt, _ *catalogparity.BuildSchemaReceipt, policy *CatalogManagementPolicy) {
				policy.ManagedResources = policy.ManagedResources[:27]
			},
			want: "managed resource set",
		},
		"extra dimension": {
			mutate: func(_ *catalogparity.AdmissionReceipt, _ *catalogparity.BuildSchemaReceipt, policy *CatalogManagementPolicy) {
				policy.RequiredDimensions = append(policy.RequiredDimensions, "invented")
			},
			want: "required dimensions",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			admission, build, policy := validCatalogManagementInput(t)
			digest := strings.Repeat("a", 64)
			admission.Evidence.BuildSchemaSHA256 = digest
			test.mutate(&admission, &build, &policy)
			_, err := BuildCatalogManagementContract(admission, digest, build, digest, policy, digest)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("BuildCatalogManagementContract() error = %v, want %q", err, test.want)
			}
		})
	}
}

func validCatalogManagementInput(t *testing.T) (catalogparity.AdmissionReceipt, catalogparity.BuildSchemaReceipt, CatalogManagementPolicy) {
	t.Helper()
	digest := strings.Repeat("a", 64)
	commit := strings.Repeat("b", 40)
	inventoryData, err := os.ReadFile("../../build/release-ready/catalog-evidence-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory catalogparity.EvidenceInventory
	if err := json.Unmarshal(inventoryData, &inventory); err != nil {
		t.Fatal(err)
	}
	admission := catalogparity.AdmissionReceipt{
		FormatVersion:         1,
		Gate:                  "catalog-admission",
		Result:                "pass",
		ProviderAddress:       catalogparity.CanonicalProviderAddress,
		SourceCommit:          commit,
		ReleasedCommit:        inventory.ReleasedProvider.Commit,
		CandidateBinarySHA256: digest,
		AdmittedSurfaceCount:  67,
		ReleaseBlockerCount:   1,
		ReleaseBlockers: []catalogparity.EvidenceGap{{
			SurfaceKey: catalogparity.SurfaceKey{Kind: catalogparity.Action, Name: "unifi_port"},
			Signal:     "hardware_claim",
		}},
	}
	for _, surface := range inventory.Surfaces {
		admission.Surfaces = append(admission.Surfaces, catalogparity.SurfaceAdmission{
			SurfaceKey:          surface.SurfaceKey,
			Wave:                surface.Wave,
			AdapterState:        catalogparity.AdapterParity,
			AdapterParitySHA256: digest,
			State:               catalogparity.Admitted,
			ReceiptSHA256:       digest,
		})
	}
	build := catalogparity.BuildSchemaReceipt{
		FormatVersion:  1,
		Gate:           "catalog-build-schema",
		Result:         "pass",
		SourceCommit:   commit,
		ReleasedCommit: inventory.ReleasedProvider.Commit,
		Platform:       "linux/amd64",
		GoVersion:      "go1.25.8",
		BuildNetwork:   "none",
		ProviderBinaries: catalogparity.ProviderBinaryEvidence{
			CandidateSHA256: digest,
		},
		SchemaEvidence: catalogparity.SchemaDifferentialEvidence{
			Terraform: catalogparity.SchemaCLIReceipt{Version: "1.15.8", BinarySHA256: digest, CanonicalSHA256: digest},
			Tofu:      catalogparity.SchemaCLIReceipt{Version: "1.12.1", BinarySHA256: digest, CanonicalSHA256: digest},
		},
	}
	policyData, err := os.ReadFile("../../provider-codegen/policy/catalog-management-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy CatalogManagementPolicy
	if err := json.Unmarshal(policyData, &policy); err != nil {
		t.Fatal(err)
	}
	return admission, build, policy
}
