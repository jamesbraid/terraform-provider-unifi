package providercompiler

import (
	"encoding/json"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// CompileInput contains the immutable structural source, provider policy, and
// released schema baseline used for one compiler run.
type CompileInput struct {
	Bootstrap       []byte
	Catalog         []byte
	Policy          []byte
	BaselineDigests []byte
	Ledger          []byte
}

// Result contains the deterministic artifacts produced by one compiler run.
type Result struct {
	ProviderCodeSpec []byte
	ImpactReport     []byte
	MappingReport    []byte
}

type bootstrap struct {
	FormatVersion int             `json:"format_version"`
	Source        bootstrapSource `json:"source"`
	Resource      bootstrapSchema `json:"resource"`
}

type bootstrapSource struct {
	Repository          string `json:"repository"`
	Commit              string `json:"commit"`
	SpecificationSHA256 string `json:"specification_sha256"`
}

type bootstrapSchema struct {
	Name   string           `json:"name"`
	Fields []bootstrapField `json:"fields"`
}

type bootstrapField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type observedCatalog struct {
	FormatVersion     int                       `json:"format_version"`
	CatalogID         string                    `json:"catalog_id"`
	Target            catalogTarget             `json:"target"`
	Sources           catalogSources            `json:"sources"`
	StructuralRecords []catalogStructuralRecord `json:"structural_records"`
	ObservedRecords   []catalogObservedRecord   `json:"observed_records"`
	Conflicts         []catalogConflict         `json:"conflicts"`
	Coverage          []catalogCoverage         `json:"coverage"`
	Admission         catalogAdmission          `json:"admission"`
	Tombstones        []string                  `json:"tombstones"`
	Migrations        []catalogMigration        `json:"migrations"`
}

type catalogTarget struct {
	Name                  string `json:"name"`
	Product               string `json:"product"`
	Version               string `json:"version"`
	Architecture          string `json:"architecture"`
	ImageIndexSHA256      string `json:"image_index_sha256"`
	ImageManifestSHA256   string `json:"image_manifest_sha256"`
	ControllerFingerprint string `json:"controller_fingerprint"`
}

type catalogObservedRecord struct {
	ID           string `json:"id"`
	Field        string `json:"field"`
	JSONType     string `json:"json_type"`
	PresentCount int    `json:"present_count"`
	NonNullCount int    `json:"non_null_count"`
}

type catalogMigration struct {
	FromID   string `json:"from_id"`
	ToID     string `json:"to_id"`
	Reason   string `json:"reason"`
	Reviewed bool   `json:"reviewed"`
}

type catalogSources struct {
	CaptureLockSHA256          string `json:"capture_lock_sha256"`
	SpecificationSHA256        string `json:"specification_sha256,omitempty"`
	StructuralProjectionSHA256 string `json:"structural_projection_sha256,omitempty"`
	SemanticPredecessorSHA256  string `json:"semantic_predecessor_sha256,omitempty"`
	SemanticIDsSHA256          string `json:"semantic_ids_sha256,omitempty"`
}

type catalogStructuralRecord struct {
	ID               string `json:"id"`
	Field            string `json:"field"`
	Type             string `json:"type"`
	DefinitionSHA256 string `json:"definition_sha256"`
	SecretCandidate  bool   `json:"secret_candidate"`
}

type catalogConflict struct {
	Kind     string `json:"kind"`
	Field    string `json:"field"`
	Expected string `json:"expected,omitempty"`
	Observed string `json:"observed,omitempty"`
}

type catalogCoverage struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

type catalogAdmission struct {
	State           string `json:"state"`
	OperationDigest string `json:"operation_digest"`
}

type policy struct {
	FormatVersion             int                       `json:"format_version"`
	SurfaceKind               catalogparity.SurfaceKind `json:"surface_kind"`
	Resource                  string                    `json:"resource"`
	GeneratorName             string                    `json:"generator_name"`
	SourceSpecificationSHA256 string                    `json:"source_specification_sha256"`
	CatalogID                 string                    `json:"catalog_id,omitempty"`
	CatalogSHA256             string                    `json:"catalog_sha256,omitempty"`
	CatalogSource             catalogPolicySource       `json:"catalog_source,omitempty"`
	CatalogTarget             catalogTarget             `json:"catalog_target,omitempty"`
	CatalogSources            catalogSources            `json:"catalog_sources,omitempty"`
	OperationDigest           string                    `json:"operation_digest,omitempty"`
	Description               string                    `json:"description"`
	Fields                    []fieldPolicy             `json:"fields"`
	ProviderOwned             []providerOwnedPolicy     `json:"provider_owned"`
	BaselineDigests           baselineDigestSet         `json:"baseline_digests"`
}

type catalogPolicySource struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Path       string `json:"path"`
}

type fieldPolicy struct {
	StructuralName string          `json:"structural_name"`
	SemanticID     string          `json:"semantic_id,omitempty"`
	TerraformName  string          `json:"terraform_name"`
	TerraformType  string          `json:"terraform_type,omitempty"`
	Disposition    string          `json:"disposition"`
	Attribute      json.RawMessage `json:"attribute,omitempty"`
}

type providerOwnedPolicy struct {
	TerraformName string          `json:"terraform_name"`
	TerraformType string          `json:"terraform_type,omitempty"`
	Disposition   string          `json:"disposition"`
	Generated     bool            `json:"generated"`
	Attribute     json.RawMessage `json:"attribute,omitempty"`
}

type baselineDigestSet struct {
	Resource     string `json:"resource"`
	Identity     string `json:"identity"`
	ListResource string `json:"list_resource"`
}

type baselineManifest struct {
	SchemaSHA256 map[string]string `json:"schema_sha256"`
}

type codeSpecification struct {
	Version   string         `json:"version"`
	Provider  codeProvider   `json:"provider"`
	Resources []codeResource `json:"resources"`
}

type codeProvider struct {
	Name string `json:"name"`
}

type codeResource struct {
	Name   string     `json:"name"`
	Schema codeSchema `json:"schema"`
}

type codeSchema struct {
	Attributes          []codeAttribute `json:"attributes"`
	MarkdownDescription string          `json:"markdown_description,omitempty"`
}

type codeAttribute struct {
	Name       string          `json:"name"`
	Type       string          `json:"-"`
	Definition json.RawMessage `json:"-"`
}

type impactReport struct {
	FormatVersion     int                       `json:"format_version"`
	SurfaceKind       catalogparity.SurfaceKind `json:"surface_kind"`
	SurfaceName       string                    `json:"surface_name"`
	Resource          string                    `json:"resource"`
	Source            bootstrapSource           `json:"source"`
	BaselineDigests   baselineDigestSet         `json:"baseline_digests"`
	StructuralFields  int                       `json:"structural_fields"`
	GeneratedAttrs    int                       `json:"generated_attributes"`
	ProviderSeams     int                       `json:"provider_owned_seams"`
	UnresolvedFields  []string                  `json:"unresolved_fields"`
	StalePolicyFields []string                  `json:"stale_policy_fields"`
}

type mappingReport struct {
	FormatVersion int                       `json:"format_version"`
	SurfaceKind   catalogparity.SurfaceKind `json:"surface_kind"`
	SurfaceName   string                    `json:"surface_name"`
	Resource      string                    `json:"resource"`
	Fields        []mappingField            `json:"fields"`
	ProviderOwned []providerOwnedMapping    `json:"provider_owned"`
}

type mappingField struct {
	StructuralName string `json:"structural_name"`
	TerraformName  string `json:"terraform_name"`
	StructuralType string `json:"structural_type"`
	TerraformType  string `json:"terraform_type"`
	Disposition    string `json:"disposition"`
}

type providerOwnedMapping struct {
	TerraformName string `json:"terraform_name"`
	TerraformType string `json:"terraform_type,omitempty"`
	Disposition   string `json:"disposition"`
	Generated     bool   `json:"generated"`
}
