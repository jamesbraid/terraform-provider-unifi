package unifi

// The wire fields unifi_wlan manages.
//
// THE LARGEST INSTANCE OF THE CLASS AND IT WAS FILED AS THE SAFER HALF.
// wlan_resource.go merges the plan into prior state and then writes the whole
// object, which I had classified as less dangerous than building from the plan
// alone. It is not: a model holds nothing for a field the provider does not
// model, whichever way it was filled, so the unmodelled fields go as zeros
// either way.
//
// TWENTY-ONE FIELDS WERE GOING AS FALSE ON EVERY APPLY -- more than the four
// Network-backed surfaces put together. dpi_enabled, rrm_enabled,
// bc_filter_enabled, auth_cache, p2p, p2p_cross_connect, tdls_prohibit,
// radius_das_enabled, iot_channel_lock, sae_psk_vlan_required and eleven more,
// plus dpigroup_id as an empty string. A WLAN configured through the
// controller UI had all of them reset by an apply that changed something else.
//
// WLAN has no custom MarshalJSON, so there is no purpose discriminator and no
// runtime filter: force-emission here is just a bare json tag. The mask is the
// assigned set, and ownership is what decides membership.
func wlanManagedWireFields() []string {
	return []string{
		"ap_group_ids",
		"ap_group_mode",
		"bc_filter_list",
		"bss_transition",
		"dtim_6e",
		"dtim_mode",
		"dtim_na",
		"dtim_ng",
		"enabled",
		"enhanced_iot",
		"fast_roaming_enabled",
		"group_rekey",
		"hide_ssid",
		"hotspot2conf_enabled",
		"iapp_enabled",
		"is_guest",
		"l2_isolation",
		"mac_filter_enabled",
		"mac_filter_list",
		"mac_filter_policy",
		"mcastenhance_enabled",
		"minrate_na_data_rate_kbps",
		"minrate_na_enabled",
		"minrate_ng_data_rate_kbps",
		"minrate_ng_enabled",
		"minrate_setting_preference",
		"mlo_enabled",
		"name",
		"name_combine_enabled",
		"nas_identifier_type",
		"networkconf_id",
		"no2ghz_oui",
		"pmf_mode",
		"private_preshared_keys",
		"private_preshared_keys_enabled",
		"proxy_arp",
		"radius_mac_auth_enabled",
		"radiusprofile_id",
		"roaming_assistant_6e_enabled",
		"roaming_assistant_6e_rssi",
		"roaming_assistant_na_enabled",
		"roaming_assistant_na_rssi",
		"schedule_enabled",
		"schedule_with_duration",
		"security",
		"uapsd_enabled",
		"usergroup_id",
		"vlan",
		"vlan_enabled",
		"wlan_band",
		"wlan_bands",
		"wlangroup_id",
		"wpa3_enhanced_192",
		"wpa3_fast_roaming",
		"wpa3_support",
		"wpa3_transition",
		"wpa_enc",
		"wpa_mode",
		"x_passphrase",
	}
}
