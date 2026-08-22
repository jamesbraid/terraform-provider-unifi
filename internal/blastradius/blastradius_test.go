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

// The other two encoder-visible mechanisms, pinned the same way.
//
// THE 62 ABOVE ARE ONE MECHANISM, NOT THE WHOLE DEFECT CLASS. An attribute can
// fail to reach the controller four ways, and only the first is what the 62
// counts:
//
//	encoder drift   the chosen alias struct has no such key -- the 62
//	cannot-clear    the key is there, but omitempty drops the empty value, so
//	                the attribute can be set and never unset
//	force-emitted   the key is there with no omitempty and no attribute behind
//	                it, so every unmasked write sends the Go zero
//	guarded-assign  the provider skips the assignment under a null check --
//	                not visible at the encoder at all, and not measured here
//
// They need different remedies, which is why they are counted apart rather than
// added together.
//
// FORCE-EMITTED IS A CEILING, NOT THE OPEN COUNT. Five surfaces filter their
// writes through a wire-field mask, and a masked write drops every name below
// because none of them is in the mask -- that is what the mask is for. What
// this measures is the encoder, which cannot see the mask, so the exposure that
// remains is whatever write paths do not use one.
//
// NOT EVERY cannotClear ENTRY IS A DEFECT. Enum-constrained and defaulted
// attributes have no meaningful empty value, and required or computed ones are
// not the practitioner's to clear. The measurement is deliberately unfiltered
// so that the judgement stays visible here rather than buried in the walk: of
// the whole set, the free-form remainder is the defect, and site-vpn's
// dynamic_routing and pfs are the sharpest of them -- booleans behind omitempty
// that can be turned on and never off.
var mechanisms = []struct {
	surface      string
	purpose      string
	always       int // keys emitted for a Network nobody touched
	forceEmitted []string
	cannotClear  []string
}{
	{
		surface: "network",
		purpose: "corporate",
		always:  39,
		forceEmitted: []string{
			"dhcpd_mac_1",
			"dhcpd_mac_2",
			"dhcpd_mac_3",
			"igmp_fastleave",
			"igmp_flood_unknown_multicast",
			"igmp_supression", //nolint:misspell // go-unifi spells the wire field this way
			"ipv6_aliases",
			"mac_override_enabled",
			"upnp_lan_enabled",
		},
		cannotClear: []string{
			"dhcp_server.tftp_server",
			"dhcp_server.unifi_controller",
			"dhcp_server.wpad_url",
			"dhcp_v6_server.start",
			"dhcp_v6_server.stop",
			"domain_name",
			"gateway_type",
			"id",
			"ipv6_client_address_assignment",
			"ipv6_interface_type",
			"ipv6_pd_interface",
			"ipv6_pd_start",
			"ipv6_pd_stop",
			"ipv6_ra_priority",
			"ipv6_static_subnet",
			"name",
			"setting_preference",
			"subnet",
		},
	},
	{
		surface: "network",
		purpose: "guest",
		always:  39,
		forceEmitted: []string{
			"dhcpd_mac_1",
			"dhcpd_mac_2",
			"dhcpd_mac_3",
			"igmp_fastleave",
			"igmp_flood_unknown_multicast",
			"igmp_supression", //nolint:misspell // go-unifi spells the wire field this way
			"ipv6_aliases",
			"mac_override_enabled",
			"upnp_lan_enabled",
		},
		cannotClear: []string{
			"dhcp_server.tftp_server",
			"dhcp_server.unifi_controller",
			"dhcp_server.wpad_url",
			"dhcp_v6_server.start",
			"dhcp_v6_server.stop",
			"domain_name",
			"gateway_type",
			"id",
			"ipv6_client_address_assignment",
			"ipv6_interface_type",
			"ipv6_pd_interface",
			"ipv6_pd_start",
			"ipv6_pd_stop",
			"ipv6_ra_priority",
			"ipv6_static_subnet",
			"name",
			"setting_preference",
			"subnet",
		},
	},
	{
		surface: "network",
		purpose: "vlan-only",
		always:  14,
		forceEmitted: []string{
			"dhcpd_mac_1",
			"dhcpd_mac_2",
			"dhcpd_mac_3",
			"networkgroup",
		},
		cannotClear: []string{
			"id",
			"name",
		},
	},
	{
		surface: "wan",
		purpose: "wan",
		always:  18,
		forceEmitted: []string{
			"interface_mtu_enabled",
			"ipv6_enabled",
			"wan_gateway_v6",
			"wan_ip_aliases",
			"wan_ipv6",
			"wan_pppoe_password_enabled",
			"wan_pppoe_username_enabled",
			"wan_username",
			"x_wan_password",
		},
		cannotClear: []string{
			"dhcpv6.options",
			"dhcpv6.wan_delegation_type",
			"dns.ipv6_preference",
			"dns.ipv6_primary",
			"dns.ipv6_secondary",
			"dns.preference",
			"dns.primary",
			"dns.secondary",
			"id",
			"igmp_proxy.downstream",
			"ipv6_setting_preference",
			"load_balance.type",
			"name",
			"networkgroup",
			"setting_preference",
			"type",
			"type_v6",
			"upnp.wan_interface",
			"wan_dslite_remote_host",
		},
	},
	{
		surface: "vpn_server",
		purpose: "remote-user-vpn",
		always:  6,
		forceEmitted: []string{
			"require_mschapv2",
			"vpn_client_configuration_remote_ip_override_enabled",
		},
		cannotClear: []string{
			"id",
			"l2tp.pre_shared_key",
			"name",
			"openvpn.auth_key",
			"openvpn.ca_crt",
			"openvpn.ca_key",
			"openvpn.dh_key",
			"openvpn.encryption_cipher",
			"openvpn.mode",
			"openvpn.server_crt",
			"openvpn.server_key",
			"openvpn.shared_client_crt",
			"openvpn.shared_client_key",
			"radiusprofile_id",
			"subnet",
			"wireguard.private_key",
		},
	},
	{
		surface: "vpn_client",
		purpose: "vpn-client",
		always:  6,
		forceEmitted: []string{
			"dhcpd_dns_enabled",
		},
		cannotClear: []string{
			"id",
			"name",
			"subnet",
			"wireguard.interface",
			"wireguard.preshared_key",
			"wireguard.private_key",
		},
	},
	{
		surface: "site_to_site_vpn",
		purpose: "site-vpn",
		always:  5,
		forceEmitted: []string{
			"ipsec_local_identifier_enabled",
			"ipsec_remote_identifier_enabled",
			"ipsec_separate_ikev2_networks",
		},
		cannotClear: []string{
			"dynamic_routing",
			"esp_encryption",
			"esp_hash",
			"id",
			"ike_encryption",
			"ike_hash",
			"interface",
			"key_exchange",
			"local_ip",
			"name",
			"peer_ip",
			"pfs",
			"pre_shared_key",
			"profile",
			"remote_subnets",
		},
	},
}

func TestTheOtherTwoEncoderMechanisms(t *testing.T) {
	for _, want := range mechanisms {
		t.Run(want.surface+"/"+want.purpose, func(t *testing.T) {
			raw, err := os.ReadFile("../../provider-codegen/generated/" + want.surface + ".mapping.json")
			if err != nil {
				t.Fatalf("read mapping: %v", err)
			}
			managed, touched, err := Ownership(raw)
			if err != nil {
				t.Fatalf("ownership: %v", err)
			}
			always, err := AlwaysEmitted(want.purpose)
			if err != nil {
				t.Fatalf("always emitted: %v", err)
			}
			if len(always) != want.always {
				t.Errorf("%s sends %d fields for an untouched Network, baseline says %d",
					want.purpose, len(always), want.always)
			}
			compare(t, "sent as the Go zero with nothing behind it", want.forceEmitted, ForceEmitted(always, touched))

			stuck, err := CannotClear(want.purpose, managed)
			if err != nil {
				t.Fatalf("cannot clear: %v", err)
			}
			compare(t, "set once and never unset", want.cannotClear, stuck)
		})
	}
}

// DROPPED AND CANNOT-CLEAR MUST NOT OVERLAP, measured rather than read back
// off the pins above. An attribute whose key the encoder never sends has no
// value to clear, so a name in both means one of the two walks is wrong.
//
// ONLY THIS PAIR CAN BE COMPARED. The first version of this test also checked
// the force-emitted set against the other two and could never have failed:
// force-emitted names are API field names and the other two are terraform
// attribute paths, so the comparison was between two vocabularies that do not
// share members. A field that is force-emitted has no attribute at all, which
// is what puts it in that set -- there is nothing to collide with.
func TestNoAttributeIsBothDroppedAndImpossibleToClear(t *testing.T) {
	for _, want := range mechanisms {
		mapped, err := os.ReadFile("../../provider-codegen/generated/" + want.surface + ".mapping.json")
		if err != nil {
			t.Fatalf("read mapping: %v", err)
		}
		managed, _, err := Ownership(mapped)
		if err != nil {
			t.Fatalf("ownership: %v", err)
		}
		_, attrs, err := Attributes(policyFor(t, want.surface))
		if err != nil {
			t.Fatalf("attributes: %v", err)
		}
		emitted, err := Emitted(want.purpose)
		if err != nil {
			t.Fatalf("emitted: %v", err)
		}
		dropped := Dropped(attrs, emitted)
		stuck, err := CannotClear(want.purpose, managed)
		if err != nil {
			t.Fatalf("cannot clear: %v", err)
		}
		for _, name := range stuck {
			if slices.Contains(dropped, name) {
				t.Errorf("%s/%s: %s is reported as never sent AND as impossible to clear",
					want.surface, want.purpose, name)
			}
		}
		if len(stuck) == 0 && len(dropped) == 0 {
			t.Errorf("%s/%s measured nothing under either mechanism, so this proves nothing",
				want.surface, want.purpose)
		}
	}
}

// THE INSTRUMENT HAS TO DISCRIMINATE, not just report. A field that IS sent
// must come out as sent, or a zero from any of the three counts above is a
// broken probe rather than a finding. vlan-only emits twenty-two keys and
// nothing may claim otherwise: enabled is on the wire, owned by an attribute,
// and survives being set to its zero because the alias struct declares it
// without omitempty.
func TestAFieldThatIsSentComesOutAsSent(t *testing.T) {
	raw, err := os.ReadFile("../../provider-codegen/generated/network.mapping.json")
	if err != nil {
		t.Fatalf("read mapping: %v", err)
	}
	managed, touched, err := Ownership(raw)
	if err != nil {
		t.Fatalf("ownership: %v", err)
	}
	if managed["enabled"] == "" {
		t.Fatal("unifi_network does not manage enabled; pick another control")
	}
	always, err := AlwaysEmitted(ui.PurposeVLANOnly)
	if err != nil {
		t.Fatalf("always emitted: %v", err)
	}
	if !slices.Contains(always, "enabled") {
		t.Error("enabled is not emitted for an untouched vlan-only network, so it is not the control this test needs")
	}
	if forced := ForceEmitted(always, touched); slices.Contains(forced, "enabled") {
		t.Error("enabled is owned by an attribute and must not read as force-emitted")
	}
	stuck, err := CannotClear(ui.PurposeVLANOnly, managed)
	if err != nil {
		t.Fatalf("cannot clear: %v", err)
	}
	if slices.Contains(stuck, managed["enabled"]) {
		t.Error("enabled has no omitempty and must not read as impossible to clear")
	}
	emitted, err := Emitted(ui.PurposeVLANOnly)
	if err != nil {
		t.Fatalf("emitted: %v", err)
	}
	if !slices.Contains(emitted, "enabled") {
		t.Error("enabled must read as sent, not dropped")
	}
}

// nilIfEmpty IS WHY THE PROBE SETS AN EMPTY VALUE RATHER THAN READING TAGS.
// marshalVLANOnly declares Name as *string with omitempty and then assigns
// nilIfEmpty(n.Name), so a name the practitioner cleared to "" reaches the
// encoder as a live pointer and leaves it as nothing at all. A struct-tag walk
// sees a pointer field and concludes an empty string would be sent.
func TestAnEmptyStringIsNilledOnItsWayPastTheEncoder(t *testing.T) {
	empty := ""
	network := ui.Network{Purpose: ui.PurposeVLANOnly, Name: &empty}
	raw, err := json.Marshal(&network)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var sent map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatalf("reread: %v", err)
	}
	if _, present := sent["name"]; present {
		t.Error("an empty name reached the wire; nilIfEmpty no longer drops it and cannotClear should shrink")
	}
}
