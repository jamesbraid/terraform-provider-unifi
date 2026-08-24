package unifi

import (
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// The control that makes the rest mean anything: the same fields set on a
// network whose purpose does carry them must produce nothing, or a check
// that warned about everything would satisfy every must-warn case below.
func TestNothingIsReportedWhenThePurposeCarriesTheFields(t *testing.T) {
	start := "10.0.0.100"
	network := &ui.Network{
		Purpose:      ui.PurposeCorporate,
		DHCPDDNS1:    "10.0.0.1",
		DHCPDStart:   &start,
		DHCPDEnabled: true,
	}
	if diags := droppedOnWrite("corporate network", network); len(diags) != 0 {
		t.Fatalf("a corporate network reported %d dropped field(s), so every case below "+
			"would pass for a check that warns unconditionally: %v", len(diags), diags)
	}
}

// The headline case. A vlan-only network carries almost no DHCP settings, and
// today the apply succeeds having sent none of them.
func TestVLANOnlyReportsTheDHCPFieldsItDiscards(t *testing.T) {
	start := "10.0.0.100"
	lease := int64(86400)
	network := &ui.Network{
		Purpose:        ui.PurposeVLANOnly,
		DHCPDDNS1:      "10.0.0.1",
		DHCPDStart:     &start,
		DHCPDLeaseTime: &lease,
		DHCPDEnabled:   true,
	}
	diags := droppedOnWrite("vlan-only network", network)
	if len(diags) != 4 {
		t.Fatalf("want one warning per dropped field (4), got %d: %v", len(diags), diags)
	}
	joined := strings.Join(func() []string {
		var out []string
		for _, d := range diags {
			out = append(out, d.Detail())
		}
		return out
	}(), "\n")
	for _, name := range []string{"dhcpd_dns_1", "dhcpd_start", "dhcpd_leasetime", "dhcpd_enabled"} {
		if !strings.Contains(joined, name) {
			t.Errorf("no warning names %q; a practitioner cannot act on a warning that does "+
				"not say which attribute: %s", name, joined)
		}
	}
	for _, d := range diags {
		if d.Severity().String() != "Warning" {
			t.Errorf("severity is %s; an error would fail an apply that succeeds today",
				d.Severity())
		}
		if !strings.Contains(d.Detail(), "vlan-only network") {
			t.Errorf("the warning does not say which kind of network refuses it: %s", d.Detail())
		}
	}
}

// Only what was set: the whole scoping decision rests on this. An
// attribute the practitioner never mentioned is a zero field here, and
// warning about it would put 44 unactionable warnings on every
// vlan-only plan.
func TestAnUnsetFieldIsNotReported(t *testing.T) {
	network := &ui.Network{Purpose: ui.PurposeVLANOnly, DHCPDDNS1: "10.0.0.1"}
	diags := droppedOnWrite("vlan-only network", network)
	if len(diags) != 1 {
		t.Fatalf("want exactly the one set field reported, got %d: %v", len(diags), diags)
	}
	if !strings.Contains(diags[0].Detail(), "dhcpd_dns_1") {
		t.Errorf("the wrong field is named: %s", diags[0].Detail())
	}
}

// The limit of the method, named rather than left to be discovered:
// ipsec_pfs on a site-vpn is dropped when false, because the encoder tags
// it omitempty where the struct does not. This helper cannot report that
// -- by the time a value reaches the SDK struct, "the practitioner set
// false" and "the practitioner said nothing" are the same zero. That's
// why site_to_site_vpn's descriptor suppresses that write directly
// instead of relying on a warning.
func TestAZeroFieldIsNotReportedEvenWhereTheEncoderWouldDropIt(t *testing.T) {
	network := &ui.Network{Purpose: ui.PurposeSiteVPN, IPSecPfs: true}
	if diags := droppedOnWrite("site-to-site VPN", network); len(diags) != 0 {
		t.Fatalf("pfs=true is carried and must not warn: %v", diags)
	}
	// pfs=false is a zero field, so it's correctly not reported here.
	network = &ui.Network{Purpose: ui.PurposeSiteVPN, IPSecPfs: false}
	if diags := droppedOnWrite("site-to-site VPN", network); len(diags) != 0 {
		t.Fatalf("a zero field must not be reported, or every unset attribute warns: %v", diags)
	}
}

// An object the encoder refuses entirely reports nothing rather than
// everything: a Network with no Purpose fails to marshal, and the write itself
// will say so far more clearly than 273 warnings would.
func TestAnUnencodableObjectIsSilent(t *testing.T) {
	if diags := droppedOnWrite("network", &ui.Network{DHCPDDNS1: "10.0.0.1"}); len(diags) != 0 {
		t.Fatalf("an unencodable object produced %d warning(s): %v", len(diags), diags)
	}
}
