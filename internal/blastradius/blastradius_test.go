package blastradius

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// The blast radius, pinned by name.
//
// Sixty-two attributes across four surfaces can be set in a configuration and
// never reach the controller. The figures were first measured with a different
// instrument -- one that parsed the alias structs out of go-unifi's source
// rather than marshalling a Network -- and this package reproduces all fourteen
// of its numbers, seven numerators and seven denominators. Two instruments
// disagreeing would mean one of them is wrong; two agreeing on fourteen numbers
// is why these are pinned rather than merely logged.
//
// NAMES, NOT A COUNT. A baseline that pinned 62 alone would pass a release in
// which one attribute started reaching the controller and another stopped. The
// point of pinning is to make both halves of that trade visible, so the sets
// are compared element by element and a passing test means nothing moved.
//
// WHEN THIS FAILS, IT IS NEWS EITHER WAY. A drop that disappears is go-unifi
// having gained a field, which is the fix this measurement exists to prompt --
// update the entry and say so in the commit. A drop that appears is a
// regression in an encoder nothing else guards.
var baseline = []struct {
	policy       string
	purpose      string
	resource     string
	attributes   int // managed attributes whose API field the policy resolves
	emitted      int // JSON field names this purpose's encoder sends
	dropped      []string
	unmeasurable []string
}{
	{
		policy:     "network",
		purpose:    "corporate",
		resource:   "unifi_network",
		attributes: 51,
		emitted:    99,
		dropped: []string{
			"nat_outbound_ip_addresses.ip_address",
			"nat_outbound_ip_addresses.ip_address_pool",
			"nat_outbound_ip_addresses.mode",
			"nat_outbound_ip_addresses.wan_network_group",
		},
		unmeasurable: []string{
			"dhcp_guarding.servers",
			"dhcp_server.boot",
			"dhcp_server.dns_servers",
			"dhcp_server.wins",
			"dhcp_v6_server.dns_servers",
			"purpose",
			"third_party_gateway",
			"vlan",
		},
	},
	{
		policy:     "network",
		purpose:    "guest",
		resource:   "unifi_network",
		attributes: 51,
		emitted:    97,
		dropped: []string{
			"nat_outbound_ip_addresses.ip_address",
			"nat_outbound_ip_addresses.ip_address_pool",
			"nat_outbound_ip_addresses.mode",
			"nat_outbound_ip_addresses.wan_network_group",
		},
		unmeasurable: []string{
			"dhcp_guarding.servers",
			"dhcp_server.boot",
			"dhcp_server.dns_servers",
			"dhcp_server.wins",
			"dhcp_v6_server.dns_servers",
			"purpose",
			"third_party_gateway",
			"vlan",
		},
	},
	{
		policy:     "network",
		purpose:    "vlan-only",
		resource:   "unifi_network",
		attributes: 51,
		emitted:    22,
		dropped: []string{
			"auto_scale",
			"dhcp_relay.enabled",
			"dhcp_relay.servers",
			"dhcp_server.conflict_checking",
			"dhcp_server.dns_enabled",
			"dhcp_server.enabled",
			"dhcp_server.gateway_enabled",
			"dhcp_server.leasetime",
			"dhcp_server.ntp_enabled",
			"dhcp_server.start",
			"dhcp_server.stop",
			"dhcp_server.tftp_server",
			"dhcp_server.time_offset_enabled",
			"dhcp_server.unifi_controller",
			"dhcp_server.wpad_url",
			"dhcp_v6_server.dns_auto",
			"dhcp_v6_server.enabled",
			"dhcp_v6_server.lease",
			"dhcp_v6_server.start",
			"dhcp_v6_server.stop",
			"domain_name",
			"gateway_type",
			"internet_access",
			"ip_aliases",
			"ipv6_client_address_assignment",
			"ipv6_interface_type",
			"ipv6_pd_auto_prefixid_enabled",
			"ipv6_pd_interface",
			"ipv6_pd_prefixid",
			"ipv6_pd_start",
			"ipv6_pd_stop",
			"ipv6_ra",
			"ipv6_ra_preferred_lifetime",
			"ipv6_ra_priority",
			"ipv6_ra_valid_lifetime",
			"ipv6_static_subnet",
			"lte_lan",
			"nat_outbound_ip_addresses",
			"nat_outbound_ip_addresses.ip_address",
			"nat_outbound_ip_addresses.ip_address_pool",
			"nat_outbound_ip_addresses.mode",
			"nat_outbound_ip_addresses.wan_network_group",
			"setting_preference",
			"subnet",
		},
		unmeasurable: []string{
			"dhcp_guarding.servers",
			"dhcp_server.boot",
			"dhcp_server.dns_servers",
			"dhcp_server.wins",
			"dhcp_v6_server.dns_servers",
			"purpose",
			"third_party_gateway",
			"vlan",
		},
	},
	{
		policy:     "wan",
		purpose:    "wan",
		resource:   "unifi_wan",
		attributes: 50,
		emitted:    65,
		dropped: []string{
			"dhcp.options.option_number",
			"dhcp.options.value",
			"dhcpv6.options.option_number",
			"dhcpv6.options.value",
			"ip_aliases",
			"mac_override_enabled",
			"provider_capabilities.download_kilobits_per_second",
			"provider_capabilities.upload_kilobits_per_second",
			"single_network_lan",
		},
		unmeasurable: nil,
	},
	{
		policy:     "vpn_server",
		purpose:    "remote-user-vpn",
		resource:   "unifi_vpn_server",
		attributes: 20,
		emitted:    48,
		dropped: []string{
			"wireguard.public_key",
		},
		unmeasurable: []string{
			"dns.servers",
			"openvpn.port",
			"wan.interface",
			"wan.ip",
			"wireguard.port",
		},
	},
	{
		policy:     "vpn_client",
		purpose:    "vpn-client",
		resource:   "unifi_vpn_client",
		attributes: 10,
		emitted:    26,
		dropped:    nil,
		unmeasurable: []string{
			"wireguard.configuration",
			"wireguard.dns_servers",
			"wireguard.peer",
		},
	},
	{
		policy:       "site_to_site_vpn",
		purpose:      "site-vpn",
		resource:     "unifi_site_to_site_vpn",
		attributes:   21,
		emitted:      34,
		dropped:      nil,
		unmeasurable: nil,
	},
}

func policyFor(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../provider-codegen/policy/" + name + ".json")
	if err != nil {
		t.Fatalf("read policy %s: %v", name, err)
	}
	return raw
}

func TestTheBlastRadiusIsStillSixtyTwoAttributes(t *testing.T) {
	total := 0
	for _, want := range baseline {
		t.Run(want.resource+"/"+want.purpose, func(t *testing.T) {
			resource, attrs, err := Attributes(policyFor(t, want.policy))
			if err != nil {
				t.Fatalf("attributes: %v", err)
			}
			if resource != want.resource {
				t.Fatalf("policy %s names %s, baseline says %s", want.policy, resource, want.resource)
			}
			emitted, err := Emitted(want.purpose)
			if err != nil {
				t.Fatalf("emitted: %v", err)
			}
			if len(emitted) != want.emitted {
				t.Errorf("the %s encoder now sends %d fields, baseline says %d -- go-unifi changed, so the dropped set below moved with it",
					want.purpose, len(emitted), want.emitted)
			}
			unmeasurable := Unmeasurable(attrs)
			if got := len(attrs) - len(unmeasurable); got != want.attributes {
				t.Errorf("%s exposes %d resolvable managed attributes, baseline says %d", resource, got, want.attributes)
			}
			compare(t, "dropped on write", want.dropped, Dropped(attrs, emitted))
			compare(t, "invisible to this method", want.unmeasurable, unmeasurable)
		})
		total += len(want.dropped)
	}
	if total != 62 {
		t.Errorf("the baseline now pins %d dropped attributes, not 62", total)
	}
}

// compare reports the two directions separately, because they are different
// news: an attribute that stopped being dropped is go-unifi having gained a
// field, and one that started is a regression.
func compare(t *testing.T, subject string, want, got []string) {
	t.Helper()
	for _, name := range want {
		if !slices.Contains(got, name) {
			t.Errorf("%s: %s no longer is -- if go-unifi gained the field, drop it from the baseline and say so", subject, name)
		}
	}
	for _, name := range got {
		if !slices.Contains(want, name) {
			t.Errorf("%s: %s now is, and was not in the baseline", subject, name)
		}
	}
}

// A ZERO HAS TO BE A MEASUREMENT. unifi_vpn_client and unifi_site_to_site_vpn
// both report nothing dropped, which is exactly what a broken instrument
// reports. Running their own attributes against an encoder that is known to
// send almost nothing has to produce a large number, or the two zeros above are
// worth nothing.
func TestNothingDroppedIsAFindingNotASilentInstrument(t *testing.T) {
	starved, err := Emitted(ui.PurposeVLANOnly)
	if err != nil {
		t.Fatalf("emitted: %v", err)
	}
	for _, policy := range []string{"vpn_client", "site_to_site_vpn"} {
		resource, attrs, err := Attributes(policyFor(t, policy))
		if err != nil {
			t.Fatalf("attributes: %v", err)
		}
		resolvable := len(attrs) - len(Unmeasurable(attrs))
		dropped := Dropped(attrs, starved)
		if len(dropped) < resolvable/2 {
			t.Errorf("%s measured against the vlan-only encoder drops only %d of %d -- the instrument cannot see drops on this surface, so its zero says nothing",
				resource, len(dropped), resolvable)
		}
	}
}

// FULL POPULATION IS WHAT MAKES omitempty VISIBLE. Every alias struct tags most
// of its fields omitempty, so a Network left at its zero value emits almost
// nothing and every attribute would read as dropped. This is the control on
// fill: the same purpose, the same encoder, one populated object and one empty
// one, and _id present only in the first.
func TestAnEmptyNetworkWouldMakeEveryAttributeLookDropped(t *testing.T) {
	populated, err := Emitted(ui.PurposeVLANOnly)
	if err != nil {
		t.Fatalf("emitted: %v", err)
	}
	if !slices.Contains(populated, "_id") {
		t.Fatal("_id is tagged omitempty and should be present once the object is populated")
	}

	empty := ui.Network{Purpose: ui.PurposeVLANOnly}
	raw, err := json.Marshal(&empty)
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	var sent map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatalf("reread: %v", err)
	}
	if _, present := sent["_id"]; present {
		t.Error("an empty Network emitted _id, so omitempty is not doing what this control assumes")
	}
	if len(sent) >= len(populated) {
		t.Errorf("an empty Network emitted %d fields against a populated one's %d -- fill is not filling anything", len(sent), len(populated))
	}
}

// A CONTAINER AND ITS MEMBERS ARE BOTH ATTRIBUTES, and collapsing them would
// quietly shrink the headline from 44 to 41. Anyone who reads the walk as
// double-counting should read this first.
func TestANestedListCountsAsItselfAndAsItsMembers(t *testing.T) {
	_, attrs, err := Attributes(policyFor(t, "network"))
	if err != nil {
		t.Fatalf("attributes: %v", err)
	}
	var paths []string
	for _, attr := range attrs {
		paths = append(paths, attr.Path)
	}
	for _, want := range []string{
		"nat_outbound_ip_addresses",
		"nat_outbound_ip_addresses.mode",
		"nat_outbound_ip_addresses.wan_network_group",
	} {
		if !slices.Contains(paths, want) {
			t.Errorf("%s is not among the attributes; the walk stopped at the container or skipped it", want)
		}
	}
	for _, attr := range attrs {
		if attr.Path == "nat_outbound_ip_addresses.mode" && attr.Wire != "mode" {
			t.Errorf("a member resolved to %q; members resolve to their own API name, not the container's", attr.Wire)
		}
	}
}

// THE COUNT IS A FLOOR. An attribute the policy hand-maps across several API
// fields has no single name to look for, so this method cannot say whether it
// arrives. Reporting it as delivered would be a guess in the direction that
// makes the number look better, so it is excluded and counted separately --
// which is why the 62 excludes the dhcpd_dns_3 and dhcpd_dns_4 defect that
// prompted the measurement in the first place.
func TestAHandMappedAttributeIsExcludedAndCounted(t *testing.T) {
	attrs := []Attribute{
		{Path: "dns.servers", Wire: ""},
		{Path: "name", Wire: "name"},
		{Path: "gone", Wire: "not_sent"},
	}
	dropped := Dropped(attrs, []string{"name"})
	if !slices.Equal(dropped, []string{"gone"}) {
		t.Errorf("dropped = %v, want [gone] -- the hand-mapped attribute must not be reported either way", dropped)
	}
	if blind := Unmeasurable(attrs); !slices.Equal(blind, []string{"dns.servers"}) {
		t.Errorf("unmeasurable = %v, want [dns.servers]", blind)
	}
}

func TestAPolicyThatCannotBeReadIsAFailureNotAnEmptyMeasurement(t *testing.T) {
	if _, _, err := Attributes([]byte("{")); err == nil {
		t.Error("unparseable policy returned no error")
	}
	if _, _, err := Attributes([]byte(`{"fields":[]}`)); err == nil {
		t.Error("a policy naming no resource returned no error")
	}
	if _, err := Emitted("not-a-purpose"); err == nil {
		t.Error("an unknown purpose returned no error; the encoder rejects it and so should this")
	}
}

func TestEveryPurposeEncodes(t *testing.T) {
	if len(Purposes) != 7 {
		t.Fatalf("Purposes lists %d purposes, go-unifi switches on 7", len(Purposes))
	}
	for _, purpose := range Purposes {
		emitted, err := Emitted(purpose)
		if err != nil {
			t.Errorf("%s: %v", purpose, err)
			continue
		}
		if len(emitted) == 0 {
			t.Errorf("%s emitted nothing", purpose)
		}
		if !slices.Contains(emitted, "purpose") {
			t.Errorf("%s emitted no purpose field", purpose)
		}
	}
}
