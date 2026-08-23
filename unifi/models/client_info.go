package models

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// AttributeTypes returns the attribute types for the client info object.
func AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"id":                           types.StringType,
		"mac":                          types.StringType,
		"name":                         types.StringType,
		"display_name":                 types.StringType,
		"hostname":                     types.StringType,
		"ip":                           types.StringType,
		"fixed_ip":                     types.StringType,
		"network_id":                   types.StringType,
		"network_name":                 types.StringType,
		"usergroup_id":                 types.StringType,
		"blocked":                      types.BoolType,
		"is_guest":                     types.BoolType,
		"is_wired":                     types.BoolType,
		"authorized":                   types.BoolType,
		"status":                       types.StringType,
		"uptime":                       timetypes.GoDurationType{},
		"first_seen":                   types.Int64Type,
		"last_seen":                    types.Int64Type,
		"oui":                          types.StringType,
		"local_dns_record":             types.StringType,
		"local_dns_record_enabled":     types.BoolType,
		"use_fixedip":                  types.BoolType,
		"ap_mac":                       types.StringType,
		"channel":                      types.Int64Type,
		"radio":                        types.StringType,
		"radio_name":                   types.StringType,
		"essid":                        types.StringType,
		"bssid":                        types.StringType,
		"signal":                       types.Int64Type,
		"rssi":                         types.Int64Type,
		"noise":                        types.Int64Type,
		"tx_rate":                      types.Int64Type,
		"rx_rate":                      types.Int64Type,
		"tx_bytes":                     types.Int64Type,
		"rx_bytes":                     types.Int64Type,
		"wired_rate_mbps":              types.Int64Type,
		"sw_port":                      types.Int64Type,
		"last_uplink_mac":              types.StringType,
		"last_uplink_name":             types.StringType,
		"last_uplink_remote_port":      types.Int64Type,
		"last_connection_network_id":   types.StringType,
		"last_connection_network_name": types.StringType,
	}
}

func ClientInfoAttrValues(
	ctx context.Context,
	clientInfo *unifi.ClientInfo,
) map[string]attr.Value {
	return map[string]attr.Value{
		"id":                         util.StringValueOrNull(clientInfo.Id),
		"mac":                        util.StringValueOrNull(clientInfo.Mac),
		"name":                       util.StringValueOrNull(clientInfo.Name),
		"display_name":               util.StringValueOrNull(clientInfo.DisplayName),
		"hostname":                   util.StringValueOrNull(clientInfo.Hostname),
		"ip":                         util.StringValueOrNull(clientInfo.IP),
		"fixed_ip":                   util.StringValueOrNull(clientInfo.FixedIP),
		"network_id":                 util.StringValueOrNull(clientInfo.NetworkId),
		"network_name":               util.StringValueOrNull(clientInfo.NetworkName),
		"usergroup_id":               util.StringValueOrNull(clientInfo.UsergroupId),
		"blocked":                    types.BoolValue(clientInfo.Blocked),
		"is_guest":                   types.BoolValue(clientInfo.IsGuest),
		"is_wired":                   types.BoolValue(clientInfo.IsWired),
		"authorized":                 types.BoolValue(clientInfo.Authorized),
		"status":                     util.StringValueOrNull(clientInfo.Status),
		"uptime":                     util.DurationPtrValue(clientInfo.Uptime, time.Second),
		"first_seen":                 types.Int64PointerValue(clientInfo.FirstSeen),
		"last_seen":                  types.Int64PointerValue(clientInfo.LastSeen),
		"oui":                        util.StringValueOrNull(clientInfo.Oui),
		"local_dns_record":           util.StringValueOrNull(clientInfo.LocalDNSRecord),
		"local_dns_record_enabled":   types.BoolValue(clientInfo.LocalDNSRecordEnabled),
		"use_fixedip":                types.BoolValue(clientInfo.UseFixedip),
		"ap_mac":                     util.StringValueOrNull(clientInfo.ApMac),
		"channel":                    types.Int64PointerValue(clientInfo.Channel),
		"radio":                      util.StringValueOrNull(clientInfo.Radio),
		"radio_name":                 util.StringValueOrNull(clientInfo.RadioName),
		"essid":                      util.StringValueOrNull(clientInfo.Essid),
		"bssid":                      util.StringValueOrNull(clientInfo.Bssid),
		"signal":                     types.Int64PointerValue(clientInfo.Signal),
		"rssi":                       types.Int64PointerValue(clientInfo.Rssi),
		"noise":                      types.Int64PointerValue(clientInfo.Noise),
		"tx_rate":                    types.Int64PointerValue(clientInfo.TxRate),
		"rx_rate":                    types.Int64PointerValue(clientInfo.RxRate),
		"tx_bytes":                   types.Int64PointerValue(clientInfo.TxBytes),
		"rx_bytes":                   types.Int64PointerValue(clientInfo.RxBytes),
		"wired_rate_mbps":            types.Int64PointerValue(clientInfo.WiredRateMbps),
		"sw_port":                    types.Int64PointerValue(clientInfo.SwPort),
		"last_uplink_mac":            util.StringValueOrNull(clientInfo.LastUplinkMac),
		"last_uplink_name":           util.StringValueOrNull(clientInfo.LastUplinkName),
		"last_uplink_remote_port":    types.Int64PointerValue(clientInfo.LastUplinkRemotePort),
		"last_connection_network_id": util.StringValueOrNull(clientInfo.LastConnectionNetworkId),
		"last_connection_network_name": util.StringValueOrNull(
			clientInfo.LastConnectionNetworkName,
		),
	}
}
