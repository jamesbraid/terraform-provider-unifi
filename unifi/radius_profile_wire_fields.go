package unifi

// The wire fields unifi_radius_profile manages.
//
// The resource fetched the object and then rebuilt it from the Terraform
// model before writing, so the fetch did not protect anything: a model holds
// nothing for a field the provider does not declare. Laundering a live read
// through the model reduces it to the schema, which is why a GET before the
// write is not evidence the write is safe.
//
// The mask is the assigned set; ownership decides membership.
func radiusProfileManagedWireFields() []string {
	return []string{
		"accounting_enabled",
		"acct_servers",
		"auth_servers",
		"interim_update_enabled",
		"interim_update_interval",
		"name",
		"use_usg_acct_server",
		"use_usg_auth_server",
		"vlan_enabled",
		"vlan_wlan_mode",
	}
}
