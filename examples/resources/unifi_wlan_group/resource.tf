# unifi_wlan_group manages a WLAN group. Every wireless network belongs to a
# group; the controller ships a built-in "Default" one.
resource "unifi_wlan_group" "example" {
  name = "example"
}
