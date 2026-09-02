# unifi_dpi_app manages a DPI application rule: a set of deep-packet-inspection
# applications or categories the controller blocks or logs, with optional QoS
# rate caps.
resource "unifi_dpi_app" "block_streaming" {
  name    = "block-streaming"
  enabled = true
  blocked = true
  log     = true
  cats    = [4]
}
