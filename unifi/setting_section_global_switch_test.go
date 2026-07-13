package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// switch_exclusions is modeled (not deferred): its golden value comes
// straight from the overlay's own input model, like every other leaf in
// this section, not from an RMW-preserved snapshot seed.
const goldenGlobalSwitch = `{"acl_device_isolation":["net-a","net-b"],"acl_l3_isolation":[{"source_network":"net-a","destination_networks":["net-b","net-c"]}],"dhcp_snoop":true,"dot1x_fallback_networkconf_id":"net-fallback","dot1x_portctrl_enabled":false,"flood_known_protocols":true,"flowctrl_enabled":false,"forward_unknown_mcast_router_ports":false,"jumboframe_enabled":true,"key":"global_switch","radiusprofile_id":"radius-profile-1","stp_version":"rstp","switch_exclusions":["AA:BB:CC:00:11:22"]}`

func globalSwitchRepresentativeModel(ctx context.Context, t *testing.T) types.Object {
	t.Helper()
	aclDeviceIsolation, diags := types.ListValueFrom(ctx, types.StringType, []string{"net-a", "net-b"})
	if diags.HasError() {
		t.Fatalf("building acl_device_isolation list: %v", diags)
	}
	destNetworks, diags2 := types.ListValueFrom(ctx, types.StringType, []string{"net-b", "net-c"})
	if diags2.HasError() {
		t.Fatalf("building destination_networks list: %v", diags2)
	}
	aclL3, diags3 := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: globalSwitchAclL3IsolationAttrTypes}, []settingGlobalSwitchAclL3IsolationModel{
		{SourceNetwork: types.StringValue("net-a"), DestinationNetworks: destNetworks},
	})
	if diags3.HasError() {
		t.Fatalf("building acl_l3_isolation list: %v", diags3)
	}
	switchExclusions, diags4 := types.ListValueFrom(ctx, types.StringType, []string{"AA:BB:CC:00:11:22"})
	if diags4.HasError() {
		t.Fatalf("building switch_exclusions list: %v", diags4)
	}
	m := settingGlobalSwitchModel{
		AclDeviceIsolation:             aclDeviceIsolation,
		AclL3Isolation:                 aclL3,
		DHCPSnoop:                      types.BoolValue(true),
		Dot1XFallbackNetworkID:         types.StringValue("net-fallback"),
		Dot1XPortctrlEnabled:           types.BoolValue(false),
		FloodKnownProtocols:            types.BoolValue(true),
		FlowctrlEnabled:                types.BoolValue(false),
		ForwardUnknownMcastRouterPorts: types.BoolValue(false),
		JumboframeEnabled:              types.BoolValue(true),
		RADIUSProfileID:                types.StringValue("radius-profile-1"),
		StpVersion:                     types.StringValue("rstp"),
		SwitchExclusions:               switchExclusions,
	}
	obj, diags5 := types.ObjectValueFrom(ctx, globalSwitchAttrTypes, m)
	if diags5.HasError() {
		t.Fatalf("building global_switch object: %v", diags5)
	}
	return obj
}

// TestGlobalSwitchSection_GoldenReproduction proves overlay() reproduces
// the golden from the input model alone (empty snapshot base) — including
// switch_exclusions, which is fully modeled like every other leaf.
func TestGlobalSwitchSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := globalSwitchSection{}

	model := settingResourceModel{GlobalSwitch: globalSwitchRepresentativeModel(ctx, t)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "global_switch" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "global_switch")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenGlobalSwitch)
}

func TestGlobalSwitchSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := globalSwitchSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "global_switch"},
		Data: map[string]any{
			"acl_device_isolation": []any{"net-a"},
			"acl_l3_isolation": []any{
				map[string]any{
					"source_network":       "net-a",
					"destination_networks": []any{"net-b"},
				},
			},
			"dhcp_snoop":                         true,
			"dot1x_fallback_networkconf_id":      "net-fallback",
			"dot1x_portctrl_enabled":             false,
			"flood_known_protocols":              true,
			"flowctrl_enabled":                   false,
			"forward_unknown_mcast_router_ports": false,
			"jumboframe_enabled":                 true,
			"radiusprofile_id":                   "radius-profile-1",
			"stp_version":                        "rstp",
			"switch_exclusions":                  []any{"AA:BB:CC:00:11:22"},
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingGlobalSwitchModel
	if diags := model.GlobalSwitch.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingGlobalSwitchModel: %v", diags)
	}

	if !got.DHCPSnoop.ValueBool() {
		t.Errorf("DHCPSnoop = %v, want true", got.DHCPSnoop.ValueBool())
	}
	if got.StpVersion.ValueString() != "rstp" {
		t.Errorf("StpVersion = %q, want %q", got.StpVersion.ValueString(), "rstp")
	}

	var aclL3 []settingGlobalSwitchAclL3IsolationModel
	if diags := got.AclL3Isolation.ElementsAs(ctx, &aclL3, false); diags.HasError() {
		t.Fatalf("extracting AclL3Isolation: %v", diags)
	}
	if len(aclL3) != 1 || aclL3[0].SourceNetwork.ValueString() != "net-a" {
		t.Errorf("AclL3Isolation = %+v, want source_network=net-a", aclL3)
	}

	var switchExclusions []string
	if diags := got.SwitchExclusions.ElementsAs(ctx, &switchExclusions, false); diags.HasError() {
		t.Fatalf("extracting SwitchExclusions: %v", diags)
	}
	if len(switchExclusions) != 1 || switchExclusions[0] != "AA:BB:CC:00:11:22" {
		t.Errorf("SwitchExclusions = %v, want [AA:BB:CC:00:11:22]", switchExclusions)
	}
}

func TestGlobalSwitchSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := globalSwitchSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "global_switch"},
		Data: map[string]any{
			"dhcp_snoop":  true,
			"x_unmanaged": "keep",
		},
	}})

	model := settingResourceModel{GlobalSwitch: globalSwitchRepresentativeModel(ctx, t)}
	prior := settingResourceModel{}

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	if got, ok := rs.Data["x_unmanaged"]; !ok || got != "keep" {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %q", "x_unmanaged", got, ok, "keep")
	}
}

func TestGlobalSwitchSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := globalSwitchSection{}

	model := settingResourceModel{GlobalSwitch: types.ObjectNull(globalSwitchAttrTypes)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if configured {
		t.Fatalf("overlay configured = true, want false")
	}
	if rs.Key != "" || len(rs.Data) != 0 {
		t.Errorf("overlay returned non-zero RawSetting when not configured: %+v", rs)
	}
}

func TestGlobalSwitchSection_InterfaceWiring(t *testing.T) {
	sec := globalSwitchSection{}
	if sec.key() != "global_switch" {
		t.Errorf("key() = %q, want %q", sec.key(), "global_switch")
	}
	if sec.attrName() != "global_switch" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "global_switch")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(globalSwitchSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("globalSwitchSection not found in settingSections registry")
	}
}
