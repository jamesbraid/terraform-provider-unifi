package resourcekit

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
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
const DefaultTimeout = 20 * time.Minute

// Backend is the SDK client, reached through closures rather than an interface
// the SDK would have to satisfy.
type Backend[S any] struct {
	// Create POSTs a whole new object. A surface that adopts an existing one
	// (unifi_device) must use CreateFields instead, or an unset plan field
	// gets asserted to zero over the object's live config.
	Create func(ctx context.Context, site string, in *S) (*S, error)
	// CreateFields is the field-masked create, for a surface whose "create" is
	// a PATCH of an object the controller already holds (unifi_device, so
	// far the only one). Exactly one of Create and CreateFields must be set.
	CreateFields func(ctx context.Context, site string, in *S, fields ...string) (*S, error)
	Read         func(ctx context.Context, site, id string) (*S, error)
	// ReadByName resolves the human handle an import supplies, for the
	// surfaces whose documented import id is a name rather than the
	// controller's 24-hex id. Optional; see Spec.Name.
	ReadByName   func(ctx context.Context, site, name string) (*S, error)
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
	// carries it as a managed field rather than as provider_owned. It exists
	// only so the contract check can exclude it: Backend.GetID reaches the
	// identity either way, so no descriptor ever maps it as a Field.
	IDWire string

	// New builds a zero SDK struct. A generated one-liner, because Go cannot
	// instantiate S from a type parameter without a constraint that would
	// exclude the SDK's own types.
	New func() *S

	// Prefetch runs before the object is built and its result is handed to both
	// other hooks. BeforeSend may mutate the SDK object; AfterReceive may write
	// model attributes the field list does not cover.
	Prefetch func(ctx context.Context, site string) (any, diag.Diagnostics)
	// BeforeSend: config is what the practitioner wrote (null where omitted);
	// effective is what the object was built from (plan on create, state with
	// plan applied on update). Derive values from effective, not the plan, or
	// an unrelated apply can silently re-derive a value the practitioner pinned.
	BeforeSend func(ctx context.Context, config, effective *M, sdk *S, prefetched any) diag.Diagnostics
	// AfterReceive's model has already been overwritten by ToModel for every
	// Field-decoded attribute; prior is the plan on create, the state on read,
	// and the state with the plan applied on update. An attribute no Field
	// touches keeps its prior value in model instead (e.g. device's
	// port_override). A list hands a zero prior, since nothing was recorded.
	AfterReceive func(ctx context.Context, sdk *S, model *M, prior M, prefetched any) diag.Diagnostics

	// BeforeDelete decides whether destroying the resource destroys the object.
	// Returning false drops it from state and leaves the controller alone.
	BeforeDelete func(ctx context.Context, model *M) (bool, diag.Diagnostics)

	// UnwritableWires reports wire names this object's encoder will not emit,
	// so the mask can drop them instead of the write being refused: go-unifi
	// hard-errors on a masked name that's absent and never emitted, but sends
	// zero for one that's absent yet the encoder would otherwise emit it --
	// only the former should be reported here.
	UnwritableWires func(sdk *S) []string

	// AlwaysWire names wire fields that BeforeSend sets, so they join the
	// update mask even when the plan doesn't mention them. Only for
	// hook-derived values; a practitioner-set field belongs in Fields.
	AlwaysWire []string

	// ID, Site and Timeouts reach the three attributes every managed surface
	// has and no policy declares as a field -- they are provider_owned in the
	// mapping, which is why they are here rather than in Fields.
	ID       func(*M) *types.String
	Site     func(*M) *types.String
	Timeouts func(*M) *timeouts.Value

	// Name opts the surface into import by name, together with
	// Backend.ReadByName. Nil means the surface imports by id alone.
	Name func(*M) *types.String
}

// WireFields lists the SDK names of every attribute the plan set, for a
// masked update. An empty result errors rather than sending nothing: a
// masked update naming no field is a no-op patch, indistinguishable from
// success while changing nothing the plan asked for.
func (s Spec[M, S]) WireFields(plan *M) ([]string, error) {
	fields := make([]string, 0, len(s.Fields))
	seen := make(map[string]struct{}, len(s.Fields))
	for _, field := range s.Fields {
		if !field.SetInPlan(plan) {
			continue
		}
		// Every name the field WILL write, not every name it can: a scattered
		// object masking a wire its Encode left alone would send that wire's
		// zero and clear the controller's value. See
		// ScatteredObjectField.ConditionalWires.
		for _, name := range fieldMaskWireNames[M, S](field, plan) {
			if _, duplicate := seen[name]; duplicate {
				return nil, fmt.Errorf("%s patch names %q twice", s.TypeName, name)
			}
			seen[name] = struct{}{}
			fields = append(fields, name)
		}
	}
	// Fields a hook derives join the mask unconditionally, since nothing in
	// the plan can put them there.
	for _, name := range s.AlwaysWire {
		if _, duplicate := seen[name]; duplicate {
			continue
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
// It exists for the contract check that compares this against the mapping
// artifact, so a field added to the policy can't go silently unmapped.
func (s Spec[M, S]) WireNames() []string {
	names := make([]string, 0, len(s.Fields))
	for _, field := range s.Fields {
		names = append(names, fieldWireNames[M, S](field)...)
	}
	return names
}

// ToSDK renders a model as the SDK struct the controller is sent. The
// diagnostics matter: a collection field's ElementsAs conversion can report a
// type mismatch instead of panicking, and dropping that ships a half-built
// object silently.
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
	s.copyUncoveredPlanValues(plan, state)
}

// copyUncoveredPlanValues moves every set plan value no Field claims -- needed
// because a hook-derived attribute (e.g. network's vlan) has no Field to carry
// the plan's change forward. A known, non-null plan value wins; null and
// unknown are left alone, matching CopyPlanToState's own rule.
func (s Spec[M, S]) copyUncoveredPlanValues(plan, state *M) {
	covered := map[uintptr]struct{}{}
	// The kit-owned attributes are not the plan's to assert: the id is the
	// controller's answer, and site and timeouts have their own handling.
	if s.ID != nil {
		covered[reflect.ValueOf(s.ID(state)).Pointer()] = struct{}{}
	}
	if s.Site != nil {
		covered[reflect.ValueOf(s.Site(state)).Pointer()] = struct{}{}
	}
	if s.Timeouts != nil {
		covered[reflect.ValueOf(s.Timeouts(state)).Pointer()] = struct{}{}
	}
	for _, field := range s.Fields {
		if wrapper, ok := field.(interface{ Unwrap() Field[M, S] }); ok {
			field = wrapper.Unwrap()
		}
		accessor := reflect.ValueOf(field).FieldByName("Model")
		if !accessor.IsValid() || accessor.Kind() != reflect.Func || accessor.IsNil() {
			continue
		}
		results := accessor.Call([]reflect.Value{reflect.ValueOf(state)})
		if len(results) == 1 && results[0].Kind() == reflect.Ptr {
			covered[results[0].Pointer()] = struct{}{}
		}
	}
	stateValue := reflect.ValueOf(state).Elem()
	planValue := reflect.ValueOf(plan).Elem()
	for i := range stateValue.NumField() {
		target := stateValue.Field(i)
		if !target.CanSet() {
			continue
		}
		if _, claimed := covered[target.Addr().Pointer()]; claimed {
			continue
		}
		value, ok := planValue.Field(i).Interface().(attr.Value)
		if !ok || value.IsNull() || value.IsUnknown() {
			continue
		}
		target.Set(planValue.Field(i))
	}
}

// Resource is the framework implementation every managed surface shares.
//
// Every surface must declare an identity schema: Create, Read and Update call
// resp.Identity.SetAttribute unconditionally, and one that doesn't fails at
// apply with an opaque framework error. Override IdentitySchema only if the
// surface keys on something other than id, as client does on mac.
type Resource[M any, S any] struct {
	Spec        Spec[M, S]
	DefaultSite string
	// ListSurface is the list surface, when the resource has one (see
	// list.go). Named ListSurface rather than List because list.ListResource
	// requires a method of that name, which a field called List would shadow.
	ListSurface ListSpec[S]
	// SchemaSpec is the schema, its version, and any state upgraders.
	SchemaSpec SchemaSpec
}

// nullTimeouts is the value a listed object carries for an attribute only a
// configuration can supply. A list result has no timeouts block, and leaving
// the field at its Go zero value would put an untyped null into a typed
// object, which the framework refuses.
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

	// Config is read ahead of ToSDK for BeforeSend, which needs to see what
	// the practitioner actually wrote.
	var config M
	if r.Spec.BeforeSend != nil {
		resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	sdk, diags := r.Spec.ToSDK(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.Spec.BeforeSend != nil {
		resp.Diagnostics.Append(r.Spec.BeforeSend(ctx, &config, &data, sdk, prefetched)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	created, err := r.createObject(ctx, site, sdk, &data)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating "+r.Spec.Subject, err.Error())
		return
	}
	// prior is the plan here, before ToModel overwrites data.
	prior := data
	resp.Diagnostics.Append(r.Spec.ToModel(ctx, created, &data, site)...)
	resp.Diagnostics.Append(r.afterReceive(ctx, created, &data, prior, prefetched)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The response doesn't outrank the plan for a value the plan set (a
	// partial encoder response would otherwise read as an inconsistent
	// result); the response only fills what the plan left null or unknown.
	r.Spec.ApplyPlanToState(&prior, &data)
	resp.Diagnostics.Append(
		resp.Identity.SetAttribute(ctx, path.Root("id"), (*r.Spec.ID(&data)))...)
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
	var found *S
	var err error
	if id == "" && r.Spec.Name != nil && r.Spec.Backend.ReadByName != nil {
		// No id means a name import landed here; a name that resolves to
		// nothing is reported as a failed import rather than a deletion.
		name := (*r.Spec.Name(&data)).ValueString()
		found, err = r.Spec.Backend.ReadByName(ctx, site, name)
		if err != nil {
			resp.Diagnostics.AddError("Error Reading "+r.Spec.Subject,
				"Could not read "+r.Spec.Subject+" with name "+name+": "+err.Error())
			return
		}
	} else {
		found, err = r.Spec.Backend.Read(ctx, site, id)
		if err != nil {
			// A deleted resource is a state to record, not an error: removing
			// it lets the next plan recreate it.
			var notFound *ui.NotFoundError
			if errors.As(err, &notFound) {
				resp.State.RemoveResource(ctx)
				return
			}
			resp.Diagnostics.AddError("Error Reading "+r.Spec.Subject,
				"Could not read "+r.Spec.Subject+" with ID "+id+": "+err.Error())
			return
		}
	}
	// AfterReceive runs here too, or an attribute the field list can't
	// express would be populated on create and blank on every refresh.
	prefetched, prefetchDiags := r.prefetch(ctx, site)
	resp.Diagnostics.Append(prefetchDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	// prior is the state here -- what the last apply recorded.
	prior := data
	resp.Diagnostics.Append(r.Spec.ToModel(ctx, found, &data, site)...)
	resp.Diagnostics.Append(r.afterReceive(ctx, found, &data, prior, prefetched)...)
	resp.Diagnostics.Append(
		resp.Identity.SetAttribute(ctx, path.Root("id"), (*r.Spec.ID(&data)))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *Resource[M, S]) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var state, plan M
	// State first, and it matters: the update sends the state's values with
	// the plan's changes applied, so an unmentioned attribute keeps what the
	// controller last reported instead of reverting to a zero.
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

	// Config is read ahead of the mask and ToSDK for the same reason as
	// Create: BeforeSend needs to see what the practitioner actually wrote.
	var config M
	if r.Spec.BeforeSend != nil {
		resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

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

	prefetched, prefetchDiags := r.prefetch(ctx, site)
	resp.Diagnostics.Append(prefetchDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if r.Spec.Backend.UpdateFields == nil {
		resp.Diagnostics.AddError("Error Updating "+r.Spec.Subject,
			r.Spec.TypeName+" declares no Backend.UpdateFields, so there is no way to write it")
		return
	}

	if r.Spec.BeforeSend != nil {
		resp.Diagnostics.Append(r.Spec.BeforeSend(ctx, &config, &state, sdk, prefetched)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	if r.Spec.UnwritableWires != nil {
		unwritable := make(map[string]struct{})
		for _, name := range r.Spec.UnwritableWires(sdk) {
			unwritable[name] = struct{}{}
		}
		kept := fields[:0:0]
		for _, name := range fields {
			if _, drop := unwritable[name]; !drop {
				kept = append(kept, name)
			}
		}
		if len(kept) == 0 {
			resp.Diagnostics.AddError("Error Updating "+r.Spec.Subject,
				r.Spec.TypeName+" reports every field in its update mask as unwritable, "+
					"so the write would say nothing; this is a descriptor fault rather "+
					"than a configuration one")
			return
		}
		fields = kept
	}

	updated, err := r.Spec.Backend.UpdateFields(ctx, site, sdk, fields...)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating "+r.Spec.Subject, err.Error())
		return
	}
	// prior is state (with plan applied) here, not the raw plan: an attribute
	// the plan doesn't mention is absent there but present in state.
	prior := state
	resp.Diagnostics.Append(r.Spec.ToModel(ctx, updated, &state, site)...)
	resp.Diagnostics.Append(r.afterReceive(ctx, updated, &state, prior, prefetched)...)
	// The same plan-over-response rule as Create's tail, with the raw plan.
	r.Spec.ApplyPlanToState(&plan, &state)
	*r.Spec.Timeouts(&state) = *r.Spec.Timeouts(&plan)
	resp.Diagnostics.Append(
		resp.Identity.SetAttribute(ctx, path.Root("id"), (*r.Spec.ID(&state)))...)
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

	// A not-found delete succeeds on every resource the kit serves: delete's
	// goal state is already reached when the object is absent, and erroring
	// there leaves state Terraform won't release.
	if r.Spec.BeforeDelete != nil {
		proceed, deleteDiags := r.Spec.BeforeDelete(ctx, &data)
		resp.Diagnostics.Append(deleteDiags...)
		if resp.Diagnostics.HasError() {
			return
		}
		if !proceed {
			return
		}
	}

	if err := r.Spec.Backend.Delete(
		ctx,
		r.Site(&data),
		(*r.Spec.ID(&data)).ValueString(),
	); err != nil {
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
	handle := req.ID
	if handle == "" && req.Identity != nil {
		// An import block's identity = {...} (Terraform 1.12+) leaves req.ID
		// empty; core hands the handle over via req.Identity instead, per the
		// one-attribute identity schema every surface shares.
		var identityID types.String
		resp.Diagnostics.Append(req.Identity.GetAttribute(ctx, path.Root("id"), &identityID)...)
		if resp.Diagnostics.HasError() {
			return
		}
		handle = identityID.ValueString()
	}
	parts := strings.Split(handle, ":")
	switch len(parts) {
	case 2:
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), parts[0])...)
		handle = parts[1]
	case 1:
	default:
		resp.Diagnostics.AddError("Invalid Import ID",
			"Import ID must be in format 'site:id' or 'id'")
		return
	}

	// A surface with a name lookup accepts a human handle: an explicit
	// "name=" prefix, or anything that isn't a 24-hex controller id.
	if r.Spec.Name != nil && r.Spec.Backend.ReadByName != nil {
		if name, ok := strings.CutPrefix(handle, "name="); ok {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
			return
		}
		if !controllerID.MatchString(handle) {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), handle)...)
			return
		}
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), handle)...)
	// Identity is set here too, not only in Read: the framework pre-populates
	// the post-import read's identity from this response, and leaving it null
	// turns a clean not-found into "Missing Resource Identity After Read"
	// instead of the normal not-found message.
	if resp.Identity != nil {
		resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), handle)...)
	}
}

// controllerID is the shape of every controller-assigned object id. A handle
// that does not match it cannot be one, which is what lets a bare name be
// told apart from an id without asking anyone.
var controllerID = regexp.MustCompile(`^[0-9a-f]{24}$`)

// prefetch reads whatever the resource needs beyond its own object, or nothing.
func (r *Resource[M, S]) prefetch(ctx context.Context, site string) (any, diag.Diagnostics) {
	if r.Spec.Prefetch == nil {
		return nil, nil
	}
	return r.Spec.Prefetch(ctx, site)
}

func (r *Resource[M, S]) afterReceive(
	ctx context.Context,
	sdk *S,
	model *M,
	prior M,
	prefetched any,
) diag.Diagnostics {
	if r.Spec.AfterReceive == nil {
		return nil
	}
	return r.Spec.AfterReceive(ctx, sdk, model, prior, prefetched)
}

// SchemaSpec is the schema half of a resource, all of it generated or declared.
type SchemaSpec struct {
	// Resource is the tfplugingen-framework output for this surface. Passed
	// rather than derived: the generated function is named per package and the
	// kit cannot reach it generically.
	Resource func(context.Context) schema.Schema

	// Version is the schema version this resource serves. Getting it wrong
	// doesn't error -- Terraform just skips the upgrader silently, and state
	// stays in the old shape forever.
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

// createObject sends the new object, masked when the surface asked for that.
// A descriptor with neither would nil-panic at the send -- a stack trace
// pointing at the kit instead of a diagnostic naming the descriptor that's
// actually wrong.
func (r *Resource[M, S]) createObject(
	ctx context.Context,
	site string,
	sdk *S,
	plan *M,
) (*S, error) {
	if r.Spec.Backend.CreateFields == nil {
		if r.Spec.Backend.Create == nil {
			return nil, fmt.Errorf(
				"%s declares neither Backend.Create nor Backend.CreateFields, "+
					"so there is no way to create it", r.Spec.TypeName)
		}
		return r.Spec.Backend.Create(ctx, site, sdk)
	}
	if r.Spec.Backend.Create != nil {
		return nil, fmt.Errorf(
			"%s declares both Backend.Create and Backend.CreateFields; "+
				"exactly one of them writes the new object", r.Spec.TypeName)
	}
	fields, err := r.Spec.WireFields(plan)
	if err != nil {
		return nil, err
	}
	return r.Spec.Backend.CreateFields(ctx, site, sdk, fields...)
}
