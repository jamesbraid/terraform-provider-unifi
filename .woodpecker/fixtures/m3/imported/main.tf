terraform {
  required_providers {
    unifi = {
      source  = "registry.terraform.io/ubiquiti-community/unifi"
      version = "0.101.2"
    }
  }
}

provider "unifi" {}

variable "dns_name" {
  type = string
}

resource "unifi_dns_record" "test" {
  name        = var.dns_name
  enabled     = false
  record_type = "A"
  ttl         = "5m0s"
  value       = "192.0.2.11"
}

output "dns_record_id" {
  value = unifi_dns_record.test.id
}
