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
	// Create POSTs a NEW object, and that is an assumption rather than a
	// description. The controller holds nothing yet, so a whole-object body is
	// correct and a field the practitioner left unset takes a controller
	// default rather than overwriting a live one.
	//
	// A SURFACE THAT ADOPTS AN EXISTING OBJECT CANNOT USE THIS -- it wants
	// CreateFields. unifi_device is the case: the device exists with its full
	// configuration before Terraform first names it, so writing the whole
	// object asserts a zero for every attribute the plan did not set.
	Create func(ctx context.Context, site string, in *S) (*S, error)
	// CreateFields is the field-masked create, for a surface whose "create" is
	// a PATCH of an object the controller already holds.
	//
	// unifi_device is the case and so far the only one. A device is not made by
	// the provider, it is ADOPTED: the object exists with its full config
	// before Terraform ever names it. ToSDK builds the SDK object from the
	// PLAN, so every attribute the practitioner did not write is elided to its
	// zero value -- and a whole-object create would assert those zeros over the
	// config the device already carries. Create-means-make-a-new-object holds
	// for every other surface here, where an unset field takes a controller
	// default rather than clobbering a live one.
	//
	// The mask is the same one Update uses: WireFields over the plan. Exactly
	// one of Create and CreateFields must be set.
	CreateFields func(ctx context.Context, site string, in *S, fields ...string) (*S, error)
	Read         func(ctx context.Context, site, id string) (*S, error)
	UpdateFields func(ctx context.Context, site string, in *S, fields ...string) (*S, error)
	// Update is the whole-object write, for the five SDK types that have no
	// Update<T>Fields: BGPConfig, PowerSupervisor, Setting, Site and
	// WireGuardPeer. Exactly one of Update and UpdateFields must be set.
	//
	// It is NOT a way to send everything. When it is used the kit fetches the
	// current object and applies only the masked fields onto it, so the object
	// that goes back is the one that came from Get -- which is the difference
	// between a whole-object write that preserves unmanaged fields and one that
	// resets them. See buildUpdateBody.
	Update func(ctx context.Context, site string, in *S) (*S, error)
	Delete func(ctx context.Context, site, id string) error
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
	//
	// BEFORESEND TAKES TWO MODELS AND THEY ANSWER DIFFERENT QUESTIONS.
	//
	// config is what the practitioner WROTE, with everything they omitted left
	// null. A hook that refuses an illegal combination has to read this one:
	// port_profile rejects tagged_networkconf_ids together with
	// excluded_networkconf_ids, and judging that against anything else would
	// reject a configuration nobody wrote.
	//
	// effective is the model the SDK object was just built FROM -- the plan on
	// a create, and the state with the plan applied on an update. A hook that
	// DERIVES a value reads this one, because it needs what the object will
	// actually carry, including attributes the plan left alone.
	//
	// Update passed the raw plan here until radius_user needed the difference:
	// it derives an account's VLAN from network_id whenever vlan is not set,
	// and against the raw plan an unchanged vlan reads as unset, so a VLAN the
	// practitioner had pinned would be silently re-derived on the next apply
	// that touched anything else. The hook exists to adjust the object ToSDK
	// produced, so handing it a different model than ToSDK used was the seam.
	//
	// AFTERRECEIVE TAKES THE PRIOR MODEL, AND UNTIL IT DID, HALF OF WHAT PEOPLE
	// REACHED FOR IT FOR WAS IMPOSSIBLE.
	//
	// The read path loads prior state into the model, runs Spec.ToModel, and
	// only then calls this hook. The boundary is ownership, not timing:
	//
	//   an attribute NO Field touches   still holds its prior value in model,
	//                                   which is how device's port_override is
	//                                   reconstructed from the managed set
	//                                   rather than from every port the switch
	//                                   reports
	//   an attribute a Field DECODES    has already been overwritten in model,
	//                                   and is readable only through prior
	//
	// Two surfaces were written against the belief that the second case worked
	// anyway, and vpn_client's wireguard field recorded it as the kit's answer.
	// port_forward is where it was found to be false, and vpn_client is where
	// being false stopped being a documentation problem: the practitioner
	// supplies a wireguard config FILE, the provider parses it and sends the
	// controller manual mode, and the controller reports manual mode forever.
	// Five attributes have to be carried forward from what was there before,
	// and without prior a create with a configuration block ends in "provider
	// produced inconsistent result after apply" -- a failed apply, not a diff.
	//
	// prior is the plan on create, the state on read, and the state with the
	// plan applied on update. Update passes the EFFECTIVE model rather than the
	// raw plan for the reason BeforeSend takes both: an attribute the plan does
	// not mention is null in the plan and present in the state, so the raw plan
	// would make an apply that changed only the name look like one that cleared
	// everything it never mentioned. A list hands a zero model, because nothing
	// was recorded for an object being discovered.
	Prefetch     func(ctx context.Context, site string) (any, diag.Diagnostics)
	BeforeSend   func(ctx context.Context, config, effective *M, sdk *S, prefetched any) diag.Diagnostics
	AfterReceive func(ctx context.Context, sdk *S, model *M, prior M, prefetched any) diag.Diagnostics

	// BeforeDelete decides whether destroying the resource destroys the object.
	// Returning false drops it from state and leaves the controller alone.
	//
	// DESTROYING A RESOURCE IS NOT ALWAYS DESTROYING A THING. unifi_device is
	// the case: a device is physical, so the provider cannot delete one. All it
	// can do is forget it, which unadopts real hardware, and the schema makes
	// that opt-in through forget_on_destroy. Without this hook the kit's Delete
	// would forget every device on every destroy -- a resource removal silently
	// unadopting hardware the practitioner asked it not to touch.
	//
	// Backend.Delete takes site and id, so it cannot see the attribute that
	// decides. This runs where the model is still in hand.
	BeforeDelete func(ctx context.Context, model *M) (bool, diag.Diagnostics)

	// AlwaysWire names wire fields that BeforeSend sets, so they join the
	// update mask whether or not the plan mentions them. Only for values a
	// hook derives: a field the practitioner sets belongs in Fields, where
	// SetInPlan decides and an unchanged attribute is left alone.
	//
	// WireNameProblems checks every name against the SDK's json tags, because
	// a typo here would drop the field from the mask -- the same silent
	// write-drop, reached through the fix for it.
	AlwaysWire []string

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
		// EVERY name the field maps, not one. A scattered object spans several
		// flat SDK attributes, and a mask carrying one of them writes one of
		// them while the apply succeeds.
		for _, name := range fieldWireNames[M, S](field) {
			if _, duplicate := seen[name]; duplicate {
				return nil, fmt.Errorf("%s patch names %q twice", s.TypeName, name)
			}
			seen[name] = struct{}{}
			fields = append(fields, name)
		}
	}
	// FIELDS A HOOK DERIVES JOIN THE MASK UNCONDITIONALLY, because nothing in
	// the plan can put them there.
	//
	// port_profile is the case. Its BeforeSend computes tagged_vlan_mgmt,
	// excluded_networkconf_ids and forward from tagged_networkconf_ids, which
	// is not a Field at all -- it has no SDK counterpart and is reconstructed
	// on read. So the practitioner changes an attribute that is in the plan,
	// and the three attributes that actually carry the change are not.
	//
	// They usually would be: all three are Optional+Computed, so a plan whose
	// config omits them inherits the prior state value. But the read mapper
	// nulls each one when the controller reports it empty, so after an import
	// or a create that came back empty the state holds null, the plan holds
	// null, and the derived value is computed and then not sent. An update
	// that silently writes nothing is exactly what the mask exists to prevent,
	// so this does not depend on inferring what the framework puts in a plan.
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
//
// It exists for the contract check rather than for the runtime: the mapping
// artifact says which fields a surface has, the descriptor says which it
// touches, and until something compares the two a field can be added to the
// policy and silently never mapped. Asking the spec rather than parsing the
// generated Go keeps the check on the thing that runs.
func (s Spec[M, S]) WireNames() []string {
	names := make([]string, 0, len(s.Fields))
	for _, field := range s.Fields {
		names = append(names, fieldWireNames[M, S](field)...)
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
	created, err := r.createObject(ctx, site, sdk, &data)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating "+r.Spec.Subject, err.Error())
		return
	}
	// THE PLAN IS THE PRIOR HERE. data holds what the practitioner wrote until
	// ToModel overwrites it, so a hook that has to carry a value forward from
	// the configuration gets it from this copy and from nowhere else.
	prior := data
	resp.Diagnostics.Append(r.Spec.ToModel(ctx, created, &data, site)...)
	resp.Diagnostics.Append(r.afterReceive(ctx, created, &data, prior, prefetched)...)
	if resp.Diagnostics.HasError() {
		return
	}
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
	// THE STATE IS THE PRIOR HERE -- what the last apply recorded.
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
	// The body is built BEFORE BeforeSend, because on the whole-object path the
	// object that gets sent is the one fetched from the controller rather than
	// the one ToSDK produced -- and a hook that derived its wire form on the
	// wrong object would be silently discarded.
	body, bodyDiags := r.buildUpdateBody(ctx, site, id, sdk, fields, &state)
	resp.Diagnostics.Append(bodyDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if r.Spec.BeforeSend != nil {
		var config M
		resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
		if resp.Diagnostics.HasError() {
			return
		}
		resp.Diagnostics.Append(r.Spec.BeforeSend(ctx, &config, &state, body, prefetched)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	var updated *S
	if r.Spec.Backend.UpdateFields != nil {
		updated, err = r.Spec.Backend.UpdateFields(ctx, site, body, fields...)
	} else {
		updated, err = r.Spec.Backend.Update(ctx, site, body)
	}
	if err != nil {
		resp.Diagnostics.AddError("Error Updating "+r.Spec.Subject, err.Error())
		return
	}
	// THE EFFECTIVE MODEL IS THE PRIOR HERE, not the raw plan, for the reason
	// BeforeSend takes both: an attribute the plan does not mention is absent
	// from it and present in the state, and a hook carrying a value forward
	// wants what the object was actually built from.
	prior := state
	resp.Diagnostics.Append(r.Spec.ToModel(ctx, updated, &state, site)...)
	resp.Diagnostics.Append(r.afterReceive(ctx, updated, &state, prior, prefetched)...)
	*r.Spec.Timeouts(&state) = *r.Spec.Timeouts(&plan)
	resp.Diagnostics.Append(
		resp.Identity.SetAttribute(ctx, path.Root("id"), (*r.Spec.ID(&state)))...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// buildUpdateBody returns the object to send.
//
// With a masked update it is what ToSDK produced: go-unifi's maskedBody keeps
// only the named keys, so everything else in the struct is discarded before the
// wire and unmanaged fields are never at risk.
//
// WITHOUT ONE the whole struct goes, and a struct built from the model carries a
// Go zero for every field the schema does not declare. So the object is fetched
// and the masked fields are applied ONTO IT -- the object passed to Update is
// then the one that came back from Get, which is the only property that
// distinguishes a safe whole-object write from a destructive one. The provider
// already does this by hand in four places: setting_resource.go's mgmt, radius,
// igmpSnooping and usg mappers all open with `setting := base`.
//
// The mask is still honoured rather than ignored: only fields the plan set are
// copied across. A field the practitioner did not mention keeps the controller's
// value instead of the model's zero.
func (r *Resource[M, S]) buildUpdateBody(
	ctx context.Context,
	site, id string,
	fromModel *S,
	fields []string,
	model *M,
) (*S, diag.Diagnostics) {
	var diags diag.Diagnostics
	if r.Spec.Backend.UpdateFields != nil {
		return fromModel, diags
	}
	// A descriptor with neither would nil-panic at the send, which is a stack
	// trace rather than a diagnostic and points at the kit rather than at the
	// descriptor that is actually wrong.
	if r.Spec.Backend.Update == nil {
		diags.AddError("Error Updating "+r.Spec.Subject,
			r.Spec.TypeName+" declares neither Backend.UpdateFields nor Backend.Update, "+
				"so there is no way to write it")
		return nil, diags
	}

	current, err := r.Spec.Backend.Read(ctx, site, id)
	if err != nil {
		diags.AddError("Error Updating "+r.Spec.Subject,
			"could not read the current "+r.Spec.Subject+" to build a whole-object "+
				"update: "+err.Error())
		return nil, diags
	}

	masked := make(map[string]struct{}, len(fields))
	for _, name := range fields {
		masked[name] = struct{}{}
	}
	for _, field := range r.Spec.Fields {
		// ANY of the field's names being masked applies it. For every kind but
		// one that is a single name; for a scattered object the names go into
		// the mask together, so any of them answers the same question --
		// indexing the first would read as if position meant something and
		// would panic on a field declaring none.
		applies := false
		for _, name := range fieldWireNames[M, S](field) {
			if _, ok := masked[name]; ok {
				applies = true
				break
			}
		}
		if applies {
			diags.Append(field.ToSDK(ctx, model, current)...)
		}
	}
	r.Spec.Backend.SetID(current, id)
	return current, diags
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

// createObject sends the new object, masked when the surface asked for that.
//
// A DESCRIPTOR WITH NEITHER WOULD NIL-PANIC AT THE SEND, which is a stack trace
// pointing at the kit rather than a diagnostic pointing at the descriptor that
// is actually wrong. The update path already guards this way; create did not,
// because until CreateFields existed there was only one field to forget.
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
