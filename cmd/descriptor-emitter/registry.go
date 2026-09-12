package main

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	resource_ap_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_ap_group"
	resource_bgp "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_bgp"
	resource_client_qos_rate "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_client_qos_rate"
	resource_dhcp_option "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dhcp_option"
	resource_dns_record "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dns_record"
	resource_dpi_app "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dpi_app"
	resource_dpi_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dpi_group"
	resource_dynamic_dns "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dynamic_dns"
	resource_firewall_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_group"
	resource_firewall_zone "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_zone"
	resource_hotspot_op "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_hotspot_op"
	resource_radius_user "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_radius_user"
	resource_schedule_task "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_schedule_task"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	resource_wireguard_peer "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wireguard_peer"
	resource_wlan_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wlan_group"
)

// surface is one descriptor the emitter serves. Everything else about it is
// derived: the SDK struct from the mapping artifact, the wires from the
// mapping's managed fields, the model from the schema's attributes.
type surface struct {
	// name is the shared basename: <name>.mapping.json,
	// <name>_descriptor.go and <name>_descriptor_gen.go.
	name string
	// schema builds the surface's generated schema. Nil for a settings
	// section, whose schema is the named SingleNestedAttribute of
	// unifi_setting's.
	schema func(context.Context) schema.Schema
	// section names the resource_setting attribute a section descriptor
	// serves; empty for a managed resource.
	section string
}

func (s surface) built(ctx context.Context) schema.Schema {
	if s.section == "" {
		return s.schema(ctx)
	}
	built := resource_setting.SettingResourceSchema(ctx)
	nested := built.Attributes[s.section].(schema.SingleNestedAttribute) //nolint:forcetypeassert // every section is a SingleNestedAttribute in the generated schema; a mismatch is a generator regression to fail loudly on.
	return schema.Schema{Attributes: nested.Attributes}
}

var surfaces = []surface{
	{name: "ap_group", schema: resource_ap_group.ApGroupResourceSchema},
	{name: "bgp", schema: resource_bgp.BgpResourceSchema},
	{name: "client_qos_rate", schema: resource_client_qos_rate.ClientQosRateResourceSchema},
	{name: "dhcp_option", schema: resource_dhcp_option.DhcpOptionResourceSchema},
	{name: "dns_record", schema: resource_dns_record.DnsRecordResourceSchema},
	{name: "dpi_app", schema: resource_dpi_app.DpiAppResourceSchema},
	{name: "dpi_group", schema: resource_dpi_group.DpiGroupResourceSchema},
	{name: "dynamic_dns", schema: resource_dynamic_dns.DynamicDnsResourceSchema},
	{name: "firewall_group", schema: resource_firewall_group.FirewallGroupResourceSchema},
	{name: "firewall_zone", schema: resource_firewall_zone.FirewallZoneResourceSchema},
	{name: "hotspot_op", schema: resource_hotspot_op.HotspotOpResourceSchema},
	{name: "radius_user", schema: resource_radius_user.RadiusUserResourceSchema},
	{name: "schedule_task", schema: resource_schedule_task.ScheduleTaskResourceSchema},
	{name: "setting_auto_speedtest", section: "auto_speedtest"},
	{name: "setting_connectivity", section: "connectivity"},
	{name: "setting_country", section: "country"},
	{name: "setting_device_supervision", section: "device_supervision"},
	{name: "setting_dpi", section: "dpi"},
	{name: "setting_global_ap", section: "global_ap"},
	{name: "setting_global_nat", section: "global_nat"},
	{name: "setting_global_network", section: "global_network"},
	{name: "setting_guest_access", section: "guest_access"},
	{name: "setting_lcm", section: "lcm"},
	{name: "setting_locale", section: "locale"},
	{name: "setting_magic_site_to_site_vpn", section: "magic_site_to_site_vpn"},
	{name: "setting_mgmt", section: "mgmt"},
	{name: "setting_netflow", section: "netflow"},
	{name: "setting_network_optimization", section: "network_optimization"},
	{name: "setting_ntp", section: "ntp"},
	{name: "setting_radio_ai", section: "radio_ai"},
	{name: "setting_radius", section: "radius"},
	{name: "setting_snmp", section: "snmp"},
	{name: "setting_ssl_inspection", section: "ssl_inspection"},
	{name: "setting_syslog", section: "syslog"},
	{name: "setting_teleport", section: "teleport"},
	{name: "setting_traffic_flow", section: "traffic_flow"},
	{name: "setting_usw", section: "usw"},
	{name: "wireguard_peer", schema: resource_wireguard_peer.WireguardPeerResourceSchema},
	{name: "wlan_group", schema: resource_wlan_group.WlanGroupResourceSchema},
}
