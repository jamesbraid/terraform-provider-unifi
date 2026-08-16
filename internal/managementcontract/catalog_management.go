package managementcontract

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

type CaptureMode string

const (
	CaptureManaged       CaptureMode = "managed"
	CaptureNotApplicable CaptureMode = "not_applicable"
)

var catalogManagementDimensions = []string{
	"capture_eligibility",
	"coverage",
	"enumeration",
	"generated_hcl",
	"identity",
	"plan_classification",
	"receipt_inputs",
	"redaction",
}

type DownstreamManifestIdentity struct {
	Repository     string `json:"repository"`
	Commit         string `json:"commit"`
	ManifestPath   string `json:"manifest_path"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

type CatalogManagementPolicy struct {
	FormatVersion      int                        `json:"format_version"`
	ProviderAddress    string                     `json:"provider_address"`
	Downstream         DownstreamManifestIdentity `json:"downstream"`
	ManagedResources   []string                   `json:"managed_resources"`
	RequiredDimensions []string                   `json:"required_dimensions"`
}

type CatalogAdmissionIdentity struct {
	ReceiptSHA256        string                      `json:"receipt_sha256"`
	Result               string                      `json:"result"`
	AdmittedSurfaceCount int                         `json:"admitted_surface_count"`
	ReleaseBlockers      []catalogparity.EvidenceGap `json:"release_blockers"`
}

type CatalogManagementSurface struct {
	catalogparity.SurfaceKey
	State          catalogparity.AdmissionState `json:"state"`
	EvidenceSHA256 string                       `json:"evidence_sha256"`
	CaptureMode    CaptureMode                  `json:"capture_mode"`
}

type CatalogManagementContract struct {
	FormatVersion      int                        `json:"format_version"`
	Gate               string                     `json:"gate"`
	Mode               string                     `json:"mode"`
	Result             string                     `json:"result"`
	ProviderAddress    string                     `json:"provider_address"`
	Provider           Provider                   `json:"provider"`
	Admission          CatalogAdmissionIdentity   `json:"admission"`
	Downstream         DownstreamManifestIdentity `json:"downstream"`
	PolicySHA256       string                     `json:"policy_sha256"`
	RequiredDimensions []string                   `json:"required_dimensions"`
	Surfaces           []CatalogManagementSurface `json:"surfaces"`
}

func BuildCatalogManagementContract(
	admission catalogparity.AdmissionReceipt,
	admissionSHA256 string,
	build catalogparity.BuildSchemaReceipt,
	buildSHA256 string,
	policy CatalogManagementPolicy,
	policySHA256 string,
) (CatalogManagementContract, error) {
	if err := validateCatalogAdmission(admission, admissionSHA256, build, buildSHA256); err != nil {
		return CatalogManagementContract{}, err
	}
	if err := validateCatalogManagementPolicy(policy, policySHA256, admission); err != nil {
		return CatalogManagementContract{}, err
	}

	managed := make(map[string]struct{}, len(policy.ManagedResources))
	for _, name := range policy.ManagedResources {
		managed[name] = struct{}{}
	}
	surfaces := make([]CatalogManagementSurface, 0, len(admission.Surfaces))
	for _, surface := range admission.Surfaces {
		mode := CaptureNotApplicable
		if surface.Kind == catalogparity.ManagedResource {
			if _, ok := managed[surface.Name]; !ok {
				return CatalogManagementContract{}, fmt.Errorf("managed resource set is missing %s", surface.Name)
			}
			mode = CaptureManaged
		}
		surfaces = append(surfaces, CatalogManagementSurface{
			SurfaceKey:     surface.SurfaceKey,
			State:          surface.State,
			EvidenceSHA256: surface.ReceiptSHA256,
			CaptureMode:    mode,
		})
	}

	return CatalogManagementContract{
		FormatVersion:   1,
		Gate:            "catalog-management-contract",
		Mode:            "provider_catalog_projection_required",
		Result:          "ready_for_downstream_verification",
		ProviderAddress: catalogparity.CanonicalProviderAddress,
		Provider: Provider{
			SourceCommit: build.SourceCommit,
			Binary:       Binary{SHA256: build.ProviderBinaries.CandidateSHA256},
			Schema: Schema{Toolchains: map[string]SchemaToolchain{
				"terraform": schemaToolchainFromReceipt(build.SchemaEvidence.Terraform),
				"tofu":      schemaToolchainFromReceipt(build.SchemaEvidence.Tofu),
			}},
		},
		Admission: CatalogAdmissionIdentity{
			ReceiptSHA256:        admissionSHA256,
			Result:               admission.Result,
			AdmittedSurfaceCount: admission.AdmittedSurfaceCount,
			ReleaseBlockers:      append([]catalogparity.EvidenceGap(nil), admission.ReleaseBlockers...),
		},
		Downstream:         policy.Downstream,
		PolicySHA256:       policySHA256,
		RequiredDimensions: append([]string(nil), policy.RequiredDimensions...),
		Surfaces:           surfaces,
	}, nil
}

func validateCatalogAdmission(admission catalogparity.AdmissionReceipt, admissionSHA256 string, build catalogparity.BuildSchemaReceipt, buildSHA256 string) error {
	if admission.FormatVersion != 1 || admission.Gate != "catalog-admission" ||
		admission.Result != "pass" || admission.ProviderAddress != catalogparity.CanonicalProviderAddress {
		return fmt.Errorf("catalog admission identity is invalid")
	}
	if !validHex(admissionSHA256, 64) || !validHex(buildSHA256, 64) ||
		admission.Evidence.BuildSchemaSHA256 != buildSHA256 {
		return fmt.Errorf("catalog admission receipt SHA-256 is invalid")
	}
	if admission.AdmittedSurfaceCount != 67 || len(admission.Surfaces) != 67 {
		return fmt.Errorf("catalog admission has %d surfaces, want 67", len(admission.Surfaces))
	}
	if admission.ReleaseBlockerCount != 1 || len(admission.ReleaseBlockers) != 1 ||
		admission.ReleaseBlockers[0] != (catalogparity.EvidenceGap{
			SurfaceKey: catalogparity.SurfaceKey{Kind: catalogparity.Action, Name: "unifi_port"},
			Signal:     "hardware_claim",
		}) {
		return fmt.Errorf("catalog admission release blockers are invalid")
	}
	if build.FormatVersion != 1 || build.Gate != "catalog-build-schema" || build.Result != "pass" ||
		len(build.PromotionBlockers) != 0 || build.Platform != "linux/amd64" ||
		build.GoVersion != "go1.25.8" || build.BuildNetwork != "none" {
		return fmt.Errorf("build/schema receipt is not promotable")
	}
	if admission.Evidence.BuildSchemaSHA256 == "" ||
		admission.SourceCommit != build.SourceCommit || admission.ReleasedCommit != build.ReleasedCommit {
		return fmt.Errorf("catalog admission source lineage differs from build/schema")
	}
	if admission.CandidateBinarySHA256 != build.ProviderBinaries.CandidateSHA256 ||
		!validHex(build.ProviderBinaries.CandidateSHA256, 64) {
		return fmt.Errorf("catalog admission candidate binary differs from build/schema")
	}
	for name, receipt := range map[string]catalogparity.SchemaCLIReceipt{
		"terraform": build.SchemaEvidence.Terraform,
		"tofu":      build.SchemaEvidence.Tofu,
	} {
		if receipt.Version == "" || !validHex(receipt.BinarySHA256, 64) || !validHex(receipt.CanonicalSHA256, 64) {
			return fmt.Errorf("%s schema receipt is incomplete", name)
		}
	}
	for _, surface := range admission.Surfaces {
		if surface.State != catalogparity.Admitted || surface.AdapterState != catalogparity.AdapterParity ||
			!validHex(surface.AdapterParitySHA256, 64) || !validHex(surface.ReceiptSHA256, 64) {
			return fmt.Errorf("surface %s/%s is not admitted and bound", surface.Kind, surface.Name)
		}
	}
	return nil
}

func validateCatalogManagementPolicy(policy CatalogManagementPolicy, policySHA256 string, admission catalogparity.AdmissionReceipt) error {
	if policy.FormatVersion != 1 || policy.ProviderAddress != catalogparity.CanonicalProviderAddress ||
		!validHex(policySHA256, 64) {
		return fmt.Errorf("catalog management policy identity is invalid")
	}
	if policy.Downstream.Repository == "" || policy.Downstream.ManifestPath == "" ||
		!validHex(policy.Downstream.Commit, 40) || !validHex(policy.Downstream.ManifestSHA256, 64) {
		return fmt.Errorf("downstream manifest identity is invalid")
	}
	if !reflect.DeepEqual(policy.RequiredDimensions, catalogManagementDimensions) {
		return fmt.Errorf("required dimensions are %v, want %v", policy.RequiredDimensions, catalogManagementDimensions)
	}
	if !sort.StringsAreSorted(policy.ManagedResources) || hasDuplicateStrings(policy.ManagedResources) {
		return fmt.Errorf("managed resource set is not sorted and unique")
	}
	want := make([]string, 0, 28)
	for _, surface := range admission.Surfaces {
		if surface.Kind == catalogparity.ManagedResource {
			want = append(want, surface.Name)
		}
	}
	sort.Strings(want)
	if !reflect.DeepEqual(policy.ManagedResources, want) {
		return fmt.Errorf("managed resource set differs from the admitted provider catalog")
	}
	return nil
}

func schemaToolchainFromReceipt(receipt catalogparity.SchemaCLIReceipt) SchemaToolchain {
	return SchemaToolchain{
		Version:               receipt.Version,
		BinarySHA256:          receipt.BinarySHA256,
		CanonicalSchemaSHA256: receipt.CanonicalSHA256,
	}
}

func hasDuplicateStrings(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] == values[index] {
			return true
		}
	}
	return false
}
