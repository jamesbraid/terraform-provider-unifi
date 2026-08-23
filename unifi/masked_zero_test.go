package unifi

// THE PARTLY FILLED BLOCK, ACROSS EVERY SURFACE THAT HAS ONE.
//
// Every other check on a scattered object judges the object as a whole: set or
// absent, fully populated or fully empty. A practitioner writes neither. They
// write
//
//	wan { port = "8080" }
//
// and the block IS set, so SetInPlan is true and every wire it spans joins the
// mask -- including the ones whose members are null and whose Encode therefore
// left alone. go-unifi sends those names' zeros over whatever the controller
// holds.
//
// IT IS #121'S FOURTH MECHANISM AND IT HAD NOTHING MEASURING IT. The other
// three were counted the day the ticket was written; this one was described as
// "invisible to every mask-shaped check" and then left, which is how a
// mechanism with no instrument grows. Measured here first, it found EIGHT on
// port_forward -- a surface whose cutover I wrote, and whose hand-written
// predecessor did not have the defect: the whole-object write dropped an empty
// pfwd_interface through omitempty, and the masked update is what put it on the
// wire.
//
// EACH SURFACE SUPPLIES ONE FULLY POPULATED OBJECT AND THE PARTIALS ARE
// DERIVED. A generated value cannot be valid everywhere -- vpn_client's
// configuration.content is a base64 WireGuard file, traffic_route's port is a
// number in a string -- and a generic probe made Encode raise a diagnostic
// rather than measure. Asking for the full case and nulling one member at a
// time keeps the values real while leaving no way to under-supply: the cases
// that matter are derived, not chosen.

import (
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// fullProbeObject populates every member of an object type, through the type
// itself so a string-valuable custom type gets a value that fits.
func fullProbeObject(t *testing.T, attrTypes map[string]attr.Type) types.Object {
	t.Helper()
	values := make(map[string]attr.Value, len(attrTypes))
	for name, attrType := range attrTypes {
		values[name] = populatedAttr(t, attrType)
	}
	object, diags := types.ObjectValue(attrTypes, values)
	if diags.HasError() {
		t.Fatalf("building the full probe object: %v", diags)
	}
	return object
}

// TestNoSurfaceMasksAZeroForAPartlyFilledBlock walks the surfaces whose
// descriptors carry a scattered object.
//
// THE UNMEASURED ONES ARE NAMED, NOT SKIPPED. A surface whose members cannot
// take a generated value reports an error from the check rather than a clean
// run, and that lands in unmeasured below -- which is pinned, so a new one
// appearing fails rather than joining a silent list. A count of zero problems
// across four surfaces means nothing if the other two were never asked.
func TestNoSurfaceMasksAZeroForAPartlyFilledBlock(t *testing.T) {
	var unmeasured []string
	note := func(surface string, err error) {
		unmeasured = append(unmeasured, surface)
		t.Logf("%s: not measured -- %v", surface, err)
	}
	// THE ALWAYS-ASSIGNED HALF IS NOT IGNORED HERE, IT IS PINNED ELSEWHERE.
	// network's twenty-three live in TestNetworkNarrowingStaysSafeWhileWiresAre-
	// Unclassified, which also gates the narrowing that currently protects them.
	// Failing on them here would duplicate that pin and make one of the two go
	// stale unnoticed. Every other surface must have none.
	alwaysAssigned := map[string]int{}

	for _, field := range scatteredFieldsOf(t, networkKitSpec()) {
		report, err := resourcekit.MaskedZeroProblems(t.Context(), field,
			fullProbeObject(t, field.AttrTypes), func(n *ui.Network) {
				n.Purpose = ui.PurposeCorporate
				subnet := "10.0.0.0/24"
				n.IPSubnet = &subnet
			})
		if err != nil {
			note("network/"+field.Wires[0], err)
			continue
		}
		for _, problem := range report.Guarded {
			t.Errorf("network: %s", problem)
		}
		alwaysAssigned["network"] += len(report.AlwaysAssigned)
	}

	for _, field := range portForwardKitSpec().Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[portForwardKitModel, ui.PortForward])
		if !ok {
			continue
		}
		report, err := resourcekit.MaskedZeroProblems(t.Context(), scattered,
			fullProbeObject(t, scattered.AttrTypes), nil)
		if err != nil {
			note("port_forward/"+scattered.Wires[0], err)
			continue
		}
		for _, problem := range report.Guarded {
			t.Errorf("port_forward: %s", problem)
		}
		for _, problem := range report.AlwaysAssigned {
			t.Errorf("port_forward: %s", problem)
		}
	}

	for _, field := range vpnClientKitSpec().Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[vpnClientResourceModel, ui.Network])
		if !ok {
			continue
		}
		report, err := resourcekit.MaskedZeroProblems(t.Context(), scattered,
			fullProbeObject(t, scattered.AttrTypes), func(n *ui.Network) {
				n.Purpose = ui.PurposeVPNClient
			})
		if err != nil {
			note("vpn_client/"+scattered.Wires[0], err)
			continue
		}
		for _, problem := range report.Guarded {
			t.Errorf("vpn_client: %s", problem)
		}
		for _, problem := range report.AlwaysAssigned {
			t.Errorf("vpn_client: %s", problem)
		}
	}

	for _, field := range trafficRouteKitSpec().Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[trafficRouteKitModel, ui.TrafficRoute])
		if !ok {
			continue
		}
		report, err := resourcekit.MaskedZeroProblems(t.Context(), scattered,
			fullProbeObject(t, scattered.AttrTypes), nil)
		if err != nil {
			note("traffic_route/"+scattered.Wires[0], err)
			continue
		}
		for _, problem := range report.Guarded {
			t.Errorf("traffic_route: %s", problem)
		}
		for _, problem := range report.AlwaysAssigned {
			t.Errorf("traffic_route: %s", problem)
		}
	}

	// network's own pin is the other half of this check, and it has to be
	// non-empty or the two have drifted apart.
	if alwaysAssigned["network"] == 0 {
		t.Error("network reports no always-assigned wires here, but " +
			"TestNetworkNarrowingStaysSafeWhileWiresAreUnclassified pins twenty-three " +
			"of them; one of the two instruments has stopped seeing its subject")
	}

	sort.Strings(unmeasured)
	// PINNED, so the silence is a measurement. Each of these needs a fixture
	// object its members will accept before this check can say anything about
	// it; until then the surface is exposed to the mechanism and nothing here
	// would notice.
	wantUnmeasured := []string{
		"traffic_route/domains",
		"vpn_client/x_wireguard_private_key",
	}
	if len(unmeasured) != len(wantUnmeasured) {
		t.Errorf("unmeasured = %v, pinned as %v", unmeasured, wantUnmeasured)
	}
	for index, name := range unmeasured {
		if index < len(wantUnmeasured) && name != wantUnmeasured[index] {
			t.Errorf("unmeasured[%d] = %s, pinned as %s", index, name, wantUnmeasured[index])
		}
	}
}

// A DECLARED PREDICATE THAT NO LONGER MATCHES Encode IS INVISIBLE TO THE CHECK
// ABOVE, because a declared wire is skipped there -- the declaration is taken
// as the answer.
//
// Found by mutation: putting port_forward's `src` back to an unguarded
// assignment failed nothing, because the ConditionalWires entry added alongside
// the guard was still there saying it is conditional. The two have to be
// compared against each other, which is what ConditionalWireProblems does, and
// port_forward had no case calling it.
//
// The objects are the full one and each partial derived from it, so every
// predicate is exercised in BOTH directions without a hand-picked list that
// could omit the case that matters.
func TestPortForwardConditionalWiresAgreeWithEncode(t *testing.T) {
	checked := 0
	for _, field := range portForwardKitSpec().Fields {
		scattered, ok := field.(resourcekit.ScatteredObjectField[portForwardKitModel, ui.PortForward])
		if !ok {
			continue
		}
		full := fullProbeObject(t, scattered.AttrTypes)
		objects := []types.Object{full}
		for name := range scattered.AttrTypes {
			objects = append(objects, withoutProbeMember(t, full, name))
		}
		for _, problem := range resourcekit.ConditionalWireProblems(scattered, objects, nil) {
			t.Errorf("port_forward/%s: %s", scattered.Wires[0], problem)
		}
		checked++
	}
	if checked != 3 {
		t.Errorf("checked %d scattered field(s) on port_forward, want 3; a field the walk "+
			"missed is a field nothing here compares against its Encode", checked)
	}
}

// withoutProbeMember nulls one member of an object and leaves the rest.
func withoutProbeMember(t *testing.T, full types.Object, unset string) types.Object {
	t.Helper()
	attrTypes := full.AttributeTypes(t.Context())
	values := make(map[string]attr.Value, len(attrTypes))
	for name, value := range full.Attributes() {
		values[name] = value
	}
	values[unset] = nullAttr(attrTypes[unset])
	object, diags := types.ObjectValue(attrTypes, values)
	if diags.HasError() {
		t.Fatalf("building a probe object without %q: %v", unset, diags)
	}
	return object
}
