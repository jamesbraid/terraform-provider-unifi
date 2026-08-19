package unifi

import "github.com/ubiquiti-community/go-unifi/unifi"

// The wire fields unifi_wan manages.
//
// TWO CALL SITES, ONE DEFECT. wan_resource.go wrote the whole object in both
// Update and adoptExistingWAN, so every field it does not model went out as
// its Go zero. Update runs on EVERY apply that touches a WAN;
// adoptExistingWAN runs once, when a create finds the interface already
// exists. The frequent one was the one nobody had named.
//
// SEVEN FIELDS WERE GOING AS ZERO, and two of them are credentials:
// x_wan_password and wan_username, with wan_pppoe_password_enabled and
// wan_pppoe_username_enabled alongside. A WAN configured for PPPoE through
// the controller UI had its username and password blanked by an apply that
// changed something else. The others are interface_mtu_enabled, wan_ipv6 and
// wan_gateway_v6.
//
// Declared and then derived: TestWANManagedWireFieldsMatchTheResource builds
// the same set from the source and fails if they disagree. The list here was
// itself built by a regex that missed a field on unifi_network, which is why
// nothing hand-written in this area is trusted without a check.
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
