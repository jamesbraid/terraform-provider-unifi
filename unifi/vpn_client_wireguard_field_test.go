package unifi

import (
	"context"
	"slices"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
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

// Every name Encode writes travels or the write is partial. Ten is the whole
// point: the eight wireguard_-prefixed ones are findable by grep, and
// dhcpd_dns_1 and dhcpd_dns_2 are not.
//
// THE PLAN HAS TO EXERCISE EVERY PATH, and it did not until seven of the ten
// were declared conditional. Encode writes only three of them unconditionally
// -- the private key, the interface and the preshared-key flag -- so a plan
// that sets a wireguard block and nothing else produces a five-name mask, which
// is correct and is not what this test is about. It is about all ten travelling
// when all ten are written.
func TestWireguardFieldPutsAllTenNamesInTheMask(t *testing.T) {
	fields, err := wireguardSpec(vpnClientWireguardWires()).WireFields(wireguardPlanWritingEverything(t))
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
	if diags := encodeVPNClientWireguard(ctx, wireguardPlanWritingEverything(t).Wireguard, network); diags.HasError() {
		t.Fatalf("Encode: %v", diags)
	}
	// CONTROL: the value really is written, or the absence below proves nothing.
	if network.DHCPDDNS1 != "10.0.0.1" {
		t.Fatalf("Encode did not write dhcpd_dns_1 (%q), so its absence from the mask "+
			"is not a demonstration of anything", network.DHCPDDNS1)
	}

	fields, err := wireguardSpec(incomplete).WireFields(wireguardPlanWritingEverything(t))
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

// wireguardPlanWithout builds the same plan with one member cleared, so the
// conditional-wire cases differ from the ten-name case by exactly the thing
// under test.
func wireguardPlanWithoutDNS(t *testing.T) *vpnClientResourceModel {
	t.Helper()
	value := wireguardModel{
		PrivateKey:          types.StringValue("privkey"),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                types.ObjectNull(wireguardPeerModel{}.AttributeTypes()),
		PresharedKeyEnabled: types.BoolValue(false),
		PresharedKey:        types.StringNull(),
		Interface:           types.StringValue("wan"),
		DnsServers:          types.ListNull(types.StringType),
	}
	object, d := types.ObjectValueFrom(context.Background(), value.AttributeTypes(), value)
	if d.HasError() {
		t.Fatal(d)
	}
	return &vpnClientResourceModel{Wireguard: object}
}

// THE TWO DNS NAMES LEAVE THE MASK WHEN NOTHING WILL WRITE THEM, and that is a
// destructive bug rather than an untidy one if they do not.
//
// go-unifi sends a masked field's ZERO when the object carries no value, so a
// mask naming dhcpd_dns_1 on an apply that set a wireguard block WITHOUT
// dns_servers writes an empty string over whatever DNS the controller holds.
// The hand-written mask this field replaces omits exactly these two and
// wire_field_masks_test.go records why, under conditionallyAssigned.
//
// Measured before ConditionalWires existed: both names were in the mask with
// the SDK object carrying "". The descriptor compiled, ElideProblems passed,
// and WireNameProblems passed because both ARE real json tags.
func TestWireguardFieldDropsTheDNSNamesWhenNothingWillWriteThem(t *testing.T) {
	ctx := context.Background()
	plan := wireguardPlanWithoutDNS(t)

	// CONTROL FIRST: Encode really does leave them empty, or their absence from
	// the mask below is protecting nothing.
	network := &ui.Network{}
	if diags := encodeVPNClientWireguard(ctx, plan.Wireguard, network); diags.HasError() {
		t.Fatalf("Encode: %v", diags)
	}
	if network.DHCPDDNS1 != "" || network.DHCPDDNS2 != "" {
		t.Fatalf("Encode wrote dhcpd_dns_1=%q dhcpd_dns_2=%q for a plan with no dns_servers; "+
			"this case is not the one it is named for", network.DHCPDDNS1, network.DHCPDDNS2)
	}

	fields, err := wireguardSpec(vpnClientWireguardWires()).WireFields(plan)
	if err != nil {
		t.Fatalf("WireFields: %v", err)
	}
	for _, name := range []string{"dhcpd_dns_1", "dhcpd_dns_2"} {
		if slices.Contains(fields, name) {
			t.Errorf("%s is in the mask although Encode left it empty; the update sends \"\" "+
				"and the controller's DNS is blanked", name)
		}
	}
	// AND THE THREE UNCONDITIONAL ONES STILL TRAVEL. A field that masks nothing
	// is the silent write-drop this kind exists to prevent, and it looks
	// identical to a correct narrowing from here. This plan sets no
	// configuration, no peer and no preshared key either, so the five other
	// conditional wires are correctly absent too.
	want := []string{
		"x_wireguard_private_key",
		"wireguard_interface",
		"wireguard_client_preshared_key_enabled",
	}
	if !slices.Equal(fields, want) {
		t.Errorf("the mask carries %v, want exactly the unconditional %v", fields, want)
	}
}

// A configuration file supplies DNS only sometimes, and the mask has to follow
// WHICH sometimes.
//
// THIS ASSERTED THE COARSE RULE AGAINST A FIXTURE THAT DOES NOT PARSE. It used
// "Zm9v" -- base64 for "foo" -- and required both names to stay in the mask
// because a configuration file "may supply" them. On that input Encode raises a
// diagnostic and writes nothing, so keeping the names masked sends two empty
// strings and blanks the controller's DNS. The reasoning was sound given the
// premise that a predicate cannot know which; it can know, by parsing exactly as
// Encode does, so the trade between dropping a write and destroying a value does
// not arise.
func TestWireguardFieldDNSNamesFollowWhatTheConfigurationSupplies(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
		masked  bool
	}{
		{"two DNS entries", "W0ludGVyZmFjZV0KUHJpdmF0ZUtleSA9IGFXUmxiblJwZEhsclpYbHBaR1Z1ZEdsMGVXdGxlV2xrWlc1MGFYUjVNREE9CkFkZHJlc3MgPSAxMC4wLjAuMi8zMgpETlMgPSAxLjEuMS4xLCA4LjguOC44CgpbUGVlcl0KUHVibGljS2V5ID0gY0dWbGNuQjFZbXhwWTJ0bGVYQmxaWEp3ZFdKc2FXTnJaWGx3WldWeU1EQT0KRW5kcG9pbnQgPSAyMDMuMC4xMTMuMTA6NTE4MjAK", true},
		{"a valid file with no DNS", "W0ludGVyZmFjZV0KUHJpdmF0ZUtleSA9IGFXUmxiblJwZEhsclpYbHBaR1Z1ZEdsMGVXdGxlV2xrWlc1MGFYUjVNREE9CkFkZHJlc3MgPSAxMC4wLjAuMi8zMgoKW1BlZXJdClB1YmxpY0tleSA9IGNHVmxjbkIxWW14cFkydGxlWEJsWlhKd2RXSnNhV05yWlhsd1pXVnlNREE9CkVuZHBvaW50ID0gMjAzLjAuMTEzLjEwOjUxODIwCg==", false},
		{"a file that does not parse", "Zm9v", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			configuration, d := types.ObjectValue(wireguardConfigurationModel{}.AttributeTypes(),
				map[string]attr.Value{
					"content":  types.StringValue(testCase.content),
					"filename": types.StringValue("wg0.conf"),
				})
			if d.HasError() {
				t.Fatal(d)
			}
			plan := wireguardPlanWithoutDNS(t)
			attributes := plan.Wireguard.Attributes()
			attributes["configuration"] = configuration
			object, d := types.ObjectValue(wireguardModel{}.AttributeTypes(), attributes)
			if d.HasError() {
				t.Fatal(d)
			}
			plan.Wireguard = object

			fields, err := wireguardSpec(vpnClientWireguardWires()).WireFields(plan)
			if err != nil {
				t.Fatalf("WireFields: %v", err)
			}
			// CONTROL: a wire that is never conditional must be present in every
			// case, or an empty mask would satisfy the false rows.
			if !slices.Contains(fields, "wireguard_interface") {
				t.Fatalf("the unconditional wires are missing too; the mask is empty: %v", fields)
			}
			for _, name := range []string{"dhcpd_dns_1", "dhcpd_dns_2"} {
				if got := slices.Contains(fields, name); got != testCase.masked {
					t.Errorf("%s masked=%v, want %v", name, got, testCase.masked)
				}
			}
		})
	}
}

// wireguardPlanWritingEverything exercises every path through Encode, so that
// all ten wires are written and all ten must therefore be masked.
//
// peer rather than configuration, because the two are the arms of one switch
// and only one can run. Both write the same four wires; the peer arm needs no
// base64 fixture to parse.
func wireguardPlanWritingEverything(t *testing.T) *vpnClientResourceModel {
	t.Helper()
	ctx := context.Background()
	dns, diags := types.ListValueFrom(ctx, types.StringType, []string{"10.0.0.1", "10.0.0.2"})
	if diags.HasError() {
		t.Fatal(diags)
	}
	peer, d := types.ObjectValue(wireguardPeerModel{}.AttributeTypes(), map[string]attr.Value{
		"ip":         types.StringValue("198.51.100.7"),
		"port":       types.Int64Value(51820),
		"public_key": types.StringValue("pubkey"),
	})
	if d.HasError() {
		t.Fatal(d)
	}
	value := wireguardModel{
		PrivateKey:          types.StringValue("privkey"),
		Configuration:       types.ObjectNull(wireguardConfigurationModel{}.AttributeTypes()),
		Peer:                peer,
		PresharedKeyEnabled: types.BoolValue(true),
		PresharedKey:        types.StringValue("psk"),
		Interface:           types.StringValue("wan"),
		DnsServers:          dns,
	}
	object, d := types.ObjectValueFrom(ctx, value.AttributeTypes(), value)
	if d.HasError() {
		t.Fatal(d)
	}
	return &vpnClientResourceModel{Wireguard: object}
}

// wireguardWireValues reads the ten wires off an SDK object as comparable
// strings. All ten have a case, not just the conditional ones, because the
// derivation below would otherwise be blind to the missing one and it would
// read as unconditional.
func wireguardWireValues(network *ui.Network) map[string]string {
	text := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	number := func(p *int64) string {
		if p == nil {
			return "<nil>"
		}
		return strconv.FormatInt(*p, 10)
	}
	return map[string]string{
		"x_wireguard_private_key":                text(network.WireguardPrivateKey),
		"wireguard_interface":                    text(network.WireguardInterface),
		"wireguard_client_preshared_key_enabled": strconv.FormatBool(network.WireguardClientPresharedKeyEnabled),
		"wireguard_client_preshared_key":         text(network.WireguardClientPresharedKey),
		"wireguard_client_mode":                  text(network.WireguardClientMode),
		"wireguard_client_peer_public_key":       text(network.WireguardClientPeerPublicKey),
		"wireguard_client_peer_ip":               text(network.WireguardClientPeerIP),
		"wireguard_client_peer_port":             number(network.WireguardClientPeerPort),
		"dhcpd_dns_1":                            network.DHCPDDNS1,
		"dhcpd_dns_2":                            network.DHCPDDNS2,
	}
}

// writtenWires reports which of the ten wires Encode ASSIGNS for this plan,
// which is not the same question as which are non-zero afterwards.
//
// TWO SEEDS, BECAUSE A DELIBERATE WRITE OF THE ZERO VALUE IS INDISTINGUISHABLE
// FROM NO WRITE. wireguard_client_preshared_key_enabled is a plain bool that
// Encode assigns on every path, so a plan with the flag off leaves it false --
// and a value-based reading calls that "not written", which is exactly the
// conflation networkMaskFor makes. Reported that way it looks like an eighth
// conditional wire and it is an instrument fault.
//
// So Encode runs twice, over two objects whose ten fields differ. A field it
// assigns ends up the same in both, because the plan decided it. A field it
// leaves alone keeps whichever seed it started with, and the two disagree.
// No per-field knowledge, and it works for a bool, where there are only two
// values to seed with.
func writtenWires(t *testing.T, plan *vpnClientResourceModel) []string {
	t.Helper()
	ctx := context.Background()
	encode := func(seed *ui.Network) map[string]string {
		if diags := encodeVPNClientWireguard(ctx, plan.Wireguard, seed); diags.HasError() {
			t.Fatalf("Encode: %v", diags)
		}
		return wireguardWireValues(seed)
	}
	sentinel := "SEEDED-NOT-WRITTEN"
	port := int64(65001)
	first := encode(&ui.Network{})
	second := encode(&ui.Network{
		WireguardPrivateKey:                &sentinel,
		WireguardInterface:                 &sentinel,
		WireguardClientPresharedKeyEnabled: true,
		WireguardClientPresharedKey:        &sentinel,
		WireguardClientMode:                &sentinel,
		WireguardClientPeerPublicKey:       &sentinel,
		WireguardClientPeerIP:              &sentinel,
		WireguardClientPeerPort:            &port,
		DHCPDDNS1:                          sentinel,
		DHCPDDNS2:                          sentinel,
	})

	var written []string
	for _, wire := range vpnClientWireguardWires() {
		before, ok := first[wire]
		if !ok {
			t.Fatalf("%s has no reader here, so the derivation cannot see it and it would "+
				"read as unconditional", wire)
		}
		if before == second[wire] {
			written = append(written, wire)
		}
	}
	slices.Sort(written)
	return written
}

// THE DECLARED CONDITIONAL SET MUST EQUAL THE DERIVED ONE.
//
// WITHOUT THIS THE TABLE BELOW CANNOT FAIL FOR A MISSING ENTRY, because it takes
// its population FROM the declaration -- delete an entry and the wire simply
// stops being checked, which is a check whose population is the thing being
// checked. A first pass declared two of the seven and every test was green.
//
// So the conditional set is derived from behaviour: run Encode against a plan
// that takes no optional path and one that takes every path, and a wire written
// only by the second is conditional by definition.
func TestTheDeclaredConditionalWiresAreTheOnesEncodeWritesConditionally(t *testing.T) {
	field := vpnClientWireguardField()
	always := writtenWires(t, wireguardPlanWithoutDNS(t))
	everything := writtenWires(t, wireguardPlanWritingEverything(t))

	var derived []string
	for _, wire := range everything {
		if !slices.Contains(always, wire) {
			derived = append(derived, wire)
		}
	}
	slices.Sort(derived)

	declared := conditionalWiresOf(t, field)
	if !slices.Equal(declared, derived) {
		t.Errorf("ConditionalWires declares %v and Encode writes %v conditionally.\n"+
			"A wire Encode writes only sometimes and the descriptor calls unconditional is "+
			"masked with nothing behind it, and go-unifi sends the zero -- the controller's "+
			"value is cleared. One the descriptor calls conditional and Encode always writes "+
			"leaves the mask and the write is silently dropped.", declared, derived)
	}
	if len(derived) == 0 {
		t.Fatal("nothing reads as conditional; the two plans do not differ and this proves nothing")
	}
}

// EVERY PREDICATE IS CHECKED AGAINST WHAT Encode ACTUALLY DOES, BOTH WAYS.
// EVERY PREDICATE IS CHECKED AGAINST WHAT Encode ACTUALLY DOES, BOTH WAYS.
//
// A ConditionalWires entry is a second statement of a guard that already exists
// inside Encode, and two lists that must agree is the shape this whole area
// keeps failing on. So each conditional wire gets a plan where its predicate
// says NO -- asserting Encode left the SDK field at its zero -- and one where it
// says YES, asserting Encode wrote it.
//
// THE SECOND HALF IS NOT OPTIONAL. A false-only assertion passes for a predicate
// that always returns false, which masks nothing and silently drops every write
// -- the same one-of-many failure pointing the other way. Twenty-two wires on
// wan will be a table, not twenty-two tests, and this is the shape to copy.
func TestEveryConditionalWireAgreesWithWhatEncodeWrites(t *testing.T) {
	field := vpnClientWireguardField()

	written := func(t *testing.T, plan *vpnClientResourceModel, wire string) bool {
		t.Helper()
		return slices.Contains(writtenWires(t, plan), wire)
	}

	// THE POPULATION IS DERIVED, not listed. A wire declared conditional and
	// absent from a hand-kept table here would go unchecked, which is the
	// two-lists problem this test exists to remove, reappearing inside it.
	conditional := conditionalWiresOf(t, field)
	if len(conditional) == 0 {
		t.Fatal("no conditional wires found; this test cannot fail and is worthless")
	}
	t.Logf("checking %d conditional wire(s) of %d", len(conditional), len(vpnClientWireguardWires()))

	noPaths := wireguardPlanWithoutDNS(t) // no dns_servers, no configuration, no peer, psk off
	allPaths := wireguardPlanWritingEverything(t)

	for _, wire := range conditional {
		t.Run(wire, func(t *testing.T) {
			predicate := field.ConditionalWires[wire]

			if predicate(noPaths.Wireguard) {
				t.Errorf("the predicate says %s will be written for a plan that sets none of "+
					"its paths", wire)
			} else if written(t, noPaths, wire) {
				t.Errorf("the predicate says %s will NOT be written and Encode wrote it; "+
					"the wire leaves the mask and the value is silently dropped", wire)
			}

			if !predicate(allPaths.Wireguard) {
				t.Errorf("the predicate says %s will not be written for a plan that sets "+
					"every path", wire)
			} else if !written(t, allPaths, wire) {
				t.Errorf("the predicate says %s WILL be written and Encode did not; the wire "+
					"joins the mask with nothing behind it and the controller's value is cleared",
					wire)
			}
		})
	}
}

func conditionalWiresOf(
	t *testing.T,
	field resourcekit.ScatteredObjectField[vpnClientResourceModel, ui.Network],
) []string {
	t.Helper()
	names := make([]string, 0, len(field.ConditionalWires))
	for name := range field.ConditionalWires {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
