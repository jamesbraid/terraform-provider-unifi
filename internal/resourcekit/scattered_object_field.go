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

	// Decode builds the model's object from the SDK's flat fields AND WHAT THE
	// OBJECT HELD BEFORE.
	//
	// prior is this field's own object as it stood in state when the read began.
	// It is not the model: passing the whole model would let a decode reach into
	// a sibling, and whether it should would then be a judgement on every wire.
	//
	// TWO THINGS NEED IT AND THEY ARE THE SAME MISSING INPUT SEEN TWICE.
	//
	// MERGING. A controller that omits a member says nothing about it, and a
	// decode built only from *S has to write the zero. wan's read path guards
	// every one of its assignments with `if network.X != nil` for that reason:
	// eight of its ten objects keep the prior value per member. Without prior,
	// transcribing them changes refresh behaviour on every member the controller
	// does not return, silently and with nothing in the guard set able to see it.
	//
	// ELIDING THE WHOLE OBJECT. `if !model.DNS.IsNull() || hasDNSData` keeps an
	// object NULL when the controller returned nothing for it and the
	// practitioner never set it. A null prior with no API data IS that case, so
	// the same parameter answers it -- which is why this is one capability and
	// not two.
	//
	// ElideZero STAYS AND NOW MEANS SOMETHING NARROWER. It answers what an
	// all-zero READ means for a field the descriptor always populates;
	// prior answers whether to populate at all. A reader who finds both needs to
	// know which is which, and the two are not interchangeable: ElideZero cannot
	// see either input, and prior does not know what the schema declared.
	//
	// MOST IMPLEMENTATIONS WILL IGNORE IT. AfterReceive gained a prior parameter
	// at 03c7eaf4 for this same seam, and both descriptors implementing it left
	// the parameter unused with no behaviour change. The same is expected here.
	Decode func(ctx context.Context, sdk *S, prior types.Object) (types.Object, diag.Diagnostics)

	// ConditionalWires names the wires Encode writes only SOMETIMES, each with
	// the test for whether THIS object will write one.
	//
	// A WIRE NAMED HERE JOINS THE MASK ONLY WHEN ITS TEST HOLDS, and every wire
	// not named travels whenever the object does. That distinction is the whole
	// point: go-unifi sends a masked field's ZERO when the object carries no
	// value, so masking a wire Encode did not write CLEARS whatever the
	// controller holds.
	//
	// vpn_client is the case and it was measured rather than argued. Its
	// wireguard object spans ten wires, and two of them -- dhcpd_dns_1 and
	// dhcpd_dns_2 -- are written only when the practitioner supplies
	// dns_servers. The hand-written mask omits exactly those two and
	// unifi/wire_field_masks_test.go records why. Declaring all ten
	// unconditionally puts two empty strings on the wire on every apply that
	// sets a wireguard block without dns_servers, which blanks the controller's
	// DNS -- the destruction that mask exists to prevent, arriving through the
	// kind that replaced it.
	//
	// THE HAND-WRITTEN OMISSION IS THE PROTECTION, AND IT WAS FIRST READ AS A
	// HOLE. The obvious conclusion from a fifteen-name mask beside a ten-wire
	// object is that the mask is missing two -- a defect in the code being
	// replaced. It is the reverse, and acting on that reading would have
	// deleted a guard and shipped the clearing. What made it checkable is that
	// the exclusion carries its reason: wire_field_masks_test.go declares it
	// per field so a new one has to be argued for.
	//
	// EVERY KEY MUST BE ONE OF Wires and WireNameProblems checks it, because a
	// key that matches nothing silently leaves the wire unconditional -- which
	// is the failure this field is meant to remove, reached by a typo.
	ConditionalWires map[string]func(object types.Object) bool

	// ReadOnlyWires names wires this field DECODES and never encodes, so they
	// stay out of the mask while still being declared.
	//
	// vpn_server's wireguard.public_key is the case. The controller issues the
	// key and accepts none: unifi.Network carries wireguard_public_key, and
	// marshalUserVPN -- the alias its purpose selects -- does not emit it. So
	// maskedBody refuses a mask naming it and the whole update fails.
	//
	// NONE OF THE OTHER THREE MECHANISMS CAN SAY THIS, which is why it is its
	// own field rather than a convention. A Fields entry masks it. AlwaysWire
	// masks it. A ConditionalWires predicate that is never true is reported by
	// ConditionalWireProblems as a direction no object exercised -- correctly,
	// because a predicate nothing makes true is one nothing could have caught
	// lying. The wire is not conditional; it is unwritable, and that is a
	// different fact.
	//
	// IT STAYS IN Wires DELIBERATELY. WireNameProblems checks names against the
	// SDK type's own json tags, where wireguard_public_key is real, so the
	// declaration still catches a typo. What it leaves is the mask.
	ReadOnlyWires []string

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
	// The prior object is read BEFORE it is overwritten, which is the whole of
	// what makes this possible: Spec.ToModel passes the model loaded from state,
	// so at this instant *f.Model(model) is still what the last read produced.
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
		// An unknown member is a value still arriving, and a NULL one is an
		// absence: a practitioner who supplies an object leaves every member
		// they omit null in the plan, and copying those nulls erased the
		// computed members the controller had just assigned --
		// wireguard.public_key, measured live.
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
