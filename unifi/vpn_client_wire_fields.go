package unifi

// The wire fields unifi_vpn_client manages.
//
// WHY A MASK AT ALL. The resource used UpdateNetwork, which sends the whole
// object, and it builds that object from the plan alone -- so every Network
// field the provider does not model went out as its Go zero on every apply.
// Measured: dhcpd_dns_enabled left as false each time, whatever the controller
// held. No concurrent writer is needed for that; an apply on an unchanged
// configuration does it.
//
// The masked update ends it by construction rather than by a rule anyone
// maintains: a field this resource never assigns cannot be in the list below,
// so it is never named, so it is never sent.
//
// THE LIST IS DECLARED AND THEN CHECKED, which is the only way a hand-kept
// enumeration is safe. TestWireFieldMasksMatchTheirMappers derives the same
// set from the source -- the fields modelToNetwork assigns, intersected with
// what the vpn-client encoder emits -- and fails if the two disagree. Adding a
// field to the mapper without adding it here would otherwise mean the new
// attribute silently stopped being written, which is the defect this fixes
// arriving from the other side.
//
// THE INTERSECTION IS LOAD-BEARING ELSEWHERE, NOT HERE. maskedBody refuses a
// mask naming a field the purpose encoder drops, so on a surface whose
// attributes the encoder discards, an unfiltered mask turns a silent no-op into
// a failed apply. For vpn-client the encoder drops none of the fifteen, so the
// filter is inert -- which is exactly why this surface is the one to prove the
// shape on.
func vpnClientWireFields() []string {
	return []string{
		"enabled",
		"ip_subnet",
		"name",
		"purpose",
		"vpn_client_default_route",
		"vpn_client_pull_dns",
		"vpn_type",
		"wireguard_client_mode",
		"wireguard_client_peer_ip",
		"wireguard_client_peer_port",
		"wireguard_client_peer_public_key",
		"wireguard_client_preshared_key",
		"wireguard_client_preshared_key_enabled",
		"wireguard_interface",
		"x_wireguard_private_key",
	}
}
