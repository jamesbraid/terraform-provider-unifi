# unifi_nat manages a source NAT rule. out_interface is the _id of a WAN
# network, not the literal "wan" -- the controller rejects anything that is
# not a WAN networkconf. The sim and a fresh site ship no WAN, so create one
# with unifi_wan and reference its id.
resource "unifi_wan" "primary" {
  name    = "WAN"
  type    = "dhcp"
  enabled = true
}

# Masquerade a LAN behind the WAN interface's own address.
resource "unifi_nat" "masquerade" {
  description   = "masquerade lan"
  type          = "MASQUERADE"
  out_interface = unifi_wan.primary.id

  source_filter = {
    filter_type = "NONE"
  }

  destination_filter = {
    filter_type = "NONE"
  }
}
