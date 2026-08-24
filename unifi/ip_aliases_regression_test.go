package unifi

// This is the controller-side IP aliases regression test, kept verbatim
// from its original source (written before the fix, by somebody else)
// apart from this header and the import block -- rewriting it to match the
// fix would destroy the one property that makes it worth more than a test
// written alongside the fix. Runs only under TF_ACC.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// probeClient talks to the controller directly, around the provider, which is
// what makes the out-of-band setup and the after-check independent of the code
// under test.
func probeClient(t *testing.T) (*ui.ApiClient, string) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance only")
	}
	client, err := ui.New(context.Background(), &ui.Config{
		BaseURL:       os.Getenv("UNIFI_API"),
		AllowInsecure: true,
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("connecting to %s: %v", os.Getenv("UNIFI_API"), err)
	}
	return client, "default"
}

// TestAnUnrelatedApplyDestroysControllerSideIPAliases proves this prediction
// end to end through the provider, not just reasoned from the encoder:
// ip_aliases is never read back into state, so an apply that changes
// something else should clear aliases set outside Terraform. Aliases are
// set out of band BETWEEN the two applies -- the first establishes state,
// the second is the unrelated change.
func TestAnUnrelatedApplyDestroysControllerSideIPAliases(t *testing.T) {
	const alias = "10.77.77.5/24"
	var networkID string

	setAliasesOutOfBand := func() {
		client, site := probeClient(t)
		ctx := context.Background()
		networks, err := client.ListNetwork(ctx, site)
		if err != nil {
			t.Fatalf("ListNetwork: %v", err)
		}
		for i := range networks {
			if networks[i].Name != nil && *networks[i].Name == "tfacc-alias-victim" {
				networkID = networks[i].ID
				break
			}
		}
		if networkID == "" {
			t.Fatal("the network the provider created is not on the controller")
		}
		n, err := client.GetNetwork(ctx, site, networkID)
		if err != nil {
			t.Fatalf("GetNetwork: %v", err)
		}
		n.IPAliases = []string{alias}
		if _, err := client.UpdateNetwork(ctx, site, n); err != nil {
			t.Fatalf("setting ip_aliases out of band: %v", err)
		}
		// POSITIVE CONTROL: the controller must actually be holding them, or the
		// check after the second apply proves nothing.
		back, err := client.GetNetwork(ctx, site, networkID)
		if err != nil {
			t.Fatal(err)
		}
		if len(back.IPAliases) == 0 {
			t.Fatalf("the controller did not accept ip_aliases=%q, so there is nothing for "+
				"the apply to destroy and this test would pass vacuously", alias)
		}
		t.Logf("POSITIVE CONTROL: controller holds ip_aliases=%v before the unrelated apply",
			back.IPAliases)
	}

	checkAliasesAfterApply := func(*terraform.State) error {
		client, site := probeClient(t)
		back, err := client.GetNetwork(context.Background(), site, networkID)
		if err != nil {
			return err
		}
		if len(back.IPAliases) == 0 {
			return fmt.Errorf(
				"ip_aliases is %v after an apply whose only change was the vlan.\n"+
					"    The controller held [%s] before it and the provider was never asked "+
					"to touch them.\n"+
					"    THIS IS EXPECTED TO FAIL UNTIL THE ip_aliases READ-BACK IS FIXED. "+
					"It asserts the behaviour we want,\n"+
					"    not the behaviour we have, so that the fix has something independent "+
					"to satisfy.",
				back.IPAliases, alias)
		}
		t.Logf("ip_aliases survived the unrelated apply as %v", back.IPAliases)
		return nil
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_network" "victim" {
	name    = "tfacc-alias-victim"
	subnet  = "10.77.77.1/24"
	vlan    = 77
	enabled = true
}
`,
			},
			{
				PreConfig: setAliasesOutOfBand,
				Config: `
resource "unifi_network" "victim" {
	name    = "tfacc-alias-victim"
	subnet  = "10.77.77.1/24"
	vlan    = 78
	enabled = true
}
`,
				Check: checkAliasesAfterApply,
			},
		},
	})
}
