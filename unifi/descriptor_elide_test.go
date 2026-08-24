package unifi

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_ap_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_ap_group"
	resource_client "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_client"
	resource_client_qos_rate "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_client_qos_rate"
	resource_device "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_device"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dns_record"
	resource_firewall_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_group"
	resource_firewall_policy "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_policy"
	resource_firewall_rule "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_rule"
	resource_firewall_zone "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_zone"
	resource_network "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_network"
	resource_port_forward "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_port_forward"
	resource_port_profile "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_port_profile"
	resource_radius_profile "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_radius_profile"
	resource_radius_user "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_radius_user"
	resource_site_to_site_vpn "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_site_to_site_vpn"
	resource_static_route "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_static_route"
	resource_traffic_route "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_traffic_route"
	resource_vpn_client "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_vpn_client"
	resource_vpn_server "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_vpn_server"
	resource_wlan "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wlan"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// Every descriptor must agree with the two things it was transcribed from: the
// generated schema, for what a zero from the API means, and the SDK struct, for
// what each field is called on the wire.
//
// This exists because the value was unverified: flipping all seven of
// dns_record's left the whole package green, so a descriptor generated across
// every surface could carry the wrong value everywhere and nothing would say
// so. resourcekit's own tests prove the check goes red on that mutation
// against a probe; the cases here apply it to the descriptors we ship.
func TestEveryDescriptorAgreesWithItsSchemaAndItsSDK(t *testing.T) {
	ctx := context.Background()

	checks := map[string]func(*testing.T){
		"firewall_zone": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(firewallZoneKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				firewallZoneKitSpec(), resource_firewall_zone.FirewallZoneResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		// traffic_route's two ScatteredObjectFields are the reason WireNameProblems
		// matters more here than anywhere else: destination puts FIVE names on
		// the mask and one of them, matching_target, is spelled after nothing in
		// the model. A typo there drops the discriminator and the route matches
		// on whatever it matched on before.
		"traffic_route": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(trafficRouteKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				trafficRouteKitSpec(), resource_traffic_route.TrafficRouteResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"vpn_server": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(vpnServerKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				vpnServerKitSpec(), resource_vpn_server.VpnServerResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"firewall_group": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(firewallGroupKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				firewallGroupKitSpec(), resource_firewall_group.FirewallGroupResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"client_qos_rate": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(clientQosRateKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				clientQosRateKitSpec(), resource_client_qos_rate.ClientQosRateResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"firewall_rule": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(firewallRuleKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				firewallRuleKitSpec(), resource_firewall_rule.FirewallRuleResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"port_profile": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(portProfileKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				portProfileKitSpec(), resource_port_profile.PortProfileResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"radius_user": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(radiusUserKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				radiusUserKitSpec(), resource_radius_user.RadiusUserResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"site_to_site_vpn": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(siteToSiteVPNKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				siteToSiteVPNKitSpec(), resource_site_to_site_vpn.SiteToSiteVpnResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"wlan": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(wlanKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				wlanKitSpec(), resource_wlan.WlanResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"port_forward": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(portForwardKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				portForwardKitSpec(), resource_port_forward.PortForwardResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"vpn_client": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(vpnClientKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				vpnClientKitSpec(), resource_vpn_client.VpnClientResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"static_route": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(staticRouteKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				staticRouteKitSpec(), resource_static_route.StaticRouteResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"firewall_policy": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(firewallPolicyKitSpec()) {
				t.Error(problem)
			}
			for _, problem := range resourcekit.NestedProblems(firewallPolicyKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				firewallPolicyKitSpec(),
				resource_firewall_policy.FirewallPolicyResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"radius_profile": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(radiusProfileKitSpec()) {
				t.Error(problem)
			}
			for _, problem := range resourcekit.NestedProblems(radiusProfileKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				radiusProfileKitSpec(), resource_radius_profile.RadiusProfileResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"ap_group": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(apGroupKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				apGroupKitSpec(), resource_ap_group.ApGroupResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"device": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(deviceKitSpec()) {
				t.Error(problem)
			}
			for _, problem := range resourcekit.NestedProblems(deviceKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				deviceKitSpec(), resource_device.DeviceResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"network": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(networkKitSpec()) {
				t.Error(problem)
			}
			for _, problem := range resourcekit.NestedProblems(networkKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				networkKitSpec(), resource_network.NetworkResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"client": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(clientKitSpec()) {
				t.Error(problem)
			}
			for _, problem := range resourcekit.NestedProblems(clientKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				clientKitSpec(), resource_client.ClientResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"dns_record": func(t *testing.T) {
			for _, problem := range resourcekit.WireNameProblems(dnsRecordKitSpec()) {
				t.Error(problem)
			}
			problems := resourcekit.ElideProblems(
				dnsRecordKitSpec(), resource_dns_record.DnsRecordResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
	}

	// The table must cover every kit surface: refusing an empty table isn't
	// enough, since a table missing one entry still passes (the named
	// surfaces check out, the count is non-zero, and the missing one reads
	// exactly like a surface with no problems) -- the same blind spot this
	// check exists to remove, one level up. So the set is compared against
	// the surfaces the provider actually serves from the kit, derived by
	// the same AST walk the policy check uses rather than a second
	// hand-kept list.
	// kitServedSurfaces is keyed by Terraform type name, which is what the
	// table's own keys are once the provider prefix comes off.
	want := map[string]bool{}
	for typeName := range kitServedSurfaces(t) {
		want[strings.TrimPrefix(typeName, "unifi_")] = true
	}

	var missing, extra []string
	for name := range want {
		if _, ok := checks[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range checks {
		if !want[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("%v are served from the kit and no case checks their Elide values; "+
			"the descriptor ships unverified in exactly the dimension this test exists for", missing)
	}
	if len(extra) > 0 {
		t.Errorf("%v are checked here but no longer served from the kit; "+
			"a case for a surface that is gone reads as coverage and is none", extra)
	}
	if len(checks) == 0 {
		t.Fatal("no descriptors are being checked; this test cannot fail and is worthless")
	}
	for name, check := range checks {
		t.Run(name, check)
	}
}

// TestTheShippedDescriptorMutatesToExactlyItsFieldCount is the end-to-end
// control, and it asserts an exact number rather than "at least one": a
// wrong Elide value already in the shipped descriptor would be "corrected"
// by the flip, cancelling one of the control's own detections and yielding
// a plausible-looking but wrong count. Asserting the exact field count is
// what makes that impossible to hide: if a future edit leaves one field
// correct under mutation, this fails rather than quietly reporting one
// fewer.
func TestTheShippedDescriptorMutatesToExactlyItsFieldCount(t *testing.T) {
	ctx := context.Background()
	spec := dnsRecordKitSpec()

	flipped := spec
	flipped.Fields = nil
	elidable := 0
	for _, field := range spec.Fields {
		value := reflect.ValueOf(field)
		if !value.FieldByName("Elide").IsValid() {
			flipped.Fields = append(flipped.Fields, field) // BoolField: nothing to flip
			continue
		}
		elidable++
		mutated := reflect.New(value.Type()).Elem()
		mutated.Set(value)
		elide := mutated.FieldByName("Elide")
		elide.SetBool(!elide.Bool())
		// Checked, because a forced assertion here would report "interface
		// conversion" and leave the reader to work out which field kind
		// stopped satisfying Field -- which is the thing this control would
		// be telling them about.
		field, ok := mutated.Interface().(resourcekit.Field[dnsRecordKitModel, ui.DNSRecord])
		if !ok {
			t.Fatalf("a mutated %T no longer satisfies Field, so the flipped descriptor "+
				"cannot be built", mutated.Interface())
		}
		flipped.Fields = append(flipped.Fields, field)
	}

	if elidable == 0 {
		t.Fatal("no field carries an Elide, so this control cannot fail")
	}
	problems := resourcekit.ElideProblems(flipped, resource_dns_record.DnsRecordResourceSchema(ctx))
	if len(problems) != elidable {
		t.Fatalf("flipping every Elide produced %d problem(s) for %d elidable field(s); "+
			"a shortfall means a value was already wrong and the mutation corrected it: %v",
			len(problems), elidable, problems)
	}
}
