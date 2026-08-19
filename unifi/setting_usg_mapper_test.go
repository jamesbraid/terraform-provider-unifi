package unifi

// The usg mapper's unit tests live HERE and not in setting_resource_test.go,
// which is one of the three frozen shared scenario owners: it is grafted onto
// the RELEASED provider during the controller differential, so every symbol it
// names must exist in both trees. A unit test that follows a signature change
// stops the released tree compiling and fails the differential an hour before
// any controller is involved.
//
// The rule this file encodes: a scenario owner holds acceptance tests, and a
// unit test that pins an internal signature belongs beside it, not in it.

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// Test_settingResource_usgModelToSetting_preservesUnmanagedFields is the fix
// for the six site-wide gateway settings this mapper was resetting.
//
// It built a FRESH settings.Usg from the model. 23 of the type's 46 fields
// carry no omitempty, so every one the schema does not declare went back as a
// Go zero on every apply of unifi_setting that names a usg block.
//
// The remedy is not a mask -- there is no UpdateSettingFields, because
// UpdateSetting takes a settings.Setting interface. It is to start from what
// the controller already holds, which is what mgmt, radius and igmpSnooping in
// this same file already do.
func Test_settingResource_usgModelToSetting_preservesUnmanagedFields(t *testing.T) {
	ctx := context.Background()
	r := &settingResource{}

	// What the controller holds. None of these six is reachable from the
	// schema, so nothing in a plan can ever restate them.
	base := &settings.Usg{
		DHCPDHostfileUpdate:    true,
		DHCPDUseDNSmasq:        true,
		DHCPRelayAgentsPackets: "forward",
		DNSmasqAllServers:      true,
		LldpEnableAll:          true,
		MdnsEnabled:            true,
		// A managed field, as the control: the model declares it below and it
		// must be overwritten, or this test would pass for a mapper that
		// ignored the model entirely.
		BroadcastPing: false,
	}

	model := &settingUSGModel{BroadcastPing: types.BoolValue(true)}
	got := r.usgModelToSetting(ctx, model, base)

	for _, field := range []struct {
		name string
		got  bool
	}{
		{"dhcpd_hostfile_update", got.DHCPDHostfileUpdate},
		{"dhcpd_use_dnsmasq", got.DHCPDUseDNSmasq},
		{"dnsmasq_all_servers", got.DNSmasqAllServers},
		{"lldp_enable_all", got.LldpEnableAll},
		// DEPRECATED IN THE SDK: "removed in UniFi Network 7; retained for
		// backwards compatibility". The controller under test is 10.4.57, so
		// this one is very likely inert and the honest count of LIVE defects
		// here is five, not six. It stays in the list because the write was
		// still zeroing it, and because inert-on-this-firmware is a claim about
		// a controller nobody has asked -- unlike the other five, which need no
		// controller to be wrong.
		{"mdns_enabled", got.MdnsEnabled}, //nolint:staticcheck // see above
	} {
		if !field.got {
			t.Errorf("%s went back as false; the provider does not model it, so a "+
				"write that zeroes it silently turns off a gateway setting nobody "+
				"asked it to touch", field.name)
		}
	}

	// The one non-bool of the six. Its zero is the empty string, which the
	// controller's own enum does not list among append|discard|forward|replace
	// -- so the write was not just changing the relay-agent policy, it was
	// sending a value outside the documented set.
	if got.DHCPRelayAgentsPackets != "forward" {
		t.Errorf("dhcp_relay_agents_packets = %q, want \"forward\"; blanking it "+
			"discards the gateway's DHCP relay-agent policy", got.DHCPRelayAgentsPackets)
	}

	if !got.BroadcastPing {
		t.Error("broadcast_ping was not taken from the model; the mapper is ignoring " +
			"the plan and every assertion above passes for the wrong reason")
	}
}

// Test_usgForceEmittedFieldCountIsPinned is the tripwire for a regenerated SDK.
//
// The six fields above are the ones measured as force-emitted AND unreachable
// from the schema. A regeneration that adds a 47th field without omitempty adds
// a seventh, and nothing else here would notice: the test above names its six
// explicitly and would keep passing.
//
// The count is pinned rather than the set, because deriving "assigned by this
// mapper" needs an AST walk scoped to one function, and scoping a derivation to
// one function has produced a wrong answer in this package twice. A count that
// moves is a prompt to re-run the census by hand, which is the measurement that
// can be trusted.
func Test_usgForceEmittedFieldCountIsPinned(t *testing.T) {
	typ := reflect.TypeOf(settings.Usg{})

	var forceEmits int
	for i := range typ.NumField() {
		tag, ok := typ.Field(i).Tag.Lookup("json")
		if !ok || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		if parts[0] == "" || parts[0] == "key" {
			continue
		}
		if !slices.Contains(parts[1:], "omitempty") {
			forceEmits++
		}
	}

	const want = 23
	if forceEmits != want {
		t.Fatalf("settings.Usg force-emits %d field(s), not %d.\n\n"+
			"Every force-emitted field the schema cannot reach is sent as a Go zero "+
			"unless the write starts from the controller's own object. Re-run the "+
			"census -- force-emitted AND never assigned -- and extend "+
			"Test_settingResource_usgModelToSetting_preservesUnmanagedFields with "+
			"whatever it finds before changing this number.", forceEmits, want)
	}
}

func Test_settingResource_usgModelToSetting(t *testing.T) {
	r := &settingResource{}
	ctx := context.Background()

	// An EMPTY base, so this still reads as it did: with nothing to preserve,
	// a null field leaves the zero in place. The preservation case proper is
	// Test_settingResource_usgModelToSetting_preservesUnmanagedFields.
	t.Run("null fields leave the base untouched", func(t *testing.T) {
		model := &settingUSGModel{
			FtpModule:       types.BoolNull(),
			BroadcastPing:   types.BoolNull(),
			DNSVerification: types.ObjectNull(nil),
		}
		got := r.usgModelToSetting(ctx, model, &settings.Usg{})
		if got == nil {
			t.Fatal("expected non-nil result")
		}
		if got.FtpModule {
			t.Error("FtpModule should be false for null input")
		}
	})

	t.Run("ftp_module set to true", func(t *testing.T) {
		model := &settingUSGModel{
			FtpModule:       types.BoolValue(true),
			BroadcastPing:   types.BoolNull(),
			DNSVerification: types.ObjectNull(nil),
		}
		got := r.usgModelToSetting(ctx, model, &settings.Usg{})
		if got == nil {
			t.Fatal("expected non-nil result")
		}
		if !got.FtpModule {
			t.Error("FtpModule should be true")
		}
	})
}
