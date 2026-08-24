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
					t.Errorf("an absent object put %q in the mask, which would "+
						"write zero over an attribute the config never mentioned", name)
				}
			}

			// The control: the SDK carries values the caller put there, and an
			// absent object must leave every one of them alone, or the
			// assertion above would pass against a kind that writes zeros anyway.
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
	// Asserted on the struct fields, not on a round trip alone: a symmetric
	// Encode/Decode pair that wrote all three values to one field would
	// round trip and still be wrong.
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
	// The control: the hook's own name must still arrive, or this passes
	// against a merge that dropped AlwaysWire entirely.
	if counts["unrelated"] != 1 {
		t.Errorf("the hook's own name did not reach the mask: %v", fields)
	}
}

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
	// The control, and the half that matters: dropping a name from the mask
	// is also what a broken predicate does, so the wires with no condition
	// must still travel, or a field that masks nothing would pass too.
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

func TestAConditionOnAnUnknownWireIsRefused(t *testing.T) {
	field := scatterField()
	// A near-miss rather than a nonsense string: the failure this guards is
	// a transcription slip, and a probe nobody could plausibly type would
	// prove less about the check.
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

func TestScatteredObjectDecodeReceivesThePriorObject(t *testing.T) {
	ctx := context.Background()
	field := scatterField()
	field.Decode = func(
		_ context.Context, sdk *scatterSDK, prior types.Object,
	) (types.Object, diag.Diagnostics) {
		// The elide case: nothing from the controller and nothing held before.
		if sdk.WireguardInterface == "" && prior.IsNull() {
			return types.ObjectNull(scatterAttrs), nil
		}
		// The merge case: keep what the object held for a member the
		// controller did not return.
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
		// The control: the value the controller did return must win, or a
		// Decode that ignored the SDK entirely would satisfy the assertion above.
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

func TestScatteredCopyPlanToStateKeepsAComputedMemberThePlanLeftNull(t *testing.T) {
	attrTypes := map[string]attr.Type{
		"private_key": types.StringType,
		"public_key":  types.StringType,
	}
	field := ScatteredObjectField[scatterModel, kitSDK]{
		Wires:     []string{"x_wireguard_private_key"},
		Model:     func(m *scatterModel) *types.Object { return &m.Wireguard },
		AttrTypes: attrTypes,
	}
	object := func(private, public attr.Value) types.Object {
		built, diags := types.ObjectValue(attrTypes, map[string]attr.Value{
			"private_key": private, "public_key": public,
		})
		if diags.HasError() {
			t.Fatalf("building probe object: %v", diags)
		}
		return built
	}
	state := scatterModel{Wireguard: object(types.StringValue("priv"), types.StringValue("THE-KEY"))}
	plan := scatterModel{Wireguard: object(types.StringValue("priv"), types.StringNull())}
	field.CopyPlanToState(&plan, &state)

	public, ok := state.Wireguard.Attributes()["public_key"].(types.String)
	if !ok || public.ValueString() != "THE-KEY" {
		t.Errorf("public_key = %v, want THE-KEY; the plan's null erased the computed member",
			state.Wireguard.Attributes()["public_key"])
	}
}
