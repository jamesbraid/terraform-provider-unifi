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
// The encoder emits 48 fields for this purpose and the mask names 21, so 27 are
// no longer sent at all. As with vpn_client the intersection with the encoder's
// emitted set is inert here -- none of the 21 is dropped by marshalUserVPN --
// so the filter is carried for uniformity rather than because it does work on
// this surface.
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
