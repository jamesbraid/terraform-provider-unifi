package unifi

// The wire fields unifi_vpn_server manages.
//
// Same defect and same fix as vpn_client: the resource built its Network from
// the plan alone and wrote it whole, so every field it does not model went out
// as its Go zero on every apply. Measured, two of them reached the wire that
// way -- require_mschapv2 and
// vpn_client_configuration_remote_ip_override_enabled, both sent as false
// whatever the controller held.
//
// THE INTERSECTION IS LOAD-BEARING HERE, and this comment said the opposite.
// It claimed none of the 21 is dropped by the encoder and that the filter was
// carried for uniformity. Both were wrong: the filter was not applied at the
// call site at all, and a live 10.4.57 controller drops twelve of the 21 --
// openvpn_encryption_cipher, openvpn_mode, radiusprofile_id and the nine x_*
// keys -- because a VPN server encodes only the fields its own protocol
// carries. Every update failed, on all three protocols.
//
// The claim was made 294 seconds before networkMaskFor was written for
// unifi_network, in that commit's comment, about a surface it did not re-check.
//
// Declared and then checked: TestWireFieldMasksMatchTheirMappers derives the
// same set from the source and fails if the two disagree.
func vpnServerWireFields() []string {
	return []string{
		"dhcpd_dns_enabled",
		"enabled",
		"ip_subnet",
		"l2tp_allow_weak_ciphers",
		"name",
		"openvpn_encryption_cipher",
		"openvpn_mode",
		"purpose",
		"radiusprofile_id",
		"setting_preference",
		"vpn_type",
		"x_auth_key",
		"x_ca_crt",
		"x_ca_key",
		"x_dh_key",
		"x_ipsec_pre_shared_key",
		"x_server_crt",
		"x_server_key",
		"x_shared_client_crt",
		"x_shared_client_key",
		"x_wireguard_private_key",
	}
}
