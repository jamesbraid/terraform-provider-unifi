package resourcekit

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// MaskedZeroProblems reports every wire a scattered field would put on the mask
// with nothing behind it, for an object the practitioner only PARTLY filled in.
//
// THE CASE NOTHING ELSE LOOKS AT. Every other check here judges a whole object:
// set or absent, full or empty. A practitioner writes neither. They write
//
//	wan { port = "8080" }
//
// and the block is SET, so SetInPlan is true and every wire it spans joins the
// mask -- including the two the Encode left alone because their members are
// null. go-unifi then sends those names' zeros over whatever the controller
// holds. Measured on port_forward, which was migrated without this: a partial
// wan block masks pfwd_interface and destination_ip with "" behind both.
//
// TWO MECHANISMS PRODUCE IT AND THEY NEED DIFFERENT REMEDIES, so they are
// reported apart:
//
//	guarded assign   `if !m.X.IsNull() { sdk.F = ... }` -- Encode does NOT
//	                 write F, so F is conditional by behaviour and a
//	                 ConditionalWires entry takes it off the mask
//	always assign    `sdk.F = m.X.ValueStringPointer()` -- Encode DOES write F,
//	                 and writes nil. Nothing distinguishes that from a real
//	                 write, so no predicate can be keyed on it and the remedy
//	                 is for the mapper to stop assigning
//
// Reporting only the first would leave the second silent while its wire
// behaviour is identical, which is the shape this codebase keeps finding.
// THE TWO CLASSES COME BACK SEPARATELY AND THE CALLER MUST HANDLE BOTH.
// Returning one list would let a caller fail on the declarable half and never
// notice the other, whose wire behaviour is identical -- and a caller that
// handles only what it is given is how a mechanism goes unmeasured.
type MaskedZeroReport struct {
	// Guarded are wires Encode does not write when a member is unset. They are
	// conditional by behaviour, so a ConditionalWires entry takes them off the
	// mask and the fix is in the descriptor.
	Guarded []string
	// AlwaysAssigned are wires Encode writes as their zero. Nothing
	// distinguishes that from a real write, so no predicate can be keyed on it
	// and the fix is in the mapper.
	AlwaysAssigned []string
}

func MaskedZeroProblems[M any, S any](
	ctx context.Context,
	field ScatteredObjectField[M, S],
	full types.Object,
	seed func(*S),
) (MaskedZeroReport, error) {
	if full.IsNull() || full.IsUnknown() {
		return MaskedZeroReport{}, fmt.Errorf("the probe object is null or unknown, which Encode never sees")
	}
	if len(field.AttrTypes) == 0 {
		return MaskedZeroReport{}, fmt.Errorf("the field declares no AttrTypes, so no probe object can be built")
	}
	// THE SURFACE SUPPLIES ONE OBJECT AND THE PARTIALS ARE DERIVED FROM IT.
	//
	// A generated value cannot be valid for every member: vpn_client's
	// configuration.content is a base64 WireGuard file and traffic_route's
	// port is a number in a string, and a generic probe made Encode raise a
	// diagnostic on both rather than measure them. Asking the surface for the
	// FULL case and nulling one member at a time keeps the values real while
	// leaving the surface no way to under-supply -- the cases that matter are
	// derived, not chosen.
	for name := range field.AttrTypes {
		if _, present := full.Attributes()[name]; !present {
			return MaskedZeroReport{}, fmt.Errorf(
				"the probe object has no %q, so nothing here can see what happens when it "+
					"is the member left unset", name)
		}
	}
	for name, value := range full.Attributes() {
		if value.IsNull() || value.IsUnknown() {
			return MaskedZeroReport{}, fmt.Errorf(
				"the probe object leaves %q unset, so it is not the fully populated case "+
					"the partials are derived from", name)
		}
	}
	readOnly := make(map[string]bool, len(field.ReadOnlyWires))
	for _, wire := range field.ReadOnlyWires {
		readOnly[wire] = true
	}

	fullAtZero, err := WiresAtZero(ctx, field, full, seed)
	if err != nil {
		return MaskedZeroReport{}, err
	}

	members := make([]string, 0, len(field.AttrTypes))
	for name := range field.AttrTypes {
		members = append(members, name)
	}
	sort.Strings(members)

	guarded := map[string]string{}
	always := map[string]string{}
	for _, member := range members {
		partial, err := withoutMember(full, member)
		if err != nil {
			return MaskedZeroReport{}, err
		}
		atZero, err := WiresAtZero(ctx, field, partial, seed)
		if err != nil {
			return MaskedZeroReport{}, err
		}
		written, err := WiresEncodeWrites(ctx, field, partial, seed)
		if err != nil {
			return MaskedZeroReport{}, err
		}
		for _, wire := range field.Wires {
			if !atZero[wire] || readOnly[wire] {
				continue
			}
			// A WIRE AT ZERO EVEN WHEN EVERY MEMBER IS SET is not about this
			// member, and reporting it here would name an unrelated cause.
			if fullAtZero[wire] {
				continue
			}
			if _, declared := field.ConditionalWires[wire]; declared {
				continue
			}
			if written[wire] {
				always[wire] = member
			} else {
				guarded[wire] = member
			}
		}
	}

	var report MaskedZeroReport
	for _, wire := range sortedKeys(guarded) {
		report.Guarded = append(report.Guarded, fmt.Sprintf(
			"%q is not written when %q is unset and is not in ConditionalWires, so a "+
				"partly filled block masks it with nothing behind it and go-unifi sends "+
				"its zero over whatever the controller holds", wire, guarded[wire]))
	}
	for _, wire := range sortedKeys(always) {
		report.AlwaysAssigned = append(report.AlwaysAssigned, fmt.Sprintf(
			"%q is ASSIGNED its zero when %q is unset, so no ConditionalWires predicate "+
				"can take it off the mask -- the mapper has to stop assigning it, or "+
				"sending the zero has to be the intent", wire, always[wire]))
	}
	return report, nil
}

// withoutMember returns the object with one member set to null and every other
// left as the caller supplied it.
func withoutMember(full types.Object, unset string) (types.Object, error) {
	attrTypes := full.AttributeTypes(context.Background())
	values := make(map[string]attr.Value, len(attrTypes))
	for name, value := range full.Attributes() {
		values[name] = value
	}
	values[unset] = nullAttrValue(context.Background(), attrTypes[unset])
	object, diags := types.ObjectValue(attrTypes, values)
	if diags.HasError() {
		return types.Object{}, fmt.Errorf("building a probe object without %q: %v", unset, diags)
	}
	return object, nil
}

func nullAttrValue(ctx context.Context, attrType attr.Type) attr.Value {
	switch concrete := attrType.(type) {
	case types.ListType:
		return types.ListNull(concrete.ElemType)
	case types.SetType:
		return types.SetNull(concrete.ElemType)
	case types.ObjectType:
		return types.ObjectNull(concrete.AttrTypes)
	}
	return attrType.ValueType(ctx)
}
