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

// testAccDNSRecordCheckDestroy read the controller address out of the DNS
// record's own attributes, which do not include one.
//
// api_url, username and password are provider configuration; a unifi_dns_record
// has none of them, so all three resolved to the empty string and unifi.New was
// handed BaseURL "". That does not fail at construction -- the nolint below
// expected it to, and returning nil there is what "best-effort" meant -- so the
// check fell through to a request against the empty URL and died with
// `unsupported protocol scheme ""`.
//
// IT COULD NOT SURFACE UNTIL SOMETHING WAS CREATED. The loop runs over the
// resources left in state, and the create step it belonged to always ended in
// ExpectError, so the state was always empty and the loop body never ran.
// Fixing the expectation is what made this reachable, on the first run.
//
// The environment is where every other CheckDestroy in this package reads the
// controller from, and preCheck already requires those variables.
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

// TestAccDNSRecordFramework_basic asserts what it creates, which it did not do
// between January and now.
//
// Its create step carried ExpectError with a pattern of ".*", added by a commit
// called "Fix Acceptance Tests" that also deleted the import step. That pattern
// is satisfied by every possible message, so the step passed on any failure --
// and on a check failure too, because the framework returns a failing Check as
// an error and matches it against the same pattern. The four
// TestCheckResourceAttr calls below sat inside that step and could not fail.
//
// One of them could not have passed either: it asserted name was "test-record"
// while the config set "test-record.example.com".
//
// THE APPLY WAS FAILING FOR A FIXTURE REASON. Running it against a 10.4.57
// controller with a pattern that could not match printed what the ".*" had been
// hiding for seven months:
//
//	api.err.StaticDnsRecordInvalidParameters: A record may not have a value set
//	on the following parameters - port, priority, weight (400)
//
// priority belongs to MX and SRV records. The config set it on an A record, the
// controller rejected the create, and the expectation made that look like a
// pass. The resource was never at fault.
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
				// Restored. The resource implements ResourceWithImportState and
				// ResourceWithIdentity, and after the step was deleted nothing
				// in the tree exercised either.
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
