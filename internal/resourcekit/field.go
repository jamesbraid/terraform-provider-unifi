// Package resourcekit is the half of a managed resource that does not vary.
package resourcekit

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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

// ElideZero says what an SDK zero value means for one attribute: for an
// optional one it's an absence the controller reports as zero, and writing
// that zero into state as a real value would produce a permanent diff. A
// required attribute has no such case, and nulling it would erase a
// legitimately empty value.
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
	// attribute as empty. It lives on the field rather than a hook because
	// List builds its models from ToModel without running any hook, so a
	// hook-based default would read one way through the resource and another
	// through the list.
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
	// This gates the wire mask as well as the write: a suppressed field
	// reporting true would be named on the wire carrying whatever the SDK
	// struct held, which is worse than not suppressing.
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

	// WriteWhen suppresses the write when it returns false; nil means always.
	// Same contract as StringField's, and it gates the wire mask as well as
	// the write for the same reason.
	WriteWhen func(*M) bool
}

func (f BoolField[M, S]) WireName() string { return f.Wire }

func (f BoolField[M, S]) ToSDK(_ context.Context, model *M, sdk *S) diag.Diagnostics {
	if f.WriteWhen != nil && !f.WriteWhen(model) {
		return nil
	}
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
	if f.WriteWhen != nil && !f.WriteWhen(plan) {
		return false
	}
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

// Int64PtrField maps a types.Int64 to a *int64: a pointer is a third state
// (absent, present-and-zero, present-and-set) the value types can't express.
type Int64PtrField[M any, S any] struct {
	Wire  string
	Model func(*M) *types.Int64
	SDK   func(*S) **int64
	Elide ElideZero

	// OmitZero sends nothing rather than a pointer to zero. See ToSDK for why
	// this is separate from Elide.
	OmitZero bool
}

func (f Int64PtrField[M, S]) WireName() string { return f.Wire }

// ToSDK does not skip an unknown value for a plain field, matching the
// hand-written resource's behavior: types.Int64Unknown().ValueInt64Pointer()
// returns a pointer to zero, indistinguishable from an explicit 0 at the wire.
func (f Int64PtrField[M, S]) ToSDK(_ context.Context, model *M, sdk *S) diag.Diagnostics {
	value := f.Model(model)
	// OmitZero must also skip Unknown, not just a known zero: an
	// Optional+Computed field with no default (site_to_site_vpn's
	// ike_dh_group) resolves to Unknown on create, and ValueInt64Pointer()
	// collapses that to the same zero pointer the controller's validator
	// rejects. OmitZero is a write rule, distinct from Elide's read rule.
	if f.OmitZero && (value.IsUnknown() || (!value.IsNull() && value.ValueInt64() == 0)) {
		return nil
	}
	*f.SDK(sdk) = value.ValueInt64Pointer()
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

// DurationField maps a timetypes.GoDuration to an integer count of Units. The
// unit itself is a fact no input carries -- it must be declared per field
// rather than inferred, which is why this isn't folded into Int64Field.
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

// StringListField maps a types.List of strings to a []string. The SDK slice
// is always emptied rather than left nil: a nil slice and an empty one
// serialize differently (absent vs present-and-empty), and the controller
// reads them as different requests.
type StringListField[M any, S any] struct {
	Wire  string
	Model func(*M) *types.List
	SDK   func(*S) *[]string
	// Elide answers whether an empty collection from the API is an absence.
	// Only an Optional-and-not-Computed attribute may set NullZero -- an
	// Optional+Computed one may hold an explicit empty from the practitioner,
	// and nulling it disagrees with config.
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
	values := *f.SDK(sdk)
	if len(values) == 0 && bool(f.Elide) {
		*f.Model(model) = types.ListNull(types.StringType)
		return nil
	}
	// Same nil-is-not-empty trap as StringSetField; see the note there.
	if values == nil {
		values = []string{}
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, values)
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
// A set is not a list with a different name: Terraform compares set
// membership without order, so a controller returning members in a different
// sequence than written produces no diff. Rendering the same data as a list
// would make that reordering a permanent, unsuppressable diff.
//
// The SDK slice is emptied, not left nil, for the reason StringListField gives.
type StringSetField[M any, S any] struct {
	Wire  string
	Model func(*M) *types.Set
	SDK   func(*S) *[]string
	// Elide answers whether an empty collection from the API is an absence.
	// Only an Optional-and-not-Computed attribute may set NullZero -- an
	// Optional+Computed one may hold an explicit empty from the practitioner,
	// and nulling it disagrees with config.
	Elide ElideZero
	// ElementType is the set's element type, defaulting to types.StringType.
	// The wire is always []string; this only types the elements in state. It
	// matters for semantic equality -- ap_group's device_macs uses
	// hwtypes.MACAddressType so different spellings of one MAC compare equal.
	ElementType attr.Type
	// KeepPrior answers whether state's existing value should survive a read.
	// It exists because a Set compares members by string value, so a custom
	// element type's semantic Equal never applies to membership -- without
	// this, the controller's spelling would overwrite the practitioner's and
	// leave an unsettleable diff. Nil takes the controller's value, right when
	// spellings can't differ.
	KeepPrior func(ctx context.Context, prior types.Set, incoming []string) bool
}

func (f StringSetField[M, S]) WireName() string { return f.Wire }

func (f StringSetField[M, S]) ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics {
	// This seed must come before the null check: FirewallZone's network_ids
	// isn't tagged omitempty, so nil marshals as null and [] marshals as an
	// explicit empty list -- moving this line would send null instead, and no
	// struct-comparison test would catch it.
	*f.SDK(sdk) = []string{}
	value := f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	return value.ElementsAs(ctx, f.SDK(sdk), false)
}

func (f StringSetField[M, S]) ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics {
	elementType := f.elementType()
	// KeepPrior runs before the elide branch and the overwrite, while model
	// still holds what was read from state -- both later steps destroy the
	// value it needs.
	if f.KeepPrior != nil && f.KeepPrior(ctx, *f.Model(model), *f.SDK(sdk)) {
		return nil
	}
	values := *f.SDK(sdk)
	if len(values) == 0 && bool(f.Elide) {
		*f.Model(model) = types.SetNull(elementType)
		return nil
	}
	// A nil slice is not an empty collection to SetValueFrom: nil produces a
	// null value, so KeepZero's "empty is a value" promise requires
	// normalizing nil to []string{} first, or the practitioner gets an
	// unresolvable state/config disagreement.
	if values == nil {
		values = []string{}
	}
	set, diags := types.SetValueFrom(ctx, elementType, values)
	*f.Model(model) = set
	return diags
}

// elementType defaults to types.StringType when ElementType is unset.
func (f StringSetField[M, S]) elementType() attr.Type {
	if f.ElementType == nil {
		return types.StringType
	}
	return f.ElementType
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

// ReadOnly wraps a field the controller owns: read from the API, never sent.
// A field the policy marks computed is one the practitioner can't set, so
// sending it would overwrite a controller-assigned value with the model's
// own. Implemented as a decorator rather than a flag on each kind, so a
// future kind gets it for free.
func ReadOnly[M any, S any](inner Field[M, S]) Field[M, S] {
	return readOnlyField[M, S]{inner: inner}
}

type readOnlyField[M any, S any] struct{ inner Field[M, S] }

func (f readOnlyField[M, S]) WireName() string { return f.inner.WireName() }

// Unwrap exposes the wrapped field so a check can reach its Elide:
// ElideProblems reflects on a field's own type, and without this a read-only
// field would report as carrying no Elide even though ToModel still applies it.
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
// Separate from DurationField because the pointer matters: nil and a
// pointer-to-zero are different things the controller distinguishes, so this
// leaves nil alone rather than writing zero through it, and never consults
// Elide for a nil.
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
