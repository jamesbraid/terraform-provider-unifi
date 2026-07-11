# Block ad and adult content for the kids network, with safe search enforced.
resource "unifi_content_filtering" "kids" {
  name        = "Kids"
  categories  = ["ADVERTISEMENT"]
  network_ids = [unifi_network.kids.id]
  block_list  = ["example-tracker.com"]
  allow_list  = ["example-school.org"]
  safe_search = ["GOOGLE", "YOUTUBE", "BING"]
}
