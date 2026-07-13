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

  guest_access = {
    auth            = "hotspot"
    portal_enabled  = true
    portal_hostname = "guest.example.internal"
    expire_number   = 8
    expire_unit     = 60 # 1 = minutes, 60 = hours, 1440 = days
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

# Configure only guest portal / access settings
resource "unifi_setting" "guest_access_only" {
  site = "default"

  guest_access = {
    auth                = "hotspot"
    portal_enabled      = true
    portal_use_hostname = true
    portal_hostname     = "guest.example.internal"

    expire_number = 8
    expire_unit   = 60 # 1 = minutes, 60 = hours, 1440 = days

    redirect_enabled  = true
    redirect_url      = "https://welcome.example.com/"
    redirect_to_https = true

    allowed_subnet         = "10.20.30.0/24"
    restricted_dns_enabled = true
    restricted_dns_servers = ["192.0.2.1", "198.51.100.1"]

    radius_enabled   = true
    radiusprofile_id = "radius-profile-example"
    radius_auth_type = "chap"

    # Secrets: replace with real values — never commit one. Omitting a
    # configured secret on a later apply preserves the value already stored
    # on the controller (it is never re-read, only ever written).
    password            = "replace-with-a-real-password" # replace with a real secret — never commit one
    facebook_enabled    = true
    facebook_app_id     = "example-app-id-123"
    facebook_app_secret = "replace-with-a-real-secret" # replace with a real secret — never commit one

    payment_enabled    = true
    gateway            = "paypal"
    paypal_use_sandbox = true
    paypal_username    = "sandbox-user"
    paypal_password    = "replace-with-a-real-secret" # replace with a real secret — never commit one
    paypal_signature   = "replace-with-a-real-secret" # replace with a real secret — never commit one
  }
}
