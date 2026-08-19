package unifi

// The wire fields unifi_ap_group manages.
//
// The resource fetched the object and then rebuilt it from the Terraform
// model before writing, so the fetch did not protect anything: a model holds
// nothing for a field the provider does not declare. Laundering a live read
// through the model reduces it to the schema, which is why a GET before the
// write is not evidence the write is safe.
//
// The mask is the assigned set; ownership decides membership.
func apGroupManagedWireFields() []string {
	return []string{
		"device_macs",
		"name",
	}
}
