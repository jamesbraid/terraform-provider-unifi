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
	// Companions are the further SDK structs a surface projects. The lead stays
	// in Resource rather than becoming the first companion, because it is not a
	// peer: the surface's identity, its baseline key and its conversion file
	// all follow the lead.
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
	Flattenings               []flatteningPolicy        `json:"flattenings,omitempty"`
	Claims                    []claimPolicy             `json:"claims,omitempty"`
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
	StructuralName string `json:"structural_name"`
	// StructuralSource names the SDK struct StructuralName belongs to, when the
	// surface projects more than one. It is empty for the lead struct, so every
	// policy written before companions existed means exactly what it did.
	//
	// PER ATTRIBUTE rather than per group, because a group can span structs:
	// client's qos_rate takes its `id` from Client.usergroup_id and its other
	// three members from ClientGroup, in one grouping.
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
	// says why. It is the same declaration a grouped member already carries,
	// permitted here because a nested shape can invent a member too: wlan's
	// schedule block presents a single day_of_week string over an SDK array of
	// them, which no derivation produces and no cardinality rule may allow.
	//
	// The reason is required for the same cause as everywhere else — a flag is
	// something set to pass a gate — and an invented member must declare its
	// own terraform_type, because there is no observed field to take one from.
	Invented string `json:"invented,omitempty"`
}

// groupingPolicy declares a nested Terraform attribute the SDK does not have.
//
// Most nested attributes mirror an SDK struct and their members are derived
// from the catalog. Thirty-three do not: the provider invented the shape over
// flat SDK fields, so there is no struct to observe and the grouping has to be
// declared. It is still a migration rather than hand-authoring, because every
// member names a field the catalog observed, and the compiler proves each
// observed field is consumed exactly once across the whole policy.
//
// Resolving a Terraform path back to an API path therefore has two cases and
// they go opposite ways. Under Fields the parent IS an observed struct, so the
// structural path EXTENDS: destination.ip_group_id resolves to
// destination.ip_group_id. Under a grouping the parent is invented and has no
// API counterpart, so the path RESETS to the member's flat field:
// dhcp_server.enabled resolves to dhcpd_enabled, not to
// dhcp_server.dhcpd_enabled.
//
// Stated here because the two shapes are indistinguishable from a member's own
// JSON -- only its position says which rule applies -- so a walker that handles
// Fields alone resolves most attributes and silently reports the grouped ones
// as unmapped. That reads as a finding about the provider rather than a gap in
// the walker, which is the expensive way to discover this.
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
	// StructuralSource names the SDK struct StructuralName belongs to, when the
	// surface projects more than one. It is empty for the lead struct, so every
	// policy written before companions existed means exactly what it did.
	//
	// PER ATTRIBUTE rather than per group, because a group can span structs:
	// client's qos_rate takes its `id` from Client.usergroup_id and its other
	// three members from ClientGroup, in one grouping.
	StructuralSource string          `json:"structural_source,omitempty"`
	TerraformName    string          `json:"terraform_name"`
	TerraformType    string          `json:"terraform_type,omitempty"`
	Disposition      string          `json:"disposition"`
	Attribute        json.RawMessage `json:"attribute,omitempty"`
	// Fields holds the per-member decisions when the observed field this member
	// consumes is itself an object or array<object>.
	//
	// Without it a grouping could only consume flat fields. wan needs both:
	// dhcp.options consumes wan_dhcp_options, an array of NetworkWANDHCPOptions,
	// and dhcpv6.options does the same one level along. The descent already
	// existed for a top-level field -- buildCodeAttribute reaches
	// nestedDefinition either way -- so the member type simply had no way to
	// carry the decisions to it, and the compiler refused with "member
	// optionNumber is unclassified".
	//
	// This is the same member list a top-level object field declares, and it is
	// governed by the same rule: every member of the observed struct is either
	// classified or omitted, so nothing is dropped without someone deciding to
	// drop it.
	Fields []fieldPolicy `json:"fields,omitempty"`
	// Invented records that this member corresponds to no observed field at
	// all. Two exist in the estate: port_forward's source_limiting.type, which
	// is computed from whether a firewall group is set, and bgp's peers, which
	// the provider parses out of an opaque config string. Both are stated here
	// so the ledger cannot report them as derived, and the reason is required
	// so the claim is legible rather than a flag someone sets to pass a gate.
	Invented string `json:"invented,omitempty"`
	// ElementMember names which member of an observed array<object> this member
	// presents, when the released attribute is a list of SCALARS over an
	// element the SDK models as a struct.
	//
	// traffic_route's destination.domain is the case and the only one in the
	// estate: the SDK carries []TrafficRouteDomains{domain, port_ranges, ports}
	// and the released attribute is a list of strings. Every other list on a
	// blocked surface is already array<string>.
	//
	// It is stated rather than derived from "whichever member is not omitted",
	// and the redundancy with Fields is the point: derived, omitting a second
	// member would silently change the attribute's element type. Stated, it is
	// cross-checked three ways against the catalog -- the member exists, it is
	// the only one not omitted, and its observed type is the declared element
	// type -- which is what separates this from a mapping, whose function names
	// nothing can verify.
	ElementMember string `json:"element_member,omitempty"`
}

// claimPolicy relates a SET of schema members to a SET of observed fields
// through a named pair of functions.
//
// It is the one mechanism for every relation that is not one-to-one, and the
// ordinary cases are its degenerate forms: a member naming one field is 1x1, an
// invented member is 1x0, an omitted field is 0x1. Everything else is a claim.
//
// It has to be declared here rather than on a member, because the relation is
// not a property of any one member. Three scopes were measured and only this
// reaches all three:
//
//	within ONE grouping       traffic_route  source.{clients,networks} <- target_devices
//	across SIBLING groupings  vpn_server     {wireguard.port, openvpn.port} <- local_port
//	at the TOP LEVEL          network        {purpose, third_party_gateway} <- purpose
//
// EXACTLY ONCE IS UNCHANGED, and deliberately stronger than the per-arm rule
// this replaced. Every observed field appears in exactly one claim, and every
// schema member appears in exactly one claim, so two members that co-claim a
// field must be listed TOGETHER under ONE named function. That listing is
// exactly what a reviewer has to check, which is why the format forces it into
// one place rather than letting two members each assert the field separately.
type claimPolicy struct {
	// TerraformMembers names the schema members by path: a top-level field by
	// its terraform_name, a grouping member as "grouping.member".
	TerraformMembers []string `json:"terraform_members"`
	// StructuralNames names the observed fields the claim consumes.
	StructuralNames []string `json:"structural_names"`
	// StructuralSource names the struct they all belong to, empty for the lead.
	// A claim whose fields span TWO structs has no surface asking for it, and
	// would need a source per name rather than one for the claim.
	StructuralSource string `json:"structural_source,omitempty"`
	// Mapping names the two functions that relate them.
	Mapping *mappingFunctions `json:"mapping"`
	// Reason says why the relation is not one-to-one, in prose. Required for
	// the same cause as everywhere else here: a flag is something set to pass a
	// gate, and the compiler cannot check the relation, so the reader has to be
	// able to.
	Reason string `json:"reason"`
}

// mappingFunctions names both directions of the relation between one Terraform
// attribute and the several observed fields behind it.
//
// BOTH HALVES ARE REQUIRED, and there is deliberately no way to say "the same
// function, inverted". The two directions in this provider are different
// functions and are not inverses. network's dhcp_server.dns_servers WRITES
// positionally into dhcpd_dns_1..4, clearing the trailing slots it does not use
// and silently dropping a fifth value; it READS with collectNonEmptyStrings,
// which COMPACTS. So {"", "b", ""} on the wire reads as ["b"] and writes back
// as slot 1 = "b" -- the value moves slot. And within that one resource,
// dhcp_guarding.servers writes positionally WITHOUT clearing the trailing
// slots. A vocabulary that could not express that difference could not describe
// this provider, and an inverse shortcut would have blessed it silently.
//
// They are NAMES, never inferences. The compiler cannot see how a provider
// divides a value, and a rule guessed from field names is the mistake this
// pipeline has already shipped twice -- static_route's `type` and wlan's
// `schedule` both matched a plausible name and bound the wrong field. A named
// function is a claim someone wrote down and that a reader can open and check.
type mappingFunctions struct {
	// ToAPI builds the observed fields from the attribute's value.
	//
	// Required on a surface that writes, and REFUSED on one that does not. A
	// data source never writes, so a to_api on one names a transform that
	// cannot exist -- and three of them carried one, six of those verbatim
	// copies of the managed resource's write-function names. That is a false
	// claim rather than a missing function, so the compiler rejects it instead
	// of asking someone to supply a name.
	ToAPI string `json:"to_api,omitempty"`
	// FromAPI builds the attribute's value from the observed fields.
	FromAPI string `json:"from_api"`
	// Kind says whether the named functions ARE the transform or merely
	// CONTAIN it, because those are different strengths of claim and a reader
	// cannot tell them apart from a name.
	//
	//   dedicated  -- the function does this relation and nothing else, so
	//                 opening it shows the relation.
	//   containing -- the relation is inline inside a larger conversion
	//                 function, so opening it shows the relation among many
	//                 others and the reader still has to find it.
	//
	// Required, and deliberately not defaulted. Every claim in this provider is
	// "containing" today: the conversions live in two large per-resource
	// functions rather than one helper each. Naming the containing function is
	// true and openable; naming a helper that does not exist was neither, and
	// read as the stronger claim. Making the weaker claim say its name is what
	// stops the distinction being lost silently.
	Kind string `json:"kind"`
}

// mapping kinds. See mappingFunctions.Kind.
const (
	mappingDedicated  = "dedicated"
	mappingContaining = "containing"
)

// flatteningPolicy declares an observed nested struct whose members the
// provider presents as top-level Terraform attributes.
//
// This is grouping inverted. A grouping invents a nested shape over flat
// observed fields; a flattening spreads an observed nested shape outward.
// power_supervisor does the second: the SDK carries a Settings struct and the
// schema presents its three members as heartbeat_interval, silence_threshold
// and power_off_duration at the top level.
//
// The accounting is the same in both directions — every member of the struct is
// either flattened or omitted, and each is consumed exactly once — because the
// risk is the same: a member silently presented twice, or dropped without
// anyone deciding to drop it.
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
	// Action is the primary digest when the surface is an action. There is one
	// action in the estate and it has no companions.
	Action string `json:"action,omitempty"`
}

type baselineManifest struct {
	SchemaSHA256 map[string]string `json:"schema_sha256"`
}

// codeSpecification mirrors the root of the Provider Code Specification that
// tfplugingen-framework consumes. The member names are the generator's
// contract rather than ours: it reads "datasources" as one word, and a
// specification that puts a surface under the wrong member is not rejected,
// it simply yields no generated code.
// codeSpecification is our document, not HashiCorp's. resources, datasources,
// provider and version are theirs and mean what their schema says. listresources
// and actions are ours, added because the format has carried the same four
// members since 0.2.0 in September 2024 while terraform-plugin-framework grew
// list and action packages, and the format offers no sanctioned extension point.
//
// They are siblings of resources rather than a new grammar: a list config schema
// is a string attribute and one nested block, which the existing vocabulary
// already expresses. So if the concept is ever defined upstream this is a key
// rename, not a redesign.
//
// tfplugingen-framework ignores members it does not know — verified, not assumed:
// a document carrying listresources still generates its resources cleanly, so one
// document serves both the generator and our own emitter.
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

// codeListResource carries a list surface's CONFIG schema — how a practitioner
// asks for a list, not what comes back. Every one in this estate is an optional
// site string plus a filter block, and none of it derives from the SDK, so a
// list policy is entirely provider-owned.
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
	// attribute. Terraform treats the two differently — configuration written
	// for one does not parse as the other — so a surface that carries blocks
	// must emit them here or lose them entirely.
	Blocks              []codeAttribute `json:"blocks,omitempty"`
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
