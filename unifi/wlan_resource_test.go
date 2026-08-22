package unifi

import (
	"context"
	"reflect"
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

resource "unifi_wlan" "test" {
	name            = "wlan1"
	security        = "wpapsk"
	passphrase      = "passphrase"
	hide_ssid       = false
}
`
}

// TestAccWLANFramework_additionalFields verifies that the newly exposed
// security/DTIM/toggle attributes are populated by the read path when a WLAN
// is imported. It follows the same import-based pattern as the basic test: a
// full create cannot be exercised here because WLAN creation currently fails
// with a pre-existing api.err.InvalidPayload that is unrelated to these
// attributes (a minimal WLAN with none of them set fails identically).
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
					// Issue #176 (secondary) asked for 0 rather than null here,
					// so an imported WLAN would not drift against the schema
					// default. That default is gone: #323 removed it because
					// the controller overrides it in auto mode, which is also
					// why these are Computed. With no default there is nothing
					// for a null to drift towards, and 0 is a rate a
					// practitioner may legitimately ask for -- it is in both
					// OneOf lists -- so recording it for "the controller said
					// nothing" asserts something the controller did not say.
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
		})
	}
}

// Test_wlanFrameworkResource_Schema_computedControllerFields guards #323: fields
// the controller assigns on its own must be Computed so a controller-supplied
// value doesn't trip "inconsistent result after apply". minimum_data_rate_*_kbps
// previously defaulted to 0 (rejected/overridden by the controller in auto mode);
// radius_profile_id and bc_filter_list were Optional-only and got populated by
// the controller.
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
			t.Errorf("attribute %q must be Computed (controller-managed, #323)", key)
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

func Test_wlanFrameworkResource_Create(t *testing.T) {
	t.Skip("requires terraform state and configured client")
}

func Test_wlanFrameworkResource_Read(t *testing.T) {
	t.Skip("requires terraform state and configured client")
}

func Test_wlanFrameworkResource_Update(t *testing.T) {
	t.Skip("requires terraform state and configured client")
}

func Test_wlanFrameworkResource_applyPlanToState(t *testing.T) {
	t.Skip("requires terraform state")
}

func Test_wlanFrameworkResource_Delete(t *testing.T) {
	t.Skip("requires terraform state and configured client")
}

func Test_wlanFrameworkResource_ImportState(t *testing.T) {
	t.Skip("requires terraform state and configured client")
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
	// THIS ASSERTION IS INVERTED FROM WHAT IT WAS, deliberately.
	//
	// It used to require a non-nil ScheduleWithDuration, pinning a guard in
	// planToWLAN that forced an empty slice so the field would not marshal as
	// null. That guard stopped working when go-unifi added omitempty to the tag
	// -- encoding/json drops a zero-length slice nil or not -- so it prevented a
	// failure that can no longer happen while the one that can, an emptied
	// schedule silently not clearing, went unguarded. See task #228.
	//
	// The guard is not carried into the descriptor, so nil is now correct and a
	// test demanding otherwise would be pinning dead code.
	if got.ScheduleWithDuration != nil {
		t.Errorf("ScheduleWithDuration = %v, want nil for an absent schedule", got.ScheduleWithDuration)
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
}

func Test_wlanFrameworkResource_List(t *testing.T) {
	t.Skip("requires configured client")
}

// TestWLANPrivatePresharedKeys_roundTrip exercises the private pre-shared key
// (PPSK) mapping added for issue #47: a plan carrying PPSK entries must be
// translated to the go-unifi WLAN struct (planToWLAN) and back into the
// resource model (wlanToModel) without losing the per-key network binding or
// password.
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

// TestApplyEnhancedIotOverrides guards #283: when enhanced_iot is enabled the
// controller forces iapp_enabled, wpa3_support, wpa3_transition, pmf_mode and
// dtim_ng, so the provider pins them in the plan to avoid an inconsistent-result
// error. When enhanced_iot is false it must be a no-op.
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

func TestAccWLANList_basic(t *testing.T) {
	// WLAN creation requires user_group_id which cannot be reliably resolved in
	// the dockerized test environment; skip until the basic create path works.
	t.Skip("WLAN creation requires user_group_id; skipping list acceptance test")
}
