package resourcekit

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// MaskedZeroProblems reports every wire a scattered field would put on the
// mask with nothing behind it, for an object the practitioner only partly
// filled in -- e.g. `wan { port = "8080" }`, where the block is SET so every
// wire it spans joins the mask, including ones Encode left alone because
// their member is null.
//
// Guarded and AlwaysAssigned are separate fields, not one list: merging them
// would let a caller fix the declarable half and never notice the other,
// whose wire behavior is identical.
type MaskedZeroReport struct {
	// Guarded are wires Encode does not write when a member is unset -- they
	// are conditional by behaviour, so a ConditionalWires entry takes them
	// off the mask and the fix is in the descriptor.
	Guarded []string
	// AlwaysAssigned are wires Encode writes as their zero regardless.
	// Nothing distinguishes that from a real write, so no predicate can be
	// keyed on it and the fix is in the mapper.
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
	// The surface supplies one full object and the partials are derived by
	// nulling one member at a time: a generic probe value isn't valid for
	// every member (vpn_client's configuration.content is a base64 file,
	// traffic_route's port is a number-in-a-string), so a real value is
	// required and the surface has no way to under-supply.
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
			// A wire at zero even when every member is set isn't about this
			// member, so it's skipped here; a field whose Encode is a no-op
			// reads as zero everywhere and is invisible to this whole
			// function (vpn_server's wan pair is one, guarded instead by
			// vpnServerUnwritableWires).
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
