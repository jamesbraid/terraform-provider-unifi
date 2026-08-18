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
// The mapping already carries the distinction as computed_optional_required, so
// the generator sets this rather than the author choosing it.
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
}

func (f StringField[M, S]) WireName() string { return f.Wire }

func (f StringField[M, S]) ToSDK(_ context.Context, model *M, sdk *S) diag.Diagnostics {
	value := f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	*f.SDK(sdk) = value.ValueString()
	return nil
}

func (f StringField[M, S]) ToModel(_ context.Context, sdk *S, model *M) diag.Diagnostics {
	raw := *f.SDK(sdk)
	if raw == "" && bool(f.Elide) {
		*f.Model(model) = types.StringNull()
		return nil
	}
	*f.Model(model) = types.StringValue(raw)
	return nil
}

func (f StringField[M, S]) SetInPlan(plan *M) bool {
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
}

func (f StringListField[M, S]) WireName() string { return f.Wire }

func (f StringListField[M, S]) ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics {
	*f.SDK(sdk) = []string{}
	value := f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	return value.ElementsAs(ctx, f.SDK(sdk), false)
}

func (f StringListField[M, S]) ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics {
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
