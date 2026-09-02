# DPI groups can be imported using the group ID.
terraform import unifi_dpi_group.example 5f3e9b2c4ee8cb0f1f4a5678

# For a non-default site, prefix the ID with the site name and a colon.
terraform import unifi_dpi_group.example default:5f3e9b2c4ee8cb0f1f4a5678
