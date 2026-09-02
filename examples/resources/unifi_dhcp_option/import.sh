# DHCP options can be imported using the option ID.
terraform import unifi_dhcp_option.example 5f3e9b2c4ee8cb0f1f4a1234

# For a non-default site, prefix the ID with the site name and a colon.
terraform import unifi_dhcp_option.example default:5f3e9b2c4ee8cb0f1f4a1234
