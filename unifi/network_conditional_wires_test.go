package unifi

import (
	"context"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestNetworkNarrowingStaysSafeWhileWiresAreUnclassified fails if network's
// mask narrowing moves from dropping names the object's encoding never
// carried to dropping names merely because the field is at its zero value,
// while any wire below is still unclassified as to which kind of absence
// that is.
func TestNetworkNarrowingStaysSafeWhileWiresAreUnclassified(t *testing.T) {
	fields := scatteredFieldsOf(t, networkKitSpec())
	if len(fields) != 4 {
		t.Fatalf("found %d scattered fields on network, want 4; the walk is wrong "+
			"and a field it missed is a field nothing below checks", len(fields))
	}

	checked := 0
	var atRisk []string
	for _, field := range fields {
		objects := networkScatteredProbeObjects(t, field.AttrTypes)
		if len(objects) < 2 {
			t.Fatalf("a scattered field got %d probe objects; conditionality cannot "+
				"be observed without at least a full one and a sparse one", len(objects))
		}

		zeroed := make([]map[string]bool, 0, len(objects))
		for _, object := range objects {
			zeroed = append(zeroed, wiresAtZeroAfterEncode(t, field, object))
		}

		for _, wire := range field.Wires {
			atZero := false
			for _, z := range zeroed {
				if z[wire] {
					atZero = true
				}
			}
			if !atZero {
				continue // never comes out at its zero: nothing for the mask to clear
			}
			if _, declared := field.ConditionalWires[wire]; declared {
				continue // already declared, so it leaves the mask when unwritten
			}
			checked++
			atRisk = append(atRisk, wire)
		}
	}
	sort.Strings(atRisk)

	// Pinned by name, not just a count: a name leaves this list only by being
	// classified, either as an intentional zero or via a ConditionalWires
	// declaration.
	wantAtRisk := []string{
		"dhcp_relay_enabled",
		"dhcpd_boot_enabled",
		"dhcpd_boot_server",
		"dhcpd_conflict_checking",
		"dhcpd_dns_1",
		"dhcpd_dns_2",
		"dhcpd_dns_3",
		"dhcpd_dns_4",
		"dhcpd_dns_enabled",
		"dhcpd_enabled",
		"dhcpd_gateway_enabled",
		"dhcpd_leasetime",
		"dhcpd_ntp_enabled",
		"dhcpd_start",
		"dhcpd_stop",
		"dhcpd_time_offset_enabled",
		"dhcpd_wins_enabled",
		"dhcpdv6_dns_auto",
		"dhcpdv6_enabled",
		"dhcpdv6_leasetime",
		"dhcpdv6_start",
		"dhcpdv6_stop",
		"dhcpguard_enabled",
	}
	for _, name := range wantAtRisk {
		if !slices.Contains(atRisk, name) {
			t.Errorf("%s no longer reads as ending at its zero. If it was classified, "+
				"remove it from this list in the same commit; if the probe stopped "+
				"seeing it, the instrument lost part of its subject", name)
		}
	}
	for _, name := range atRisk {
		if !slices.Contains(wantAtRisk, name) {
			t.Errorf("%s now ends at its zero and was not in the pinned set; a wire the "+
				"mask would carry with nothing behind it has appeared", name)
		}
	}

	if checked == 0 {
		t.Error("no wire came out at its zero on any of network's four scattered " +
			"fields, which contradicts the positional slot writers; the probe " +
			"objects are not discriminating and this test asserts nothing")
	}
	t.Logf("%d undeclared wire(s) end at zero when their member is unset: %v",
		len(atRisk), atRisk)

	// While any wire above is unclassified, the narrowing must stay the kind
	// that drops a zero-valued name.
	if len(atRisk) == 0 {
		return
	}
	spec := networkKitSpec()
	if spec.UnwritableWires == nil {
		t.Fatal("network declares no UnwritableWires at all; a vlan-only update " +
			"would be refused outright")
	}
	// A corporate network with nothing set: dhcpd_wpad_url is a name the encoder
	// WOULD emit when populated, so a would-emit narrowing keeps it and a
	// did-emit narrowing drops it. Which one comes back says which is in force.
	sparse := &ui.Network{Purpose: ui.PurposeCorporate}
	dropped := map[string]bool{}
	for _, name := range spec.UnwritableWires(sparse) {
		dropped[name] = true
	}
	if !dropped["dhcpd_wpad_url"] {
		t.Errorf("network's narrowing no longer drops a zero-valued name, so it has "+
			"moved to would-emit -- but %d wire(s) that end at zero are still "+
			"undeclared: %v.\n\nEach of those is now an explicit zero sent over "+
			"whatever the controller holds. Classify them first: either sending "+
			"the zero is the intent, or the wire belongs in ConditionalWires.",
			len(atRisk), atRisk)
	}
}

// networkScatteredProbeObjects builds a fully-populated object and a sparse one
// for the same shape. The sparse one is what makes a conditional write visible.
func networkScatteredProbeObjects(
	t *testing.T,
	attrTypes map[string]attr.Type,
) []types.Object {
	t.Helper()
	full := map[string]attr.Value{}
	sparse := map[string]attr.Value{}
	names := make([]string, 0, len(attrTypes))
	for name := range attrTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		full[name] = populatedAttr(t, attrTypes[name])
		sparse[name] = nullAttr(attrTypes[name])
	}
	fullObject, d := types.ObjectValue(attrTypes, full)
	if d.HasError() {
		t.Fatalf("building the populated probe object: %v", d)
	}
	sparseObject, d := types.ObjectValue(attrTypes, sparse)
	if d.HasError() {
		t.Fatalf("building the sparse probe object: %v", d)
	}
	return []types.Object{fullObject, sparseObject}
}

func populatedAttr(t *testing.T, typ attr.Type) attr.Value {
	t.Helper()
	switch concrete := typ.(type) {
	case types.ListType:
		element := populatedAttr(t, concrete.ElemType)
		list, d := types.ListValue(concrete.ElemType, []attr.Value{element, element, element})
		if d.HasError() {
			t.Fatalf("building a probe list: %v", d)
		}
		return list
	case types.SetType:
		// One element, not three: duplicate values in a set collapse or fail
		// on comparison, and one is enough to make the member non-null.
		element := populatedAttr(t, concrete.ElemType)
		set, d := types.SetValue(concrete.ElemType, []attr.Value{element})
		if d.HasError() {
			t.Fatalf("building a probe set: %v", d)
		}
		return set
	case types.ObjectType:
		inner := map[string]attr.Value{}
		for name, attrType := range concrete.AttrTypes {
			inner[name] = populatedAttr(t, attrType)
		}
		object, d := types.ObjectValue(concrete.AttrTypes, inner)
		if d.HasError() {
			t.Fatalf("building a probe object: %v", d)
		}
		return object
	}
	// Built through the type itself so a string-valuable custom type (e.g.
	// timetypes.GoDuration) gets its own value, not a plain string.
	ctx := t.Context()
	tfType := typ.TerraformType(ctx)
	var raw tftypes.Value
	switch {
	case tfType.Is(tftypes.Bool):
		raw = tftypes.NewValue(tfType, true)
	case tfType.Is(tftypes.Number):
		raw = tftypes.NewValue(tfType, 9)
	default:
		raw = tftypes.NewValue(tfType, "2s")
	}
	value, err := typ.ValueFromTerraform(ctx, raw)
	if err != nil {
		t.Fatalf("building a probe value for %T: %v", typ, err)
	}
	return value
}

func nullAttr(typ attr.Type) attr.Value {
	switch concrete := typ.(type) {
	case types.ListType:
		return types.ListNull(concrete.ElemType)
	case types.SetType:
		// Not the fallthrough below: that builds an untyped null (no
		// ElemType), not a null of the set's own element type.
		return types.SetNull(concrete.ElemType)
	case types.ObjectType:
		return types.ObjectNull(concrete.AttrTypes)
	}
	return typ.ValueType(context.Background())
}

// scatteredFieldsOf pulls the ScatteredObjectField entries out of a Spec.
func scatteredFieldsOf(
	t *testing.T, spec resourcekit.Spec[netModel, ui.Network],
) []resourcekit.ScatteredObjectField[netModel, ui.Network] {
	t.Helper()
	var out []resourcekit.ScatteredObjectField[netModel, ui.Network]
	for _, field := range spec.Fields {
		if scattered, ok := field.(resourcekit.ScatteredObjectField[netModel, ui.Network]); ok {
			out = append(out, scattered)
		}
	}
	return out
}

// networkPerMemberProbeObjects builds a full object, an empty object, and one
// per member with just that member absent -- the per-member set is what
// identifies which member decided a wire's behavior.
func networkPerMemberProbeObjects(
	t *testing.T,
	attrTypes map[string]attr.Type,
) []types.Object {
	t.Helper()
	names := make([]string, 0, len(attrTypes))
	for name := range attrTypes {
		names = append(names, name)
	}
	sort.Strings(names)

	build := func(omit string) types.Object {
		values := map[string]attr.Value{}
		for _, name := range names {
			if name == omit {
				values[name] = nullAttr(attrTypes[name])
				continue
			}
			values[name] = populatedAttr(t, attrTypes[name])
		}
		object, d := types.ObjectValue(attrTypes, values)
		if d.HasError() {
			t.Fatalf("building a probe object omitting %q: %v", omit, d)
		}
		return object
	}

	objects := []types.Object{build("")} // nothing omitted: everything present
	for _, name := range names {
		objects = append(objects, build(name))
	}
	// And one with everything absent, so a wire written only when SOMETHING is
	// set is still seen skipping.
	allNull := map[string]attr.Value{}
	for _, name := range names {
		allNull[name] = nullAttr(attrTypes[name])
	}
	sparse, d := types.ObjectValue(attrTypes, allNull)
	if d.HasError() {
		t.Fatalf("building the all-absent probe object: %v", d)
	}
	return append(objects, sparse)
}

// TestNetworkHasNoConditionallyWrittenWires asserts network's Encodes assign
// every wire on every path, so none of the zero-ending wires can be expressed
// as a ConditionalWires declaration -- the remedy for one that should keep
// the controller's value belongs in Encode itself.
func TestNetworkHasNoConditionallyWrittenWires(t *testing.T) {
	seed := func(n *ui.Network) {
		n.Purpose = ui.PurposeCorporate
		subnet := "10.0.0.0/24"
		n.IPSubnet = &subnet
	}
	const conditionalReport = "for some of these objects and leaves it alone for others"

	for _, field := range scatteredFieldsOf(t, networkKitSpec()) {
		objects := networkPerMemberProbeObjects(t, field.AttrTypes)
		if len(objects) < 3 {
			t.Fatalf("%d probe objects; one per member plus both extremes is the "+
				"point, and fewer cannot show a wire behaving differently",
				len(objects))
		}
		for _, problem := range resourcekit.ConditionalWireProblems(field, objects, seed) {
			if !strings.Contains(problem, conditionalReport) {
				continue
			}
			t.Errorf("a wire is conditionally written and undeclared, which contradicts "+
				"the measurement that Encode assigns all of them: %s", problem)
		}

		// The positive control uses a deliberately wrong predicate rather
		// than a live defect, so it keeps proving the check works after
		// every repair.
		probe := field
		probe.ConditionalWires = map[string]func(types.Object) bool{
			field.Wires[0]: func(types.Object) bool { return false },
		}
		sawPerObject := false
		for _, problem := range resourcekit.ConditionalWireProblems(probe, objects, seed) {
			if strings.HasPrefix(problem, "object ") {
				sawPerObject = true
			}
		}
		if !sawPerObject {
			t.Errorf("the check produced no per-object finding for %q even with a "+
				"deliberately wrong predicate, so it never ran Encode over these "+
				"objects. A bare non-empty report wouldn't prove this either: the "+
				"check also emits a declaration-only complaint with no objects "+
				"behind it, which is why this requires the per-object one.", field.Wires[0])
		}
	}
}

// wiresAtZeroAfterEncode reports which wires hold their type's zero once Encode
// has run for this object -- which is what the mask would send if the narrowing
// kept the name.
func wiresAtZeroAfterEncode(
	t *testing.T,
	field resourcekit.ScatteredObjectField[netModel, ui.Network],
	object types.Object,
) map[string]bool {
	t.Helper()
	atZero, err := resourcekit.WiresAtZero(t.Context(), field, object,
		func(n *ui.Network) {
			n.Purpose = ui.PurposeCorporate
			subnet := "10.0.0.0/24"
			n.IPSubnet = &subnet
		})
	if err != nil {
		t.Fatalf("asking which wires end at their zero: %v", err)
	}
	return atZero
}
