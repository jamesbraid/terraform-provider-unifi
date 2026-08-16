terraform {
  required_providers {
    unifi = {
      source = "registry.terraform.io/ubiquiti-community/unifi"
    }
  }
}

provider "unifi" {}

# The exact shape that fails in TestAccFirewallPolicyFramework_basic: a network
# created with nothing but name, subnet and vlan. On the released provider the
# schema default fills setting_preference = "auto" before the controller is
# consulted.
resource "unifi_network" "bare_corporate" {
  name   = "tf-upg-corp"
  subnet = "10.191.0.1/24"
  vlan   = 191
}

# The second operator state. network_resource.go:1109 preserves the PLAN value
# for a vlan-only network instead of taking the controller's, so this one's
# state can hold "auto" while the controller says something else. That
# disagreement is where a removed default is most likely to surface a diff.
# Expressed as third_party_gateway rather than purpose = "vlan-only", which is
# how the acceptance suite writes it. Setting purpose directly and leaving
# third_party_gateway to its default made the CLEAN provider fail with
# "produced an unexpected new value: .third_party_gateway: was cty.False, but
# now cty.True" -- a second instance of this same defect class, on the released
# shape, recorded separately. It is not what this measurement is about.
resource "unifi_network" "vlan_only" {
  name                = "tf-upg-vlanonly"
  subnet              = "10.192.0.1/24"
  vlan                = 192
  third_party_gateway = true
}

# The third operator state, and the only one known to reproduce the defect.
# TestAccNetworkFramework_basic creates a near-identical bare network and
# settles as "auto"; the fixture that fails is attached to a firewall zone. So
# an upgrade measurement over bare networks alone would miss the very state the
# fix exists for. Whether the zone attachment is what makes the controller
# answer "manual" is the open question -- controller-answer.sh reads the
# controller's own reply for all three, which settles it either way.
resource "unifi_network" "zoned" {
  name   = "tf-upg-zoned"
  subnet = "10.193.0.1/24"
  vlan   = 193
}

resource "unifi_firewall_zone" "zoned" {
  name        = "tf-upg-zone"
  network_ids = [unifi_network.zoned.id]
}
