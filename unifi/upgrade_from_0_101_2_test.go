package unifi

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// 0.101.2 is the release live states were written by before the kit. Its
// stored identities differ from what this build would write: a client kept
// mac alone, and an imported dynamic DNS kept site as null. Refreshing either
// must neither fail to decode nor change the identity.
var provider0_101_2 = map[string]resource.ExternalProvider{
	"unifi": {Source: "registry.terraform.io/jamesbraid/unifi", VersionConstraint: "0.101.2"},
}

const testAccUpgradeFrom0_101_2Config = `
resource "unifi_client" "test" {
	name = "tfacc-upgrade-client"
	mac  = "01:23:45:67:89:c0"
}

resource "unifi_dynamic_dns" "created" {
	service   = "dyndns"
	host_name = "tfacc-upgrade.example.com"
	server    = "dyndns.example.com"
	login     = "testuser"
	password  = "password"
}
`

// The import adopts the object Terraform just created under a second
// address, because 0.101.2 only stores a null site on the import path.
const testAccUpgradeFrom0_101_2ImportConfig = testAccUpgradeFrom0_101_2Config + `
import {
	to = unifi_dynamic_dns.imported
	id = unifi_dynamic_dns.created.id
}

resource "unifi_dynamic_dns" "imported" {
	service   = "dyndns"
	host_name = "tfacc-upgrade.example.com"
	server    = "dyndns.example.com"
	login     = "testuser"
	password  = "password"
}
`

const testAccUpgradeFrom0_101_2ForgetConfig = `
resource "unifi_client" "test" {
	name = "tfacc-upgrade-client"
	mac  = "01:23:45:67:89:c0"
}

removed {
	from = unifi_dynamic_dns.created
	lifecycle {
		destroy = false
	}
}

resource "unifi_dynamic_dns" "imported" {
	service   = "dyndns"
	host_name = "tfacc-upgrade.example.com"
	server    = "dyndns.example.com"
	login     = "testuser"
	password  = "password"
}
`

func TestAccUpgradeFrom0_101_2(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck: func() { preCheck(t) },
		Steps: []resource.TestStep{
			{
				ExternalProviders: provider0_101_2,
				Config:            testAccUpgradeFrom0_101_2Config,
			},
			{
				ExternalProviders: provider0_101_2,
				Config:            testAccUpgradeFrom0_101_2ImportConfig,
				// The shapes the homelab state carries, so the next step
				// starts from the same place.
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectIdentity("unifi_client.test", map[string]knownvalue.Check{
						"mac": knownvalue.StringExact("01:23:45:67:89:c0"),
					}),
					statecheck.ExpectIdentity("unifi_dynamic_dns.imported", map[string]knownvalue.Check{
						"id":   knownvalue.NotNull(),
						"site": knownvalue.Null(),
					}),
				},
			},
			{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Config:                   testAccUpgradeFrom0_101_2ImportConfig,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// Upgraded to the id+mac shape with id left null: the
					// upgrade sees only the old identity, which has no id.
					statecheck.ExpectIdentity("unifi_client.test", map[string]knownvalue.Check{
						"id":  knownvalue.Null(),
						"mac": knownvalue.StringExact("01:23:45:67:89:c0"),
					}),
					statecheck.ExpectIdentity("unifi_dynamic_dns.imported", map[string]knownvalue.Check{
						"id":   knownvalue.NotNull(),
						"site": knownvalue.Null(),
					}),
					statecheck.ExpectIdentityValueMatchesState(
						"unifi_dynamic_dns.imported", tfjsonpath.New("id")),
				},
			},
			{
				// Forget the second address before the post-test destroy: a
				// delete of an object already gone is api.err.IdInvalid here,
				// not a not-found.
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Config:                   testAccUpgradeFrom0_101_2ForgetConfig,
			},
		},
	})
}
