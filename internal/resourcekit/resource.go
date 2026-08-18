package resourcekit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// DefaultTimeout is what every operation gets when the configuration sets none.
//
// One value rather than four, because all four hand-written resources used
// 20*time.Minute for all four operations and no reason was ever recorded for it
// to differ per operation. A resource that needs its own can set Timeouts.
const DefaultTimeout = 20 * time.Minute

// Backend is the SDK client, reached through closures rather than an interface
// the SDK would have to satisfy.
//
// GENERATED PER RESOURCE AND CHECKED BY THE COMPILER. The method names are the
// one part of a resource that no artifact carries: CreateDNSRecord and
// ListDNSRecord follow from the struct name, and UpdateDNSRecordFields does
// not -- a field-masked update is a different method from a whole-object one,
// and choosing it is a decision about what the provider may overwrite.
type Backend[S any] struct {
	Create       func(ctx context.Context, site string, in *S) (*S, error)
	Read         func(ctx context.Context, site, id string) (*S, error)
	UpdateFields func(ctx context.Context, site string, in *S, fields ...string) (*S, error)
	Delete       func(ctx context.Context, site, id string) error
	// List is only needed by a surface that registers a list resource, which
	// is 25 of the 27. Nil on the rest.
	List func(ctx context.Context, site string) ([]S, error)

	// ID reads and writes the SDK struct's identity. Separate accessors rather
	// than a field name, for the same reason as everything else here: a wrong
	// one does not compile.
	GetID func(*S) string
	SetID func(*S, string)
}

// Spec is everything about one resource that varies.
type Spec[M any, S any] struct {
	// TypeName is the suffix, without the provider prefix: "dns_record".
	TypeName string
	// Subject names the resource in diagnostics: "Dns Record".
	Subject string
	Fields  []Field[M, S]
	Backend Backend[S]
	// IDWire is the SDK's own name for the identity field, when the mapping
	// carries it as a managed field rather than as provider_owned.
	//
	// IT EXISTS FOR THE CONTRACT CHECK AND FOR NOTHING ELSE. dns_record's
	// mapping puts id under provider_owned; firewall_zone's puts _id in the
	// managed field list. The identity is reached through Backend.GetID either
	// way, so a descriptor never maps it as a field -- and a check comparing the
	// managed set against the descriptor would report it permanently absent on
	// the resources of the second kind. Declaring it excludes exactly one name
	// rather than hardcoding an exemption.
	IDWire string

	// New builds a zero SDK struct. A generated one-liner, because Go cannot
	// instantiate S from a type parameter without a constraint that would
	// exclude the SDK's own types.
	New func() *S

	// THE THREE HOOKS, AND THEY EXIST BECAUSE port_profile FORCED THEM.
	//
	// A resource whose wire form is a pure function of its own attributes needs
	// none of these, which is every simple surface. port_profile is not one: the
	// practitioner declares which networks ARE tagged and the controller accepts
	// only which are EXCLUDED, so the complement has to be computed against the
	// site's whole network inventory -- an object this resource does not own and
	// has to fetch. 340 of its 1,321 lines are that inversion.
	//
	// NONE OF IT IS GENERATED AND NONE OF IT SHOULD BE. It is semantics, not
	// mapping: a set complement against a separately-fetched inventory is a
	// decision about what the provider means, and a generator that emitted it
	// would be encoding a policy nobody wrote down. What the kit provides is the
	// place to call it from, so the 693 lines of CRUD around it still collapse.
	//
	// Prefetch runs before the object is built and its result is handed to both
	// other hooks. BeforeSend may mutate the SDK object; AfterReceive may write
	// model attributes the field list does not cover.
	Prefetch     func(ctx context.Context, site string) (any, diag.Diagnostics)
	BeforeSend   func(ctx context.Context, config, plan *M, sdk *S, prefetched any) diag.Diagnostics
	AfterReceive func(ctx context.Context, sdk *S, model *M, prefetched any) diag.Diagnostics

	// ID, Site and Timeouts reach the three attributes every managed surface
	// has and no policy declares as a field -- they are provider_owned in the
	// mapping, which is why they are here rather than in Fields.
	//
	// CLOSURES RATHER THAN AN INTERFACE, and the reason is Go rather than
	// taste. An interface with setters needs pointer receivers, which makes the
	// constraint *M and leaves the generic code unable to declare a value of M
	// to decode into. The two-type-parameter workaround for that propagates
	// through every signature in this package. Accessors cost the generator
	// three lines and are checked the same way the fields are.
	ID       func(*M) *types.String
	Site     func(*M) *types.String
	Timeouts func(*M) *timeouts.Value
}

// WireFields lists the SDK names of every attribute the plan set.
//
// THE MASK IS THE POINT OF THE UPDATE PATH. Sending the whole object would
// overwrite controller-owned attributes this provider does not model, which is
// the defect a field-masked update exists to avoid. An empty mask is refused
// rather than sent, because an update that names no field is a whole-object
// write by another route.
func (s Spec[M, S]) WireFields(plan *M) ([]string, error) {
	fields := make([]string, 0, len(s.Fields))
	seen := make(map[string]struct{}, len(s.Fields))
	for _, field := range s.Fields {
		if !field.SetInPlan(plan) {
			continue
		}
		name := field.WireName()
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("%s patch names %q twice", s.TypeName, name)
		}
		seen[name] = struct{}{}
		fields = append(fields, name)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%s patch needs at least one managed field", s.TypeName)
	}
	return fields, nil
}

// WireNames lists every SDK attribute this spec maps, in declaration order.
//
// It exists for the contract check rather than for the runtime: the mapping
// artifact says which fields a surface has, the descriptor says which it
// touches, and until something compares the two a field can be added to the
// policy and silently never mapped. Asking the spec rather than parsing the
// generated Go keeps the check on the thing that runs.
func (s Spec[M, S]) WireNames() []string {
	names := make([]string, 0, len(s.Fields))
	for _, field := range s.Fields {
		names = append(names, field.WireName())
	}
	return names
}

// ToSDK renders a model as the SDK struct the controller is sent.
//
// THE DIAGNOSTICS ARE NOT DECORATION. A collection attribute converts through
// the framework's ElementsAs, which reports a type mismatch rather than
// panicking, and dropping that report would send a half-built object to the
// controller with nothing said. The scalar fields never produce one; the
// signature exists for the fields that can.
func (s Spec[M, S]) ToSDK(ctx context.Context, model *M) (*S, diag.Diagnostics) {
	var diags diag.Diagnostics
	sdk := s.New()
	for _, field := range s.Fields {
		diags.Append(field.ToSDK(ctx, model, sdk)...)
	}
	return sdk, diags
}

// ToModel writes what the controller returned back onto the model.
func (s Spec[M, S]) ToModel(ctx context.Context, sdk *S, model *M, site string) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, field := range s.Fields {
		diags.Append(field.ToModel(ctx, sdk, model)...)
	}
	*s.ID(model) = types.StringValue(s.Backend.GetID(sdk))
	*s.Site(model) = types.StringValue(site)
	return diags
}

// ApplyPlanToState moves the plan's set values onto the state, leaving the rest.
func (s Spec[M, S]) ApplyPlanToState(plan, state *M) {
	for _, field := range s.Fields {
		field.CopyPlanToState(plan, state)
	}
}

// A SURFACE MUST DECLARE AN IDENTITY SCHEMA. Create, Read and Update all call
// resp.Identity.SetAttribute unconditionally, so a resource reaching them
// without one fails at apply time with "the resource does not indicate support
// via a resource identity schema" -- a runtime error for a wiring mistake. The
// kit's own IdentitySchema serves the single "id" attribute every managed
// surface here uses; override it only if the surface keys on something else,
// as client does on mac.
//
// Resource is the framework implementation every managed surface shares.
type Resource[M any, S any] struct {
	Spec        Spec[M, S]
	DefaultSite string
	// ListSurface is the list surface, when the resource has one. See list.go.
	//
	// NAMED ListSurface RATHER THAN List so the METHOD can be called List:
	// list.ListResource requires a method of that name, and a field would
	// shadow it on any type embedding this one.
	ListSurface ListSpec[S]
	// SchemaSpec is the schema, its version, and any state upgraders.
	SchemaSpec SchemaSpec
}

// nullTimeouts is the value a listed object carries for an attribute that only
// a configuration can supply.
//
// A LIST RESULT IS NOT A MANAGED RESOURCE and has no timeouts block, so leaving
// the field at its zero value would put an untyped null into a typed object and
// the framework would refuse the whole result.
func nullTimeouts() timeouts.Value {
	return timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{
		"create": types.StringType,
		"read":   types.StringType,
		"update": types.StringType,
		"delete": types.StringType,
	})}
}

func (r *Resource[M, S]) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_" + r.Spec.TypeName
}

// Site resolves the configured site against the provider default. An empty
// site in state means "wherever the provider points", not the empty string.
func (r *Resource[M, S]) Site(model *M) string {
	if site := r.Spec.Site(model).ValueString(); site != "" {
		return site
	}
	return r.DefaultSite
}

func (r *Resource[M, S]) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data M
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := (*r.Spec.Timeouts(&data)).Create(ctx, DefaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	site := r.Site(&data)
	prefetched, prefetchDiags := r.prefetch(ctx, site)
	resp.Diagnostics.Append(prefetchDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	sdk, diags := r.Spec.ToSDK(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.Spec.BeforeSend != nil {
		// THE CONFIG, NOT ONLY THE PLAN. A computed attribute is unknown in the
		// config and resolved in the plan, and port_profile's inversion needs
		// what the practitioner WROTE rather than what Terraform derived.
		var config M
		resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
		if resp.Diagnostics.HasError() {
			return
		}
		resp.Diagnostics.Append(r.Spec.BeforeSend(ctx, &config, &data, sdk, prefetched)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	created, err := r.Spec.Backend.Create(ctx, site, sdk)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating "+r.Spec.Subject, err.Error())
		return
	}
	resp.Diagnostics.Append(r.Spec.ToModel(ctx, created, &data, site)...)
	resp.Diagnostics.Append(r.afterReceive(ctx, created, &data, prefetched)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), (*r.Spec.ID(&data)))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *Resource[M, S]) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data M
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := (*r.Spec.Timeouts(&data)).Read(ctx, DefaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	site := r.Site(&data)
	id := (*r.Spec.ID(&data)).ValueString()
	found, err := r.Spec.Backend.Read(ctx, site, id)
	if err != nil {
		// A DELETED RESOURCE IS NOT AN ERROR, it is a state to record. Removing
		// it lets the next plan recreate it; reporting it makes the practitioner
		// remove it by hand.
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading "+r.Spec.Subject,
			"Could not read "+r.Spec.Subject+" with ID "+id+": "+err.Error())
		return
	}
	// AfterReceive runs here for the same reason it runs after Create's write:
	// a surface whose model carries attributes the field list cannot express
	// would have them populated on create and blank on every refresh, which
	// reads as the controller having dropped them.
	prefetched, prefetchDiags := r.prefetch(ctx, site)
	resp.Diagnostics.Append(prefetchDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.Spec.ToModel(ctx, found, &data, site)...)
	resp.Diagnostics.Append(r.afterReceive(ctx, found, &data, prefetched)...)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), (*r.Spec.ID(&data)))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *Resource[M, S]) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var state, plan M
	// STATE FIRST AND IT MATTERS. The update sends the state's values with the
	// plan's changes applied, so an attribute the plan does not mention keeps
	// what the controller last reported rather than reverting to a zero.
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := (*r.Spec.Timeouts(&plan)).Update(ctx, DefaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r.Spec.ApplyPlanToState(&plan, &state)
	site := r.Site(&state)

	fields, err := r.Spec.WireFields(&plan)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating "+r.Spec.Subject, err.Error())
		return
	}
	sdk, sdkDiags := r.Spec.ToSDK(ctx, &state)
	resp.Diagnostics.Append(sdkDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := (*r.Spec.ID(&state)).ValueString()
	if id == "" {
		resp.Diagnostics.AddError("Error Updating "+r.Spec.Subject,
			r.Spec.Subject+" patch has an empty ID")
		return
	}
	r.Spec.Backend.SetID(sdk, id)

	// THE HOOKS RAN ON CREATE ONLY, AND THAT IS THE DEFECT THIS CLOSES.
	//
	// Prefetch, BeforeSend and AfterReceive were declared, documented for
	// port_profile's tagged-network inversion, and wired into Create alone. A
	// surface using them would have derived its wire form correctly on the
	// first apply and silently stopped on every update -- which is worse than
	// no hook, because the first apply looks right and the second sends a
	// different object. Nothing has used them yet, which is why nobody hit it.
	//
	// The config is re-read for the reason Create gives: a computed attribute
	// is unknown in the config and resolved in the plan, and an inversion needs
	// what the practitioner WROTE.
	prefetched, prefetchDiags := r.prefetch(ctx, site)
	resp.Diagnostics.Append(prefetchDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.Spec.BeforeSend != nil {
		var config M
		resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
		if resp.Diagnostics.HasError() {
			return
		}
		resp.Diagnostics.Append(r.Spec.BeforeSend(ctx, &config, &plan, sdk, prefetched)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	updated, err := r.Spec.Backend.UpdateFields(ctx, site, sdk, fields...)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating "+r.Spec.Subject, err.Error())
		return
	}
	resp.Diagnostics.Append(r.Spec.ToModel(ctx, updated, &state, site)...)
	resp.Diagnostics.Append(r.afterReceive(ctx, updated, &state, prefetched)...)
	*r.Spec.Timeouts(&state) = *r.Spec.Timeouts(&plan)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), (*r.Spec.ID(&state)))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *Resource[M, S]) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data M
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := (*r.Spec.Timeouts(&data)).Delete(ctx, DefaultTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// A NOT-FOUND DELETE SUCCEEDS, ON EVERY RESOURCE. James decided this rather
	// than it being per-resource, and it is a behaviour CHANGE on the ten that
	// currently report the error -- dns_record, network, radius_user, setting,
	// site, vpn_client, vpn_server, wan, wireguard_peer and wlan.
	//
	// The reasoning is that delete is the one operation whose goal state is
	// already reached when the object is absent. Reporting an error there leaves
	// the practitioner with state Terraform will not release and a resource
	// nobody can remove without editing state by hand, which is the worst
	// outcome available for an object that is already gone.
	//
	// USER-VISIBLE, so it needs a release note when this lands.
	if err := r.Spec.Backend.Delete(ctx, r.Site(&data), (*r.Spec.ID(&data)).ValueString()); err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return
		}
		resp.Diagnostics.AddError("Error Deleting "+r.Spec.Subject, err.Error())
		return
	}
}

// ImportState accepts "site:id" or "id".
//
// The form is the provider's, not the resource's: every managed surface is
// scoped by site and identified by an opaque controller id, so this is one
// implementation rather than twenty-seven identical ones.
func (r *Resource[M, S]) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	parts := strings.Split(req.ID, ":")
	switch len(parts) {
	case 2:
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), parts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
	case 1:
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	default:
		resp.Diagnostics.AddError("Invalid Import ID",
			"Import ID must be in format 'site:id' or 'id'")
	}
}

// prefetch reads whatever the resource needs beyond its own object, or nothing.
func (r *Resource[M, S]) prefetch(ctx context.Context, site string) (any, diag.Diagnostics) {
	if r.Spec.Prefetch == nil {
		return nil, nil
	}
	return r.Spec.Prefetch(ctx, site)
}

func (r *Resource[M, S]) afterReceive(ctx context.Context, sdk *S, model *M, prefetched any) diag.Diagnostics {
	if r.Spec.AfterReceive == nil {
		return nil
	}
	return r.Spec.AfterReceive(ctx, sdk, model, prefetched)
}

// SchemaSpec is the schema half of a resource, all of it generated or declared.
type SchemaSpec struct {
	// Resource is the tfplugingen-framework output for this surface. Passed
	// rather than derived: the generated function is named per package and the
	// kit cannot reach it generically.
	Resource func(context.Context) schema.Schema

	// Version is the schema version this resource serves.
	//
	// DECLARED RATHER THAN DEFAULTED TO ZERO. A resource that has ever migrated
	// its state carries a non-zero version forever, and getting it wrong does
	// not error -- Terraform simply does not run the upgrader, and the
	// practitioner's state stays in the old shape while the provider reads it
	// as the new one. dns_record is at 1 because its ttl moved from seconds to
	// a duration string.
	Version int64

	// Timeouts says which operations accept a timeout. Every managed surface in
	// this provider accepts all four.
	Timeouts timeouts.Opts

	// Upgraders migrate prior state, keyed by the version being upgraded FROM.
	// Genuinely per-resource: what changed between two versions of one schema
	// is not derivable from either.
	Upgraders func(context.Context, schema.Schema) map[int64]resource.StateUpgrader
}

func (r *Resource[M, S]) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = r.SchemaSpec.Resource(ctx)
	resp.Schema.Version = r.SchemaSpec.Version
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx, r.SchemaSpec.Timeouts)
}

// IdentitySchema is one attribute on every managed surface in this provider:
// the controller's opaque id, required for import.
func (r *Resource[M, S]) IdentitySchema(
	_ context.Context,
	_ resource.IdentitySchemaRequest,
	resp *resource.IdentitySchemaResponse,
) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{RequiredForImport: true},
		},
	}
}

// UpgradeState hands the upgraders the schema they are migrating TO, which is
// the one thing they all need and none of them can build.
func (r *Resource[M, S]) UpgradeState(ctx context.Context) map[int64]resource.StateUpgrader {
	if r.SchemaSpec.Upgraders == nil {
		return nil
	}
	var built resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &built)
	return r.SchemaSpec.Upgraders(ctx, built.Schema)
}
