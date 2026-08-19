package unifi

import (
	"encoding/json"

	"github.com/ubiquiti-community/go-unifi/unifi"
)

// The wire fields unifi_network manages.
//
// THE LIST IS WHAT THIS RESOURCE ASSIGNS; THE PURPOSE FILTER IS APPLIED AT
// RUNTIME. unifi_network writes three purposes -- corporate, guest and
// vlan-only -- and each encodes a different field set, so a single static mask
// cannot be right for all three. networkWireFields intersects this list with
// what the object actually encodes, which is purpose-correct by construction
// and needs no per-purpose table to drift.
//
// _id and site_id are excluded deliberately: the id is the request path, not a
// field to patch, and site_id is the controller's own.
//
// Measured before the change: a corporate or guest network sent NINE unmodelled
// fields as their Go zero on every apply -- dhcpd_mac_1..3, the three igmp_*,
// ipv6_aliases, mac_override_enabled and upnp_lan_enabled. A vlan-only network
// sent three of the nine. None of them appears below, because this resource
// never assigns them.
func networkManagedWireFields() []string {
	return []string{
		"auto_scale_enabled",
		"dhcp_relay_enabled",
		"dhcp_relay_servers",
		"dhcpd_boot_enabled",
		"dhcpd_boot_filename",
		"dhcpd_boot_server",
		"dhcpd_conflict_checking",
		"dhcpd_dns_1",
		"dhcpd_dns_2",
		"dhcpd_dns_3",
		"dhcpd_dns_4",
		"dhcpd_dns_enabled",
		"dhcpd_enabled",
		"dhcpd_gateway_enabled",
		"dhcpd_ip_1",
		"dhcpd_ip_2",
		"dhcpd_ip_3",
		"dhcpd_leasetime",
		"dhcpd_ntp_enabled",
		"dhcpd_start",
		"dhcpd_stop",
		"dhcpd_tftp_server",
		"dhcpd_time_offset_enabled",
		"dhcpd_unifi_controller",
		"dhcpd_wins_1",
		"dhcpd_wins_2",
		"dhcpd_wins_enabled",
		"dhcpd_wpad_url",
		"dhcpdv6_dns_auto",
		"dhcpdv6_enabled",
		"dhcpdv6_leasetime",
		"dhcpdv6_start",
		"dhcpdv6_stop",
		"dhcpguard_enabled",
		"domain_name",
		"enabled",
		"gateway_type",
		"igmp_snooping",
		"internet_access_enabled",
		"ip_aliases",
		"ip_subnet",
		"ipv6_client_address_assignment",
		"ipv6_interface_type",
		"ipv6_pd_auto_prefixid_enabled",
		"ipv6_pd_interface",
		"ipv6_pd_prefixid",
		"ipv6_pd_start",
		"ipv6_pd_stop",
		"ipv6_ra_enabled",
		"ipv6_ra_preferred_lifetime",
		"ipv6_ra_priority",
		"ipv6_ra_valid_lifetime",
		"ipv6_subnet",
		"lte_lan_enabled",
		"mdns_enabled",
		"name",
		"nat_outbound_ip_addresses",
		"network_isolation_enabled",
		"networkgroup",
		"purpose",
		"setting_preference",
		"vlan",
		"vlan_enabled",
		"wan_networkgroup",
	}
}

// networkWireFields narrows the managed list to what THIS object will encode.
//
// IT ASKS THE ENCODER RATHER THAN A PER-PURPOSE TABLE. go-unifi's Network
// serialises through one of seven structs chosen by Purpose, so marshalling the
// object about to be sent and reading back its keys gives exactly the fields
// that purpose carries -- no second copy of go-unifi's knowledge, and no table
// to fall out of date when a purpose gains a field.
//
// THE INTERSECTION IS LOAD-BEARING HERE, and on vpn_server too -- this comment
// used to name vpn_server as a surface that did not need it, which was measured
// false: twelve of its 21 names are dropped by the encoder and every update
// failed. vpn_client remains the surface where it is inert.
// maskedBody refuses a mask naming a field the encoder drops, and a vlan-only
// network encodes 22 fields against corporate's 99 -- so an unfiltered mask
// would turn today's silent no-op into a failed apply on the surface with the
// most to lose.
//
// A field the mapper set to its zero value drops out here when the encoder tags
// it omitempty, and that is correct rather than a gap: the encoder would not
// send it either, so naming it would only get the write refused. The plan-time
// warning in dropped_on_write.go is what tells the practitioner about those.
func networkWireFields(network *unifi.Network) []string {
	return networkMaskFor(networkManagedWireFields(), network)
}

// networkMaskFor is the shared half, used by unifi_network and unifi_wan.
//
// Both are Network-backed and both need the same rule: a mask may name only
// what this object's purpose actually encodes. Keeping one implementation means
// the two cannot diverge on the question that decides whether go-unifi accepts
// the write.
func networkMaskFor(managed []string, network *unifi.Network) []string {
	raw, err := json.Marshal(network)
	if err != nil {
		// An object that cannot encode has a bigger problem than its mask, and
		// the write will report it.
		return nil
	}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil
	}
	mask := make([]string, 0, len(managed))
	for _, name := range managed {
		if _, carried := encoded[name]; carried {
			mask = append(mask, name)
		}
	}
	return mask
}
