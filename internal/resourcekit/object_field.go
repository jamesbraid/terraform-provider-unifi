package resourcekit

import (
	"context"
	"reflect"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ObjectField carries a nested object: a types.Object on the model, a *E on
// the SDK struct. The descriptor supplies Encode and Decode for the members;
// what the kind adds is checking whether the nested SDK type carries members
// the schema does not declare and the wire cannot omit.
//
// That check has to live here because a field mask names top-level keys only,
// so masking a nested object sends the whole thing -- there's no way to send
// source.zone_id but not source.match_mac. A force-emitted member the model
// doesn't carry then goes as a Go zero on every apply, invisibly to the
// descriptor author.
//
// The constraint is types.Object, not a typed <X>Value, deliberately:
// cmd/nested-custom-type-strip removes the CustomType binding from every
// nested attribute after generation, so no served schema ever binds one, and
// a field declaring <X>Value would fail an identity check in another package
// with no way to connect the two.
type ObjectField[M any, S any, E any] struct {
	Wire  string
	Model func(*M) *types.Object
	SDK   func(*S) **E

	// AttrTypes types the object in state. It must match the schema's nested
	// object exactly or the value does not fit.
	AttrTypes map[string]attr.Type

	// Encode builds the SDK object from the model's object value. Returning nil
	// means "the practitioner did not configure this", and the SDK pointer is
	// left nil so an omitempty key drops out.
	Encode func(ctx context.Context, object types.Object) (*E, diag.Diagnostics)
	// Decode builds the model's object value from what the controller returned.
	Decode func(ctx context.Context, sdk *E) (types.Object, diag.Diagnostics)

	// Unmodelled enumerates the wire names of members this descriptor
	// knowingly leaves to the controller. A list rather than a flag on
	// purpose: a blanket "preserve whatever I don't model" would silently
	// absorb a new member from a later SDK regeneration, where an
	// enumeration fails on it instead. Naming a member here is a claim its
	// zero is harmless, not a way to silence the check.
	Unmodelled []string

	Elide ElideZero
}

func (f ObjectField[M, S, E]) WireName() string { return f.Wire }

func (f ObjectField[M, S, E]) ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics {
	value := *f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		*f.SDK(sdk) = nil
		return nil
	}
	encoded, diags := f.Encode(ctx, value)
	*f.SDK(sdk) = encoded
	return diags
}

func (f ObjectField[M, S, E]) ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics {
	nested := *f.SDK(sdk)
	if nested == nil {
		// Elide has nothing to choose between here -- an absent nested object
		// is null either way, unlike a collection. It stays on the struct
		// only because ElideProblems reflects on every field, and because a
		// future controller response could add the empty-vs-none distinction.
		*f.Model(model) = types.ObjectNull(f.AttrTypes)
		return nil
	}
	object, diags := f.Decode(ctx, nested)
	*f.Model(model) = object
	return diags
}

func (f ObjectField[M, S, E]) SetInPlan(plan *M) bool {
	value := *f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

// CopyPlanToState merges member by member rather than replacing the object: a
// wholesale copy would write an unknown Computed member's plan value into
// state (firewall_policy's matching_target_type does this on create), and
// Terraform rejects an unknown after apply.
func (f ObjectField[M, S, E]) CopyPlanToState(plan, state *M) {
	planned, current := *f.Model(plan), *f.Model(state)
	if planned.IsNull() || planned.IsUnknown() {
		return
	}
	if current.IsNull() || current.IsUnknown() {
		*f.Model(state) = planned
		return
	}
	merged := make(map[string]attr.Value, len(f.AttrTypes))
	for name, value := range current.Attributes() {
		merged[name] = value
	}
	for name, value := range planned.Attributes() {
		if value.IsUnknown() {
			continue
		}
		merged[name] = value
	}
	object, diags := types.ObjectValue(f.AttrTypes, merged)
	if diags.HasError() {
		// A merge that cannot be typed leaves state alone rather than writing a
		// half-built object; the diagnostics surface at the write instead.
		return
	}
	*f.Model(state) = object
}

// nestedMemberChecker is what NestedProblems needs from a field without knowing
// its element type.
type nestedMemberChecker interface {
	nestedProblems() []string
}

func (f ObjectField[M, S, E]) nestedProblems() []string {
	var element E
	return nestedTypeProblems(f.Wire, reflect.TypeOf(element), f.AttrTypes, f.Unmodelled)
}

// nestedTypeProblems reports every force-emitted member of a nested SDK type
// that the object's attribute types do not declare and the descriptor has not
// enumerated.
//
// A member with omitempty is fine: nil or zero drops out of the encoding and
// the controller keeps what it holds. A member WITHOUT omitempty is sent
// whatever happens, so if the model cannot carry a value for it, the value sent
// is the Go zero.
func nestedTypeProblems(
	wire string,
	element reflect.Type,
	declared map[string]attr.Type,
	exempt []string,
) []string {
	if element == nil || element.Kind() != reflect.Struct {
		return nil
	}
	var problems []string
	for i := range element.NumField() {
		field := element.Field(i)
		tag, ok := field.Tag.Lookup("json")
		if !ok || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		if name == "" || slices.Contains(parts[1:], "omitempty") {
			continue
		}
		if _, modelled := declared[name]; modelled {
			continue
		}
		if slices.Contains(exempt, name) {
			continue
		}
		problems = append(problems, wire+"."+name+" is emitted unconditionally by "+
			element.Name()+", and the object does not declare it. A field mask names "+
			"top-level keys, so masking "+wire+" sends the whole nested object and this "+
			"member goes as its Go zero on every apply. Declare it, or name it in the "+
			"field's Unmodelled list to record that sending its zero is harmless.")
	}
	return problems
}

// NestedProblems runs the check over a spec's fields. It is a descriptor-time
// check in the same idiom as ElideProblems and WireNameProblems: a test asks
// the question once, for the author, rather than the kit refusing at apply time
// where the practitioner cannot act on it.
func NestedProblems[M any, S any](spec Spec[M, S]) []string {
	var problems []string
	for _, field := range spec.Fields {
		inner := field
		if unwrapper, ok := any(field).(interface{ Unwrap() Field[M, S] }); ok {
			inner = unwrapper.Unwrap()
		}
		if checker, ok := any(inner).(nestedMemberChecker); ok {
			problems = append(problems, checker.nestedProblems()...)
		}
	}
	return problems
}

// ObjectListField carries a list of nested objects: a types.List on the model,
// a []E on the SDK struct. It is what serves a ListNestedAttribute or a
// ListNestedBlock, where ObjectField serves a SingleNested one.
//
// SAME DIVISION OF LABOUR as ObjectField: the descriptor supplies Encode and
// Decode for one element, and the kind does the list plumbing and the check.
// Doing it per-element rather than per-list is what keeps a descriptor from
// re-implementing iteration eleven times.
type ObjectListField[M any, S any, E any] struct {
	Wire  string
	Model func(*M) *types.List
	SDK   func(*S) *[]E

	// AttrTypes types ONE element, not the list.
	AttrTypes map[string]attr.Type

	Encode func(ctx context.Context, object types.Object) (E, diag.Diagnostics)
	Decode func(ctx context.Context, element E) (types.Object, diag.Diagnostics)

	// Unmodelled enumerates wire names of ELEMENT members knowingly left to the
	// controller. See ObjectField.Unmodelled: it is a list rather than a flag so
	// a member added by a later SDK regeneration fails rather than being
	// absorbed.
	Unmodelled []string

	Elide ElideZero
}

func (f ObjectListField[M, S, E]) WireName() string { return f.Wire }

func (f ObjectListField[M, S, E]) ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics {
	var diags diag.Diagnostics
	value := *f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		*f.SDK(sdk) = nil
		return diags
	}
	elements := value.Elements()
	// Allocated even when empty: nil marshals to null and empty to [] for a
	// field without omitempty, and the two mean different things to the
	// controller. See StringSetField.ToSDK.
	out := make([]E, 0, len(elements))
	for _, element := range elements {
		object, ok := element.(types.Object)
		if !ok {
			diags.AddError("Converting "+f.Wire,
				"a list element is not an object, so the descriptor's Encode cannot read it")
			continue
		}
		encoded, d := f.Encode(ctx, object)
		diags.Append(d...)
		out = append(out, encoded)
	}
	*f.SDK(sdk) = out
	return diags
}

func (f ObjectListField[M, S, E]) ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics {
	var diags diag.Diagnostics
	objectType := types.ObjectType{AttrTypes: f.AttrTypes}
	elements := *f.SDK(sdk)
	if len(elements) == 0 && bool(f.Elide) {
		*f.Model(model) = types.ListNull(objectType)
		return diags
	}
	values := make([]attr.Value, 0, len(elements))
	for _, element := range elements {
		object, d := f.Decode(ctx, element)
		diags.Append(d...)
		values = append(values, object)
	}
	// An absent collection becomes an empty list rather than a null one
	// unless Elide says otherwise.
	list, d := types.ListValue(objectType, values)
	diags.Append(d...)
	*f.Model(model) = list
	return diags
}

func (f ObjectListField[M, S, E]) SetInPlan(plan *M) bool {
	value := *f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

// CopyPlanToState replaces the list wholesale rather than merging per element
// like ObjectField: list elements have no identity, so a positional merge
// could silently graft a computed value onto a different element than the
// one it came from.
func (f ObjectListField[M, S, E]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}

func (f ObjectListField[M, S, E]) nestedProblems() []string {
	var element E
	return nestedTypeProblems(f.Wire, reflect.TypeOf(element), f.AttrTypes, f.Unmodelled)
}
