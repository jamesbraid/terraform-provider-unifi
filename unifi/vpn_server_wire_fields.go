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
		// The port the practitioner sets as wireguard.port / openvpn.port /
		// l2tp.port. It reaches network.LocalPort through
		// vpnServerLocalPortToNetwork, and it was missing here, so a port change
		// was accepted at plan and never written -- measured on 10.4.57, an
		// update from 51820 to 51821 read back as 51820 and the apply failed
		// with an inconsistent result for the whole wireguard block.
		"local_port",
		// THE PER-TYPE WAN FIELDS, reached through vpnServerWANIPToNetwork and
		// vpnServerWANInterfaceToNetwork. Each helper assigns only the pair
		// belonging to this server's own type, so the other four stay nil,
		// drop out of the encoding on omitempty, and networkMaskFor removes
		// them before the write. They were missing here for the same reason
		// local_port was: the mask check could not see an assignment made in a
		// helper.
		"l2tp_interface",
		"l2tp_local_wan_ip",
		"name",
		"openvpn_encryption_cipher",
		"openvpn_interface",
		"openvpn_local_wan_ip",
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
		"wireguard_interface",
		"wireguard_local_wan_ip",
		"x_wireguard_private_key",
	}
}
