terraform {
  required_providers {
    unifi = {
      source = "registry.terraform.io/ubiquiti-community/unifi"
    }
  }
}

provider "unifi" {}

variable "dns_name" {
  type = string
}

resource "unifi_dns_record" "test" {
  name        = var.dns_name
  record_type = "A"
  ttl         = 300
  value       = "192.0.2.20"
}
