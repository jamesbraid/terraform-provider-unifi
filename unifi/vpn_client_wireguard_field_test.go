package unifi

import (
	"context"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

func wireguardSpec(wires []string) resourcekit.Spec[vpnClientResourceModel, ui.Network] {
	field := vpnClientWireguardField()
	field.Wires = wires
	return resourcekit.Spec[vpnClientResourceModel, ui.Network]{
		TypeName: "unifi_vpn_client",
		Fields:   []resourcekit.Field[vpnClientResourceModel, ui.Network]{field},
	}
}

func wireguardPlan(t *testing.T) *vpnClientResourceModel {
	t.Helper()
	dns, diags := types.ListValueFrom(context.Background(), types.StringType,
		[]string{"10.0.0.1", "10.0.0.2"})
	if diags.HasError() {
		t.Fatal(diags)
	}
	value := wireguardModel{
		PrivateKey:          types.StringValue("privkey"),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                types.ObjectNull(wireguardPeerModel{}.AttributeTypes()),
		PresharedKeyEnabled: types.BoolValue(false),
		PresharedKey:        types.StringNull(),
		Interface:           types.StringValue("wan"),
		DnsServers:          dns,
	}
	object, d := types.ObjectValueFrom(context.Background(), value.AttributeTypes(), value)
	if d.HasError() {
		t.Fatal(d)
	}
	return &vpnClientResourceModel{Wireguard: object}
}

// THE PROPERTY THE TICKET ASKED FOR: do the names this field contributes match
// the tags on the real unifi.Network? Against a synthetic struct a wrong name is
// a wrong name in a fixture; against the SDK it is the one a descriptor author
// would actually have written.
func TestWireguardFieldNamesAreAttributesOfNetwork(t *testing.T) {
	if problems := resourcekit.WireNameProblems(wireguardSpec(vpnClientWireguardWires())); len(problems) != 0 {
		t.Errorf("the wireguard field names attributes unifi.Network does not have:\n  %v", problems)
	}

	// THE TRAP, as a control. The Go field is WireguardPrivateKey, so a name
	// transcribed from the struct rather than the tag is wireguard_private_key.
	// Without this the assertion above passes against a check that approves
	// anything.
	guessed := slices.Clone(vpnClientWireguardWires())
	guessed[slices.Index(guessed, "x_wireguard_private_key")] = "wireguard_private_key"
	problems := resourcekit.WireNameProblems(wireguardSpec(guessed))
	if len(problems) != 1 {
		t.Fatalf("wireguard_private_key produced %d problem(s), want 1: %v", len(problems), problems)
	}
}

// Every name travels together or the write is partial. Ten is the whole point:
// the eight wireguard_-prefixed ones are findable by grep, and dhcpd_dns_1 and
// dhcpd_dns_2 are not.
func TestWireguardFieldPutsAllTenNamesInTheMask(t *testing.T) {
	fields, err := wireguardSpec(vpnClientWireguardWires()).WireFields(wireguardPlan(t))
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	if len(fields) != 10 {
		t.Errorf("the mask carries %d name(s), want 10: %v", len(fields), fields)
	}
	for _, name := range []string{"dhcpd_dns_1", "dhcpd_dns_2", "x_wireguard_private_key"} {
		if !slices.Contains(fields, name) {
			t.Errorf("%s is missing from the mask, so the value is written and never sent", name)
		}
	}
}

// THE DEFECT THE KIND PREVENTS, DEMONSTRATED RATHER THAN DESCRIBED. An author
// who enumerates the wires by grepping the SDK for "Wireguard" gets eight. The
// object still WRITES the two dns fields -- Encode is unchanged -- so the values
// land on the struct, the mask omits them, and the apply succeeds having sent
// neither.
func TestWireguardFieldWithTheDNSNamesMissingWritesThemAndCannotSendThem(t *testing.T) {
	ctx := context.Background()
	incomplete := slices.DeleteFunc(slices.Clone(vpnClientWireguardWires()), func(name string) bool {
		return name == "dhcpd_dns_1" || name == "dhcpd_dns_2"
	})

	network := &ui.Network{}
	if diags := encodeVPNClientWireguard(ctx, wireguardPlan(t).Wireguard, network); diags.HasError() {
		t.Fatalf("Encode: %v", diags)
	}
	// CONTROL: the value really is written, or the absence below proves nothing.
	if network.DHCPDDNS1 != "10.0.0.1" {
		t.Fatalf("Encode did not write dhcpd_dns_1 (%q), so its absence from the mask "+
			"is not a demonstration of anything", network.DHCPDDNS1)
	}

	fields, err := wireguardSpec(incomplete).WireFields(wireguardPlan(t))
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	if slices.Contains(fields, "dhcpd_dns_1") {
		t.Fatal("the incomplete list still masked dhcpd_dns_1; the case is not set up")
	}
	t.Logf("Encode wrote dhcpd_dns_1=%q and the mask of %d name(s) does not carry it: "+
		"the apply succeeds and the controller keeps its old DNS",
		network.DHCPDDNS1, len(fields))
}

func TestWireguardFieldRoundTripsWhatTheControllerReturns(t *testing.T) {
	ctx := context.Background()
	field := vpnClientWireguardField()

	network := &ui.Network{}
	model := wireguardPlan(t)
	if diags := field.ToSDK(ctx, model, network); diags.HasError() {
		t.Fatalf("ToSDK: %v", diags)
	}
	// Asserted on the struct fields: a symmetric pair writing everything to one
	// field would round trip and be wrong.
	if network.WireguardPrivateKey == nil || *network.WireguardPrivateKey != "privkey" {
		t.Error("private key did not land")
	}
	if network.WireguardInterface == nil || *network.WireguardInterface != "wan" {
		t.Error("interface did not land")
	}
	if network.DHCPDDNS1 != "10.0.0.1" || network.DHCPDDNS2 != "10.0.0.2" {
		t.Errorf("dns servers landed as %q/%q", network.DHCPDDNS1, network.DHCPDDNS2)
	}

	back := &vpnClientResourceModel{}
	if diags := field.ToModel(ctx, network, back); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	var decoded wireguardModel
	if d := back.Wireguard.As(ctx, &decoded, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatal(d)
	}
	if decoded.Interface.ValueString() != "wan" {
		t.Errorf("interface came back as %q", decoded.Interface.ValueString())
	}
	// THE WRITE-ONLY MEMBERS COME BACK NULL, and that is the finding rather than
	// a defect: the controller never returns them, Decode is given only the SDK
	// object, and carrying them forward is AfterReceive's job.
	if !decoded.PrivateKey.IsNull() {
		t.Errorf("private key came back as %q; the controller does not return it, so a "+
			"non-null here means Decode invented a value", decoded.PrivateKey.ValueString())
	}
}
