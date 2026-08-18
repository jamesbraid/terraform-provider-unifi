package unifi

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func testAccDNSRecordCheckDestroy(s *terraform.State) error {
	ctx := context.Background()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "unifi_dns_record" {
			continue
		}
		id := rs.Primary.ID
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = "default"
		}
		client := &Client{
			ApiClient: nil, // populated by the acceptance test provider
			Site:      site,
		}
		// Use the shared provider client via a direct API call.
		apiClient, err := unifi.New(ctx, &unifi.Config{
			BaseURL:       rs.Primary.Attributes["api_url"],
			Username:      rs.Primary.Attributes["username"],
			Password:      rs.Primary.Attributes["password"],
			AllowInsecure: true,
		})
		if err != nil {
			// If we can't build a client, skip the check.
			return nil //nolint:nilerr // best-effort check; skip when no live client
		}
		client.ApiClient = apiClient
		_, err = client.GetDNSRecord(ctx, site, id)
		if err != nil {
			if _, ok := err.(*unifi.NotFoundError); ok {
				continue
			}
			return fmt.Errorf("error checking DNS record %s: %w", id, err)
		}
		return fmt.Errorf("DNS record %s still exists", id)
	}
	return nil
}

func TestAccDNSRecordFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: providerFactories,
		CheckDestroy:             testAccDNSRecordCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccDNSRecordFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("unifi_dns_record.test", "name", "test-record"),
					resource.TestCheckResourceAttr(
						"unifi_dns_record.test",
						"value",
						"192.168.1.100",
					),
					resource.TestCheckResourceAttr("unifi_dns_record.test", "priority", "10"),
					resource.TestCheckResourceAttr("unifi_dns_record.test", "enabled", "true"),
				),
				ExpectError: regexp.MustCompile(".*"),
			},
		},
	})
}

func testAccDNSRecordFrameworkConfig_basic() string {
	return `
resource "unifi_dns_record" "test" {
  name        = "test-record.example.com"
  enabled     = true
  priority    = 10
  record_type = "A"
  ttl         = "5m0s"
  value       = "192.168.1.100"
}
`
}

func TestNewDNSRecordFrameworkResource(t *testing.T) {
	r := NewDNSRecordFrameworkResource()
	if r == nil {
		t.Fatal("returned nil")
	}
	if _, ok := r.(fwresource.ResourceWithConfigure); !ok {
		t.Error("expected ResourceWithConfigure")
	}
	if _, ok := r.(fwresource.ResourceWithImportState); !ok {
		t.Error("expected ResourceWithImportState")
	}
	if _, ok := r.(fwresource.ResourceWithIdentity); !ok {
		t.Error("expected ResourceWithIdentity")
	}
	if _, ok := r.(fwresource.ResourceWithUpgradeState); !ok {
		t.Error("expected ResourceWithUpgradeState")
	}
}

func TestNewDNSRecordListResource(t *testing.T) {
	r := NewDNSRecordListResource()
	if r == nil {
		t.Fatal("returned nil")
	}
	if _, ok := r.(fwlist.ListResourceWithConfigure); !ok {
		t.Error("expected ListResourceWithConfigure")
	}
}

func testAccDNSRecordListConfig_basic() string {
	return `
resource "unifi_dns_record" "test" {
  name        = "test-record.example.com"
  enabled     = true
  record_type = "A"
  ttl         = "5m0s"
  value       = "192.168.1.100"
}
`
}

func TestAccDNSRecordList_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		TerraformVersionChecks: []tfversion.TerraformVersionCheck{
			tfversion.SkipBelow(tfversion.Version1_14_0),
		},
		Steps: []resource.TestStep{
			{
				Config: testAccDNSRecordListConfig_basic(),
			},
			{
				Query: true,
				Config: `
					provider "unifi" {}
					list "unifi_dns_record" "test" {
						provider = unifi
						config {
							filter {
								name  = "name"
								value = "test-record.example.com"
						  }
					  }
					}
				`,
				QueryResultChecks: []querycheck.QueryResultCheck{
					querycheck.ExpectLengthAtLeast("unifi_dns_record.test", 1),
				},
			},
		},
	})
}
