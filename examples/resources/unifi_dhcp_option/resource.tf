# unifi_dhcp_option defines a custom DHCP option: a code, a name and the value
# type DHCP clients receive. Codes the controller manages itself (15, 42, 43,
# 44, 51, 66, 67, 252) cannot be defined here.
resource "unifi_dhcp_option" "capwap_controller" {
  name = "capwap-controller"
  code = "138"
  type = "ipaddress"
}

resource "unifi_dhcp_option" "lease_seconds" {
  name  = "lease-seconds"
  code  = "100"
  type  = "integer"
  width = 32
}
