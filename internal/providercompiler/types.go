package providercompiler

import (
	"encoding/json"
)

// SurfaceKind names which of the four Terraform surface shapes a policy
// compiles for.
type SurfaceKind string

const (
	ManagedResource SurfaceKind = "managed_resource"
	DataSource      SurfaceKind = "data_source"
	ListResource    SurfaceKind = "list_resource"
	Action          SurfaceKind = "action"
)

// CompileInput contains the immutable structural source and provider policy
// used for one compiler run.
type CompileInput struct {
	Bootstrap []byte
	Policy    []byte
}

// Result contains the deterministic artifacts produced by one compiler run.
type Result struct {
	ProviderCodeSpec []byte
	MappingReport    []byte
}

type bootstrap struct {
	FormatVersion int             `json:"format_version"`
	Source        bootstrapSource `json:"source"`
	Resource      bootstrapSchema `json:"resource"`
	// Companions are the further SDK structs a surface projects. The lead
	// stays in Resource, not the first companion, since the surface's
	// identity, baseline key and conversion file all follow the lead.
	Companions []bootstrapCompanion `json:"companions,omitempty"`
}

// bootstrapCompanion is one further observed struct, named by its GO TYPE.
// There is no resource name for it; the policy qualifies a field by this name.
type bootstrapCompanion struct {
	Struct string           `json:"struct"`
	Fields []bootstrapField `json:"fields"`
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
	// Fields carries the members of an object or array<object>: the observed
	// shape from the catalog, not policy. What each member becomes in
	// Terraform is still a policy decision.
	Fields []bootstrapField `json:"fields,omitempty"`
	// SecretCandidate is set by cmd/sdk-bootstrap for x_-prefixed SDK fields.
	SecretCandidate bool `json:"secret_candidate,omitempty"`
	// GoName and Pointer aren't derivable from the wire name or shape (see
	// cmd/sdk-bootstrap); the compiler decodes strictly, so both must be
	// declared here to survive decoding.
	GoName  string `json:"go_name,omitempty"`
	Pointer bool   `json:"pointer,omitempty"`
}

type policy struct {
	FormatVersion             int                `json:"format_version"`
	SurfaceKind               SurfaceKind        `json:"surface_kind"`
	Resource                  string             `json:"resource"`
	GeneratorName             string             `json:"generator_name"`
	SourceSpecificationSHA256 string             `json:"source_specification_sha256"`
	Groupings                 []groupingPolicy   `json:"groupings,omitempty"`
	Flattenings               []flatteningPolicy `json:"flattenings,omitempty"`
	Claims                    []claimPolicy      `json:"claims,omitempty"`
	Description               string             `json:"description"`
	Fields                    []fieldPolicy      `json:"fields"`
	// Omitted is a compact form of a bare omitted field: one with no
	// disposition beyond "omitted" itself -- no attribute, no semantic_id, no
	// nested fields, nothing an object form would carry that this would
	// lose. Each entry is a structural name, qualified as "Struct.field" for
	// a companion or bare for the lead, exactly as qualifyField renders one.
	// expandOmittedFields folds these into Fields before anything else reads
	// it, so every check downstream sees one shape regardless of which form
	// named the field.
	Omitted       []string              `json:"omitted,omitempty"`
	ProviderOwned []providerOwnedPolicy `json:"provider_owned"`
}

type fieldPolicy struct {
	StructuralName string `json:"structural_name"`
	// StructuralSource names the SDK struct StructuralName belongs to, when
	// the surface projects more than one; empty means the lead struct. It's
	// per attribute rather than per group, since one group can span structs.
	StructuralSource string          `json:"structural_source,omitempty"`
	SemanticID       string          `json:"semantic_id,omitempty"`
	TerraformName    string          `json:"terraform_name"`
	TerraformType    string          `json:"terraform_type,omitempty"`
	Disposition      string          `json:"disposition"`
	Attribute        json.RawMessage `json:"attribute,omitempty"`
	// Fields holds the per-member decisions for an object or array<object>.
	// The catalog supplies the members; this supplies what each one becomes,
	// exactly as the top level does for scalars.
	Fields []fieldPolicy `json:"fields,omitempty"`
	// Invented records that this member corresponds to no observed field, and
	// requires a reason -- a flag is something set to pass a gate. An
	// invented member must also declare its own terraform_type, since there's
	// no observed field to take one from.
	Invented string `json:"invented,omitempty"`
}

// groupingPolicy declares a nested Terraform attribute invented over flat
// SDK fields, with no SDK struct behind it. Every member still names a field
// the catalog observed, and the compiler proves each observed field is
// consumed exactly once across the whole policy.
//
// Path resolution has two cases, and they go opposite ways: under Fields the
// structural path extends (destination.ip_group_id -> destination.ip_group_id);
// under a grouping it resets to the member's flat field (dhcp_server.enabled
// -> dhcpd_enabled, not dhcp_server.dhcpd_enabled). The two look identical in
// a member's own JSON -- only its position in the policy says which applies.
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
	StructuralName string `json:"structural_name,omitempty"`
	// StructuralSource names the SDK struct StructuralName belongs to, when
	// the surface projects more than one; empty means the lead struct. It's
	// per attribute rather than per group, since one group can span structs.
	StructuralSource string          `json:"structural_source,omitempty"`
	TerraformName    string          `json:"terraform_name"`
	TerraformType    string          `json:"terraform_type,omitempty"`
	Disposition      string          `json:"disposition"`
	Attribute        json.RawMessage `json:"attribute,omitempty"`
	// Fields holds per-member decisions when the observed field this member
	// consumes is itself an object or array<object>, letting a grouping
	// nest instead of only consuming flat fields. Same member list and same
	// exhaustiveness rule as a top-level object field: every member of the
	// observed struct is classified or omitted, nothing dropped silently.
	Fields []fieldPolicy `json:"fields,omitempty"`
	// Invented records that this member corresponds to no observed field at
	// all, so the ledger doesn't report it as derived. The reason is
	// required, not a flag someone sets to pass a gate.
	Invented string `json:"invented,omitempty"`
	// ElementMember names which member of an observed array<object> this
	// member presents, when the released attribute is a list of scalars over
	// an element the SDK models as a struct. Stated rather than derived from
	// "whichever member is not omitted", and cross-checked three ways against
	// the catalog: the member exists, it's the only one not omitted, and its
	// observed type matches the declared element type.
	ElementMember string `json:"element_member,omitempty"`
}

// claimPolicy relates a set of schema members to a set of observed fields
// through a named pair of functions -- the one mechanism for every relation
// that isn't one-to-one (a member naming one field, an invented member, or
// an omitted field are its degenerate 1x1/1x0/0x1 forms). It's declared
// here rather than on a member because the relation isn't a property of any
// one member: it can span one grouping, sibling groupings, or the top level.
//
// Every observed field and every schema member appears in exactly one
// claim, so two members that co-claim a field must be listed together under
// one named function, not asserted separately.
type claimPolicy struct {
	// TerraformMembers names the schema members by path: a top-level field by
	// its terraform_name, a grouping member as "grouping.member".
	TerraformMembers []string `json:"terraform_members"`
	// StructuralNames names the observed fields the claim consumes.
	StructuralNames []string `json:"structural_names"`
	// StructuralSource names the struct they all belong to, empty for the
	// lead. A claim whose fields span two structs has no surface for it.
	StructuralSource string `json:"structural_source,omitempty"`
	// Mapping names the two functions that relate them.
	Mapping *mappingFunctions `json:"mapping"`
	// Reason says why the relation is not one-to-one, in prose. Required
	// since the compiler can't check the relation itself.
	Reason string `json:"reason"`
}

// mappingFunctions names both directions of the relation between one
// Terraform attribute and the several observed fields behind it. Both
// halves are required, deliberately with no way to say "the same function,
// inverted": the two directions in this provider can be asymmetric (e.g.
// one direction compacts empty slots that the other clears positionally),
// so an inverse shortcut would silently misdescribe it.
//
// The functions are named, never inferred from field names: a rule guessed
// from a plausible-looking name has bound the wrong field before.
type mappingFunctions struct {
	// ToAPI builds the observed fields from the attribute's value. Required
	// on a surface that writes, and refused on one that doesn't -- a data
	// source never writes, so a to_api on one names a transform that can't
	// exist.
	ToAPI string `json:"to_api,omitempty"`
	// FromAPI builds the attribute's value from the observed fields.
	FromAPI string `json:"from_api"`
	// Kind says whether the named functions are the transform themselves or
	// merely contain it inline inside a larger conversion function -- two
	// different strengths of claim a reader can't tell apart from a name
	// alone. Required, and deliberately not defaulted.
	//
	//   dedicated  -- the function does this relation and nothing else.
	//   containing -- the relation is inline inside a larger function.
	Kind string `json:"kind"`
}

// mapping kinds. See mappingFunctions.Kind.
const (
	mappingDedicated  = "dedicated"
	mappingContaining = "containing"
)

// flatteningPolicy declares an observed nested struct whose members the
// provider presents as top-level Terraform attributes -- grouping inverted:
// a grouping invents a nested shape over flat fields, a flattening spreads
// an observed nested shape outward. Every member of the struct is either
// flattened or omitted, and each is consumed exactly once.
type flatteningPolicy struct {
	// StructuralName is the observed object field being spread.
	StructuralName string `json:"structural_name"`
	// StructuralSource names the struct it belongs to, empty for the lead.
	StructuralSource string            `json:"structural_source,omitempty"`
	Members          []flattenedMember `json:"members"`
}

// flattenedMember promotes one member of a nested struct to a top-level
// attribute. It carries the same decisions a top-level field policy does.
type flattenedMember struct {
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

// codeSpecification mirrors the root of the Provider Code Specification that
// tfplugingen-framework consumes. The member names ("datasources" as one
// word) are the generator's contract, not ours -- a surface under the wrong
// member is not rejected, it simply yields no generated code.
//
// ListResources and Actions are ours, not the generator's: the format has no
// sanctioned extension point for them, but tfplugingen-framework ignores
// members it doesn't know, so one document serves both the generator and
// our own emitter.
type codeSpecification struct {
	Version       string             `json:"version"`
	Provider      codeProvider       `json:"provider"`
	Resources     []codeResource     `json:"resources,omitempty"`
	DataSources   []codeDataSource   `json:"datasources,omitempty"`
	ListResources []codeListResource `json:"listresources,omitempty"`
	Actions       []codeAction       `json:"actions,omitempty"`
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

// codeListResource carries a list surface's config schema -- how a
// practitioner asks for a list, not what comes back. None of it derives
// from the SDK, so a list policy is entirely provider-owned.
type codeListResource struct {
	Name   string     `json:"name"`
	Schema codeSchema `json:"schema"`
}

// codeAction carries an action's schema, which is shaped like a resource's
// rather than like a list's.
type codeAction struct {
	Name   string     `json:"name"`
	Schema codeSchema `json:"schema"`
}

type codeSchema struct {
	Attributes []codeAttribute `json:"attributes"`
	// Blocks are a separate member of the specification, not a kind of
	// attribute; a surface that carries blocks must emit them here or lose
	// them entirely.
	Blocks              []codeAttribute `json:"blocks,omitempty"`
	MarkdownDescription string          `json:"markdown_description,omitempty"`
}

type codeAttribute struct {
	Name       string          `json:"name"`
	Type       string          `json:"-"`
	Definition json.RawMessage `json:"-"`
}

type mappingReport struct {
	FormatVersion int                    `json:"format_version"`
	SurfaceKind   SurfaceKind            `json:"surface_kind"`
	SurfaceName   string                 `json:"surface_name"`
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
