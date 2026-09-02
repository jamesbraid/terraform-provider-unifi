# DPI application rules can be imported using the rule ID.
terraform import unifi_dpi_app.block_streaming 5f3e9b2c4ee8cb0f1f4a1234

# For a non-default site, prefix the ID with the site name and a colon.
terraform import unifi_dpi_app.block_streaming default:5f3e9b2c4ee8cb0f1f4a1234
