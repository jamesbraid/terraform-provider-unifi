# unifi_hotspot_op manages a hotspot operator: a guest-portal login account
# with a name, an optional note and a password.
resource "unifi_hotspot_op" "front_desk" {
  name     = "front-desk"
  password = "change-me"
  note     = "lobby kiosk operator"
}
