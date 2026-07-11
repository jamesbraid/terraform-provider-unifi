# Port-map style destination NAT: forward WAN tcp/443 to an internal host.
resource "unifi_nat_rule" "https_dnat" {
  type         = "DNAT"
  description  = "HTTPS to reverse proxy"
  in_interface = "eth4"
  protocol     = "tcp"
  ip_address   = "10.0.10.5"
  port         = 443

  destination_filter = {
    filter_type = "ADDRESS_AND_PORT"
    port        = 443
  }
}

# Source NAT a lab network out of a specific WAN address.
resource "unifi_nat_rule" "lab_snat" {
  type          = "SNAT"
  description   = "Lab egress address"
  out_interface = "eth8"
  ip_address    = "203.0.113.7"

  source_filter = {
    filter_type = "NETWORK_CONF"
    network_id  = unifi_network.lab.id
  }
}
