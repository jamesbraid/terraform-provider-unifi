# Content filtering policies can be imported using the policy ID.
terraform import unifi_content_filtering.example 5f3e9b2c4ee8cb0f1f4a1234

# For a non-default site, prefix the ID with the site name and a colon.
terraform import unifi_content_filtering.example default:5f3e9b2c4ee8cb0f1f4a1234
