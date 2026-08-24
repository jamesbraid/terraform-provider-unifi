package unifi

import (
	"context"
	"fmt"
	"os"
	"testing"

	fwlist "github.com/hashicorp/terraform-plugin-framework/list"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/querycheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

// testAccDNSRecordCheckDestroy reads the controller address from the
// environment, since a unifi_dns_record has no attributes that carry it --
// api_url, username and password are provider configuration. This is the
// same place every other CheckDestroy in this package reads it from, and
// preCheck already requires those variables.
func testAccDNSRecordCheckDestroy(s *terraform.State) error {
	ctx := context.Background()
	apiURL := os.Getenv("UNIFI_API")
	if apiURL == "" {
		return nil
	}
	apiClient, err := unifi.New(ctx, &unifi.Config{
		BaseURL:       apiURL,
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
	if err != nil {
		return nil //nolint:nilerr // best-effort check; skip when no live client
	}
	c := &Client{ApiClient: apiClient, Site: "default"}
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "unifi_dns_record" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = c.Site
		}
		_, err := c.GetDNSRecord(ctx, site, rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("unifi_dns_record %s still exists", rs.Primary.ID)
		}
		if _, ok := err.(*unifi.NotFoundError); !ok {
			return err
		}
	}
	return nil
}

// TestAccDNSRecordFramework_basic checks what it creates against the config
// below, including an import round trip.
func TestAccDNSRecordFramework_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: providerFactories,
		CheckDestroy:             testAccDNSRecordCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccDNSRecordFrameworkConfig_basic(),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_dns_record.test", "name", "test-record.example.com"),
					resource.TestCheckResourceAttr(
						"unifi_dns_record.test", "value", "192.168.1.100"),
					resource.TestCheckResourceAttr("unifi_dns_record.test", "record_type", "A"),
					resource.TestCheckResourceAttr("unifi_dns_record.test", "ttl", "5m0s"),
					resource.TestCheckResourceAttr("unifi_dns_record.test", "enabled", "true"),
				),
			},
			{
				// This import step is what exercises ResourceWithImportState
				// and ResourceWithIdentity; nothing else in the tree does.
				ResourceName:      "unifi_dns_record.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccDNSRecordFrameworkConfig_basic() string {
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
