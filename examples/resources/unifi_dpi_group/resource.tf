# unifi_dpi_group gathers DPI application rules under one name so the
# controller can enable or disable them together.
resource "unifi_dpi_group" "example" {
  name        = "example"
  enabled     = true
  dpiapp_ids  = [unifi_dpi_app.block_streaming.id]
}

resource "unifi_dpi_app" "block_streaming" {
  name    = "block-streaming"
  blocked = true
  cats    = [4]
}
