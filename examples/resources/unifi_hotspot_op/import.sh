# Hotspot operators can be imported using the operator ID. The password is not
# returned by the controller, so set it in configuration after importing.
terraform import unifi_hotspot_op.front_desk 5f3e9b2c4ee8cb0f1f4a9abc

# For a non-default site, prefix the ID with the site name and a colon.
terraform import unifi_hotspot_op.front_desk default:5f3e9b2c4ee8cb0f1f4a9abc
