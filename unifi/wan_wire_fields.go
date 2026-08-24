package unifi

import "github.com/ubiquiti-community/go-unifi/unifi"

// wanManagedWireFields lists every wire unifi_wan can write. Declared and then
// derived: TestWANManagedWireFieldsMatchTheResource builds the same set from
// the source and fails if they disagree, since nothing hand-written in this
// area is trusted without a check.
func wanManagedWireFields() []string {
	return []string{
		"attr_hidden_id",
		"enabled",
		"igmp_proxy_for",
		"igmp_proxy_upstream",
		"ipv6_setting_preference",
		"ipv6_wan_delegation_type",
		"mac_override_enabled",
		"name",
		"purpose",
		"report_wan_event",
		"setting_preference",
		"single_network_lan",
		"upnp_enabled",
		"upnp_nat_pmp_enabled",
		"upnp_secure_mode",
		"upnp_wan_interface",
		"wan_dhcp_cos",
		"wan_dhcp_options",
		"wan_dhcpv6_cos",
		"wan_dhcpv6_options",
		"wan_dhcpv6_pd_size",
		"wan_dhcpv6_pd_size_auto",
		"wan_dns1",
		"wan_dns2",
		"wan_dns_preference",
		"wan_dslite_remote_host",
		"wan_dslite_remote_host_auto",
		"wan_egress_qos",
		"wan_egress_qos_enabled",
		"wan_failover_priority",
		"wan_ip_aliases",
		"wan_ipv6_dns1",
		"wan_ipv6_dns2",
		"wan_ipv6_dns_preference",
		"wan_load_balance_type",
		"wan_load_balance_weight",
		"wan_networkgroup",
		"wan_network_group",
		"wan_provider_capabilities",
		"wan_smartq_down_rate",
		"wan_smartq_enabled",
		"wan_smartq_up_rate",
		"wan_type",
		"wan_type_v6",
		"wan_vlan",
		"wan_vlan_enabled",
	}
}

// wanWireFields narrows the managed list to what this object will encode.
// Shares networkMaskFor with unifi_network: one purpose here rather than three,
// so the filter is nearly inert, but the shape is the same and a WAN that grew
// a second purpose would be covered without anyone remembering to change this.
func wanWireFields(network *unifi.Network) []string {
	return networkMaskFor(wanManagedWireFields(), network)
}
