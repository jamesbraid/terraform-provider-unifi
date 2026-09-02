# An OSPF router announcing two networks into the backbone area
resource "unifi_ospf_router" "backbone" {
  router_id = "10.255.0.1"

  areas = [{
    area_id = "0.0.0.0"
    name    = "backbone"
    network_ids = [
      unifi_network.lan.id,
      unifi_network.iot.id,
    ]
  }]
}

# Redistribute static and connected routes, with per-interface settings
resource "unifi_ospf_router" "full" {
  router_id              = "10.255.0.2"
  announce_default_route = true

  redistribute_static_routes    = true
  redistribute_connected_routes = true

  areas = [{
    area_id     = "0.0.0.1"
    name        = "branch"
    network_ids = [unifi_network.branch.id]
  }]

  interfaces = [{
    network_id     = unifi_network.branch.id
    cost           = "10"
    hello_interval = "10"
    dead_interval  = "40"
  }]
}
