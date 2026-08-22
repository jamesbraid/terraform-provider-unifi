package resourcekit

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ScatteredObjectField carries a nested object the SDK does not have a struct
// for: a types.Object on the model, SEVERAL FLAT FIELDS on the SDK type.
//
// ObjectField and ObjectListField both need the SDK to own a nested struct --
// `func(*S) **E` and `func(*S) *[]E`. Across the seven surfaces still waiting on
// the kit, 24 of 24 object fields have no such struct. vpn_client's `wireguard`
// is one model object over WireguardPrivateKey, WireguardClientPresharedKeyEnabled
// and WireguardInterface: three siblings on Network, grouped only by the schema.
//
// THE MASK IS WHY THIS IS A DIFFERENT KIND AND NOT A CONVENIENCE. Every other
// kind names exactly one wire attribute, and Field says so -- WireName returns a
// string. A scattered object names as many as it spans, and a mask carrying one
// of three writes one third of what the practitioner set while the apply
// succeeds. That is the silent write-drop the masked update exists to prevent,
// arriving through the mask itself, which is what WireNameProblems was written
// for after two mutations left the suite green.
//
// AND IT INVERTS ObjectField's HAZARD RATHER THAN SHARING IT. NestedProblems
// asks which force-emitted members of a nested SDK struct the model fails to
// declare, because a mask names top-level keys and cannot say "send source.zone_id
// but not source.match_mac" -- so masking the parent sends the unmodelled member
// as a Go zero. HERE THE MEMBERS ARE ALREADY TOP-LEVEL KEYS. One this object does
// not declare is simply absent from the mask and never written, which is the safe
// direction. So NestedProblems does not apply, deliberately, and the check that
// does is the mirror image: every name in Wires must be a real json tag on S, and
// WireNameProblems verifies it by the same route it already uses for AlwaysWire.
//
// ENCODE WRITES ONTO THE STRUCT rather than returning one, because there is
// nothing to return. Decode reads the same siblings back. The descriptor still
// owns the member-by-member decisions for the reason ObjectField gives.
type ScatteredObjectField[M any, S any] struct {
	// Wires are the SDK's own names for every flat field this object spans.
	// ALL of them reach the mask; naming a subset writes a subset.
	Wires []string

	Model func(*M) *types.Object

	// AttrTypes types the object in state. It must match the schema's nested
	// object exactly or the value does not fit.
	AttrTypes map[string]attr.Type

	// Encode writes the object's members onto the SDK struct. A null or unknown
	// object never reaches it: ToSDK leaves the struct alone, so the fields keep
	// whatever the caller put there.
	Encode func(ctx context.Context, object types.Object, sdk *S) diag.Diagnostics

	// Decode builds the model's object from the SDK's flat fields.
	Decode func(ctx context.Context, sdk *S) (types.Object, diag.Diagnostics)

	// Elide says what an all-zero read means. Unlike ObjectField, where a nil
	// pointer answers it, nothing here distinguishes "the controller returned
	// nothing" from "it returned zeros" -- the fields are always present. So the
	// descriptor's Decode decides, and this records the decision for
	// ElideProblems to check against the schema.
	Elide ElideZero
}

// The interface is asserted here rather than in a test. A test that only fails
// to compile reports a build error at the package, not a named property, and the
// failure it produces reads as a broken suite rather than a broken field.
var _ Field[struct{}, struct{}] = ScatteredObjectField[struct{}, struct{}]{}

// multiWireField is the optional interface a field implements when it maps onto
// more than one SDK attribute. It follows the kit's existing extension idiom --
// the Unwrap and nestedMemberChecker assertions -- rather than widening Field,
// so every kind that names one attribute is untouched.
type multiWireField interface {
	wireNames() []string
}

func (f ScatteredObjectField[M, S]) wireNames() []string { return f.Wires }

// fieldWireNames is what every mask consumer asks instead of WireName, so a
// scattered field contributes all of its names and every other field contributes
// its one.
func fieldWireNames[M any, S any](field Field[M, S]) []string {
	if multi, ok := any(field).(multiWireField); ok {
		return multi.wireNames()
	}
	return []string{field.WireName()}
}

// WireName returns the first of Wires so the field satisfies Field and reads
// sensibly in a diagnostic. It is a real attribute rather than a label, but it
// is NOT the whole answer -- anything deciding what goes on the wire must use
// fieldWireNames, and a caller reaching for WireName here is the bug this kind
// exists to prevent.
func (f ScatteredObjectField[M, S]) WireName() string {
	if len(f.Wires) == 0 {
		return ""
	}
	return f.Wires[0]
}

func (f ScatteredObjectField[M, S]) ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics {
	object := *f.Model(model)
	if object.IsNull() || object.IsUnknown() {
		// Nothing is written, and nothing is zeroed. SetInPlan reports the same
		// absence, so none of these names joins the mask and the controller keeps
		// what it holds -- which is only true because the members are top-level.
		return nil
	}
	return f.Encode(ctx, object, sdk)
}

func (f ScatteredObjectField[M, S]) ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics {
	object, diags := f.Decode(ctx, sdk)
	if diags.HasError() {
		return diags
	}
	*f.Model(model) = object
	return diags
}

func (f ScatteredObjectField[M, S]) SetInPlan(plan *M) bool {
	value := *f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

// CopyPlanToState merges member by member, for the reason ObjectField's does: a
// Computed member is unknown in the plan on create, and a wholesale copy writes
// that unknown into state where Terraform rejects it after apply.
func (f ScatteredObjectField[M, S]) CopyPlanToState(plan, state *M) {
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
		return
	}
	*f.Model(state) = object
}
