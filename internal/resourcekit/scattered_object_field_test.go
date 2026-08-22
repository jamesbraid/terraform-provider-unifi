package resourcekit

import (
	"context"
	"reflect"
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
		Decode: func(_ context.Context, sdk *scatterSDK) (types.Object, diag.Diagnostics) {
			return types.ObjectValue(scatterAttrs, map[string]attr.Value{
				"private_key":           types.StringValue(sdk.WireguardPrivateKey),
				"preshared_key_enabled": types.BoolValue(sdk.WireguardPresharedK),
				"interface":             types.StringValue(sdk.WireguardInterface),
			})
		},
	}
}

func scatterObject(t *testing.T, key, iface string, preshared bool) types.Object {
	t.Helper()
	object, diags := types.ObjectValue(scatterAttrs, map[string]attr.Value{
		"private_key":           types.StringValue(key),
		"preshared_key_enabled": types.BoolValue(preshared),
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
	plan := &scatterModel{Wireguard: scatterObject(t, "abc", "wg0", true)}

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
	model := &scatterModel{Wireguard: scatterObject(t, "privkey", "wg1", true)}
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
	state := &scatterModel{Wireguard: scatterObject(t, "from-read", "wg-read", true)}

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
