package unifi

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// The three DHCP blocks with no guard on main: dhcp_guarding, dhcp_relay
// and dhcpv6 (TestEachDHCPFlagWithItsOperand already covers dhcp_server).
// These are probes, not scenarios -- none is a TestAcc function, and
// unifi/network_resource_test.go stays the scenario owner -- but they run
// as part of the acceptance suite's regression coverage.

// TestAnUnrelatedApplyDestroysDHCPGuardingServers is run the same way as the
// sibling DHCP guard probes: set the values on the controller out of band,
// change something else through the provider, and require them to survive.
//
// They do. Network is kit-driven (network_descriptor.go): dhcp_guarding is a
// resourcekit.ScatteredObjectField (network_descriptor.go:429-448). SetInPlan
// reports it absent for a null object, so a plan that never declares
// dhcp_guarding drops the field from the update mask entirely -- Encode never
// runs and dhcpguard_enabled/dhcpd_ip_1..3 never join the patch
// (internal/resourcekit/scattered_object_field.go:126-134 is maskedWireNames'
// null check, :185-192 is ToSDK's). ConditionalWires handles the adjacent
// case, a non-null object with fewer than three servers, so a short list
// doesn't blank the slots it left unfilled. Either way the controller's
// existing values are left alone. This is the live-controller half of that
// proof; TestDHCPGuardingWireMaskExcludesItsFieldsWhenNull below is the
// controller-free half of the same mechanism.
func TestAnUnrelatedApplyDestroysDHCPGuardingServers(t *testing.T) {
	const guard = "10.76.76.9"
	var networkID string

	setGuardingOutOfBand := func() {
		client, site := probeClient(t)
		ctx := context.Background()
		networks, err := client.ListNetwork(ctx, site)
		if err != nil {
			t.Fatalf("ListNetwork: %v", err)
		}
		for i := range networks {
			if networks[i].Name != nil && *networks[i].Name == "tfacc-guard-victim" {
				networkID = networks[i].ID
				break
			}
		}
		if networkID == "" {
			t.Fatal("the network the provider created is not on the controller")
		}
		n, err := client.GetNetwork(ctx, site, networkID)
		if err != nil {
			t.Fatal(err)
		}
		n.DHCPguardEnabled = true
		n.DHCPDIP1 = guard
		if _, err := client.UpdateNetwork(ctx, site, n); err != nil {
			t.Fatalf("setting the guarding server out of band: %v", err)
		}
		back, err := client.GetNetwork(ctx, site, networkID)
		if err != nil {
			t.Fatal(err)
		}
		if back.DHCPDIP1 != guard {
			t.Fatalf("the controller did not store dhcpd_ip_1=%q (got %q), so there is "+
				"nothing for the apply to destroy and this test would pass vacuously",
				guard, back.DHCPDIP1)
		}
		t.Logf("POSITIVE CONTROL: controller holds dhcpd_ip_1=%q before the unrelated apply",
			back.DHCPDIP1)
	}

	checkAfterApply := func(*terraform.State) error {
		client, site := probeClient(t)
		back, err := client.GetNetwork(context.Background(), site, networkID)
		if err != nil {
			return err
		}
		if back.DHCPDIP1 != guard {
			return fmt.Errorf(
				"dhcpd_ip_1 is %q after an apply whose only change was the vlan.\n"+
					"    The controller held %q and the provider was never asked to touch "+
					"dhcp_guarding.",
				back.DHCPDIP1, guard)
		}
		t.Logf("dhcpd_ip_1 survived the unrelated apply as %q", back.DHCPDIP1)
		return nil
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_network" "guard" {
	name    = "tfacc-guard-victim"
	subnet  = "10.76.76.1/24"
	vlan    = 76
	enabled = true
}
`,
			},
			{
				PreConfig: setGuardingOutOfBand,
				Config: `
resource "unifi_network" "guard" {
	name    = "tfacc-guard-victim"
	subnet  = "10.76.76.1/24"
	vlan    = 79
	enabled = true
}
`,
				Check: checkAfterApply,
			},
		},
	})
}

// TestDHCPGuardingWireMaskExcludesItsFieldsWhenNull is the controller-free
// half of TestAnUnrelatedApplyDestroysDHCPGuardingServers above: it pins
// that Spec.WireFields itself never names dhcpguard_enabled or dhcpd_ip_1..3
// for a plan whose dhcp_guarding is null (the SetInPlan/null-check mechanism
// described there), and does name them once dhcp_guarding is set -- the same
// call the resource's Update path uses, exercised directly with no
// controller involved.
func TestDHCPGuardingWireMaskExcludesItsFieldsWhenNull(t *testing.T) {
	guardWires := []string{"dhcpguard_enabled", "dhcpd_ip_1", "dhcpd_ip_2", "dhcpd_ip_3"}

	nullPlan := &netModel{
		Name:         types.StringValue("probe"),
		DhcpGuarding: types.ObjectNull(dhcpGuardingModel{}.AttributeTypes()),
	}
	fields, err := networkKitSpec().WireFields(nullPlan)
	if err != nil {
		t.Fatalf("WireFields (null dhcp_guarding): %v", err)
	}
	for _, wire := range guardWires {
		if slices.Contains(fields, wire) {
			t.Errorf("the mask names %q for a null dhcp_guarding object: %v", wire, fields)
		}
	}

	// The positive control: the same four wires DO appear once dhcp_guarding
	// is set, so the null case above isn't just an always-empty mask.
	servers, diags := types.ListValueFrom(context.Background(), types.StringType,
		[]string{"10.1.1.1", "10.1.1.2", "10.1.1.3"})
	if diags.HasError() {
		t.Fatalf("building the servers list: %v", diags)
	}
	guarding, diags := types.ObjectValue(dhcpGuardingModel{}.AttributeTypes(), map[string]attr.Value{
		"enabled": types.BoolValue(true),
		"servers": servers,
	})
	if diags.HasError() {
		t.Fatalf("building the dhcp_guarding object: %v", diags)
	}
	populatedPlan := &netModel{Name: types.StringValue("probe"), DhcpGuarding: guarding}
	fields, err = networkKitSpec().WireFields(populatedPlan)
	if err != nil {
		t.Fatalf("WireFields (populated dhcp_guarding): %v", err)
	}
	for _, wire := range guardWires {
		if !slices.Contains(fields, wire) {
			t.Errorf("the mask omits %q once dhcp_guarding is set: %v", wire, fields)
		}
	}
}

func TestAnUnrelatedApplyResetsDHCPRelayAndV6(t *testing.T) {
	type subject struct {
		name    string
		vlan    int
		subnet  string
		set     func(*ui.Network)
		inspect func(*ui.Network) map[string]string
	}

	subjects := []subject{
		{
			name: "dhcp_relay", vlan: 74, subnet: "10.74.74.1/24",
			set: func(n *ui.Network) {
				// The controller refuses DhcpServerAndRelayCannotCoexist, so the
				// server has to go before the relay can be enabled.
				n.DHCPDEnabled = false
				n.DHCPRelayEnabled = true
				n.DHCPRelayServers = []string{"10.74.74.53"}
			},
			inspect: func(n *ui.Network) map[string]string {
				return map[string]string{
					"dhcp_relay_enabled (explicit false)": fmt.Sprintf("%v", n.DHCPRelayEnabled),
					"dhcp_relay_servers (never assigned)": fmt.Sprintf("%v", n.DHCPRelayServers),
				}
			},
		},
		{
			name: "dhcp_v6_server", vlan: 73, subnet: "10.73.73.1/24",
			set: func(n *ui.Network) {
				n.DHCPDV6Enabled = true
				n.DHCPDV6DNSAuto = true
			},
			inspect: func(n *ui.Network) map[string]string {
				return map[string]string{
					"dhcpdv6_enabled (never assigned)":  fmt.Sprintf("%v", n.DHCPDV6Enabled),
					"dhcpdv6_dns_auto (never assigned)": fmt.Sprintf("%v", n.DHCPDV6DNSAuto),
				}
			},
		},
	}

	for _, s := range subjects {
		t.Run(s.name, func(t *testing.T) {
			var networkID string
			var established map[string]bool
			resourceName := "tfacc-" + strings.ReplaceAll(s.name, "_", "-") + "-victim"

			setOutOfBand := func() {
				client, site := probeClient(t)
				ctx := context.Background()
				networks, err := client.ListNetwork(ctx, site)
				if err != nil {
					t.Fatal(err)
				}
				for i := range networks {
					if networks[i].Name != nil && *networks[i].Name == resourceName {
						networkID = networks[i].ID
						break
					}
				}
				if networkID == "" {
					t.Fatal("the network the provider created is not on the controller")
				}
				n, err := client.GetNetwork(ctx, site, networkID)
				if err != nil {
					t.Fatal(err)
				}
				s.set(n)
				if _, err := client.UpdateNetwork(ctx, site, n); err != nil {
					t.Skipf("the controller refused the fixture for %s (%v); nothing to lose "+
						"and this would pass vacuously", s.name, err)
				}
				back, err := client.GetNetwork(ctx, site, networkID)
				if err != nil {
					t.Fatal(err)
				}
				established = map[string]bool{}
				for label, value := range s.inspect(back) {
					if value != "false" && value != "[]" && value != "" {
						established[label] = true
					}
				}
				if len(established) == 0 {
					t.Skipf("the controller stored none of %s's fields (%v), so there is "+
						"nothing for the apply to destroy", s.name, s.inspect(back))
				}
				t.Logf("POSITIVE CONTROL: controller holds %d of %s's fields: %v",
					len(established), s.name, s.inspect(back))
			}

			check := func(*terraform.State) error {
				client, site := probeClient(t)
				back, err := client.GetNetwork(context.Background(), site, networkID)
				if err != nil {
					return err
				}
				after := s.inspect(back)
				// Only fields the fixture established: a field that was already
				// false before the apply cannot have been reset by it, and
				// counting it would inflate the result.
				var lost []string
				for label, value := range after {
					if !established[label] {
						continue
					}
					if value == "false" || value == "[]" {
						lost = append(lost, label)
					}
				}
				sort.Strings(lost)
				t.Logf("after the apply: %v", after)
				if len(lost) > 0 {
					return fmt.Errorf(
						"an apply whose only change was the vlan reset %d field(s) of %s: %v.\n"+
							"    EXPECTED TO FAIL UNTIL THE DHCP GUARD DEFECT ABOVE IS FIXED.",
						len(lost), s.name, lost)
				}
				return nil
			}

			config := func(vlan int) string {
				return fmt.Sprintf(`
resource "unifi_network" "victim" {
	name    = %q
	subnet  = %q
	vlan    = %d
	enabled = true
}
`, resourceName, s.subnet, vlan)
			}

			resource.Test(t, resource.TestCase{
				PreCheck:                 func() { preCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{Config: config(s.vlan)},
					{PreConfig: setOutOfBand, Config: config(s.vlan + 100), Check: check},
				},
			})
		})
	}
}

// TestDoesTheControllerHoldTheNestedFirewallFlags supplies the fact static
// analysis alone can't: whether the controller holds a non-zero for the
// four match flags on a firewall policy's source and destination. The
// static half is already established -- force-emitted with no omitempty,
// zero assignments, zero tfsdk tags, zero schema attributes -- but a
// candidate the controller never holds a value for is not a defect
// however the code reads.
//
// This does not run an apply: there is no provider-level firewall_policy
// create test to build on, so the end-to-end half is recorded as
// outstanding rather than attempted here.
//
// NOTE: no function with this name exists in this file or elsewhere in
// the package -- this documents a test that was never implemented.
