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
	// Fields carries the members of an object or array<object>. It is the
	// observed shape of the SDK struct, so it comes from the catalog rather
	// than from policy; what each member becomes in Terraform is still a
	// policy decision.
	Fields []bootstrapField `json:"fields,omitempty"`
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
	Groupings                 []groupingPolicy          `json:"groupings,omitempty"`
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
	// Fields holds the per-member decisions for an object or array<object>.
	// The catalog supplies the members; this supplies what each one becomes,
	// exactly as the top level does for scalars.
	Fields []fieldPolicy `json:"fields,omitempty"`
}

// groupingPolicy declares a nested Terraform attribute the SDK does not have.
//
// Most nested attributes mirror an SDK struct and their members are derived
// from the catalog. Thirty-three do not: the provider invented the shape over
// flat SDK fields, so there is no struct to observe and the grouping has to be
// declared. It is still a migration rather than hand-authoring, because every
// member names a field the catalog observed, and the compiler proves each
// observed field is consumed exactly once across the whole policy.
type groupingPolicy struct {
	TerraformName string `json:"terraform_name"`
	// TerraformType is single_nested, list_nested or set_nested. As with
	// collections, the SDK cannot say whether order matters.
	TerraformType string          `json:"terraform_type"`
	Attribute     json.RawMessage `json:"attribute,omitempty"`
	Members       []groupedMember `json:"members"`
}

// groupedMember is one member of a declared grouping.
type groupedMember struct {
	// StructuralName names the observed flat field this member consumes. It is
	// empty only for an invented member, which must say why.
	StructuralName string          `json:"structural_name,omitempty"`
	TerraformName  string          `json:"terraform_name"`
	TerraformType  string          `json:"terraform_type,omitempty"`
	Disposition    string          `json:"disposition"`
	Attribute      json.RawMessage `json:"attribute,omitempty"`
	// Invented records that this member corresponds to no observed field at
	// all. Two exist in the estate: port_forward's source_limiting.type, which
	// is computed from whether a firewall group is set, and bgp's peers, which
	// the provider parses out of an opaque config string. Both are stated here
	// so the ledger cannot report them as derived, and the reason is required
	// so the claim is legible rather than a flag someone sets to pass a gate.
	Invented string `json:"invented,omitempty"`
}

type providerOwnedPolicy struct {
	TerraformName string          `json:"terraform_name"`
	TerraformType string          `json:"terraform_type,omitempty"`
	Disposition   string          `json:"disposition"`
	Generated     bool            `json:"generated"`
	Attribute     json.RawMessage `json:"attribute,omitempty"`
}

type baselineDigestSet struct {
	Resource   string `json:"resource"`
	DataSource string `json:"data_source,omitempty"`
	// Identity and ListResource are companions of a managed resource and are
	// per-surface optional: unifi_bgp and unifi_setting carry no identity
	// schema, and unifi_account, unifi_bgp and unifi_setting carry no list
	// resource. An omitted digest declares the companion absent, and
	// validateBaseline cross-checks that declaration against the baseline
	// manifest so an omission cannot silently skip a check.
	Identity     string `json:"identity,omitempty"`
	ListResource string `json:"list_resource,omitempty"`
}

type baselineManifest struct {
	SchemaSHA256 map[string]string `json:"schema_sha256"`
}

// codeSpecification mirrors the root of the Provider Code Specification that
// tfplugingen-framework consumes. The member names are the generator's
// contract rather than ours: it reads "datasources" as one word, and a
// specification that puts a surface under the wrong member is not rejected,
// it simply yields no generated code.
type codeSpecification struct {
	Version     string           `json:"version"`
	Provider    codeProvider     `json:"provider"`
	Resources   []codeResource   `json:"resources,omitempty"`
	DataSources []codeDataSource `json:"datasources,omitempty"`
}

type codeProvider struct {
	Name string `json:"name"`
}

type codeResource struct {
	Name   string     `json:"name"`
	Schema codeSchema `json:"schema"`
}

type codeDataSource struct {
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
