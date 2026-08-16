# PROVISIONAL FIXTURE, pending accept's verbatim configuration.
#
# A plain network is NOT sufficient and a green here would prove nothing: every
# plain network settles by itself, so the control and the subject would both
# report no changes whatever either provider did. The shape that reproduces is a
# network attached to a firewall zone, which is why one is included.
#
# accept ran this by hand and its configuration is the specification. Replace
# this with that one rather than trusting that this is equivalent.

terraform {
  required_providers {
    unifi = {
      source = "registry.terraform.io/ubiquiti-community/unifi"
    }
  }
}

provider "unifi" {}

resource "unifi_firewall_zone" "upgrade" {
  name = "upgrade-fixture"
}

resource "unifi_network" "upgrade" {
  name    = "upgrade-fixture"
  purpose = "corporate"
  subnet  = "10.79.0.1/24"
  vlan_id = 79

  firewall_zone_id = unifi_firewall_zone.upgrade.id
}
