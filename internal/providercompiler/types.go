package providercompiler

import "encoding/json"

// CompileInput contains the immutable structural source, provider policy, and
// released schema baseline used for one compiler run.
type CompileInput struct {
	Bootstrap       []byte
	Policy          []byte
	BaselineDigests []byte
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

type policy struct {
	FormatVersion             int                   `json:"format_version"`
	Resource                  string                `json:"resource"`
	GeneratorName             string                `json:"generator_name"`
	SourceSpecificationSHA256 string                `json:"source_specification_sha256"`
	Description               string                `json:"description"`
	Fields                    []fieldPolicy         `json:"fields"`
	ProviderOwned             []providerOwnedPolicy `json:"provider_owned"`
	BaselineDigests           baselineDigestSet     `json:"baseline_digests"`
}

type fieldPolicy struct {
	StructuralName string          `json:"structural_name"`
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
	Resources []codeResource `json:"resources"`
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
	FormatVersion     int               `json:"format_version"`
	Resource          string            `json:"resource"`
	Source            bootstrapSource   `json:"source"`
	BaselineDigests   baselineDigestSet `json:"baseline_digests"`
	StructuralFields  int               `json:"structural_fields"`
	GeneratedAttrs    int               `json:"generated_attributes"`
	ProviderSeams     int               `json:"provider_owned_seams"`
	UnresolvedFields  []string          `json:"unresolved_fields"`
	StalePolicyFields []string          `json:"stale_policy_fields"`
}

type mappingReport struct {
	FormatVersion int                    `json:"format_version"`
	Resource      string                 `json:"resource"`
	Fields        []mappingField         `json:"fields"`
	ProviderOwned []providerOwnedMapping `json:"provider_owned"`
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
