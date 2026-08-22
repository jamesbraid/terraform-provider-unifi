package unifi

import (
	"context"
	"encoding/json"
	"reflect"
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

// THE WIRES network's SCATTERED ENCODES LEAVE AT ZERO, and the reason its mask
// narrowing cannot yet be improved.
//
// network narrows its update mask by dropping names THIS object's encoding did
// not carry. That is what makes a vlan-only update possible at all -- go-unifi
// refuses a mask naming a field the encoder never emits -- and it carries a
// known cost: a name absent merely because its field is at the zero value is
// dropped too, so unifi_network cannot clear a field (#178).
//
// The fix is to ask whether a POPULATED object of the same purpose would emit
// the name, which separates the two absences. It is also the change that arms a
// destruction, and this is the measurement that says so for this surface.
// Dropping a zero-valued name is what has been protecting every wire these
// Encodes leave at zero when a member is unset: under a would-emit narrowing
// the name stays on the mask and go-unifi sends its zero over whatever the
// controller holds.
//
// So this enumerates those wires and asserts the narrowing is still the safe
// kind while any of them is unclassified. Each needs an answer before the fix
// can land -- "sending the zero IS the intent here", as it is for the wpad_url,
// tftp_server and dns slots the old mapper explicitly blanked, or a
// ConditionalWires declaration so the name leaves the mask instead.
//
// I READ THE HELPERS AND CONCLUDED THEY WERE ALL UNCONDITIONAL. They clear the
// slots they skip, which looked like "always written". It is not the same
// question: a slot cleared to "" and a slot never assigned both arrive at the
// zero, and it is the zero that travels.
//
// HOW "ENDS AT ZERO" IS DECIDED. resourcekit.WiresAtZero runs Encode onto a
// seeded probe and reads the struct field back, so the answer is what the field
// HOLDS rather than what the encoding shows.
//
// IT USED TO ASK WHETHER Encode WROTE THE WIRE, VIA THE ENCODED FORM, and that
// was two mistakes cancelling. A pointer assigned nil is absent from the
// encoding exactly as an untouched one is, so "written" meant "written to
// something real" only by accident -- and swapping in a struct-level comparison
// took the reported population from seventeen to ZERO, because every one of
// these Encodes assigns every wire on every path. None of them is conditional.
// What they do is assign the zero, which is a different fact and the one that
// matters here: ConditionalWires cannot express it, because there is nothing to
// key a predicate on. The remedy for such a wire is to stop assigning it, in
// the mapper, not to declare it in the descriptor.
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

	// THE POPULATION IS PINNED BY NAME, NOT LOGGED.
	//
	// A count that is only printed cannot catch an instrument that quietly
	// stops seeing part of its subject, and this one did: reading the ENCODED
	// form instead of the struct field drops every bool -- dhcpd_enabled,
	// dhcpguard_enabled and nine more -- along with the DHCP pool range, because
	// a false behind omitempty is absent exactly as an unwritten field is. The
	// reported set went from 23 to 19 and nothing failed, since the gate below
	// is satisfied by any non-empty list.
	//
	// EXPECT THIS LIST TO SHRINK. Each name leaves it by being classified:
	// either sending the zero is the intent here, or the mapper stops assigning
	// the wire and the descriptor declares it in ConditionalWires. Removing a
	// name without doing one of those is the change this pin exists to catch.
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

	// THE GATE. While any of those is unclassified, the narrowing must still be
	// the kind that drops a zero-valued name -- otherwise each of them is an
	// explicit zero on the wire.
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

// wiresWrittenByEncode asks the KIT which wires Encode assigns, rather than
// deciding here.
//
// IT USED TO COMPARE MARSHALLED KEYS AND THAT WAS THE #240 CONFLATION WITH THE
// OPPOSITE SIGN. A pointer field Encode assigns NIL is absent from both probes'
// encodings, so "present in both and equal" called it NOT written -- and a
// field Encode never touched reads the same way. Assigned-nil and untouched are
// different facts and the encoded form cannot hold the difference, so the 17
// wires this test reports were a mixture of the two with no way to separate
// them. resourcekit.WiresEncodeWrites compares the struct fields.
func wiresWrittenByEncode(
	t *testing.T,
	field resourcekit.ScatteredObjectField[netModel, ui.Network],
	object types.Object,
) map[string]bool {
	t.Helper()
	// The corporate encoder derives DHCP range defaults from the subnet and
	// logs when it will not parse. A real CIDR keeps the probe quiet without
	// changing which keys are emitted, and the purpose is not optional: a zero
	// Network cannot marshal at all.
	written, err := resourcekit.WiresEncodeWrites(t.Context(), field, object,
		func(n *ui.Network) {
			n.Purpose = ui.PurposeCorporate
			subnet := "10.0.0.0/24"
			n.IPSubnet = &subnet
		})
	if err != nil {
		t.Fatalf("asking which wires Encode writes: %v", err)
	}
	return written
}

func marshalKeys(t *testing.T, network *ui.Network) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(network)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	return out
}

// sentinelFill puts a distinguishable non-zero in every settable field, so that
// a field Encode does not touch differs from the bare run.
func sentinelFill(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("sentinel")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(9)
	case reflect.Ptr:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		sentinelFill(v.Elem())
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.String {
			v.Set(reflect.ValueOf([]string{"sentinel"}))
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Field(i).CanSet() {
				sentinelFill(v.Field(i))
			}
		}
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
	// BUILT THROUGH THE TYPE ITSELF, so a string-valuable custom type --
	// timetypes.GoDuration on dhcp_server.leasetime -- gets its own value
	// rather than a plain string that will not fit.
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

// networkPerMemberProbeObjects builds the object set the classification needs:
// one with every member present, one with every member absent, and -- for each
// member in turn -- one that is otherwise full with just that member absent.
//
// THE LAST GROUP IS WHAT DISCRIMINATES. A full and a sparse object alone tell
// you a wire behaved differently between them; they do not tell you WHICH
// member decided it. Dropping one member at a time does.
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

// THE CLASSIFICATION OF THE SEVENTEEN, DERIVED RATHER THAN READ.
//
// Each of network's zero-ending wires is one of two things and they want
// opposite remedies. A wire the old mapper blanked DELIBERATELY -- emptyIfUnset
// and the slot-clearing loops assign "" -- should keep travelling, so a
// would-emit narrowing is right for it and it needs no declaration. A wire the
// mapper simply left alone was omitted by the whole-object PUT and the
// controller kept its value, so it needs ConditionalWires or the zero blanks it.
//
// I HAD THIS AS A HYPOTHESIS FROM READING THE MAPPER, and reading the mapper is
// what produced the wrong answer the first time. ConditionalWireProblems settles
// it by construction: its two-seed derivation asks whether Encode OVERWROTE the
// field, so a deliberate clear counts as written and a skip does not -- exactly
// the distinction I got wrong, mechanised.
// THE SEVENTEEN ARE NOT CONDITIONALLY-WRITTEN, SO ConditionalWires CANNOT
// EXPRESS THEM. That is the classification, and it is not the answer the split
// hypothesis predicted.
//
// ConditionalWires keys on whether Encode TOUCHED the field: its two-seed
// derivation calls a wire written when both structs agree afterwards, so a
// deliberate clear counts and a skip does not. network's Encodes assign every
// one of the seventeen on every path. Measured on the hardest case -- an
// all-null dhcp_server object, and DHCPDLeaseTime seeded to 9999 in one struct:
//
//	started nil  -> DHCPDLeaseTime=nil
//	started 9999 -> DHCPDLeaseTime=nil     the 9999 was overwritten
//
// So the check reports no conditional wire on any of the four objects, and it
// is right. What changed between the two mechanisms is not whether the field is
// assigned but what assigning NIL MEANS. Under the whole-object PUT a nil
// *int64 with omitempty dropped out of the body and the controller kept its
// value. Under a masked write that asks "would the encoder emit this name", the
// same nil is a name on the mask with no value behind it, and go-unifi sends
// the zero.
//
// THE REMEDY IS THEREFORE IN Encode, NOT IN A DECLARATION. A wire that should
// keep the controller's value has to stop being assigned when its member is
// unset -- and only then is it conditional, and only then can it be declared.
// The four pointer wires (dhcpd_leasetime, dhcpdv6_leasetime, dhcpdv6_start,
// dhcpdv6_stop) are the candidates; the strings the old mapper blanked through
// emptyIfUnset and the slot-clearing loops are already correct as they are.
//
// This asserts the state rather than a preference: no conditional wire is
// reported, which is why TestNetworkNarrowingStaysSafeWhileWiresAreUnclassified
// is still the thing holding the narrowing where it is.
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

		// THE POSITIVE CONTROL, and it deliberately does not lean on a defect.
		//
		// An earlier version of this checked non-vacuity by requiring the report
		// to be non-empty, and what kept it non-empty was the slice false
		// positive in fillSentinel. When that was fixed the total went to zero
		// and the guard fired -- correctly, because it had been measuring the
		// bug rather than the surface. A control satisfied by a defect is
		// discharged the moment the defect is.
		//
		// So the control is a KNOWN-WRONG input instead: declare a predicate
		// that claims a wire is written when Encode plainly writes it anyway,
		// inverted, and require the check to say so. That proves the check is
		// looking at THIS field with THESE objects, and it stays true after
		// every repair.
		probe := field
		probe.ConditionalWires = map[string]func(types.Object) bool{
			field.Wires[0]: func(types.Object) bool { return false },
		}
		// AND IT HAS TO BE THE PER-OBJECT MESSAGE, not merely a non-empty
		// report. A pinned predicate also trips the "no object makes this true"
		// complaint, which the check emits from the declaration alone -- so
		// asserting non-emptiness passes with NO objects at all. I wrote it
		// that way first and the mutation went green.
		sawPerObject := false
		for _, problem := range resourcekit.ConditionalWireProblems(probe, objects, seed) {
			if strings.HasPrefix(problem, "object ") {
				sawPerObject = true
			}
		}
		if !sawPerObject {
			t.Errorf("the check produced no per-object finding for %q even with a "+
				"deliberately wrong predicate, so it never ran Encode over these "+
				"objects and the silence above means nothing", field.Wires[0])
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
