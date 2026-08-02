# A VPN server is configured with exactly one of the `wireguard`, `l2tp`, or
# `openvpn` nested blocks. The block you choose determines the server type.
# The `subnet` is the server's tunnel network in gateway form (the first
# address, e.g. `10.100.0.1/24`, is the server's tunnel IP).

# WireGuard server. If `private_key` is omitted the provider generates one at
# create time and exposes the derived `public_key` as a computed attribute
# (useful for configuring `unifi_wireguard_peer` resources).
resource "unifi_vpn_server" "wireguard" {
  name   = "wireguard"
  subnet = "10.100.0.1/24"

  wireguard = {
    port = 51820
  }
}

# WireGuard server that also pushes custom DNS servers to connecting clients and
# binds to a specific WAN interface. `dns.enabled` defaults to true when
# `servers` is non-empty.
resource "unifi_vpn_server" "wireguard_dns" {
  name   = "wireguard-dns"
  subnet = "10.101.0.1/24"

  wireguard = {
    port = 51821
  }

  dns = {
    servers = ["1.1.1.1", "1.0.0.1"]
  }

  wan = {
    ip        = "any"
    interface = "wan"
  }
}

# L2TP/IPsec server. `pre_shared_key` is required by the controller. L2TP
# servers authenticate users against a RADIUS profile.
resource "unifi_radius_profile" "l2tp" {
  name = "l2tp-radius"

  auth_server {
    ip     = "192.168.1.100"
    port   = 1812
    secret = "radius-secret"
  }
}

resource "unifi_vpn_server" "l2tp" {
  name             = "l2tp"
  subnet           = "10.110.0.1/24"
  radiusprofile_id = unifi_radius_profile.l2tp.id

  l2tp = {
    pre_shared_key     = "change-me-to-a-strong-secret"
    allow_weak_ciphers = false
  }
}

# OpenVPN server. The controller generates the certificates and keys, which are
# exposed as computed (sensitive) attributes. `mode` is one of `server` or
# `site-to-site`; `encryption_cipher` is one of `AES_256_CBC`
# or `BF_CBC`.
#
# A VPN server always needs a `radiusprofile_id`. This one points at an external
# RADIUS server, which needs nothing else. To authenticate against the
# controller's own accounts instead, reference its built-in profile and enable
# the built-in server — see the local-credentials example below.
resource "unifi_vpn_server" "openvpn" {
  name             = "openvpn"
  subnet           = "10.120.0.1/24"
  radiusprofile_id = unifi_radius_profile.l2tp.id

  openvpn = {
    port              = 1194
    mode              = "server"
    encryption_cipher = "AES_256_CBC"
  }
}

# Local credentials instead of an external RADIUS server. The controller keeps
# those accounts in its own RADIUS server, which has to be running first: create
# a VPN server against the built-in profile while it is off and the controller
# answers api.err.RadiusServerNotEnabled. The UniFi UI enables it inline,
# prompting for the pre-shared key as you create the server.
resource "unifi_setting" "radius" {
  radius = {
    enabled = true
    secret  = var.radius_secret
  }
}

data "unifi_radius_profile" "default" {
  name = "Default"
}

resource "unifi_vpn_server" "openvpn_local" {
  depends_on = [unifi_setting.radius]

  name             = "openvpn-local"
  subnet           = "10.122.0.1/24"
  radiusprofile_id = data.unifi_radius_profile.default.id

  openvpn = {
    mode              = "server"
    encryption_cipher = "AES_256_CBC"
  }
}
