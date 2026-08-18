// Package resourcekit is the half of a managed resource that does not vary.
//
// MEASURED BEFORE IT WAS WRITTEN. unifi/dns_record_resource.go and its backend
// are 838 lines, of which 119 mention a field of the resource and 572 are fixed
// logic that varies by nothing but type names -- read the plan, apply a timeout,
// default the site, call the backend, write the state back. A package per
// resource regenerates those 572 lines once per surface; this package holds
// them once and each resource generates only the part that varies.
//
// WHY THE MAPPING IS CLOSURES RATHER THAN REFLECTION, which is the decision the
// rest of the design follows from. The framework will decode a plan into a
// types.Object and let the mapping be a table of strings, and that would be
// less generated code. It would also move every field's type and name from the
// compiler to run time, in the one place where the compiler is what holds the
// mapping correct -- and this repository has just finished removing a hundred
// unchecked type assertions for that reason. A closure that reaches a field is
// checked when it is built: a wrong SDK field name does not compile.
package resourcekit

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Field maps one attribute between the Terraform model M and the SDK struct S.
//
// Presence is the concept the two write paths share and the reason this is one
// interface rather than two. A create sends every managed value; an update
// sends only the attributes the PLAN set, so the same predicate decides which
// fields join the wire mask and which plan values overwrite state.
type Field[M any, S any] interface {
	// WireName is the SDK's own name for the attribute -- the structural_name
	// in the mapping, which is what the field-mask update puts on the wire. It
	// is not the Terraform name and the two differ often enough to matter:
	// dns_record's `name` is the controller's `key`.
	WireName() string

	// ToSDK writes the model's value onto the SDK struct.
	ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics

	// ToModel writes the SDK's value onto the model.
	ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics

	// SetInPlan reports whether the plan carries a value for this attribute.
	// Null and unknown both mean absent.
	SetInPlan(plan *M) bool

	// CopyPlanToState moves a set plan value onto the state, and does nothing
	// when the plan has none -- which is what preserves a computed value the
	// controller assigned.
	CopyPlanToState(plan, state *M)
}

// ElideZero says what an SDK zero value means for one attribute.
//
// It is NOT a formatting preference. An optional attribute the practitioner
// left out comes back from the controller as a zero, and writing that zero into
// state as a real value produces a permanent diff against a configuration that
// never mentioned the attribute. A required attribute has no such case, and
// nulling it on zero would erase a legitimately empty value.
//
// THE GENERATED SCHEMA carries the distinction, and the generator reads it from
// there rather than the author choosing it. Not the mapping: the string
// computed_optional_required appears in none of the 62 mapping files, only in
// the codegen scaffold tools, and this comment named the wrong source until a
// generated descriptor disagreed with a hand-written one and the disagreement
// had to be adjudicated. ElideProblems enforces the rule; nothing did before,
// and flipping every value in a descriptor left the provider suite green.
type ElideZero bool

const (
	KeepZero ElideZero = false // required, or computed with a default
	NullZero ElideZero = true  // optional: a zero is an absence
)

// StringField maps a types.String to a string.
type StringField[M any, S any] struct {
	Wire  string
	Model func(*M) *types.String
	SDK   func(*S) *string
	Elide ElideZero

	// WriteWhen suppresses the write when it returns false; nil means always.
	// See conditional_field.go for why this cannot be expressed by the SDK
	// accessor: that one takes *S and can decide, this one returns a pointer
	// and cannot.
	WriteWhen func(*M) bool

	// ReadDefault is the value the model takes when the controller reports the
	// attribute as empty. Empty means the field has none, which is unambiguous
	// because an empty default is exactly KeepZero.
	//
	// IT BELONGS TO THE FIELD RATHER THAN TO A HOOK, and that is the whole
	// reason it exists as a capability. AfterReceive would have served the
	// resource path, but List builds its models from ToModel WITHOUT running
	// any hook -- so the same object would have read one way through the
	// resource and another way through the list, which is the hook-symmetry
	// defect this kit already had once. ToModel is the single place both paths
	// share, so putting the substitution here makes it true everywhere by
	// construction rather than by remembering to wire it up.
	ReadDefault string
}

func (f StringField[M, S]) WireName() string { return f.Wire }

func (f StringField[M, S]) ToSDK(_ context.Context, model *M, sdk *S) diag.Diagnostics {
	if f.WriteWhen != nil && !f.WriteWhen(model) {
		return nil
	}
	value := f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	*f.SDK(sdk) = value.ValueString()
	return nil
}

func (f StringField[M, S]) ToModel(_ context.Context, sdk *S, model *M) diag.Diagnostics {
	raw := *f.SDK(sdk)
	if raw == "" && f.ReadDefault != "" {
		// Ahead of the elision, because a field carrying a default has no
		// absence to represent: the substitute IS what an empty read means.
		*f.Model(model) = types.StringValue(f.ReadDefault)
		return nil
	}
	if raw == "" && bool(f.Elide) {
		*f.Model(model) = types.StringNull()
		return nil
	}
	*f.Model(model) = types.StringValue(raw)
	return nil
}

func (f StringField[M, S]) SetInPlan(plan *M) bool {
	// The predicate gates the WIRE MASK as well as the write. A suppressed
	// field that still reported true would be named on the wire carrying
	// whatever the SDK struct held, which is worse than not suppressing.
	if f.WriteWhen != nil && !f.WriteWhen(plan) {
		return false
	}
	value := f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

func (f StringField[M, S]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}

// BoolField maps a types.Bool to a bool.
//
// No elision: a false is a value. An optional bool that the controller reports
// as false is indistinguishable from one the practitioner set to false, so
// nulling it would fight the configuration rather than agree with it.
type BoolField[M any, S any] struct {
	Wire  string
	Model func(*M) *types.Bool
	SDK   func(*S) *bool
}

func (f BoolField[M, S]) WireName() string { return f.Wire }

func (f BoolField[M, S]) ToSDK(_ context.Context, model *M, sdk *S) diag.Diagnostics {
	value := f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	*f.SDK(sdk) = value.ValueBool()
	return nil
}

func (f BoolField[M, S]) ToModel(_ context.Context, sdk *S, model *M) diag.Diagnostics {
	*f.Model(model) = types.BoolValue(*f.SDK(sdk))
	return nil
}

func (f BoolField[M, S]) SetInPlan(plan *M) bool {
	value := f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

func (f BoolField[M, S]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}

// Int64Field maps a types.Int64 to an int64.
type Int64Field[M any, S any] struct {
	Wire  string
	Model func(*M) *types.Int64
	SDK   func(*S) *int64
	Elide ElideZero
}

func (f Int64Field[M, S]) WireName() string { return f.Wire }

func (f Int64Field[M, S]) ToSDK(_ context.Context, model *M, sdk *S) diag.Diagnostics {
	value := f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	*f.SDK(sdk) = value.ValueInt64()
	return nil
}

func (f Int64Field[M, S]) ToModel(_ context.Context, sdk *S, model *M) diag.Diagnostics {
	raw := *f.SDK(sdk)
	if raw == 0 && bool(f.Elide) {
		*f.Model(model) = types.Int64Null()
		return nil
	}
	*f.Model(model) = types.Int64Value(raw)
	return nil
}

func (f Int64Field[M, S]) SetInPlan(plan *M) bool {
	value := f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

func (f Int64Field[M, S]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}

// Int64PtrField maps a types.Int64 to a *int64.
//
// A SEPARATE TYPE RATHER THAN A FLAG, because the pointer is a third state the
// SDK can express and the value types cannot: absent, present-and-zero, and
// present-and-set. dns_record's port is *int64 for exactly that reason, and the
// mapping artifact records it as int64 -- pointer-ness is one of the two facts
// the compiler does not currently carry.
type Int64PtrField[M any, S any] struct {
	Wire  string
	Model func(*M) *types.Int64
	SDK   func(*S) **int64
	Elide ElideZero
}

func (f Int64PtrField[M, S]) WireName() string { return f.Wire }

// ToSDK DOES NOT SKIP AN UNKNOWN, and that is bug-compatibility rather than a
// design.
//
// types.Int64Unknown().ValueInt64Pointer() returns a pointer to ZERO -- measured,
// not assumed -- so an unknown is indistinguishable from an explicit 0 by the
// time it reaches the wire. The hand-written resource calls that method
// unconditionally and therefore sends port: 0 for an unknown port. Skipping it
// here would send nothing, which is arguably right and is a DIFFERENT provider.
//
// The difference is believed unreachable for this attribute: port is optional
// and not computed, so Terraform resolves it to null or to the configured value
// and never to unknown. That belief is not a measurement and the case is not
// exercised anywhere, which is why the behaviour is reproduced rather than
// improved.
func (f Int64PtrField[M, S]) ToSDK(_ context.Context, model *M, sdk *S) diag.Diagnostics {
	*f.SDK(sdk) = f.Model(model).ValueInt64Pointer()
	return nil
}

func (f Int64PtrField[M, S]) ToModel(_ context.Context, sdk *S, model *M) diag.Diagnostics {
	raw := *f.SDK(sdk)
	if raw == nil || (*raw == 0 && bool(f.Elide)) {
		*f.Model(model) = types.Int64Null()
		return nil
	}
	// COPIED, not aliased. The SDK struct outlives this call in the caller's
	// hands, and handing Terraform state a pointer into it would let a later
	// mutation of the response change what state says was read.
	copied := *raw
	*f.Model(model) = types.Int64PointerValue(&copied)
	return nil
}

func (f Int64PtrField[M, S]) SetInPlan(plan *M) bool {
	value := f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

func (f Int64PtrField[M, S]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}

// DurationField maps a timetypes.GoDuration to an integer count of Units.
//
// THE UNIT IS THE ONE FACT NO INPUT CARRIES. The mapping records that ttl is
// int64 on the wire and string in Terraform, which is what says a conversion
// happens; it does not say the integer counts seconds. That has to be declared
// per field, and it is why this exists rather than being folded into Int64Field.
//
// The conversion itself is unifi/util's, not a second copy. Those two functions
// already define what this provider means by a duration on the wire -- including
// that sub-unit remainders truncate -- and a reimplementation here would be a
// second answer to a question that has one.
type DurationField[M any, S any] struct {
	Wire  string
	Model func(*M) *timetypes.GoDuration
	SDK   func(*S) *int64
	Units time.Duration
	Elide ElideZero
}

func (f DurationField[M, S]) WireName() string { return f.Wire }

func (f DurationField[M, S]) ToSDK(_ context.Context, model *M, sdk *S) diag.Diagnostics {
	value := f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	*f.SDK(sdk) = util.DurationUnits(*value, f.Units)
	return nil
}

func (f DurationField[M, S]) ToModel(_ context.Context, sdk *S, model *M) diag.Diagnostics {
	raw := *f.SDK(sdk)
	if raw == 0 && bool(f.Elide) {
		*f.Model(model) = timetypes.NewGoDurationNull()
		return nil
	}
	*f.Model(model) = util.DurationValue(raw, f.Units)
	return nil
}

func (f DurationField[M, S]) SetInPlan(plan *M) bool {
	value := f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

func (f DurationField[M, S]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}

// StringListField maps a types.List of strings to a []string.
//
// FOUND BY MEASURING THE SECOND RESOURCE, WHICH IS WHY THE INTERFACE HAS A
// CONTEXT AND DIAGNOSTICS AT ALL. dns_record is entirely scalar and gave no
// reason for either; firewall_zone's network_ids converts through the
// framework's ElementsAs, which needs a context and reports a type mismatch
// rather than panicking. A design settled on one resource had the wrong
// signature and would have had to change after two lanes built against it.
//
// THE SDK SLICE IS EMPTIED, NOT LEFT NIL, when the model has no value. The
// hand-written resource initialises NetworkIDs to []string{} before deciding
// whether to fill it, because a nil slice and an empty one serialise
// differently -- absent versus present-and-empty -- and the controller reads
// those as different requests.
type StringListField[M any, S any] struct {
	Wire  string
	Model func(*M) *types.List
	SDK   func(*S) *[]string
	// Elide answers whether an empty collection from the API is an absence.
	// It was missing from both collection types until a surface needed one,
	// and ElideProblems skipped fields without it -- so the omission hid
	// itself. Only an Optional-and-not-Computed attribute may null an empty:
	// an Optional+Computed one may have been set to an explicit empty by the
	// practitioner, and nulling that makes state disagree with config.
	Elide ElideZero
}

func (f StringListField[M, S]) WireName() string { return f.Wire }

func (f StringListField[M, S]) ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics {
	// See StringSetField.ToSDK: firewall_zone's network_ids is the field that
	// needs the seed, and it is a list.
	*f.SDK(sdk) = []string{}
	value := f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	return value.ElementsAs(ctx, f.SDK(sdk), false)
}

func (f StringListField[M, S]) ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics {
	if len(*f.SDK(sdk)) == 0 && bool(f.Elide) {
		*f.Model(model) = types.ListNull(types.StringType)
		return nil
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, *f.SDK(sdk))
	*f.Model(model) = list
	return diags
}

func (f StringListField[M, S]) SetInPlan(plan *M) bool {
	value := f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

func (f StringListField[M, S]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}

// StringSetField maps a types.Set of strings to a []string.
//
// WRITTEN BY THE DESCRIPTOR LANE, NOT BY sweep. Only one lane touches this
// package, and that is deliberate: the last time two lanes each built the
// permanent version of one thing, both shipped and the duplication was the
// defect. If you are about to add a field kind, check whether this one already
// covers it.
//
// A SET IS NOT A LIST WITH A DIFFERENT NAME, and the mapping distinguishes them
// for a reason a generator cannot infer: the framework compares set membership
// without order, so a controller that returns group members in a different
// sequence than the practitioner wrote produces no diff. Rendering the same
// data as a list makes that reordering a permanent change the practitioner
// cannot suppress. Seven field sites across four surfaces are declared set --
// device_macs, group_members, the two firewallgroup_ids and
// excluded_networkconf_ids -- and every one of them is a membership question.
//
// THE SDK SLICE IS EMPTIED, NOT LEFT NIL, for the reason StringListField gives:
// a nil slice and an empty one serialise as absent versus present-and-empty,
// and the controller reads those as different requests.
type StringSetField[M any, S any] struct {
	Wire  string
	Model func(*M) *types.Set
	SDK   func(*S) *[]string
	// Elide answers whether an empty collection from the API is an absence.
	// It was missing from both collection types until a surface needed one,
	// and ElideProblems skipped fields without it -- so the omission hid
	// itself. Only an Optional-and-not-Computed attribute may null an empty:
	// an Optional+Computed one may have been set to an explicit empty by the
	// practitioner, and nulling that makes state disagree with config.
	Elide ElideZero
}

func (f StringSetField[M, S]) WireName() string { return f.Wire }

func (f StringSetField[M, S]) ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics {
	// THE EMPTY SLICE IS DELIBERATE AND ONE SURFACE DEPENDS ON IT. A nil slice
	// and an empty one are the same JSON for a field tagged omitempty, and
	// three of the four collections served here are -- but FirewallZone's
	// network_ids is not, so nil marshals as null and empty marshals as [].
	// Its hand-written mapper seeded []string{} for exactly that reason. Moving
	// this line below the null check would send null where the controller was
	// being told the zone has no networks, and no test comparing Go structs
	// would see the difference.
	*f.SDK(sdk) = []string{}
	value := f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	return value.ElementsAs(ctx, f.SDK(sdk), false)
}

func (f StringSetField[M, S]) ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics {
	if len(*f.SDK(sdk)) == 0 && bool(f.Elide) {
		*f.Model(model) = types.SetNull(types.StringType)
		return nil
	}
	set, diags := types.SetValueFrom(ctx, types.StringType, *f.SDK(sdk))
	*f.Model(model) = set
	return diags
}

func (f StringSetField[M, S]) SetInPlan(plan *M) bool {
	value := f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

func (f StringSetField[M, S]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}

// BoolPtrField maps a types.Bool to a *bool.
//
// A pointer bool has the three states a bool cannot: unset, false, true.
// firewall_zone's default_zone is one, and reading it through BoolField would
// turn "the controller did not say" into "the controller said false".
type BoolPtrField[M any, S any] struct {
	Wire  string
	Model func(*M) *types.Bool
	SDK   func(*S) **bool
}

func (f BoolPtrField[M, S]) WireName() string { return f.Wire }

func (f BoolPtrField[M, S]) ToSDK(_ context.Context, model *M, sdk *S) diag.Diagnostics {
	*f.SDK(sdk) = f.Model(model).ValueBoolPointer()
	return nil
}

func (f BoolPtrField[M, S]) ToModel(_ context.Context, sdk *S, model *M) diag.Diagnostics {
	*f.Model(model) = types.BoolPointerValue(*f.SDK(sdk))
	return nil
}

func (f BoolPtrField[M, S]) SetInPlan(plan *M) bool {
	value := f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

func (f BoolPtrField[M, S]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}

// StringPtrField maps a types.String to a *string.
type StringPtrField[M any, S any] struct {
	Wire  string
	Model func(*M) *types.String
	SDK   func(*S) **string
}

func (f StringPtrField[M, S]) WireName() string { return f.Wire }

func (f StringPtrField[M, S]) ToSDK(_ context.Context, model *M, sdk *S) diag.Diagnostics {
	*f.SDK(sdk) = f.Model(model).ValueStringPointer()
	return nil
}

func (f StringPtrField[M, S]) ToModel(_ context.Context, sdk *S, model *M) diag.Diagnostics {
	*f.Model(model) = types.StringPointerValue(*f.SDK(sdk))
	return nil
}

func (f StringPtrField[M, S]) SetInPlan(plan *M) bool {
	value := f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

func (f StringPtrField[M, S]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}

// ReadOnly wraps a field the controller owns: read from the API, never sent.
//
// DERIVED FROM THE POLICY RATHER THAN CHOSEN. A field whose
// computed_optional_required is "computed" is one the practitioner cannot set,
// so sending it would overwrite a controller-assigned value with whatever the
// model happened to hold. firewall_zone has three -- _id, default_zone and
// zone_key -- and its hand-written modelToFirewallZone sends exactly the other
// two, name and network_ids, which are required and computed_optional.
//
// MEASURED ON TWO RESOURCES, WHICH IS NOT MANY. dns_record has no computed
// field at all and sends all eight of its own, so it is consistent with the rule
// and cannot confirm it. Treat this as the shape rather than the law until
// somebody counts across every policy.
//
// BUT THREE INDEPENDENT AUTHORITIES ALREADY AGREE ON firewall_zone'"'"'S SET, which
// is why the shape is worth trusting further than one sample usually would:
//
//	the policy          dispositions _id, zone_key and default_zone "computed"
//	the hand-written    modelToFirewallZone sends exactly the other two
//	the SDK itself      FirewallZone.MarshalJSON drops those three from a write,
//	                    shadowing each with a *struct{} -- because the controller
//	                    REJECTS them on a write
//
// Three separately-authored things, none deriving from the others, naming the
// same set. That is one resource'"'"'s worth of agreement rather than twenty-seven,
// and it is much stronger than one resource'"'"'s worth of assertion.
//
// A DECORATOR RATHER THAN A FLAG ON EACH KIND, so read-only-ness is one
// implementation instead of seven, and so a kind added later gets it for free.
func ReadOnly[M any, S any](inner Field[M, S]) Field[M, S] {
	return readOnlyField[M, S]{inner: inner}
}

type readOnlyField[M any, S any] struct{ inner Field[M, S] }

func (f readOnlyField[M, S]) WireName() string { return f.inner.WireName() }

// Unwrap exposes the wrapped field so a check can reach its Elide.
//
// WITHOUT IT THE WRAPPER HIDES THE CLAIM. ElideProblems reflects on a field's
// own type, so a read-only field reported as "carries no Elide" -- which is
// false: the inner field has one, and it still governs how an API zero reaches
// the model, because ToModel is forwarded even though ToSDK is not. Exempting
// the wrapper would have silenced the check on every computed field instead.
func (f readOnlyField[M, S]) Unwrap() Field[M, S] { return f.inner }

// ToSDK does nothing. The field never reaches the controller.
func (f readOnlyField[M, S]) ToSDK(context.Context, *M, *S) diag.Diagnostics { return nil }

func (f readOnlyField[M, S]) ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics {
	return f.inner.ToModel(ctx, sdk, model)
}

// SetInPlan is always false, which keeps the field out of the update's wire
// mask. A computed attribute appearing in a mask would ask the controller to
// accept a value it is the author of.
func (f readOnlyField[M, S]) SetInPlan(*M) bool { return false }

// CopyPlanToState does nothing: there is no plan value to carry, and the state
// already holds what the controller last reported.
func (f readOnlyField[M, S]) CopyPlanToState(*M, *M) {}

// DurationPtrField maps a timetypes.GoDuration to a *int64 of some unit.
//
// SEPARATE FROM DurationField BECAUSE THE POINTER IS THE DISTINCTION THAT
// MATTERS. dns_record's ttl is an int64 where zero means unset, and its Elide
// says so. port_profile's dot1x_idle_timeout is a *int64, where nil and a
// pointer to zero are different things the controller distinguishes -- so this
// one leaves the pointer nil rather than writing a zero through it, and reads
// nil back as null without consulting Elide at all.
type DurationPtrField[M any, S any] struct {
	Wire  string
	Model func(*M) *timetypes.GoDuration
	SDK   func(*S) **int64
	Units time.Duration

	// Elide governs only a pointer to the zero value. A nil pointer is always
	// null, because there is nothing else it could mean.
	Elide ElideZero
}

func (f DurationPtrField[M, S]) WireName() string { return f.Wire }

func (f DurationPtrField[M, S]) ToSDK(_ context.Context, model *M, sdk *S) diag.Diagnostics {
	value := f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	seconds := util.DurationUnits(*value, f.Units)
	*f.SDK(sdk) = &seconds
	return nil
}

func (f DurationPtrField[M, S]) ToModel(_ context.Context, sdk *S, model *M) diag.Diagnostics {
	raw := *f.SDK(sdk)
	if raw == nil {
		*f.Model(model) = timetypes.NewGoDurationNull()
		return nil
	}
	if *raw == 0 && bool(f.Elide) {
		*f.Model(model) = timetypes.NewGoDurationNull()
		return nil
	}
	*f.Model(model) = util.DurationValue(*raw, f.Units)
	return nil
}

func (f DurationPtrField[M, S]) SetInPlan(plan *M) bool {
	value := f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

func (f DurationPtrField[M, S]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}
