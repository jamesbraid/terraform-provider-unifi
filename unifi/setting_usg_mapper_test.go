package unifi

// Kept out of setting_resource_test.go: that file is grafted onto the
// RELEASED provider during the release-comparison harness's run, so every
// symbol it names must exist in both trees -- a unit test pinning an
// internal signature belongs beside it, not in it.

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// Test_settingResource_usgModelToSetting_preservesUnmanagedFields: the
// mapper must start from what the controller holds (there is no
// UpdateSettingFields mask -- UpdateSetting takes an interface), or every
// force-emitted field the schema doesn't declare goes back as a Go zero.
func Test_settingResource_usgModelToSetting_preservesUnmanagedFields(t *testing.T) {
	ctx := context.Background()
	r := &settingResource{}

	// None of these six is reachable from the schema, so nothing in a plan
	// can ever restate them.
	base := &settings.Usg{
		DHCPDHostfileUpdate:    true,
		DHCPDUseDNSmasq:        true,
		DHCPRelayAgentsPackets: "forward",
		DNSmasqAllServers:      true,
		LldpEnableAll:          true,
		MdnsEnabled:            true,
		// The control: managed, declared in the model below, must be overwritten.
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
		{"mdns_enabled", got.MdnsEnabled}, //nolint:staticcheck // deprecated in the SDK but the write was still zeroing it
	} {
		if !field.got {
			t.Errorf("%s went back as false; the provider does not model it, so a "+
				"write that zeroes it silently turns off a gateway setting nobody "+
				"asked it to touch", field.name)
		}
	}

	// The one non-bool of the six; its zero "" sits outside the controller's
	// own append|discard|forward|replace enum.
	if got.DHCPRelayAgentsPackets != "forward" {
		t.Errorf("dhcp_relay_agents_packets = %q, want \"forward\"; blanking it "+
			"discards the gateway's DHCP relay-agent policy", got.DHCPRelayAgentsPackets)
	}

	if !got.BroadcastPing {
		t.Error("broadcast_ping was not taken from the model; the mapper is ignoring " +
			"the plan and every assertion above passes for the wrong reason")
	}
}

// Test_usgForceEmittedFieldCountIsPinned trips when a regenerated SDK adds a
// field without omitempty -- the test above names its six explicitly and
// would keep passing. A moved count means: re-run the census by hand.
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

	// An empty base: with nothing to preserve, a null field leaves the zero
	// in place. Preservation proper is tested above.
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
