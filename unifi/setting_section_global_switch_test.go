package unifi

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_globalSwitchModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	dst, d := types.SetValueFrom(ctx, types.StringType, []string{"net2"})
	if d.HasError() {
		t.Fatal(d)
	}
	rule, d := types.ObjectValueFrom(ctx, globalSwitchACLL3IsolationAttrTypes,
		settingGlobalSwitchACLL3IsolationModel{
			SourceNetwork:       types.StringValue("net1"),
			DestinationNetworks: dst,
		})
	if d.HasError() {
		t.Fatal(d)
	}
	rules, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: globalSwitchACLL3IsolationAttrTypes},
		[]types.Object{rule})
	if d.HasError() {
		t.Fatal(d)
	}
	exclusions, d := types.SetValueFrom(ctx, types.StringType,
		[]string{"aa:bb:cc:dd:ee:ff"})
	if d.HasError() {
		t.Fatal(d)
	}

	m := &settingGlobalSwitchModel{
		ACLDeviceIsolation:             types.SetNull(types.StringType),
		ACLL3Isolation:                 rules,
		SwitchExclusions:               exclusions,
		DHCPSnoop:                      types.BoolValue(true),
		Dot1XFallbackNetworkID:         types.StringValue("fallback1"),
		Dot1XPortctrlEnabled:           types.BoolNull(),
		FloodKnownProtocols:            types.BoolNull(),
		FlowctrlEnabled:                types.BoolValue(false),
		ForwardUnknownMcastRouterPorts: types.BoolNull(),
		JumboframeEnabled:              types.BoolValue(true),
		RADIUSProfileID:                types.StringNull(),
		StpVersion:                     types.StringValue("rstp"),
	}

	// The live controller has fields go-unifi does not model (e.g.
	// link_debounce); the raw merge must preserve them verbatim.
	data := map[string]any{"link_debounce": true, "dot1x_portctrl_enabled": true}

	globalSwitchModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["link_debounce"] != true {
		t.Fatal("unmodeled link_debounce was clobbered")
	}
	if data["dot1x_portctrl_enabled"] != true {
		t.Fatal("null dot1x_portctrl_enabled overwrote remote value")
	}
	if data["dhcp_snoop"] != true || data["flowctrl_enabled"] != false ||
		data["jumboframe_enabled"] != true {
		t.Fatalf("bool fields wrong: %v", data)
	}
	if data["stp_version"] != "rstp" {
		t.Fatalf("stp_version = %v", data["stp_version"])
	}
	if data["dot1x_fallback_networkconf_id"] != "fallback1" {
		t.Fatalf("dot1x_fallback_networkconf_id = %v", data["dot1x_fallback_networkconf_id"])
	}
	if _, present := data["radiusprofile_id"]; present {
		t.Fatal("null radius_profile_id should not be written")
	}
	if _, present := data["acl_device_isolation"]; present {
		t.Fatal("null acl_device_isolation should not be written")
	}
	l3, ok := data["acl_l3_isolation"].([]map[string]any)
	if !ok || len(l3) != 1 || l3[0]["source_network"] != "net1" {
		t.Fatalf("acl_l3_isolation = %v", data["acl_l3_isolation"])
	}
	excl, ok := data["switch_exclusions"].([]string)
	if !ok || len(excl) != 1 || excl[0] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("switch_exclusions = %v", data["switch_exclusions"])
	}
}

func Test_globalSwitchSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := globalSwitchSettingToModel(ctx, &settings.GlobalSwitch{
		AclDeviceIsolation: []string{"dev1"},
		AclL3Isolation: []settings.SettingGlobalSwitchAclL3Isolation{
			{SourceNetwork: "net1", DestinationNetworks: []string{"net2"}},
		},
		DHCPSnoop:         true,
		JumboframeEnabled: true,
		StpVersion:        "rstp",
		RADIUSProfileID:   "rp1",
		SwitchExclusions:  []string{"aa:bb:cc:dd:ee:ff"},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if !m.DHCPSnoop.ValueBool() || !m.JumboframeEnabled.ValueBool() {
		t.Fatal("bools not mapped")
	}
	if m.StpVersion.ValueString() != "rstp" {
		t.Fatalf("stp_version = %v", m.StpVersion)
	}
	if m.RADIUSProfileID.ValueString() != "rp1" {
		t.Fatalf("radius_profile_id = %v", m.RADIUSProfileID)
	}
	if m.Dot1XFallbackNetworkID.IsUnknown() || !m.Dot1XFallbackNetworkID.IsNull() {
		t.Fatalf("empty dot1x fallback should be null, got %v", m.Dot1XFallbackNetworkID)
	}
	var rules []settingGlobalSwitchACLL3IsolationModel
	diags.Append(m.ACLL3Isolation.ElementsAs(ctx, &rules, false)...)
	if len(rules) != 1 || rules[0].SourceNetwork.ValueString() != "net1" {
		t.Fatalf("acl_l3_isolation = %v", rules)
	}
}

func TestAccSettingResource_globalSwitch(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_globalSwitch(true, "rstp"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "global_switch.jumboframe_enabled", "true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "global_switch.stp_version", "rstp",
					),
				),
			},
			{
				Config: testAccSettingConfig_globalSwitch(false, "stp"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "global_switch.jumboframe_enabled", "false",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "global_switch.stp_version", "stp",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_globalSwitch(jumbo bool, stp string) string {
	return fmt.Sprintf(`
resource "unifi_setting" "test" {
  global_switch = {
    jumboframe_enabled = %t
    stp_version        = %q
  }
}
`, jumbo, stp)
}
