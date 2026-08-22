package unifi

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// portOverrideModel describes the port override data model.
type portOverrideModel struct {
	Index                      types.Int64          `tfsdk:"index"`
	Name                       types.String         `tfsdk:"name"`
	PortProfileID              types.String         `tfsdk:"port_profile_id"`
	OpMode                     types.String         `tfsdk:"op_mode"`
	PoeMode                    types.String         `tfsdk:"poe_mode"`
	AggregateMembers           types.List           `tfsdk:"aggregate_members"`
	Autoneg                    types.Bool           `tfsdk:"autoneg"`
	Dot1XCtrl                  types.String         `tfsdk:"dot1x_ctrl"`
	Dot1XIDleTimeout           timetypes.GoDuration `tfsdk:"dot1x_idle_timeout"`
	EgressRateLimitKbps        types.Int64          `tfsdk:"egress_rate_limit_kbps"`
	EgressRateLimitKbpsEnabled types.Bool           `tfsdk:"egress_rate_limit_kbps_enabled"`
	ExcludedNetworkIDs         types.List           `tfsdk:"excluded_networkconf_ids"`
	FecMode                    types.String         `tfsdk:"fec_mode"`
	FlowControlEnabled         types.Bool           `tfsdk:"flow_control_enabled"`
	Forward                    types.String         `tfsdk:"forward"`
	FullDuplex                 types.Bool           `tfsdk:"full_duplex"`
	Isolation                  types.Bool           `tfsdk:"isolation"`
	LldpmedEnabled             types.Bool           `tfsdk:"lldpmed_enabled"`
	LldpmedNotifyEnabled       types.Bool           `tfsdk:"lldpmed_notify_enabled"`
	MirrorPortIDX              types.Int64          `tfsdk:"mirror_port_idx"`
	MulticastRouterNetworkIDs  types.List           `tfsdk:"multicast_router_networkconf_ids"`
	NativeNetworkID            types.String         `tfsdk:"native_networkconf_id"`
	PortKeepaliveEnabled       types.Bool           `tfsdk:"port_keepalive_enabled"`
	PortSecurityEnabled        types.Bool           `tfsdk:"port_security_enabled"`
	PortSecurityMACAddress     types.List           `tfsdk:"port_security_mac_address"`
	PriorityQueue1Level        types.Int64          `tfsdk:"priority_queue1_level"`
	PriorityQueue2Level        types.Int64          `tfsdk:"priority_queue2_level"`
	PriorityQueue3Level        types.Int64          `tfsdk:"priority_queue3_level"`
	PriorityQueue4Level        types.Int64          `tfsdk:"priority_queue4_level"`
	SettingPreference          types.String         `tfsdk:"setting_preference"`
	Speed                      types.Int64          `tfsdk:"speed"`
	StormctrlBroadcastEnabled  types.Bool           `tfsdk:"stormctrl_bcast_enabled"`
	StormctrlBroadcastLevel    types.Int64          `tfsdk:"stormctrl_bcast_level"`
	StormctrlBroadcastRate     types.Int64          `tfsdk:"stormctrl_bcast_rate"`
	StormctrlMcastEnabled      types.Bool           `tfsdk:"stormctrl_mcast_enabled"`
	StormctrlMcastLevel        types.Int64          `tfsdk:"stormctrl_mcast_level"`
	StormctrlMcastRate         types.Int64          `tfsdk:"stormctrl_mcast_rate"`
	StormctrlType              types.String         `tfsdk:"stormctrl_type"`
	StormctrlUcastEnabled      types.Bool           `tfsdk:"stormctrl_ucast_enabled"`
	StormctrlUcastLevel        types.Int64          `tfsdk:"stormctrl_ucast_level"`
	StormctrlUcastRate         types.Int64          `tfsdk:"stormctrl_ucast_rate"`
	StpPortMode                types.Bool           `tfsdk:"stp_port_mode"`
	TaggedNetworkIDs           types.List           `tfsdk:"tagged_networkconf_ids"`
	TaggedVLANMgmt             types.String         `tfsdk:"tagged_vlan_mgmt"`
	VoiceNetworkID             types.String         `tfsdk:"voice_networkconf_id"`
}

func (m portOverrideModel) AttributeTypes() map[string]attr.Type {
	return portOverrideAttrTypes()
}

// configNetworkModel describes the config network data model.
type configNetworkModel struct {
	Type           types.String `tfsdk:"type"`
	IP             types.String `tfsdk:"ip"`
	Netmask        types.String `tfsdk:"netmask"`
	Gateway        types.String `tfsdk:"gateway"`
	DNS1           types.String `tfsdk:"dns1"`
	DNS2           types.String `tfsdk:"dns2"`
	DNSsuffix      types.String `tfsdk:"dnssuffix"`
	BondingEnabled types.Bool   `tfsdk:"bonding_enabled"`
}

// radioTableModel describes the radio table data model.
type radioTableModel struct {
	Radio                 types.String `tfsdk:"radio"`
	Channel               types.String `tfsdk:"channel"`
	Ht                    types.Int64  `tfsdk:"ht"`
	TxPower               types.String `tfsdk:"tx_power"`
	TxPowerMode           types.String `tfsdk:"tx_power_mode"`
	MinRssiEnabled        types.Bool   `tfsdk:"min_rssi_enabled"`
	MinRssi               types.Int64  `tfsdk:"min_rssi"`
	AntennaGain           types.Int64  `tfsdk:"antenna_gain"`
	AntennaID             types.Int64  `tfsdk:"antenna_id"`
	Dfs                   types.Bool   `tfsdk:"dfs"`
	HardNoiseFloorEnabled types.Bool   `tfsdk:"hard_noise_floor_enabled"`
	LoadbalanceEnabled    types.Bool   `tfsdk:"loadbalance_enabled"`
	Maxsta                types.Int64  `tfsdk:"maxsta"`
	Name                  types.String `tfsdk:"name"`
	SensLevel             types.Int64  `tfsdk:"sens_level"`
	SensLevelEnabled      types.Bool   `tfsdk:"sens_level_enabled"`
	VwireEnabled          types.Bool   `tfsdk:"vwire_enabled"`
}

// outletOverrideModel describes the outlet override data model.
type outletOverrideModel struct {
	Index        types.Int64  `tfsdk:"index"`
	Name         types.String `tfsdk:"name"`
	RelayState   types.Bool   `tfsdk:"relay_state"`
	CycleEnabled types.Bool   `tfsdk:"cycle_enabled"`
}

// dropAssistedRoaming removes the radio_table attributes that UniFi Network 10.x
// dropped. Prior state carries them, and the framework decodes it against the
// current schema, which rejects attributes the schema no longer declares.
func dropAssistedRoaming(state map[string]any) {
	radios, ok := state["radio_table"].([]any)
	if !ok {
		return
	}
	for _, r := range radios {
		if rm, ok := r.(map[string]any); ok {
			delete(rm, "assisted_roaming_enabled")
			delete(rm, "assisted_roaming_rssi")
		}
	}
}

// mergePortOverridesByIndex overlays the user-declared port overrides onto the
// device's current overrides, keyed by port_idx. The UniFi PUT replaces the whole
// port_overrides array, so to manage only a subset of ports without clobbering the
// rest (#266) we start from what the controller already has and replace just the
// declared ports. Ports present only in the current set are preserved; ports
// declared but not yet present are appended. Declared order is preserved for the
// appended entries so the result is deterministic.
// portOverridesForUpdate decides what the PUT body carries for port_overrides.
//
// The UniFi PUT treats the array as a FULL REPLACE, so whatever goes in the
// body is the device's entire override set afterwards. Sending only the
// declared subset wipes every other port (#266), which is what
// mergePortOverridesByIndex exists to prevent.
//
// THERE IS NO "LEAVE IT ALONE" OPTION. port_overrides has no omitempty, so a
// nil slice does not drop out of the body -- it marshals to [], which is a
// full replace with nothing and clears every override the controller holds.
// The merge therefore has to run even when the practitioner declared no
// blocks at all, and merging against an empty declared set is exactly the
// case mergePortOverridesByIndex already returns `current` for.
//
// That also matches what the schema promises: removing a block stops managing
// that port rather than resetting it. Removing the last one is not a special
// case.
func portOverridesForUpdate(
	currentDevice *unifi.Device,
	declared []unifi.DevicePortOverrides,
) []unifi.DevicePortOverrides {
	if currentDevice == nil {
		return declared
	}
	return mergePortOverridesByIndex(currentDevice.PortOverrides, declared)
}

func mergePortOverridesByIndex(
	current, declared []unifi.DevicePortOverrides,
) []unifi.DevicePortOverrides {
	if len(declared) == 0 {
		return current
	}

	declaredByIdx := make(map[int64]int, len(declared))
	for i, po := range declared {
		if po.PortIDX != nil {
			declaredByIdx[*po.PortIDX] = i
		}
	}

	merged := make([]unifi.DevicePortOverrides, 0, len(current)+len(declared))
	used := make([]bool, len(declared))
	for _, po := range current {
		if po.PortIDX != nil {
			if i, ok := declaredByIdx[*po.PortIDX]; ok {
				merged = append(merged, declared[i])
				used[i] = true
				continue
			}
		}
		merged = append(merged, po)
	}
	// Append declared ports not already merged: newly-managed ports, or any entry
	// without a port_idx (which we cannot key on).
	for i, po := range declared {
		if !used[i] {
			merged = append(merged, po)
		}
	}
	return merged
}

// reconcilePortOverrides rebuilds the port_override Set from the API response,
// but only for ports and fields that the user explicitly configured. This lets
// Terraform detect drift (e.g. tagged VLANs not applied) without the phantom
// drift caused by computed fields the API adds for every port.

// cleanMAC normalizes MAC address format.
func cleanMAC(mac string) string {
	mac = strings.ReplaceAll(mac, "-", ":")
	mac = strings.ToLower(mac)
	return mac
}

// portOverrideAttrTypes returns the attribute types for port override objects.
func portOverrideAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"index":                            types.Int64Type,
		"name":                             types.StringType,
		"port_profile_id":                  types.StringType,
		"op_mode":                          types.StringType,
		"poe_mode":                         types.StringType,
		"aggregate_members":                types.ListType{ElemType: types.Int64Type},
		"autoneg":                          types.BoolType,
		"dot1x_ctrl":                       types.StringType,
		"dot1x_idle_timeout":               timetypes.GoDurationType{},
		"egress_rate_limit_kbps":           types.Int64Type,
		"egress_rate_limit_kbps_enabled":   types.BoolType,
		"excluded_networkconf_ids":         types.ListType{ElemType: types.StringType},
		"fec_mode":                         types.StringType,
		"flow_control_enabled":             types.BoolType,
		"forward":                          types.StringType,
		"full_duplex":                      types.BoolType,
		"isolation":                        types.BoolType,
		"lldpmed_enabled":                  types.BoolType,
		"lldpmed_notify_enabled":           types.BoolType,
		"mirror_port_idx":                  types.Int64Type,
		"multicast_router_networkconf_ids": types.ListType{ElemType: types.StringType},
		"native_networkconf_id":            types.StringType,
		"port_keepalive_enabled":           types.BoolType,
		"port_security_enabled":            types.BoolType,
		"port_security_mac_address":        types.ListType{ElemType: types.StringType},
		"priority_queue1_level":            types.Int64Type,
		"priority_queue2_level":            types.Int64Type,
		"priority_queue3_level":            types.Int64Type,
		"priority_queue4_level":            types.Int64Type,
		"setting_preference":               types.StringType,
		"speed":                            types.Int64Type,
		"stormctrl_bcast_enabled":          types.BoolType,
		"stormctrl_bcast_level":            types.Int64Type,
		"stormctrl_bcast_rate":             types.Int64Type,
		"stormctrl_mcast_enabled":          types.BoolType,
		"stormctrl_mcast_level":            types.Int64Type,
		"stormctrl_mcast_rate":             types.Int64Type,
		"stormctrl_type":                   types.StringType,
		"stormctrl_ucast_enabled":          types.BoolType,
		"stormctrl_ucast_level":            types.Int64Type,
		"stormctrl_ucast_rate":             types.Int64Type,
		"stp_port_mode":                    types.BoolType,
		"tagged_networkconf_ids":           types.ListType{ElemType: types.StringType},
		"tagged_vlan_mgmt":                 types.StringType,
		"voice_networkconf_id":             types.StringType,
	}
}

// configNetworkAttrTypes returns the attribute types for config network objects.
func configNetworkAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"type":            types.StringType,
		"ip":              types.StringType,
		"netmask":         types.StringType,
		"gateway":         types.StringType,
		"dns1":            types.StringType,
		"dns2":            types.StringType,
		"dnssuffix":       types.StringType,
		"bonding_enabled": types.BoolType,
	}
}

// radioTableAttrTypes returns the attribute types for radio table objects.
func radioTableAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"radio":                    types.StringType,
		"channel":                  types.StringType,
		"ht":                       types.Int64Type,
		"tx_power":                 types.StringType,
		"tx_power_mode":            types.StringType,
		"min_rssi_enabled":         types.BoolType,
		"min_rssi":                 types.Int64Type,
		"antenna_gain":             types.Int64Type,
		"antenna_id":               types.Int64Type,
		"dfs":                      types.BoolType,
		"hard_noise_floor_enabled": types.BoolType,
		"loadbalance_enabled":      types.BoolType,
		"maxsta":                   types.Int64Type,
		"name":                     types.StringType,
		"sens_level":               types.Int64Type,
		"sens_level_enabled":       types.BoolType,
		"vwire_enabled":            types.BoolType,
	}
}

// outletOverrideAttrTypes returns the attribute types for outlet override objects.
func outletOverrideAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"index":         types.Int64Type,
		"name":          types.StringType,
		"relay_state":   types.BoolType,
		"cycle_enabled": types.BoolType,
	}
}

// stringOrNull returns a types.String with the value or null if empty.
func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// int64OrNull returns a types.Int64 with the value or null if zero.
func int64OrNull(i int64) types.Int64 { //nolint:unused
	if i == 0 {
		return types.Int64Null()
	}
	return types.Int64Value(i)
}
