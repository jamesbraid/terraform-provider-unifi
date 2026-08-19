package unifi

// #193's regression test, WRITTEN BEFORE THE FIX AND BY SOMEBODY ELSE.
//
// That provenance is the point. A test written after a fix, by whoever wrote
// the fix, can only assert what the new code does; this one had something to
// satisfy that was not derived from it. It was red against the defect, end to
// end through the provider against a real controller, before any of the code it
// now guards existed.
//
// Kept verbatim from evidence's branch (a7fec5e5,
// unifi/controller_zero_semantics_test.go) apart from this header and the
// import block. Rewriting it to match the fix would have thrown away the only
// property that makes it worth more than the tests I wrote alongside the fix.
//
// It runs only under TF_ACC.

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

// TestAnUnrelatedApplyDestroysControllerSideIPAliases is task 193's prediction,
// run end to end through the provider rather than reasoned from the encoder.
//
// The reading says: ip_aliases is never read back into state (networkToModel
// sets it null on every operation), the struct is pre-set to an empty slice, the
// guarded assignment cannot fire on a null model value, and the key is inside
// the masked update. So an apply that changes something else should clear
// aliases the practitioner set elsewhere.
//
// EVERY LINK OF THAT IS ALREADY MEASURED SEPARATELY. This is the composition,
// and it is worth running because a chain of four correct facts can still be
// wrong about the whole -- which is how three findings died in this lane.
//
// IT ASSERTS THE BEHAVIOUR WE WANT AND SO IT FAILS TODAY. The first version
// logged which way the round trip went and returned nil either way, which is a
// check that cannot fail -- the exact shape this repository keeps finding, in a
// test written to demonstrate one. A test that reports an outcome is a probe; a
// test that requires one is evidence.
//
// It is written before the fix and against the defect deliberately. A test
// written after the fix, by whoever wrote it, can only assert what the new code
// does; this one has something to satisfy that was not derived from it.
//
// It runs only under TF_ACC, so the ordinary suite stays green while it is red.
//
// The aliases are set BETWEEN the two applies, so the first establishes state and
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
					"    THIS IS EXPECTED TO FAIL UNTIL TASK 193 IS FIXED. It asserts the "+
					"behaviour we want,\n"+
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
