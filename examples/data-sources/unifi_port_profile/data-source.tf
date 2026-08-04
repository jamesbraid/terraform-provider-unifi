# Look up a switch port profile by name. The profile ID and its native network
# are computed, which is handy for assigning the profile to a device port.
data "unifi_port_profile" "all" {
  name = "All"
}

output "port_profile_id" {
  description = "The ID of the port profile, for use in device port overrides."
  value       = data.unifi_port_profile.all.id
}

output "port_profile_native_network_id" {
  description = "The native (untagged) network ID for the port profile."
  value       = data.unifi_port_profile.all.native_networkconf_id
}

output "port_profile_tagged_vlan_mode" {
  description = "The stored tagged VLAN mode (`auto`, `block_all`, or `custom`)."
  value       = data.unifi_port_profile.all.tagged_vlan_mgmt
}

output "port_profile_tagged_network_ids" {
  description = "The actual tagged networks after applying the mode and exclusions."
  value       = data.unifi_port_profile.all.tagged_networkconf_ids
}

output "port_profile_excluded_network_ids" {
  description = "The controller-facing exclusions used by Custom mode."
  value       = data.unifi_port_profile.all.excluded_networkconf_ids
}
