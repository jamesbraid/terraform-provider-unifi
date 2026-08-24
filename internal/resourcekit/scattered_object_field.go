package resourcekit

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ScatteredObjectField carries a nested object the SDK does not have a struct
// for: a types.Object on the model, several flat fields on the SDK type.
// Unlike ObjectField and ObjectListField, which need the SDK to own a nested
// struct, this serves an object whose members are separate sibling fields
// (vpn_client's wireguard is three siblings on Network, grouped only by the
// schema).
//
// This has to be its own kind, not a convenience: Field's WireName returns
// one string, but a scattered object spans several wire attributes, and
// masking only one of them writes a partial value while the apply silently
// succeeds.
//
// It inverts ObjectField's hazard rather than sharing it: there, an
// undeclared member is force-emitted as a Go zero because the mask can't
// reach inside the object. Here the members ARE the top-level keys, so an
// undeclared one is simply absent from the mask -- the safe direction. What
// WireNameProblems checks instead is that every name in Wires is a real json
// tag on S.
//
// Encode writes onto the struct rather than returning one, since there is
// nothing to return; Decode reads the same siblings back.
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

	// Decode builds the model's object from the SDK's flat fields and what
	// the object held before.
	//
	// prior is this field's own object as it stood before the read, not the
	// whole model -- passing the model would let a decode reach into a
	// sibling field. It answers two things: merging (a controller that omits
	// a member says nothing about it, so a decode without prior would write
	// a zero instead of keeping what state held) and whether to null the
	// whole object when the controller returned nothing and the practitioner
	// never set it.
	//
	// ElideZero and prior aren't interchangeable: ElideZero says what an
	// all-zero read means for a field the descriptor always populates,
	// prior says whether to populate it at all.
	Decode func(ctx context.Context, sdk *S, prior types.Object) (types.Object, diag.Diagnostics)

	// ConditionalWires names the wires Encode writes only sometimes, each
	// with the test for whether THIS object will write one. A wire named
	// here joins the mask only when its test holds; every wire not named
	// travels whenever the object does. Masking a wire Encode didn't write
	// sends its zero, clearing whatever the controller holds.
	//
	// Every key must be one of Wires -- WireNameProblems checks it, since a
	// key matching nothing silently leaves that wire unconditional, the same
	// failure this field exists to prevent, reached by a typo.
	ConditionalWires map[string]func(object types.Object) bool

	// ReadOnlyWires names wires this field decodes and never encodes, so they
	// stay out of the mask while still being declared (vpn_server's
	// wireguard.public_key is the case: the controller issues the key but the
	// encoder never emits it, and a mask naming it would make maskedBody
	// refuse the whole update).
	//
	// None of the other three mechanisms can say this: a Fields entry or
	// AlwaysWire would mask it regardless, and a ConditionalWires predicate
	// that's never true gets reported as an unexercised direction rather than
	// as unwritable -- a different fact. It stays in Wires deliberately, so
	// WireNameProblems still catches a typo against the SDK's json tags; what
	// it's excluded from is only the mask.
	ReadOnlyWires []string

	// Elide says what an all-zero read means. Unlike ObjectField, no nil
	// pointer here distinguishes "the controller returned nothing" from "it
	// returned zeros" -- the fields are always present, so Decode decides and
	// this records the decision for ElideProblems.
	Elide ElideZero
}

// Asserted here rather than in a test, so a failure reads as a broken field
// rather than a broken suite.
var _ Field[struct{}, struct{}] = ScatteredObjectField[struct{}, struct{}]{}

// multiWireField is the optional interface a field implements when it maps onto
// more than one SDK attribute. It follows the kit's existing extension idiom --
// the Unwrap and nestedMemberChecker assertions -- rather than widening Field,
// so every kind that names one attribute is untouched.
type multiWireField interface {
	wireNames() []string
}

func (f ScatteredObjectField[M, S]) wireNames() []string { return f.Wires }

// maskWireField is the second optional interface, and it answers a DIFFERENT
// QUESTION from wireNames.
//
//	wireNames()               every wire this field CAN write -- what the
//	                          checks verify against the SDK's tags
//	maskedWireNames(plan)     the wires it WILL write for this plan -- what
//	                          the update mask may name
//
// They are the same set for every field with no conditional wire, which is all
// of them but one. Keeping them separate is what stops a check that wants the
// declared set from silently reading a plan-narrowed one.
type maskWireField[M any] interface {
	maskedWireNames(plan *M) []string
}

func (f ScatteredObjectField[M, S]) maskedWireNames(plan *M) []string {
	object := *f.Model(plan)
	if object.IsNull() || object.IsUnknown() {
		// SetInPlan already excluded the field, so reaching here would mean the
		// caller asked without checking. Answering nothing is the safe reading:
		// Encode writes nothing for a null object.
		return nil
	}
	if len(f.ConditionalWires) == 0 && len(f.ReadOnlyWires) == 0 {
		return f.Wires
	}
	readOnly := make(map[string]bool, len(f.ReadOnlyWires))
	for _, wire := range f.ReadOnlyWires {
		readOnly[wire] = true
	}
	names := make([]string, 0, len(f.Wires))
	for _, wire := range f.Wires {
		// A wire nothing encodes never reaches the mask, whatever the plan says.
		if readOnly[wire] {
			continue
		}
		if writes, conditional := f.ConditionalWires[wire]; conditional && !writes(object) {
			continue
		}
		names = append(names, wire)
	}
	return names
}

// fieldMaskWireNames is what the UPDATE MASK asks. Every other consumer asks
// fieldWireNames, which reports the declared set.
func fieldMaskWireNames[M any, S any](field Field[M, S], plan *M) []string {
	if masked, ok := any(field).(maskWireField[M]); ok {
		return masked.maskedWireNames(plan)
	}
	return fieldWireNames(field)
}

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
// sensibly in a diagnostic -- it is NOT the whole answer. Anything deciding
// what goes on the wire must use fieldWireNames instead.
func (f ScatteredObjectField[M, S]) WireName() string {
	if len(f.Wires) == 0 {
		return ""
	}
	return f.Wires[0]
}

func (f ScatteredObjectField[M, S]) ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics {
	object := *f.Model(model)
	if object.IsNull() || object.IsUnknown() {
		// Nothing is written and nothing is zeroed: SetInPlan reports the
		// same absence, so none of these names joins the mask, and the
		// controller keeps what it holds -- true only because the members
		// are top-level.
		return nil
	}
	return f.Encode(ctx, object, sdk)
}

func (f ScatteredObjectField[M, S]) ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics {
	// prior is read before it's overwritten: Spec.ToModel passes the model
	// loaded from state, so at this instant *f.Model(model) still holds what
	// the last read produced.
	object, diags := f.Decode(ctx, sdk, *f.Model(model))
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
		// An unknown member is a value still arriving; a null one is an
		// absence. A practitioner who supplies an object leaves every
		// omitted member null in the plan, so copying those nulls would
		// erase a computed member the controller just assigned
		// (wireguard.public_key).
		if value.IsUnknown() || value.IsNull() {
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
