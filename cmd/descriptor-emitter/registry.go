package main

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	resource_ap_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_ap_group"
	resource_bgp "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_bgp"
	resource_client "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_client"
	resource_client_qos_rate "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_client_qos_rate"
	resource_device "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_device"
	resource_dhcp_option "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dhcp_option"
	resource_dns_record "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dns_record"
	resource_dpi_app "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dpi_app"
	resource_dpi_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dpi_group"
	resource_dynamic_dns "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dynamic_dns"
	resource_firewall_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_group"
	resource_firewall_policy "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_policy"
	resource_firewall_rule "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_rule"
	resource_firewall_zone "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_zone"
	resource_hotspot_op "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_hotspot_op"
	resource_nat "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_nat"
	resource_network "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_network"
	resource_port_forward "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_port_forward"
	resource_port_profile "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_port_profile"
	resource_power_supervisor "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_power_supervisor"
	resource_radius_profile "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_radius_profile"
	resource_radius_user "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_radius_user"
	resource_schedule_task "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_schedule_task"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	resource_site "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_site"
	resource_site_to_site_vpn "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_site_to_site_vpn"
	resource_static_route "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_static_route"
	resource_traffic_route "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_traffic_route"
	resource_vpn_client "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_vpn_client"
	resource_vpn_server "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_vpn_server"
	resource_wan "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wan"
	resource_wireguard_peer "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wireguard_peer"
	resource_wlan "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wlan"
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
	// handModel keeps the top-level model struct in the hand descriptor: the
	// resource injects an attribute the generated schema does not carry
	// (site_to_site_vpn's and wlan's write-only secrets), so a schema-derived
	// model would be missing a member the framework requires.
	handModel bool
	// handNested keeps the nested element models and attr-type maps hand,
	// for the same reason one nesting level down (vpn_client's wireguard
	// object gains private_key_wo at serve time).
	handNested bool
	// mirror emits the whole descriptor -- Spec, Backend, Section -- for a
	// settings section that reads the document, writes back the wires the
	// plan named, and does nothing else. A section with a hook, a sibling
	// document or a conditional write is not a mirror.
	//
	// crud is the managed-resource counterpart: the Spec, schema, list and
	// Backend of a resource whose whole life is the SDK's five site-scoped
	// methods over the generated field list, listed and filtered by name.
	mirror bool
	crud   bool
	// subject names the surface in diagnostics ("NetFlow Setting"). The one
	// token no artifact spells: the schema carries the wire name, not the
	// capitalisation a practitioner reads in an error.
	subject string
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
	{name: "client", schema: resource_client.ClientResourceSchema, handNested: true},
	{name: "device", schema: resource_device.DeviceResourceSchema, handNested: true},
	{name: "firewall_policy", schema: resource_firewall_policy.FirewallPolicyResourceSchema, handNested: true},
	// nat: source_filter and destination_filter are one shared shape over two
	// distinct SDK structs, kept hand as one natFilterModel like
	// firewall_policy's endpoint.
	{name: "nat", schema: resource_nat.NatResourceSchema, handNested: true},
	{name: "firewall_rule", schema: resource_firewall_rule.FirewallRuleResourceSchema},
	{name: "network", schema: resource_network.NetworkResourceSchema, handNested: true},
	{name: "port_forward", schema: resource_port_forward.PortForwardResourceSchema},
	{name: "port_profile", schema: resource_port_profile.PortProfileResourceSchema},
	{name: "power_supervisor", schema: resource_power_supervisor.PowerSupervisorResourceSchema},
	{name: "radius_profile", schema: resource_radius_profile.RadiusProfileResourceSchema},
	// site: the schema has no site attribute (a site is not site-scoped),
	// so the hand model carries an untagged dummy member for Spec.Site.
	{name: "site", schema: resource_site.SiteResourceSchema, handModel: true},
	{name: "site_to_site_vpn", schema: resource_site_to_site_vpn.SiteToSiteVpnResourceSchema, handModel: true},
	{name: "static_route", schema: resource_static_route.StaticRouteResourceSchema},
	{name: "traffic_route", schema: resource_traffic_route.TrafficRouteResourceSchema, handNested: true},
	{name: "vpn_client", schema: resource_vpn_client.VpnClientResourceSchema, handNested: true},
	{name: "vpn_server", schema: resource_vpn_server.VpnServerResourceSchema, handNested: true},
	{name: "wan", schema: resource_wan.WanResourceSchema, handNested: true},
	{name: "wlan", schema: resource_wlan.WlanResourceSchema, handModel: true},
	{name: "bgp", schema: resource_bgp.BgpResourceSchema},
	{name: "client_qos_rate", schema: resource_client_qos_rate.ClientQosRateResourceSchema},
	{name: "dhcp_option", schema: resource_dhcp_option.DhcpOptionResourceSchema, crud: true, subject: "DHCP Option"},
	{name: "dns_record", schema: resource_dns_record.DnsRecordResourceSchema},
	{name: "dpi_app", schema: resource_dpi_app.DpiAppResourceSchema, crud: true, subject: "DPI Application"},
	{name: "dpi_group", schema: resource_dpi_group.DpiGroupResourceSchema, crud: true, subject: "DPI Group"},
	{name: "dynamic_dns", schema: resource_dynamic_dns.DynamicDnsResourceSchema},
	{name: "firewall_group", schema: resource_firewall_group.FirewallGroupResourceSchema},
	{name: "firewall_zone", schema: resource_firewall_zone.FirewallZoneResourceSchema},
	{name: "hotspot_op", schema: resource_hotspot_op.HotspotOpResourceSchema, crud: true, subject: "Hotspot Operator"},
	{name: "radius_user", schema: resource_radius_user.RadiusUserResourceSchema},
	{name: "schedule_task", schema: resource_schedule_task.ScheduleTaskResourceSchema},
	{name: "setting_auto_speedtest", section: "auto_speedtest", mirror: true, subject: "Auto Speedtest Setting"},
	{name: "setting_dashboard", section: "dashboard"},
	{name: "setting_doh", section: "doh"},
	{name: "setting_ether_lighting", section: "ether_lighting"},
	{name: "setting_global_switch", section: "global_switch"},
	{name: "setting_igmp_snooping", section: "igmp_snooping", mirror: true, subject: "IGMP Snooping Setting"},
	{name: "setting_ips", section: "ips"},
	{name: "setting_ipsec", section: "ipsec", mirror: true, subject: "IPsec Setting"},
	{name: "setting_mdns", section: "mdns"},
	{name: "setting_usg", section: "usg"},
	{name: "setting_connectivity", section: "connectivity", mirror: true, subject: "Connectivity Setting"},
	{name: "setting_country", section: "country", mirror: true, subject: "Country Setting"},
	{name: "setting_device_supervision", section: "device_supervision", mirror: true, subject: "Device Supervision Setting"},
	{name: "setting_dpi", section: "dpi", mirror: true, subject: "DPI Setting"},
	{name: "setting_global_ap", section: "global_ap", mirror: true, subject: "Global AP Setting"},
	{name: "setting_global_nat", section: "global_nat", mirror: true, subject: "Global NAT Setting"},
	{name: "setting_global_network", section: "global_network", mirror: true, subject: "Global Network Setting"},
	{name: "setting_guest_access", section: "guest_access"},
	{name: "setting_lcm", section: "lcm", mirror: true, subject: "LCM Setting"},
	{name: "setting_locale", section: "locale", mirror: true, subject: "Locale Setting"},
	{name: "setting_magic_site_to_site_vpn", section: "magic_site_to_site_vpn", mirror: true, subject: "Magic Site-to-Site VPN Setting"},
	{name: "setting_mgmt", section: "mgmt"},
	{name: "setting_netflow", section: "netflow", mirror: true, subject: "NetFlow Setting"},
	{name: "setting_network_optimization", section: "network_optimization", mirror: true, subject: "Network Optimization Setting"},
	{name: "setting_ntp", section: "ntp", mirror: true, subject: "NTP Setting"},
	{name: "setting_radio_ai", section: "radio_ai"},
	{name: "setting_radius", section: "radius"},
	{name: "setting_snmp", section: "snmp"},
	{name: "setting_ssl_inspection", section: "ssl_inspection", mirror: true, subject: "SSL Inspection Setting"},
	{name: "setting_syslog", section: "syslog", mirror: true, subject: "Syslog Setting"},
	{name: "setting_teleport", section: "teleport", mirror: true, subject: "Teleport Setting"},
	{name: "setting_traffic_flow", section: "traffic_flow", mirror: true, subject: "Traffic Flow Setting"},
	{name: "setting_usw", section: "usw", mirror: true, subject: "USW Setting"},
	{name: "wireguard_peer", schema: resource_wireguard_peer.WireguardPeerResourceSchema},
	{name: "wlan_group", schema: resource_wlan_group.WlanGroupResourceSchema, crud: true, subject: "WLAN Group"},
}
