package unifi

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func TestAccWLANFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWLANFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_wlan.test", "name", "wlan1"),
					resource.TestCheckResourceAttr("unifi_wlan.test", "security", "wpapsk"),
					resource.TestCheckResourceAttr("unifi_wlan.test", "passphrase", "passphrase"),
					resource.TestCheckResourceAttr("unifi_wlan.test", "hide_ssid", "false"),
					resource.TestCheckResourceAttr("unifi_wlan.test", "mac_filter.enabled", "true"),
					resource.TestCheckResourceAttr("unifi_wlan.test", "mac_filter.policy", "allow"),
					resource.TestCheckResourceAttr("unifi_wlan.test", "mac_filter.list.#", "1"),
				),
			},
			{
				ResourceName:  "unifi_wlan.test",
				ImportState:   true,
				ImportStateId: "wlan1",
			},
		},
	})
}

func testAccWLANFrameworkConfig_basic() string {
	return `
data "unifi_client_qos_rate" "default" {
	name = "Default"
}

data "unifi_network" "default" {
	name = "Default"
}

resource "unifi_wlan" "test" {
	name          = "wlan1"
	security      = "wpapsk"
	passphrase    = "passphrase"
	hide_ssid     = false
	user_group_id = data.unifi_client_qos_rate.default.id
	network_id    = data.unifi_network.default.id
	mac_filter = {
		enabled = true
		policy  = "allow"
		list    = ["00:11:22:33:44:55"]
	}
}
`
}

// TestAccWLANFramework_additionalFields verifies that the newly exposed
// security/DTIM/toggle attributes are populated by the read path once a WLAN
// exists, following the same create/check-then-import shape as the basic
// test above.
func TestAccWLANFramework_additionalFields(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWLANFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_wlan.test", "wpa_mode"),
					resource.TestCheckResourceAttrSet("unifi_wlan.test", "wpa_enc"),
					resource.TestCheckResourceAttrSet("unifi_wlan.test", "dtim_mode"),
					resource.TestCheckResourceAttrSet("unifi_wlan.test", "group_rekey"),
					resource.TestCheckResourceAttrSet("unifi_wlan.test", "iapp_enabled"),
					resource.TestCheckResourceAttrSet("unifi_wlan.test", "mlo_enabled"),
					// No default here deliberately: the controller overrides
					// this in auto mode (why it's Computed), and 0 is a rate a
					// practitioner may legitimately request, so asserting "0"
					// for "the controller said nothing" would assert
					// something the controller never said.
					//
					// The absence of drift is inferred from the schema and has
					// not been observed. This step is what would show it, so a
					// failure here is a finding, not a stale expectation.
					resource.TestCheckNoResourceAttr(
						"unifi_wlan.test",
						"minimum_data_rate_2g_kbps",
					),
					resource.TestCheckNoResourceAttr(
						"unifi_wlan.test",
						"minimum_data_rate_5g_kbps",
					),
				),
			},
			{
				ResourceName:  "unifi_wlan.test",
				ImportState:   true,
				ImportStateId: "wlan1",
			},
		},
	})
}

func TestNewWLANFrameworkResource(t *testing.T) {
	got := NewWLANFrameworkResource()
	if got == nil {
		t.Fatal("NewWLANFrameworkResource() returned nil")
	}
	// Verify interface compliance
	_ = got
	if _, ok := got.(fwresource.ResourceWithImportState); !ok {
		t.Errorf("does not implement fwresource.ResourceWithImportState")
	}
	if _, ok := got.(fwresource.ResourceWithIdentity); !ok {
		t.Errorf("does not implement fwresource.ResourceWithIdentity")
	}
	if _, ok := got.(fwresource.ResourceWithUpgradeState); !ok {
		t.Errorf("does not implement fwresource.ResourceWithUpgradeState")
	}
}

func TestNewWLANListResource(t *testing.T) {
	got := NewWLANListResource()
	if got == nil {
		t.Fatal("NewWLANListResource() returned nil")
	}
	_ = got
	if _, ok := got.(fwlist.ListResourceWithConfigure); !ok {
		t.Errorf("does not implement fwlist.ListResourceWithConfigure")
	}
}

func Test_wlanPrivatePresharedKeyModel_AttributeTypes(t *testing.T) {
	tests := []struct {
		name string
		m    wlanPrivatePresharedKeyModel
		want map[string]attr.Type
	}{
		{
			name: "returns correct attribute types",
			m:    wlanPrivatePresharedKeyModel{},
			want: map[string]attr.Type{
				"network_id": types.StringType,
				"password":   types.StringType,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.AttributeTypes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"wlanPrivatePresharedKeyModel.AttributeTypes() = %v, want %v",
					got,
					tt.want,
				)
			}
		})
	}
}

func Test_wlanFrameworkResource_IdentitySchema(t *testing.T) {
	type args struct {
		in0  context.Context
		in1  fwresource.IdentitySchemaRequest
		resp *fwresource.IdentitySchemaResponse
	}
	tests := []struct {
		name string
		r    *wlanFrameworkResource
		args args
	}{
		{
			name: "does not panic",
			r:    &wlanFrameworkResource{},
			args: args{
				in0:  context.Background(),
				in1:  fwresource.IdentitySchemaRequest{},
				resp: &fwresource.IdentitySchemaResponse{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.IdentitySchema(tt.args.in0, tt.args.in1, tt.args.resp)
			if tt.args.resp.Diagnostics.HasError() {
				t.Fatalf("IdentitySchema() diagnostics: %v", tt.args.resp.Diagnostics.Errors())
			}
			if len(tt.args.resp.IdentitySchema.Attributes) == 0 {
				t.Fatal("IdentitySchema() returned no attributes")
			}
		})
	}
}

// Fields the controller assigns on its own must be Computed so a
// controller-supplied value doesn't trip "inconsistent result after apply".
func Test_wlanFrameworkResource_Schema_computedControllerFields(t *testing.T) {
	resp := &fwresource.SchemaResponse{}
	(&wlanFrameworkResource{}).Schema(context.Background(), fwresource.SchemaRequest{}, resp)

	for _, key := range []string{
		"minimum_data_rate_2g_kbps",
		"minimum_data_rate_5g_kbps",
		"radius_profile_id",
		"bc_filter_list",
		// UniFi Network 10.x replaced unifi_device.radio_table.assisted_roaming_*
		// with these per-WLAN attributes.
		"roaming_assistant_na_enabled",
		"roaming_assistant_na_rssi",
		"roaming_assistant_6e_enabled",
		"roaming_assistant_6e_rssi",
	} {
		attr, ok := resp.Schema.Attributes[key]
		if !ok {
			t.Errorf("Schema missing attribute %q", key)
			continue
		}
		if !attr.IsComputed() {
			t.Errorf("attribute %q must be Computed (controller-managed)", key)
		}
	}
}

// Test_wlanFrameworkResource_Schema_noAssistedRoaming guards against the
// removed unifi_device attributes being reintroduced here under their old
// names. The per-WLAN replacements are spelled roaming_assistant_*.
func Test_wlanFrameworkResource_Schema_noAssistedRoaming(t *testing.T) {
	resp := &fwresource.SchemaResponse{}
	(&wlanFrameworkResource{}).Schema(context.Background(), fwresource.SchemaRequest{}, resp)

	for _, key := range []string{"assisted_roaming_enabled", "assisted_roaming_rssi"} {
		if _, ok := resp.Schema.Attributes[key]; ok {
			t.Errorf("attribute %q should not exist; use roaming_assistant_* instead", key)
		}
	}
}

func Test_wlanFrameworkResource_UpgradeState(t *testing.T) {
	r := newWLANKitResource()
	got := r.UpgradeState(context.Background())
	if got == nil {
		t.Fatal("UpgradeState() returned nil")
	}
	if _, ok := got[0]; !ok {
		t.Error("UpgradeState() missing key 0")
	}
}

func Test_wlanFrameworkResource_planToWLAN(t *testing.T) {
	ctx := context.Background()
	spec := wlanKitSpec()

	plan := wlanKitModel{
		Name:     types.StringValue("test"),
		Security: types.StringValue("wpapsk"),
		MacFilter: types.ObjectNull(map[string]attr.Type{
			"enabled": types.BoolType,
			"list":    types.SetType{ElemType: types.StringType},
			"policy":  types.StringType,
		}),
		PrivatePresharedKeys: types.ListNull(
			types.ObjectType{AttrTypes: wlanPrivatePresharedKeyModel{}.AttributeTypes()},
		),
		ApGroupIDs:          types.SetNull(types.StringType),
		WLANBands:           types.SetNull(types.StringType),
		Schedule:            types.ListNull(types.ObjectType{}),
		BroadcastFilterList: types.SetNull(types.StringType),
	}

	got, diags := spec.ToSDK(ctx, &plan)
	if diags.HasError() {
		t.Fatalf("ToSDK() diagnostics: %v", diags)
	}
	if got.Name != "test" {
		t.Errorf("Name = %q, want %q", got.Name, "test")
	}
	if got.Security != "wpapsk" {
		t.Errorf("Security = %q, want %q", got.Security, "wpapsk")
	}
	// nil is correct here: the guard that used to force an empty slice (so
	// planToWLAN wouldn't marshal null) isn't carried into the descriptor,
	// since go-unifi's omitempty tag now drops a zero-length slice either way.
	if got.ScheduleWithDuration != nil {
		t.Errorf("ScheduleWithDuration = %v, want nil for an absent schedule", got.ScheduleWithDuration)
	}
}

// TestWLANDtimFieldsOmitAnUnknownRatherThanAZero pins the create-time fix: the
// three dtim fields are Optional+Computed with no schema default, so an
// unset one is Unknown on create (UseStateForUnknown has no prior state to
// fall back to), and Int64PtrField.ToSDK without OmitZero sent
// ValueInt64Pointer()'s pointer to zero -- which the controller's
// ^([1-9]|...|25[0-5])$|^$ validator on every one of the three rejects
// (api.err.InvalidValue). OmitZero must keep an Unknown value off the wire
// and out of the update mask, while a real value still reaches both.
func TestWLANDtimFieldsOmitAnUnknownRatherThanAZero(t *testing.T) {
	ctx := context.Background()
	spec := wlanKitSpec()

	plan := wlanKitModel{
		Name:     types.StringValue("test"),
		Security: types.StringValue("wpapsk"),
		MacFilter: types.ObjectNull(map[string]attr.Type{
			"enabled": types.BoolType,
			"list":    types.SetType{ElemType: types.StringType},
			"policy":  types.StringType,
		}),
		PrivatePresharedKeys: types.ListNull(
			types.ObjectType{AttrTypes: wlanPrivatePresharedKeyModel{}.AttributeTypes()},
		),
		ApGroupIDs:          types.SetNull(types.StringType),
		WLANBands:           types.SetNull(types.StringType),
		Schedule:            types.ListNull(types.ObjectType{}),
		BroadcastFilterList: types.SetNull(types.StringType),

		DTIMNg: types.Int64Unknown(),
		DTIMNa: types.Int64Null(),
		DTIM6E: types.Int64Value(3),
	}

	got, diags := spec.ToSDK(ctx, &plan)
	if diags.HasError() {
		t.Fatalf("ToSDK() diagnostics: %v", diags)
	}
	if got.DTIMNg != nil {
		t.Errorf("DTIMNg = %d, want nil: an Unknown value must not reach the wire as a "+
			"pointer to zero", *got.DTIMNg)
	}
	if got.DTIMNa != nil {
		t.Errorf("DTIMNa = %d, want nil for a null plan value", *got.DTIMNa)
	}
	// The control: a real value is unaffected by OmitZero and still reaches
	// the wire, or the assertions above would hold for a ToSDK that never
	// writes anything.
	if got.DTIM6E == nil || *got.DTIM6E != 3 {
		t.Fatalf("DTIM6E = %v, want a pointer to 3", got.DTIM6E)
	}

	mask, err := spec.WireFields(&plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	for _, name := range []string{"dtim_ng", "dtim_na"} {
		if slices.Contains(mask, name) {
			t.Errorf("%s is in the update mask; an Unknown/null plan value should not be "+
				"named in a masked update either", name)
		}
	}
	if !slices.Contains(mask, "dtim_6e") {
		t.Error("dtim_6e is missing from the update mask; a practitioner-set value would " +
			"never be written")
	}
}

func Test_wlanFrameworkResource_wlanToModel(t *testing.T) {
	ctx := context.Background()
	spec := wlanKitSpec()

	wlan := &unifi.WLAN{
		ID:       "wlan-123",
		Name:     "test-wlan",
		Security: "wpapsk",
	}
	var model wlanKitModel
	diags := spec.ToModel(ctx, wlan, &model, "default")
	if diags.HasError() {
		t.Fatalf("ToModel() diagnostics: %v", diags)
	}
	if model.ID.ValueString() != "wlan-123" {
		t.Errorf("ID = %q, want %q", model.ID.ValueString(), "wlan-123")
	}
	if model.Name.ValueString() != "test-wlan" {
		t.Errorf("Name = %q, want %q", model.Name.ValueString(), "test-wlan")
	}
	if model.Site.ValueString() != "default" {
		t.Errorf("Site = %q, want %q", model.Site.ValueString(), "default")
	}
	if model.Security.ValueString() != "wpapsk" {
		t.Errorf("Security = %q, want %q", model.Security.ValueString(), "wpapsk")
	}
}

func Test_wlanFrameworkResource_ListResourceConfigSchema(t *testing.T) {
	r := newWLANKitResource()
	resp := &fwlist.ListResourceSchemaResponse{}
	r.ListResourceConfigSchema(context.Background(), fwlist.ListResourceSchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("ListResourceConfigSchema() diagnostics: %v", resp.Diagnostics.Errors())
	}
	if len(resp.Schema.Attributes) == 0 {
		t.Fatal("ListResourceConfigSchema() returned no attributes")
	}
}

// A WLAN whose secret is managed by passphrase_wo must never let the
// controller's echo land in the (non-write-only) passphrase attribute --
// neither in the apply that used passphrase_wo, nor in a later bare refresh.
// wlanBeforeSend's stash (model.PassphraseWO) answers the question for the
// first case; prior's own nullness, which is all a refresh has since Read
// never calls BeforeSend, answers it for the second.
func TestWLANAfterReceive_passphraseWriteOnly(t *testing.T) {
	sdk := &unifi.WLAN{Passphrase: "controller-echo"}

	t.Run("this apply used passphrase_wo", func(t *testing.T) {
		model := &wlanKitModel{
			Passphrase:   types.StringValue("controller-echo"),
			PassphraseWO: types.StringValue("configured-secret"), // wlanBeforeSend's stash
		}
		prior := wlanKitModel{} // create: nothing persisted yet
		if diags := wlanAfterReceive(context.Background(), sdk, model, prior, nil); diags.HasError() {
			t.Fatalf("wlanAfterReceive: %v", diags)
		}
		if !model.Passphrase.IsNull() {
			t.Errorf("Passphrase = %v, want null", model.Passphrase)
		}
		if !model.PassphraseWO.IsNull() {
			t.Errorf("PassphraseWO = %v, want null (never persisted)", model.PassphraseWO)
		}
	})

	t.Run("bare refresh of a passphrase_wo-managed WLAN", func(t *testing.T) {
		model := &wlanKitModel{
			Passphrase: types.StringValue("controller-echo"), // what ToModel just wrote
		}
		prior := wlanKitModel{Passphrase: types.StringNull()} // last apply's state
		if diags := wlanAfterReceive(context.Background(), sdk, model, prior, nil); diags.HasError() {
			t.Fatalf("wlanAfterReceive: %v", diags)
		}
		if !model.Passphrase.IsNull() {
			t.Errorf(
				"Passphrase = %v, want null (a refresh must not resurrect the echo)",
				model.Passphrase,
			)
		}
	})

	t.Run("config-managed passphrase keeps round-tripping", func(t *testing.T) {
		model := &wlanKitModel{
			Passphrase: types.StringValue("controller-echo"),
		}
		prior := wlanKitModel{Passphrase: types.StringValue("previous-secret")}
		if diags := wlanAfterReceive(context.Background(), sdk, model, prior, nil); diags.HasError() {
			t.Fatalf("wlanAfterReceive: %v", diags)
		}
		if model.Passphrase.ValueString() != "controller-echo" {
			t.Errorf("Passphrase = %v, want the echoed value unchanged", model.Passphrase)
		}
	})
}

// TestWLANPrivatePresharedKeys_roundTrip exercises the private pre-shared
// key (PPSK) mapping: a plan carrying PPSK entries must be translated to
// the go-unifi WLAN struct (Spec.ToSDK) and back into the resource model
// (Spec.ToModel) without losing the per-key network binding or password.
func TestWLANPrivatePresharedKeys_roundTrip(t *testing.T) {
	ctx := context.Background()
	spec := wlanKitSpec()

	ppskType := types.ObjectType{AttrTypes: wlanPrivatePresharedKeyModel{}.AttributeTypes()}
	ppskList, d := types.ListValueFrom(ctx, ppskType, []wlanPrivatePresharedKeyModel{
		{NetworkID: types.StringValue("net-a"), Password: types.StringValue("secretpass1")},
		{NetworkID: types.StringValue(""), Password: types.StringValue("secretpass2")},
	})
	if d.HasError() {
		t.Fatalf("building PPSK list: %v", d)
	}

	plan := wlanKitModel{
		Name:                        types.StringValue("ppsk-wlan"),
		Security:                    types.StringValue("wpapsk"),
		PrivatePresharedKeysEnabled: types.BoolValue(true),
		PrivatePresharedKeys:        ppskList,
	}

	// plan -> API
	wlan, diags := spec.ToSDK(ctx, &plan)
	if diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	if !wlan.PrivatePresharedKeysEnabled {
		t.Errorf("PrivatePresharedKeysEnabled = false, want true")
	}
	if got := len(wlan.PrivatePresharedKeys); got != 2 {
		t.Fatalf("PrivatePresharedKeys len = %d, want 2", got)
	}
	if wlan.PrivatePresharedKeys[0].NetworkID != "net-a" ||
		wlan.PrivatePresharedKeys[0].Password != "secretpass1" {
		t.Errorf("PPSK[0] = %+v, want {net-a secretpass1}", wlan.PrivatePresharedKeys[0])
	}
	if wlan.PrivatePresharedKeys[1].NetworkID != "" ||
		wlan.PrivatePresharedKeys[1].Password != "secretpass2" {
		t.Errorf("PPSK[1] = %+v, want { secretpass2}", wlan.PrivatePresharedKeys[1])
	}

	// API -> model
	var model wlanKitModel
	if diags := spec.ToModel(ctx, wlan, &model, "default"); diags.HasError() {
		t.Fatalf("wlanToModel: %v", diags)
	}
	if !model.PrivatePresharedKeysEnabled.ValueBool() {
		t.Errorf("model.PrivatePresharedKeysEnabled = false, want true")
	}
	if model.PrivatePresharedKeys.IsNull() {
		t.Fatalf("model.PrivatePresharedKeys is null, want 2 entries")
	}
	var got []wlanPrivatePresharedKeyModel
	if diags := model.PrivatePresharedKeys.ElementsAs(ctx, &got, false); diags.HasError() {
		t.Fatalf("decoding model PPSK: %v", diags)
	}
	if len(got) != 2 {
		t.Fatalf("model PPSK len = %d, want 2", len(got))
	}
	if got[0].NetworkID.ValueString() != "net-a" ||
		got[0].Password.ValueString() != "secretpass1" {
		t.Errorf("model PPSK[0] = %+v, want {net-a secretpass1}", got[0])
	}
}

// TestWLANPrivatePresharedKeys_emptyIsNull verifies that a WLAN without PPSK
// entries reads back as a null list (not an empty list), avoiding spurious
// plan drift for WLANs that don't use private pre-shared keys.
func TestWLANPrivatePresharedKeys_emptyIsNull(t *testing.T) {
	ctx := context.Background()
	spec := wlanKitSpec()

	var model wlanKitModel
	if diags := spec.ToModel(ctx, &unifi.WLAN{}, &model, "default"); diags.HasError() {
		t.Fatalf("wlanToModel: %v", diags)
	}
	if model.PrivatePresharedKeysEnabled.ValueBool() {
		t.Errorf("PrivatePresharedKeysEnabled = true, want false")
	}
	if !model.PrivatePresharedKeys.IsNull() {
		t.Errorf("PrivatePresharedKeys = %v, want null", model.PrivatePresharedKeys)
	}
}

// A refresh has no plan to fall back on the way Create and Update do: if
// UniFi returns the same PPSK bindings in another order, or omits a
// password UniFi is redacting rather than clearing, ToModel's plain decode
// would replace the configured state and manufacture drift.
// wlanPrivatePresharedKeysState is called from wlanAfterReceive rather than
// from ToModel because ToModel has no argument for what state used to hold.
func TestWLANPrivatePresharedKeys_preservesStateForPartialResponse(t *testing.T) {
	ctx := context.Background()
	ppskType := types.ObjectType{AttrTypes: wlanPrivatePresharedKeyModel{}.AttributeTypes()}
	prior, diags := types.ListValueFrom(ctx, ppskType, []wlanPrivatePresharedKeyModel{
		{NetworkID: types.StringValue("net-a"), Password: types.StringValue("secretpass1")},
		{NetworkID: types.StringValue("net-b"), Password: types.StringValue("secretpass2")},
	})
	if diags.HasError() {
		t.Fatalf("building prior PPSK list: %v", diags)
	}

	for name, keys := range map[string][]unifi.WLANPrivatePresharedKeys{
		"passwords omitted and list reordered": {
			{NetworkID: "net-b"},
			{NetworkID: "net-a"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			wlan := &unifi.WLAN{
				PrivatePresharedKeysEnabled: true,
				PrivatePresharedKeys:        keys,
			}
			got, diags := wlanPrivatePresharedKeysState(ctx, wlan, prior)
			if diags.HasError() {
				t.Fatalf("wlanPrivatePresharedKeysState: %v", diags)
			}
			if !got.Equal(prior) {
				t.Fatalf("PrivatePresharedKeys = %v, want %v", got, prior)
			}
		})
	}
}

func TestWLANPrivatePresharedKeys_usesControllerChanges(t *testing.T) {
	ctx := context.Background()
	ppskType := types.ObjectType{AttrTypes: wlanPrivatePresharedKeyModel{}.AttributeTypes()}
	list := func(keys ...wlanPrivatePresharedKeyModel) types.List {
		value, diags := types.ListValueFrom(ctx, ppskType, keys)
		if diags.HasError() {
			t.Fatalf("building PPSK list: %v", diags)
		}
		return value
	}
	prior := list(
		wlanPrivatePresharedKeyModel{
			NetworkID: types.StringValue("net-a"),
			Password:  types.StringValue("secretpass1"),
		},
	)
	duplicateBindings := list(
		wlanPrivatePresharedKeyModel{
			NetworkID: types.StringValue("net-a"),
			Password:  types.StringValue("secretpass1"),
		},
		wlanPrivatePresharedKeyModel{
			NetworkID: types.StringValue("net-a"),
			Password:  types.StringValue("secretpass2"),
		},
	)

	tests := []struct {
		name  string
		wlan  *unifi.WLAN
		prior types.List
		want  types.List
	}{
		{
			name:  "disabled",
			wlan:  &unifi.WLAN{},
			prior: prior,
			want:  types.ListNull(ppskType),
		},
		{
			name:  "list omitted",
			wlan:  &unifi.WLAN{PrivatePresharedKeysEnabled: true},
			prior: prior,
			want:  types.ListNull(ppskType),
		},
		{
			name: "explicit deletion",
			wlan: &unifi.WLAN{
				PrivatePresharedKeysEnabled: true,
				PrivatePresharedKeys:        []unifi.WLANPrivatePresharedKeys{},
			},
			prior: prior,
			want:  types.ListNull(ppskType),
		},
		{
			// prior is null (nothing persisted yet), so the response is taken
			// as-is; it happens to carry the same values as the "prior"
			// fixture above, which is what "want" is compared against.
			name: "import",
			wlan: &unifi.WLAN{
				PrivatePresharedKeysEnabled: true,
				PrivatePresharedKeys: []unifi.WLANPrivatePresharedKeys{
					{NetworkID: "net-a", Password: "secretpass1"},
				},
			},
			prior: types.ListNull(ppskType),
			want:  prior,
		},
		{
			name: "key added",
			wlan: &unifi.WLAN{
				PrivatePresharedKeysEnabled: true,
				PrivatePresharedKeys: []unifi.WLANPrivatePresharedKeys{
					{NetworkID: "net-a", Password: "secretpass1"},
					{NetworkID: "net-b", Password: "secretpass2"},
				},
			},
			prior: prior,
			want: list(
				wlanPrivatePresharedKeyModel{
					NetworkID: types.StringValue("net-a"),
					Password:  types.StringValue("secretpass1"),
				},
				wlanPrivatePresharedKeyModel{
					NetworkID: types.StringValue("net-b"),
					Password:  types.StringValue("secretpass2"),
				},
			),
		},
		{
			name: "binding changed",
			wlan: &unifi.WLAN{
				PrivatePresharedKeysEnabled: true,
				PrivatePresharedKeys: []unifi.WLANPrivatePresharedKeys{
					{NetworkID: "net-b", Password: "secretpass1"},
				},
			},
			prior: prior,
			want: list(wlanPrivatePresharedKeyModel{
				NetworkID: types.StringValue("net-b"),
				Password:  types.StringValue("secretpass1"),
			}),
		},
		{
			name: "binding changed without password",
			wlan: &unifi.WLAN{
				PrivatePresharedKeysEnabled: true,
				PrivatePresharedKeys: []unifi.WLANPrivatePresharedKeys{
					{NetworkID: "net-b"},
				},
			},
			prior: prior,
			want: list(wlanPrivatePresharedKeyModel{
				NetworkID: types.StringValue("net-b"),
				Password:  types.StringValue(""),
			}),
		},
		{
			name: "password changed",
			wlan: &unifi.WLAN{
				PrivatePresharedKeysEnabled: true,
				PrivatePresharedKeys: []unifi.WLANPrivatePresharedKeys{
					{NetworkID: "net-a", Password: "changedpass1"},
				},
			},
			prior: prior,
			want: list(wlanPrivatePresharedKeyModel{
				NetworkID: types.StringValue("net-a"),
				Password:  types.StringValue("changedpass1"),
			}),
		},
		{
			name: "matching response",
			wlan: &unifi.WLAN{
				PrivatePresharedKeysEnabled: true,
				PrivatePresharedKeys: []unifi.WLANPrivatePresharedKeys{
					{NetworkID: "net-a", Password: "secretpass1"},
				},
			},
			prior: prior,
			want:  prior,
		},
		{
			name: "duplicate bindings match once",
			wlan: &unifi.WLAN{
				PrivatePresharedKeysEnabled: true,
				PrivatePresharedKeys: []unifi.WLANPrivatePresharedKeys{
					{NetworkID: "net-a"},
					{NetworkID: "net-a", Password: "secretpass1"},
				},
			},
			prior: duplicateBindings,
			want:  duplicateBindings,
		},
		{
			name: "duplicate binding replaced",
			wlan: &unifi.WLAN{
				PrivatePresharedKeysEnabled: true,
				PrivatePresharedKeys: []unifi.WLANPrivatePresharedKeys{
					{NetworkID: "net-a"},
					{NetworkID: "net-a", Password: "changedpass1"},
				},
			},
			prior: duplicateBindings,
			want: list(
				wlanPrivatePresharedKeyModel{
					NetworkID: types.StringValue("net-a"),
					Password:  types.StringValue(""),
				},
				wlanPrivatePresharedKeyModel{
					NetworkID: types.StringValue("net-a"),
					Password:  types.StringValue("changedpass1"),
				},
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, diags := wlanPrivatePresharedKeysState(ctx, test.wlan, test.prior)
			if diags.HasError() {
				t.Fatalf("wlanPrivatePresharedKeysState: %v", diags)
			}
			if !got.Equal(test.want) {
				t.Fatalf("wlanPrivatePresharedKeysState = %v, want %v", got, test.want)
			}
		})
	}
}

func TestWLANPrivatePresharedKeys_rejectsInvalidPriorState(t *testing.T) {
	_, diags := wlanPrivatePresharedKeysState(
		context.Background(),
		&unifi.WLAN{
			PrivatePresharedKeysEnabled: true,
			PrivatePresharedKeys: []unifi.WLANPrivatePresharedKeys{
				{NetworkID: "net-a", Password: "secretpass1"},
			},
		},
		types.ListValueMust(types.StringType, []attr.Value{types.StringValue("invalid")}),
	)
	if !diags.HasError() {
		t.Fatal("wlanPrivatePresharedKeysState accepted invalid prior state")
	}
}

// When enhanced_iot is enabled the controller forces iapp_enabled,
// wpa3_support, wpa3_transition, pmf_mode and dtim_ng, so the provider pins
// them in the plan to avoid an inconsistent-result error. When enhanced_iot
// is false it must be a no-op.
func TestApplyEnhancedIotOverrides(t *testing.T) {
	t.Run("enhanced_iot true forces the controller-managed fields", func(t *testing.T) {
		m := &wlanKitModel{
			EnhancedIot:    types.BoolValue(true),
			IappEnabled:    types.BoolValue(false),
			WPA3Support:    types.BoolValue(true),
			WPA3Transition: types.BoolValue(true),
			PMFMode:        types.StringValue("optional"),
			DTIMNg:         types.Int64Value(3),
		}
		if !applyEnhancedIotOverrides(m) {
			t.Fatal("expected overrides to be applied")
		}
		if !m.IappEnabled.ValueBool() {
			t.Errorf("iapp_enabled = %v, want true", m.IappEnabled.ValueBool())
		}
		if m.WPA3Support.ValueBool() {
			t.Errorf("wpa3_support = %v, want false", m.WPA3Support.ValueBool())
		}
		if m.WPA3Transition.ValueBool() {
			t.Errorf("wpa3_transition = %v, want false", m.WPA3Transition.ValueBool())
		}
		if m.PMFMode.ValueString() != "disabled" {
			t.Errorf("pmf_mode = %q, want disabled", m.PMFMode.ValueString())
		}
		if m.DTIMNg.ValueInt64() != 1 {
			t.Errorf("dtim_ng = %d, want 1", m.DTIMNg.ValueInt64())
		}
	})

	t.Run("enhanced_iot false is a no-op", func(t *testing.T) {
		m := &wlanKitModel{
			EnhancedIot: types.BoolValue(false),
			WPA3Support: types.BoolValue(true),
			PMFMode:     types.StringValue("optional"),
		}
		if applyEnhancedIotOverrides(m) {
			t.Fatal("expected no overrides when enhanced_iot is false")
		}
		if !m.WPA3Support.ValueBool() || m.PMFMode.ValueString() != "optional" {
			t.Errorf("non-IoT fields were modified: wpa3=%v pmf=%q",
				m.WPA3Support.ValueBool(), m.PMFMode.ValueString())
		}
	})
}
