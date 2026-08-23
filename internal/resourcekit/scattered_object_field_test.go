package resourcekit

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// scatterSDK stands in for go-unifi's Network: the three wireguard members are
// siblings on the type, grouped only by the schema. The json tags are what the
// wire-name check reads.
type scatterSDK struct {
	WireguardPrivateKey string `json:"wireguard_private_key"`
	WireguardPresharedK bool   `json:"wireguard_client_preshared_key_enabled"`
	WireguardInterface  string `json:"wireguard_interface"`
	Unrelated           string `json:"unrelated"`
}

type scatterModel struct {
	Wireguard types.Object
}

var scatterAttrs = map[string]attr.Type{
	"private_key":           types.StringType,
	"preshared_key_enabled": types.BoolType,
	"interface":             types.StringType,
}

func scatterField() ScatteredObjectField[scatterModel, scatterSDK] {
	return ScatteredObjectField[scatterModel, scatterSDK]{
		Wires: []string{
			"wireguard_private_key",
			"wireguard_client_preshared_key_enabled",
			"wireguard_interface",
		},
		Model:     func(m *scatterModel) *types.Object { return &m.Wireguard },
		AttrTypes: scatterAttrs,
		Encode: func(_ context.Context, object types.Object, sdk *scatterSDK) diag.Diagnostics {
			attrs := object.Attributes()
			var diags diag.Diagnostics
			key, ok := attrs["private_key"].(types.String)
			if !ok {
				diags.AddError("private_key", "not a string")
				return diags
			}
			preshared, ok := attrs["preshared_key_enabled"].(types.Bool)
			if !ok {
				diags.AddError("preshared_key_enabled", "not a bool")
				return diags
			}
			iface, ok := attrs["interface"].(types.String)
			if !ok {
				diags.AddError("interface", "not a string")
				return diags
			}
			sdk.WireguardPrivateKey = key.ValueString()
			sdk.WireguardPresharedK = preshared.ValueBool()
			sdk.WireguardInterface = iface.ValueString()
			return nil
		},
		Decode: func(_ context.Context, sdk *scatterSDK, _ types.Object) (types.Object, diag.Diagnostics) {
			return types.ObjectValue(scatterAttrs, map[string]attr.Value{
				"private_key":           types.StringValue(sdk.WireguardPrivateKey),
				"preshared_key_enabled": types.BoolValue(sdk.WireguardPresharedK),
				"interface":             types.StringValue(sdk.WireguardInterface),
			})
		},
	}
}

func scatterObject(t *testing.T, key, iface string) types.Object {
	t.Helper()
	object, diags := types.ObjectValue(scatterAttrs, map[string]attr.Value{
		"private_key":           types.StringValue(key),
		"preshared_key_enabled": types.BoolValue(true),
		"interface":             types.StringValue(iface),
	})
	if diags.HasError() {
		t.Fatalf("building the fixture object: %v", diags)
	}
	return object
}

// THE PROPERTY THE KIND EXISTS FOR. Every other kind names one attribute, so a
// mask built from WireName is complete. This one spans three, and a mask
// carrying one of them writes one third of what the practitioner set while the
// apply succeeds -- the silent write-drop the masked update was introduced to
// prevent, arriving through the mask.
func TestScatteredObjectPutsEveryNameInTheMask(t *testing.T) {
	spec := Spec[scatterModel, scatterSDK]{
		TypeName: "unifi_scatter",
		Fields:   []Field[scatterModel, scatterSDK]{scatterField()},
	}
	plan := &scatterModel{Wireguard: scatterObject(t, "abc", "wg0")}

	fields, err := spec.WireFields(plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	want := []string{
		"wireguard_private_key",
		"wireguard_client_preshared_key_enabled",
		"wireguard_interface",
	}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("the mask carries %v, want all three of %v.\n"+
			"A mask naming a subset writes a subset and the apply still succeeds.",
			fields, want)
	}
}

// The other direction, and the reason it is asserted separately: an absent
// object must contribute NO names, or the mask writes zeros over three
// attributes the configuration never mentioned.
func TestScatteredObjectAbsentContributesNoNamesAndWritesNothing(t *testing.T) {
	spec := Spec[scatterModel, scatterSDK]{
		TypeName: "unifi_scatter",
		Fields: []Field[scatterModel, scatterSDK]{
			scatterField(),
			// A second field so WireFields has something to return; it refuses
			// an empty mask, which is correct and would mask the assertion.
			StringField[scatterModel, scatterSDK]{
				Wire:  "unrelated",
				Model: func(m *scatterModel) *types.String { return &unrelatedHolder },
				SDK:   func(s *scatterSDK) *string { return &s.Unrelated },
			},
		},
	}
	unrelatedHolder = types.StringValue("set")

	for _, absent := range []struct {
		name  string
		value types.Object
	}{
		{"null", types.ObjectNull(scatterAttrs)},
		{"unknown", types.ObjectUnknown(scatterAttrs)},
	} {
		t.Run(absent.name, func(t *testing.T) {
			plan := &scatterModel{Wireguard: absent.value}
			fields, err := spec.WireFields(plan)
			if err != nil {
				t.Fatalf("WireFields: %v", err)
			}
			for _, name := range fields {
				if name != "unrelated" {
					t.Errorf("an absent object put %q in the mask", name)
				}
			}

			// CONTROL: the SDK carries values the caller put there, and an
			// absent object must leave every one of them alone. Without this the
			// assertion above passes against a kind that writes zeros anyway.
			sdk := &scatterSDK{
				WireguardPrivateKey: "held",
				WireguardPresharedK: true,
				WireguardInterface:  "wg9",
			}
			if diags := scatterField().ToSDK(context.Background(), plan, sdk); diags.HasError() {
				t.Fatalf("ToSDK: %v", diags)
			}
			if sdk.WireguardPrivateKey != "held" || !sdk.WireguardPresharedK || sdk.WireguardInterface != "wg9" {
				t.Errorf("an absent object overwrote the struct: %+v", sdk)
			}
		})
	}
}

var unrelatedHolder types.String

func TestScatteredObjectRoundTripsThroughTheFlatFields(t *testing.T) {
	ctx := context.Background()
	field := scatterField()

	sdk := &scatterSDK{}
	model := &scatterModel{Wireguard: scatterObject(t, "privkey", "wg1")}
	if diags := field.ToSDK(ctx, model, sdk); diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	// Asserted on the STRUCT FIELDS, not on a round trip alone: a symmetric
	// Encode/Decode pair that wrote all three values to one field would round
	// trip and still be wrong.
	if sdk.WireguardPrivateKey != "privkey" {
		t.Errorf("private key landed as %q", sdk.WireguardPrivateKey)
	}
	if !sdk.WireguardPresharedK {
		t.Error("preshared flag did not land")
	}
	if sdk.WireguardInterface != "wg1" {
		t.Errorf("interface landed as %q", sdk.WireguardInterface)
	}

	back := &scatterModel{}
	if diags := field.ToModel(ctx, sdk, back); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if !back.Wireguard.Equal(model.Wireguard) {
		t.Errorf("round trip changed the object:\n before %v\n after  %v",
			model.Wireguard, back.Wireguard)
	}
}

// A Computed member is unknown in the plan on create, and a wholesale copy
// writes that unknown into state where Terraform rejects it after apply.
func TestScatteredObjectCopyPlanToStateKeepsWhatTheReadProduced(t *testing.T) {
	planned, diags := types.ObjectValue(scatterAttrs, map[string]attr.Value{
		"private_key":           types.StringValue("configured"),
		"preshared_key_enabled": types.BoolUnknown(),
		"interface":             types.StringUnknown(),
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	plan := &scatterModel{Wireguard: planned}
	state := &scatterModel{Wireguard: scatterObject(t, "from-read", "wg-read")}

	scatterField().CopyPlanToState(plan, state)

	got := state.Wireguard.Attributes()
	if v := scatterString(t, got, "private_key"); v != "configured" {
		t.Errorf("a set plan value did not win: private_key = %q", v)
	}
	if v := scatterString(t, got, "interface"); v != "wg-read" {
		t.Errorf("an unknown plan member overwrote the read: interface = %q", v)
	}
	preshared, ok := got["preshared_key_enabled"].(types.Bool)
	if !ok {
		t.Fatalf("preshared_key_enabled is %T, not a bool", got["preshared_key_enabled"])
	}
	if !preshared.ValueBool() {
		t.Error("an unknown plan member overwrote the read: preshared_key_enabled")
	}
}

// WireNameProblems has no SDK accessor to follow here, so it verifies each name
// against the type's own tags. Both directions, because a check that only ever
// passes is the shape this package keeps finding.
func TestScatteredObjectWireNamesAreCheckedAgainstTheSDK(t *testing.T) {
	good := Spec[scatterModel, scatterSDK]{
		TypeName: "unifi_scatter",
		Fields:   []Field[scatterModel, scatterSDK]{scatterField()},
	}
	if problems := WireNameProblems(good); len(problems) != 0 {
		t.Errorf("three real attributes were reported as problems: %v", problems)
	}

	broken := scatterField()
	broken.Wires = []string{"wireguard_private_key", "wireguard_typo", "wireguard_interface"}
	spec := Spec[scatterModel, scatterSDK]{
		TypeName: "unifi_scatter",
		Fields:   []Field[scatterModel, scatterSDK]{broken},
	}
	problems := WireNameProblems(spec)
	if len(problems) != 1 {
		t.Fatalf("a name that is not an attribute of the SDK type produced %d problem(s), want 1: %v",
			len(problems), problems)
	}

	none := scatterField()
	none.Wires = nil
	if problems := WireNameProblems(Spec[scatterModel, scatterSDK]{
		TypeName: "unifi_scatter",
		Fields:   []Field[scatterModel, scatterSDK]{none},
	}); len(problems) != 1 {
		t.Errorf("a field naming no attribute produced %d problem(s), want 1", len(problems))
	}
}

// THE REGRESSION CASE THE LEAD NAMED. firewall_policy uses ObjectField with
// three real nested structs behind it, and it is the discriminator that proved
// the seven genuinely differ. A field that names one attribute must still
// contribute exactly one, through the same accessor every mask consumer now
// uses.
func TestASingleNameFieldStillContributesExactlyOne(t *testing.T) {
	field := StringField[scatterModel, scatterSDK]{
		Wire:  "unrelated",
		Model: func(m *scatterModel) *types.String { return &unrelatedHolder },
		SDK:   func(s *scatterSDK) *string { return &s.Unrelated },
	}
	names := fieldWireNames[scatterModel, scatterSDK](field)
	if !reflect.DeepEqual(names, []string{"unrelated"}) {
		t.Errorf("a single-name field contributed %v", names)
	}
}

func scatterString(t *testing.T, attrs map[string]attr.Value, name string) string {
	t.Helper()
	value, ok := attrs[name].(types.String)
	if !ok {
		t.Fatalf("%s is %T, not a string", name, attrs[name])
	}
	return value.ValueString()
}

// AlwaysWire was flagged as an unexamined interaction, so it is asserted rather
// than reasoned about. A hook's names dedupe against the same set the fields
// filled, and it SKIPS a duplicate where the field loop ERRORS on one -- so a
// scattered field already carrying a name a hook also derives is fine, and the
// name appears once.
func TestScatteredObjectNamesDedupeAgainstAlwaysWire(t *testing.T) {
	spec := Spec[scatterModel, scatterSDK]{
		TypeName:   "unifi_scatter",
		Fields:     []Field[scatterModel, scatterSDK]{scatterField()},
		AlwaysWire: []string{"wireguard_interface", "unrelated"},
	}
	plan := &scatterModel{Wireguard: scatterObject(t, "abc", "wg0")}

	fields, err := spec.WireFields(plan)
	if err != nil {
		t.Fatalf("a hook naming an attribute a scattered field already carries: %v", err)
	}
	counts := map[string]int{}
	for _, name := range fields {
		counts[name]++
	}
	if counts["wireguard_interface"] != 1 {
		t.Errorf("wireguard_interface appears %d time(s); a mask naming a field twice is "+
			"refused by go-unifi", counts["wireguard_interface"])
	}
	// CONTROL: the hook's OWN name must still arrive, or this passes against a
	// merge that dropped AlwaysWire entirely.
	if counts["unrelated"] != 1 {
		t.Errorf("the hook's own name did not reach the mask: %v", fields)
	}
}

// The other side of the same seam: two fields claiming one attribute is a
// descriptor bug the mask cannot express, and it was UNDETECTABLE before a field
// could name more than one. Now it is an error at build time.
func TestTwoFieldsClaimingOneAttributeAreRefused(t *testing.T) {
	overlapping := scatterField()
	overlapping.Wires = []string{"wireguard_interface", "unrelated"}
	spec := Spec[scatterModel, scatterSDK]{
		TypeName: "unifi_scatter",
		Fields: []Field[scatterModel, scatterSDK]{
			scatterField(),
			overlapping,
		},
	}
	plan := &scatterModel{Wireguard: scatterObject(t, "abc", "wg0")}
	if _, err := spec.WireFields(plan); err == nil {
		t.Error("two fields naming wireguard_interface produced a mask rather than an error")
	}
}

// A WIRE Encode WRITES ONLY SOMETIMES MUST NOT BE MASKED WHEN IT WILL NOT BE.
//
// go-unifi sends a masked field's ZERO when the object carries no value, so a
// mask naming a wire Encode left alone CLEARS whatever the controller holds.
// vpn_client is the case: two of its wireguard object's ten wires -- dhcpd_dns_1
// and dhcpd_dns_2 -- are written only when the practitioner supplies
// dns_servers, and the hand-written mask this kind replaces omits exactly those
// two for exactly this reason.
//
// Measured before it was built: the field as it stood put both names in the
// mask with the SDK object carrying empty strings, so every apply setting a
// wireguard block without dns_servers would have blanked the controller's DNS.
// The descriptor compiled and every existing check passed.
func scatterFieldWithConditionalInterface() ScatteredObjectField[scatterModel, scatterSDK] {
	field := scatterField()
	field.ConditionalWires = map[string]func(types.Object) bool{
		"wireguard_interface": func(object types.Object) bool {
			iface, ok := object.Attributes()["interface"].(types.String)
			return ok && !iface.IsNull() && iface.ValueString() != ""
		},
	}
	return field
}

func TestAConditionalWireLeavesTheMaskWhenItsMemberIsUnset(t *testing.T) {
	spec := Spec[scatterModel, scatterSDK]{
		TypeName: "unifi_scatter",
		Fields:   []Field[scatterModel, scatterSDK]{scatterFieldWithConditionalInterface()},
	}
	plan := &scatterModel{Wireguard: scatterObject(t, "abc", "")}

	fields, err := spec.WireFields(plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	if slices.Contains(fields, "wireguard_interface") {
		t.Error("wireguard_interface is in the mask although Encode will not write it; " +
			"go-unifi sends a masked field's zero, so this clears the controller's value")
	}
	// THE CONTROL, AND IT IS THE HALF THAT MATTERS. Dropping a name from the
	// mask is also what a broken predicate does, and a field that masks nothing
	// is the silent write-drop this kind exists to prevent. The wires with no
	// condition must still travel.
	for _, wire := range []string{
		"wireguard_private_key",
		"wireguard_client_preshared_key_enabled",
	} {
		if !slices.Contains(fields, wire) {
			t.Errorf("%s left the mask too; only the conditional wire may", wire)
		}
	}
}

func TestAConditionalWireJoinsTheMaskWhenItsMemberIsSet(t *testing.T) {
	spec := Spec[scatterModel, scatterSDK]{
		TypeName: "unifi_scatter",
		Fields:   []Field[scatterModel, scatterSDK]{scatterFieldWithConditionalInterface()},
	}
	plan := &scatterModel{Wireguard: scatterObject(t, "abc", "wg0")}

	fields, err := spec.WireFields(plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	if !slices.Contains(fields, "wireguard_interface") {
		t.Error("wireguard_interface is absent from the mask although the practitioner set " +
			"it; a value that is set and not masked is one the apply silently drops")
	}
}

// THE DECLARED SET AND THE MASKED SET ANSWER DIFFERENT QUESTIONS, and the
// checks must keep reading the first. A conditional wire is still a wire this
// field can write, so it still has to be a real attribute of the SDK type --
// narrowing what the checks see would make a typo in a conditional name
// unverifiable exactly when the plan happens not to trigger it.
func TestAConditionalWireIsStillCheckedAgainstTheSDK(t *testing.T) {
	field := scatterFieldWithConditionalInterface()
	if names := field.wireNames(); !slices.Contains(names, "wireguard_interface") {
		t.Errorf("wireNames() = %v, want the conditional wire among them", names)
	}
	spec := Spec[scatterModel, scatterSDK]{
		TypeName: "unifi_scatter",
		Fields:   []Field[scatterModel, scatterSDK]{field},
	}
	if problems := WireNameProblems(spec); len(problems) != 0 {
		t.Errorf("WireNameProblems = %v, want none", problems)
	}
}

// A CONDITION ON A NAME THAT IS NOT A WIRE READS AS A GUARD AND IS NONE. The
// key matches nothing, the wire it was meant to guard stays unconditional, and
// the descriptor says in writing that it does not -- a masked zero reaching the
// controller with a comment above it explaining why it cannot.
func TestAConditionOnAnUnknownWireIsRefused(t *testing.T) {
	field := scatterField()
	// A NEAR-MISS RATHER THAN A NONSENSE STRING, because the failure this
	// guards is a transcription slip and a probe that could not plausibly be
	// typed proves the check fires on something nobody would write.
	field.ConditionalWires = map[string]func(types.Object) bool{
		"wireguard_iface": func(types.Object) bool { return false },
	}
	spec := Spec[scatterModel, scatterSDK]{
		TypeName: "unifi_scatter",
		Fields:   []Field[scatterModel, scatterSDK]{field},
	}
	problems := WireNameProblems(spec)
	if len(problems) == 0 {
		t.Fatal("a condition naming no wire was accepted")
	}
	if !strings.Contains(problems[0], "guards nothing") {
		t.Errorf("problem = %q, want it to say the condition guards nothing", problems[0])
	}
}

// Decode's prior parameter is this field's own object as it stood in state when
// the read began, and the two things that need it are one missing input seen
// twice.
//
// MERGING. A controller that omits a member says nothing about it. wan's read
// path guards every assignment with `if network.X != nil` for that reason --
// eight of its ten objects keep the prior value per member -- and a Decode built
// only from *S has to write the zero instead.
//
// ELIDING. An object the controller returned nothing for and the practitioner
// never set must stay NULL rather than materialise fully zeroed. A null prior
// with no API data is exactly that case, which is why one parameter answers both.
func TestScatteredObjectDecodeReceivesThePriorObject(t *testing.T) {
	ctx := context.Background()
	field := scatterField()
	field.Decode = func(
		_ context.Context, sdk *scatterSDK, prior types.Object,
	) (types.Object, diag.Diagnostics) {
		// THE ELIDE CASE: nothing from the controller and nothing held before.
		if sdk.WireguardInterface == "" && prior.IsNull() {
			return types.ObjectNull(scatterAttrs), nil
		}
		// THE MERGE CASE: keep what the object held for a member the controller
		// did not return.
		iface := types.StringValue(sdk.WireguardInterface)
		key := types.StringNull()
		if !prior.IsNull() {
			if held, ok := prior.Attributes()["private_key"].(types.String); ok {
				key = held
			}
		}
		object, diags := types.ObjectValue(scatterAttrs, map[string]attr.Value{
			"private_key":           key,
			"preshared_key_enabled": types.BoolValue(sdk.WireguardPresharedK),
			"interface":             iface,
		})
		return object, diags
	}

	t.Run("a member the controller omits keeps what state held", func(t *testing.T) {
		state := &scatterModel{Wireguard: scatterObject(t, "held-secret", "wg-old")}
		sdk := &scatterSDK{WireguardInterface: "wg-new"}
		if diags := field.ToModel(ctx, sdk, state); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		got := state.Wireguard.Attributes()
		if v := scatterString(t, got, "private_key"); v != "held-secret" {
			t.Errorf("the prior value was lost: private_key = %q", v)
		}
		// CONTROL: the value the controller DID return must win, or a Decode
		// that ignored the SDK entirely would satisfy the assertion above.
		if v := scatterString(t, got, "interface"); v != "wg-new" {
			t.Errorf("the controller's value did not land: interface = %q", v)
		}
	})

	t.Run("nothing held and nothing returned stays null", func(t *testing.T) {
		state := &scatterModel{Wireguard: types.ObjectNull(scatterAttrs)}
		if diags := field.ToModel(ctx, &scatterSDK{}, state); diags.HasError() {
			t.Fatalf("ToModel: %v", diags)
		}
		if !state.Wireguard.IsNull() {
			t.Errorf("an unset object materialised as %v", state.Wireguard)
		}
	})
}

// THE PRIOR MUST BE READ BEFORE IT IS OVERWRITTEN, which is the whole of what
// makes this possible and is one statement's ordering away from being useless.
func TestScatteredObjectDecodeSeesTheStateValueNotTheDecodedOne(t *testing.T) {
	field := scatterField()
	var seen types.Object
	field.Decode = func(
		_ context.Context, _ *scatterSDK, prior types.Object,
	) (types.Object, diag.Diagnostics) {
		seen = prior
		return scatterObject(t, "decoded", "decoded"), nil
	}
	state := &scatterModel{Wireguard: scatterObject(t, "from-state", "from-state")}
	if diags := field.ToModel(context.Background(), &scatterSDK{}, state); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if seen.IsNull() {
		t.Fatal("Decode was handed a null prior; the model was overwritten first")
	}
	if v := scatterString(t, seen.Attributes(), "private_key"); v != "from-state" {
		t.Errorf("Decode saw %q, so it was handed the value it had just produced", v)
	}
}
