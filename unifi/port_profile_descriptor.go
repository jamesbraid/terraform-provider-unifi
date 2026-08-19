package unifi

// The port_profile descriptor.
//
// THE FIRST SURFACE TO USE THE HOOKS AS DESIGNED. Its tagged VLAN membership is
// not a field of the port profile at all: the controller stores a mode plus an
// EXCLUSION list, and the practitioner writes an INCLUSION list, so translating
// between them needs the site's whole network inventory. Prefetch fetches it
// once, BeforeSend turns the inclusion list into a mode and an exclusion list,
// and AfterReceive turns them back. Everything about that translation is in
// port_profile_resource.go, where it belongs -- this file only wires it up.
//
// tagged_networkconf_ids and excluded_networkconf_ids are absent from Fields
// deliberately. The first has no SDK counterpart to map to; the second is read
// back conditionally (only when the mode is "custom") which no field kind
// expresses. Both are handled by the hooks, and AlwaysWire is what keeps the
// values they derive in the update mask.

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_port_profile"
	resource_port_profile "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_port_profile"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type portProfileKitModel struct {
	ID                         types.String         `tfsdk:"id"`
	Site                       types.String         `tfsdk:"site"`
	Autoneg                    types.Bool           `tfsdk:"autoneg"`
	Dot1XCtrl                  types.String         `tfsdk:"dot1x_ctrl"`
	Dot1XIdleTimeout           timetypes.GoDuration `tfsdk:"dot1x_idle_timeout"`
	EgressRateLimitKbps        types.Int64          `tfsdk:"egress_rate_limit_kbps"`
	EgressRateLimitKbpsEnabled types.Bool           `tfsdk:"egress_rate_limit_kbps_enabled"`
	Forward                    types.String         `tfsdk:"forward"`
	FullDuplex                 types.Bool           `tfsdk:"full_duplex"`
	Isolation                  types.Bool           `tfsdk:"isolation"`
	LLDPMedEnabled             types.Bool           `tfsdk:"lldpmed_enabled"`
	LLDPMedNotifyEnabled       types.Bool           `tfsdk:"lldpmed_notify_enabled"`
	NativeNetworkConfID        types.String         `tfsdk:"native_networkconf_id"`
	Name                       types.String         `tfsdk:"name"`
	OpMode                     types.String         `tfsdk:"op_mode"`
	PoeMode                    types.String         `tfsdk:"poe_mode"`
	PortSecurityEnabled        types.Bool           `tfsdk:"port_security_enabled"`
	PortSecurityMacAddress     types.Set            `tfsdk:"port_security_mac_address"`
	PriorityQueue1Level        types.Int64          `tfsdk:"priority_queue1_level"`
	PriorityQueue2Level        types.Int64          `tfsdk:"priority_queue2_level"`
	PriorityQueue3Level        types.Int64          `tfsdk:"priority_queue3_level"`
	PriorityQueue4Level        types.Int64          `tfsdk:"priority_queue4_level"`
	Speed                      types.Int64          `tfsdk:"speed"`
	StormctrlBcastEnabled      types.Bool           `tfsdk:"stormctrl_bcast_enabled"`
	StormctrlBcastLevel        types.Int64          `tfsdk:"stormctrl_bcast_level"`
	StormctrlBcastRate         types.Int64          `tfsdk:"stormctrl_bcast_rate"`
	StormctrlMcastEnabled      types.Bool           `tfsdk:"stormctrl_mcast_enabled"`
	StormctrlMcastLevel        types.Int64          `tfsdk:"stormctrl_mcast_level"`
	StormctrlMcastRate         types.Int64          `tfsdk:"stormctrl_mcast_rate"`
	StormctrlType              types.String         `tfsdk:"stormctrl_type"`
	StormctrlUcastEnabled      types.Bool           `tfsdk:"stormctrl_ucast_enabled"`
	StormctrlUcastLevel        types.Int64          `tfsdk:"stormctrl_ucast_level"`
	StormctrlUcastRate         types.Int64          `tfsdk:"stormctrl_ucast_rate"`
	STPPortMode                types.Bool           `tfsdk:"stp_port_mode"`
	TaggedNetworkConfIDs       types.Set            `tfsdk:"tagged_networkconf_ids"`
	VoiceNetworkConfID         types.String         `tfsdk:"voice_networkconf_id"`
	ExcludedNetworkConfIDs     types.Set            `tfsdk:"excluded_networkconf_ids"`
	MulticastRouterNetworkIDs  types.Set            `tfsdk:"multicast_router_networkconf_ids"`
	TaggedVLANMgmt             types.String         `tfsdk:"tagged_vlan_mgmt"`
	FecMode                    types.String         `tfsdk:"fec_mode"`
	SettingPreference          types.String         `tfsdk:"setting_preference"`
	PortKeepaliveEnabled       types.Bool           `tfsdk:"port_keepalive_enabled"`
	Timeouts                   timeouts.Value       `tfsdk:"timeouts"`
}

type ppModel = portProfileKitModel

type ppSDK = ui.PortProfile

func ppString(
	wire string,
	model func(*ppModel) *types.String,
	sdk func(*ppSDK) *string,
	elide resourcekit.ElideZero,
) resourcekit.StringField[ppModel, ppSDK] {
	return resourcekit.StringField[ppModel, ppSDK]{Wire: wire, Model: model, SDK: sdk, Elide: elide}
}

func ppBool(
	wire string,
	model func(*ppModel) *types.Bool,
	sdk func(*ppSDK) *bool,
) resourcekit.BoolField[ppModel, ppSDK] {
	return resourcekit.BoolField[ppModel, ppSDK]{Wire: wire, Model: model, SDK: sdk}
}

func ppInt(
	wire string,
	model func(*ppModel) *types.Int64,
	sdk func(*ppSDK) **int64,
) resourcekit.Int64PtrField[ppModel, ppSDK] {
	return resourcekit.Int64PtrField[ppModel, ppSDK]{
		Wire: wire, Model: model, SDK: sdk, Elide: resourcekit.NullZero,
	}
}

func portProfileKitSpec() resourcekit.Spec[ppModel, ppSDK] {
	return resourcekit.Spec[ppModel, ppSDK]{
		TypeName: "port_profile",
		Subject:  "Port Profile",
		New:      func() *ppSDK { return &ppSDK{} },
		ID:       func(m *ppModel) *types.String { return &m.ID },
		Site:     func(m *ppModel) *types.String { return &m.Site },
		Timeouts: func(m *ppModel) *timeouts.Value { return &m.Timeouts },

		// Prefetch is bound in Configure, where the client exists.
		BeforeSend:   portProfileBeforeSend,
		AfterReceive: portProfileAfterReceive,

		// The three wire fields BeforeSend derives from tagged_networkconf_ids,
		// which is not itself a field and so cannot put them in the mask.
		AlwaysWire: []string{"tagged_vlan_mgmt", "excluded_networkconf_ids", "forward"},

		Fields: []resourcekit.Field[ppModel, ppSDK]{
			ppBool("autoneg", func(m *ppModel) *types.Bool { return &m.Autoneg },
				func(s *ppSDK) *bool { return &s.Autoneg }),
			// "force_authorized" on an empty read.
			resourcekit.StringField[ppModel, ppSDK]{
				Wire:        "dot1x_ctrl",
				Model:       func(m *ppModel) *types.String { return &m.Dot1XCtrl },
				SDK:         func(s *ppSDK) *string { return &s.Dot1XCtrl },
				ReadDefault: "force_authorized",
			},
			resourcekit.DurationPtrField[ppModel, ppSDK]{
				Wire:  "dot1x_idle_timeout",
				Model: func(m *ppModel) *timetypes.GoDuration { return &m.Dot1XIdleTimeout },
				SDK:   func(s *ppSDK) **int64 { return &s.Dot1XIDleTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			ppInt(
				"egress_rate_limit_kbps",
				func(m *ppModel) *types.Int64 { return &m.EgressRateLimitKbps },
				func(s *ppSDK) **int64 { return &s.EgressRateLimitKbps },
			),
			ppBool(
				"egress_rate_limit_kbps_enabled",
				func(m *ppModel) *types.Bool { return &m.EgressRateLimitKbpsEnabled },
				func(s *ppSDK) *bool { return &s.EgressRateLimitKbpsEnabled },
			),
			// "native" on an empty read, and BeforeSend overrides it whenever a
			// VLAN mode is derived.
			resourcekit.StringField[ppModel, ppSDK]{
				Wire:        "forward",
				Model:       func(m *ppModel) *types.String { return &m.Forward },
				SDK:         func(s *ppSDK) *string { return &s.Forward },
				ReadDefault: "native",
			},
			ppBool("full_duplex", func(m *ppModel) *types.Bool { return &m.FullDuplex },
				func(s *ppSDK) *bool { return &s.FullDuplex }),
			ppBool("isolation", func(m *ppModel) *types.Bool { return &m.Isolation },
				func(s *ppSDK) *bool { return &s.Isolation }),
			ppBool("lldpmed_enabled", func(m *ppModel) *types.Bool { return &m.LLDPMedEnabled },
				func(s *ppSDK) *bool { return &s.LldpmedEnabled }),
			ppBool(
				"lldpmed_notify_enabled",
				func(m *ppModel) *types.Bool { return &m.LLDPMedNotifyEnabled },
				func(s *ppSDK) *bool { return &s.LldpmedNotifyEnabled },
			),
			ppString(
				"native_networkconf_id",
				func(m *ppModel) *types.String { return &m.NativeNetworkConfID },
				func(s *ppSDK) *string { return &s.NATiveNetworkID },
				resourcekit.KeepZero,
			),
			ppString("name", func(m *ppModel) *types.String { return &m.Name },
				func(s *ppSDK) *string { return &s.Name }, resourcekit.NullZero),
			// "switch" on an empty read.
			resourcekit.StringField[ppModel, ppSDK]{
				Wire:        "op_mode",
				Model:       func(m *ppModel) *types.String { return &m.OpMode },
				SDK:         func(s *ppSDK) *string { return &s.OpMode },
				ReadDefault: "switch",
			},
			ppString("poe_mode", func(m *ppModel) *types.String { return &m.PoeMode },
				func(s *ppSDK) *string { return &s.PoeMode }, resourcekit.NullZero),
			ppBool(
				"port_security_enabled",
				func(m *ppModel) *types.Bool { return &m.PortSecurityEnabled },
				func(s *ppSDK) *bool { return &s.PortSecurityEnabled },
			),
			resourcekit.StringSetField[ppModel, ppSDK]{
				Wire:  "port_security_mac_address",
				Model: func(m *ppModel) *types.Set { return &m.PortSecurityMacAddress },
				SDK:   func(s *ppSDK) *[]string { return &s.PortSecurityMACAddress },
				Elide: resourcekit.NullZero,
			},
			ppInt(
				"priority_queue1_level",
				func(m *ppModel) *types.Int64 { return &m.PriorityQueue1Level },
				func(s *ppSDK) **int64 { return &s.PriorityQueue1Level },
			),
			ppInt(
				"priority_queue2_level",
				func(m *ppModel) *types.Int64 { return &m.PriorityQueue2Level },
				func(s *ppSDK) **int64 { return &s.PriorityQueue2Level },
			),
			ppInt(
				"priority_queue3_level",
				func(m *ppModel) *types.Int64 { return &m.PriorityQueue3Level },
				func(s *ppSDK) **int64 { return &s.PriorityQueue3Level },
			),
			ppInt(
				"priority_queue4_level",
				func(m *ppModel) *types.Int64 { return &m.PriorityQueue4Level },
				func(s *ppSDK) **int64 { return &s.PriorityQueue4Level },
			),
			ppInt("speed", func(m *ppModel) *types.Int64 { return &m.Speed },
				func(s *ppSDK) **int64 { return &s.Speed }),
			ppBool(
				"stormctrl_bcast_enabled",
				func(m *ppModel) *types.Bool { return &m.StormctrlBcastEnabled },
				func(s *ppSDK) *bool { return &s.StormctrlBroadcastastEnabled },
			),
			ppInt(
				"stormctrl_bcast_level",
				func(m *ppModel) *types.Int64 { return &m.StormctrlBcastLevel },
				func(s *ppSDK) **int64 { return &s.StormctrlBroadcastastLevel },
			),
			ppInt(
				"stormctrl_bcast_rate",
				func(m *ppModel) *types.Int64 { return &m.StormctrlBcastRate },
				func(s *ppSDK) **int64 { return &s.StormctrlBroadcastastRate },
			),
			ppBool(
				"stormctrl_mcast_enabled",
				func(m *ppModel) *types.Bool { return &m.StormctrlMcastEnabled },
				func(s *ppSDK) *bool { return &s.StormctrlMcastEnabled },
			),
			ppInt(
				"stormctrl_mcast_level",
				func(m *ppModel) *types.Int64 { return &m.StormctrlMcastLevel },
				func(s *ppSDK) **int64 { return &s.StormctrlMcastLevel },
			),
			ppInt(
				"stormctrl_mcast_rate",
				func(m *ppModel) *types.Int64 { return &m.StormctrlMcastRate },
				func(s *ppSDK) **int64 { return &s.StormctrlMcastRate },
			),
			ppString("stormctrl_type", func(m *ppModel) *types.String { return &m.StormctrlType },
				func(s *ppSDK) *string { return &s.StormctrlType }, resourcekit.NullZero),
			ppBool(
				"stormctrl_ucast_enabled",
				func(m *ppModel) *types.Bool { return &m.StormctrlUcastEnabled },
				func(s *ppSDK) *bool { return &s.StormctrlUcastEnabled },
			),
			ppInt(
				"stormctrl_ucast_level",
				func(m *ppModel) *types.Int64 { return &m.StormctrlUcastLevel },
				func(s *ppSDK) **int64 { return &s.StormctrlUcastLevel },
			),
			ppInt(
				"stormctrl_ucast_rate",
				func(m *ppModel) *types.Int64 { return &m.StormctrlUcastRate },
				func(s *ppSDK) **int64 { return &s.StormctrlUcastRate },
			),
			ppBool("stp_port_mode", func(m *ppModel) *types.Bool { return &m.STPPortMode },
				func(s *ppSDK) *bool { return &s.StpPortMode }),
			ppString(
				"voice_networkconf_id",
				func(m *ppModel) *types.String { return &m.VoiceNetworkConfID },
				func(s *ppSDK) *string { return &s.VoiceNetworkID },
				resourcekit.NullZero,
			),
			resourcekit.StringSetField[ppModel, ppSDK]{
				Wire:  "multicast_router_networkconf_ids",
				Model: func(m *ppModel) *types.Set { return &m.MulticastRouterNetworkIDs },
				SDK:   func(s *ppSDK) *[]string { return &s.MulticastRouterNetworkIDs },
				Elide: resourcekit.NullZero,
			},
			ppString(
				"tagged_vlan_mgmt",
				func(m *ppModel) *types.String { return &m.TaggedVLANMgmt },
				func(s *ppSDK) *string { return &s.TaggedVLANMgmt },
				resourcekit.NullZero,
			),
			ppString("fec_mode", func(m *ppModel) *types.String { return &m.FecMode },
				func(s *ppSDK) *string { return &s.FecMode }, resourcekit.NullZero),
			ppString(
				"setting_preference",
				func(m *ppModel) *types.String { return &m.SettingPreference },
				func(s *ppSDK) *string { return &s.SettingPreference },
				resourcekit.NullZero,
			),
			ppBool(
				"port_keepalive_enabled",
				func(m *ppModel) *types.Bool { return &m.PortKeepaliveEnabled },
				func(s *ppSDK) *bool { return &s.PortKeepaliveEnabled },
			),
		},
		Backend: resourcekit.Backend[ppSDK]{
			GetID: func(s *ppSDK) string { return s.ID },
			SetID: func(s *ppSDK, id string) { s.ID = id },
		},
	}
}

func portProfileKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_port_profile.PortProfileResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
		Version:  1,
	}
}

func portProfileKitList() resourcekit.ListSpec[ppSDK] {
	return resourcekit.ListSpec[ppSDK]{
		ConfigSchema: listresource_port_profile.PortProfileListResourceSchema,
		DisplayName: func(s *ppSDK) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ppSDK) string{
			"name": func(s *ppSDK) string { return s.Name },
		},
	}
}

func portProfileKitBackend(client *ui.ApiClient) resourcekit.Backend[ppSDK] {
	return resourcekit.Backend[ppSDK]{
		Create: func(ctx context.Context, site string, in *ppSDK) (*ppSDK, error) {
			return client.CreatePortProfile(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ppSDK, error) {
			return client.GetPortProfile(ctx, site, id)
		},
		// THE MASKED UPDATE, WHERE THE HAND-WRITTEN RESOURCE SENT THE WHOLE
		// OBJECT. UpdatePortProfileFields exists and was not being used, so
		// every update rewrote all 55 wire fields from a read-modify-write --
		// which is #121's shape. The mask narrows it to what the plan touched
		// plus the three AlwaysWire fields.
		UpdateFields: func(ctx context.Context, site string, in *ppSDK, fields ...string) (*ppSDK, error) {
			return client.UpdatePortProfileFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeletePortProfile(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ppSDK, error) {
			return client.ListPortProfile(ctx, site)
		},
		GetID: func(s *ppSDK) string { return s.ID },
		SetID: func(s *ppSDK, id string) { s.ID = id },
	}
}
