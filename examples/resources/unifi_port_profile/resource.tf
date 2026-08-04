variable "native_vlan_id" {
  default = 10
}

variable "tagged_vlan_id" {
  default = 20
}

resource "unifi_network" "native" {
  name   = "management"
  subnet = "10.0.0.1/24"
  vlan   = var.native_vlan_id

  dhcp_server = {
    enabled = true
    start   = "10.0.0.6"
    stop    = "10.0.0.254"
  }
}

resource "unifi_network" "tagged" {
  name   = "servers"
  subnet = "10.0.20.1/24"
  vlan   = var.tagged_vlan_id
}

resource "unifi_port_profile" "server_trunk" {
  name                  = "Server Trunk"
  native_networkconf_id = unifi_network.native.id

  # This is an exact set. The provider writes the controller's inverse
  # excluded_networkconf_ids representation and keeps it current on apply.
  tagged_networkconf_ids = [
    unifi_network.tagged.id,
  ]
}

resource "unifi_port_profile" "poe_disabled" {
  name = "POE Disabled"

  native_networkconf_id = unifi_network.native.id
  poe_mode              = "off"
}
