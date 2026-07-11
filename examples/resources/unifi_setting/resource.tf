# Example: Managing UniFi Settings with opt-in configuration

# Configure only management settings
resource "unifi_setting" "mgmt_only" {
  site = "default"

  mgmt = {
    auto_upgrade = true
    ssh_enabled  = true
    ssh_keys = [{
      name    = "admin-key"
      type    = "ssh-rsa"
      key     = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQD... admin@example.com"
      comment = "Administrator SSH Key"
    }]
  }
}

# Configure multiple settings types
resource "unifi_setting" "combined" {
  site = "default"

  mgmt = {
    auto_upgrade = true
    ssh_enabled  = false
  }

  radius = {
    accounting_enabled      = true
    auth_port               = 1812
    acct_port               = 1813
    interim_update_interval = "10m"
    secret                  = "my-radius-secret"
  }

  usg = {
    broadcast_ping = false
    upnp_enabled   = true
    ftp_module     = false

    # DNS verification is a nested object on the USG/gateway settings.
    dns_verification = {
      domain             = "example.com"
      primary_dns_server = "1.1.1.1"
    }
  }
}

# Configure only RADIUS settings
resource "unifi_setting" "radius_only" {
  site = "default"

  radius = {
    accounting_enabled = true
    auth_port          = 1812
  }
}

# Site-wide switching, NAT, and locale settings
resource "unifi_setting" "globals" {
  site = "default"

  global_switch = {
    stp_version        = "rstp"
    jumboframe_enabled = false
    dhcp_snoop         = true
  }

  global_nat = {
    mode = "auto"
  }

  locale = {
    timezone = "America/Vancouver"
  }
}

# Connectivity services: mDNS, Teleport, Site Magic VPN, traffic flows,
# and Etherlighting
resource "unifi_setting" "connectivity" {
  site = "default"

  mdns = {
    mode                = "custom"
    predefined_services = ["apple_airPlay", "google_chromecast", "printers"]
    custom_services = [{
      name    = "Home Assistant"
      address = "_home-assistant._tcp"
    }]
  }

  teleport = {
    enabled = true
    subnet  = "192.168.100.0/24"
  }

  # The controller generates and manages the WireGuard key pair;
  # private_key/public_key never need to be set.
  magic_site_to_site_vpn = {
    enabled = true
  }

  traffic_flow = {
    enabled_allowed_traffic         = true
    gateway_dns_enabled             = true
    unifi_device_management_enabled = false
    unifi_services_enabled          = true
  }

  # Only declare colors that differ from the controller's built-in
  # defaults — identical overrides are silently dropped by the controller.
  ether_lighting = {
    speed_overrides = [{
      speed     = "GbE"
      color_hex = "ff6c14"
    }]
    # network_overrides = [{
    #   network_id = unifi_network.iot.id
    #   color_hex  = "0544ff"
    # }]
  }
}

# Monitoring & security settings. SNMP credentials are sensitive — source
# them from variables, never literals.
variable "snmp_community" {
  type      = string
  sensitive = true
}

resource "unifi_setting" "monitoring" {
  site = "default"

  snmp = {
    enabled   = true
    community = var.snmp_community
  }

  netflow = {
    enabled = true
    server  = "192.0.2.10"
    port    = 2055
    version = 10
  }

  ssl_inspection = {
    state = "off"
  }

  # Requires a gateway-class controller (UDM/USG):
  global_network = {
    default_security_posture = "ALLOW_ALL"
  }

  usg_geo = {
    ip_filtering = {
      enabled           = true
      action            = "block"
      countries         = "KP"
      traffic_direction = "both"
    }
  }

  ipsec = {
    ikev2_reauthentication_method = "make-before-break"
  }
}

# Portal, radio optimization, and dashboard settings. Guest portal
# credentials are sensitive — source them from variables, never literals.
variable "guest_portal_password" {
  type      = string
  sensitive = true
}

resource "unifi_setting" "portal" {
  site = "default"

  guest_access = {
    auth             = "hotspot"
    password         = var.guest_portal_password
    password_enabled = true
    portal_enabled   = true

    portal_customization = {
      customized = true
      title      = "Guest WiFi"
      bg_color   = "#005ED9"
    }
  }

  # Co-managed by the controller: only enabled/setting_preference are set
  # here. Leave setting_preference = "auto" (the default) unless you need to
  # pin specific channels — see the attribute's churn warning.
  radio_ai = {
    enabled = true
  }

  dashboard = {
    layout_preference = "auto"
  }

  # Requires go-unifi PR 0 (settings.ProviderCapabilities). Not modeled by
  # every controller — lightweight/simulated controllers (e.g. the demo
  # container used in this provider's acceptance tests) reject this key
  # outright with api.err.Invalid; it is intended for ISP-gateway-class
  # deployments that report WAN capacity for utilization displays and Smart
  # Queues sizing.
  provider_capabilities = {
    download = 1000000
    upload   = 500000
  }
}
