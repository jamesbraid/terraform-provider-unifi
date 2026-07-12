package unifi

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// usgGoldenReproModel builds the exact settingUSGModel used by
// TestGolden_usg (setting_golden_test.go) so both the legacy-converter
// golden and this section's overlay can be proven to produce the same PUT
// body from the same representative model.
func usgGoldenReproModel(t *testing.T, ctx context.Context) settingUSGModel {
	t.Helper()

	dnsVerif, diags := types.ObjectValueFrom(ctx, usgDNSVerificationAttrTypes, dnsVerificationModel{
		Domain:             types.StringValue("example.com"),
		PrimaryDNSServer:   types.StringValue("192.0.2.53"),
		SecondaryDNSServer: types.StringValue("192.0.2.54"),
		SettingPreference:  types.StringValue("manual"),
	})
	if diags.HasError() {
		t.Fatalf("building dns_verification: %v", diags)
	}

	return settingUSGModel{
		BroadcastPing:                  types.BoolValue(false),
		DNSVerification:                dnsVerif,
		FtpModule:                      types.BoolValue(true),
		GeoIPFilteringBlock:            types.StringValue("block"),
		GeoIPFilteringCountries:        types.StringValue("US"),
		GeoIPFilteringEnabled:          types.BoolValue(true),
		GeoIPFilteringTrafficDirection: types.StringValue("both"),
		GreModule:                      types.BoolValue(false),
		H323Module:                     types.BoolValue(false),
		ICMPTimeout:                    util.DurationValue(30, time.Second),
		MssClamp:                       types.StringValue("auto"),
		OffloadAccounting:              types.BoolValue(false),
		OffloadL2Blocking:              types.BoolValue(false),
		OffloadSch:                     types.BoolValue(true),
		OtherTimeout:                   util.DurationValue(600, time.Second),
		PptpModule:                     types.BoolValue(false),
		ReceiveRedirects:               types.BoolValue(false),
		SendRedirects:                  types.BoolValue(true),
		SipModule:                      types.BoolValue(true),
		SynCookies:                     types.BoolValue(true),
		TCPCloseTimeout:                util.DurationValue(10, time.Second),
		TCPCloseWaitTimeout:            util.DurationValue(20, time.Second),
		TCPEstablishedTimeout:          util.DurationValue(3600, time.Second),
		TCPFinWaitTimeout:              util.DurationValue(120, time.Second),
		TCPLastAckTimeout:              util.DurationValue(30, time.Second),
		TCPSynRecvTimeout:              util.DurationValue(60, time.Second),
		TCPSynSentTimeout:              util.DurationValue(120, time.Second),
		TCPTimeWaitTimeout:             util.DurationValue(120, time.Second),
		TFTPModule:                     types.BoolValue(true),
		TimeoutSettingPreference:       types.StringValue("auto"),
		UDPOtherTimeout:                util.DurationValue(30, time.Second),
		UDPStreamTimeout:               util.DurationValue(180, time.Second),
		UnbindWANMonitors:              types.BoolValue(false),
		UPnPEnabled:                    types.BoolValue(true),
		UPnPNATPmpEnabled:              types.BoolValue(false),
		UPnPSecureMode:                 types.BoolValue(true),
		UPnPWANInterface:               types.StringValue("WAN"),
	}
}

// TestUsgSection_GoldenReproduction proves overlay() reproduces the Task-21
// golden PUT body (TestGolden_usg) — the biggest section: 37 modeled leaves
// (18 bool, 6 string, 12 GoDuration, 1 nested object) plus 11 unmodeled
// always-present fields that are RMW-preserved from the snapshot's existing
// section data. Seeding the snapshot base with those 11 zero-value fields
// and overlaying the representative model on top must reproduce the golden
// byte-for-byte — this is the oracle that catches any missed leaf.
func TestUsgSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := usgSection{}

	m := usgGoldenReproModel(t, ctx)
	obj, diags := types.ObjectValueFrom(ctx, usgAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building usg object: %v", diags)
	}

	model := settingResourceModel{USG: obj}
	prior := settingResourceModel{}
	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "usg"},
		Data: map[string]any{
			"dhcp_relay_agents_packets": "",
			"dhcp_relay_server_1":       "",
			"dhcp_relay_server_2":       "",
			"dhcp_relay_server_3":       "",
			"dhcp_relay_server_4":       "",
			"dhcp_relay_server_5":       "",
			"dhcpd_hostfile_update":     false,
			"dhcpd_use_dnsmasq":         false,
			"dnsmasq_all_servers":       false,
			"lldp_enable_all":           false,
			"mdns_enabled":              false,
		},
	}})

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "usg" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "usg")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenUSG)
}

// TestUsgSection_DecodeRoundTrip proves decode() reads a representative
// subset of the 37 modeled leaves (a few bools/strings, a GoDuration leaf,
// and the nested dns_verification object) from a snapshot section's data
// into model.USG.
func TestUsgSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := usgSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "usg"},
		Data: map[string]any{
			"broadcast_ping":         true,
			"ftp_module":             false,
			"geo_ip_filtering_block": "allow",
			"mss_clamp":              "custom",
			"icmp_timeout":           float64(30),
			"udp_stream_timeout":     float64(180),
			"dns_verification": map[string]any{
				"domain":               "example.com",
				"primary_dns_server":   "192.0.2.53",
				"secondary_dns_server": "192.0.2.54",
				"setting_preference":   "manual",
			},
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.USG.IsNull() || model.USG.IsUnknown() {
		t.Fatalf("model.USG is null/unknown after decode")
	}

	var got settingUSGModel
	if diags := model.USG.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingUSGModel: %v", diags)
	}

	if !got.BroadcastPing.ValueBool() {
		t.Errorf("BroadcastPing = %v, want true", got.BroadcastPing)
	}
	if got.FtpModule.ValueBool() {
		t.Errorf("FtpModule = %v, want false", got.FtpModule)
	}
	if got.GeoIPFilteringBlock.ValueString() != "allow" {
		t.Errorf("GeoIPFilteringBlock = %q, want %q", got.GeoIPFilteringBlock.ValueString(), "allow")
	}
	if got.MssClamp.ValueString() != "custom" {
		t.Errorf("MssClamp = %q, want %q", got.MssClamp.ValueString(), "custom")
	}

	if got.ICMPTimeout.IsNull() || got.ICMPTimeout.IsUnknown() {
		t.Fatalf("ICMPTimeout is null/unknown after decode")
	}
	dur, ddiags := got.ICMPTimeout.ValueGoDuration()
	if ddiags.HasError() {
		t.Fatalf("ValueGoDuration: %v", ddiags)
	}
	if dur != 30*time.Second {
		t.Errorf("ICMPTimeout = %v, want 30s", dur)
	}

	if got.DNSVerification.IsNull() || got.DNSVerification.IsUnknown() {
		t.Fatalf("DNSVerification is null/unknown after decode")
	}
	var dv dnsVerificationModel
	if diags := got.DNSVerification.As(ctx, &dv, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting dnsVerificationModel: %v", diags)
	}
	if dv.Domain.ValueString() != "example.com" {
		t.Errorf("DNSVerification.Domain = %q, want %q", dv.Domain.ValueString(), "example.com")
	}
	if dv.PrimaryDNSServer.ValueString() != "192.0.2.53" {
		t.Errorf("DNSVerification.PrimaryDNSServer = %q, want %q", dv.PrimaryDNSServer.ValueString(), "192.0.2.53")
	}
	if dv.SecondaryDNSServer.ValueString() != "192.0.2.54" {
		t.Errorf("DNSVerification.SecondaryDNSServer = %q, want %q", dv.SecondaryDNSServer.ValueString(), "192.0.2.54")
	}
	if dv.SettingPreference.ValueString() != "manual" {
		t.Errorf("DNSVerification.SettingPreference = %q, want %q", dv.SettingPreference.ValueString(), "manual")
	}
}

// TestUsgSection_Preservation proves overlay() preserves an unmodeled key
// already present in the snapshot's section data (RMW), using one of the 11
// always-present-but-unmodeled fields (lldp_enable_all) plus a synthetic
// unknown key.
func TestUsgSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := usgSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "usg"},
		Data: map[string]any{
			"lldp_enable_all": true,
			"x_unmanaged":     "keep",
		},
	}})

	m := usgGoldenReproModel(t, ctx)
	obj, diags := types.ObjectValueFrom(ctx, usgAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building usg object: %v", diags)
	}

	model := settingResourceModel{USG: obj}
	prior := settingResourceModel{}

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	if got, ok := rs.Data["lldp_enable_all"]; !ok || got != true {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %v", "lldp_enable_all", got, ok, true)
	}
	if got, ok := rs.Data["x_unmanaged"]; !ok || got != "keep" {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %q", "x_unmanaged", got, ok, "keep")
	}
}

// TestUsgSection_DnsVerificationNull proves the nested-object null path in
// both directions: decode() of a snapshot with no "dns_verification" key
// produces a null DNSVerification object, and overlay() of a model whose
// DNSVerification is null does not write a dns_verification key onto a base
// that didn't already have one.
func TestUsgSection_DnsVerificationNull(t *testing.T) {
	ctx := context.Background()
	sec := usgSection{}

	t.Run("decode with no dns_verification key yields null", func(t *testing.T) {
		snap := newRawSettings([]settings.RawSetting{{
			BaseSetting: settings.BaseSetting{Key: "usg"},
			Data: map[string]any{
				"broadcast_ping": true,
			},
		}})
		prior := settingResourceModel{}
		model := settingResourceModel{}

		diags := sec.decode(ctx, snap, prior, &model)
		if diags.HasError() {
			t.Fatalf("decode diagnostics: %v", diags)
		}

		var got settingUSGModel
		if diags := model.USG.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
			t.Fatalf("extracting settingUSGModel: %v", diags)
		}
		if !got.DNSVerification.IsNull() {
			t.Errorf("DNSVerification = %v, want null", got.DNSVerification)
		}
	})

	t.Run("overlay with null DNSVerification writes no dns_verification key", func(t *testing.T) {
		m := usgGoldenReproModel(t, ctx)
		m.DNSVerification = types.ObjectNull(usgDNSVerificationAttrTypes)
		obj, diags := types.ObjectValueFrom(ctx, usgAttrTypes, m)
		if diags.HasError() {
			t.Fatalf("building usg object: %v", diags)
		}

		model := settingResourceModel{USG: obj}
		prior := settingResourceModel{}
		snap := newRawSettings(nil) // empty base: no pre-existing dns_verification

		rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
		if oDiags.HasError() {
			t.Fatalf("overlay diagnostics: %v", oDiags)
		}
		if !configured {
			t.Fatalf("overlay configured = false, want true")
		}
		if _, ok := rs.Data["dns_verification"]; ok {
			t.Errorf("rs.Data[%q] present, want absent (null nested object is a no-op)", "dns_verification")
		}
	})
}

// TestUsgSection_NotConfigured proves overlay() returns configured == false
// and a zero-value RawSetting when model.USG is null.
func TestUsgSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := usgSection{}

	model := settingResourceModel{USG: types.ObjectNull(usgAttrTypes)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if configured {
		t.Fatalf("overlay configured = true, want false")
	}
	if rs.Key != "" || len(rs.Data) != 0 {
		t.Errorf("overlay returned non-zero RawSetting when not configured: %+v", rs)
	}
}

// TestUsgSection_InterfaceWiring is a light structural check that the
// section is registered and key()/attrName() both return "usg" (no
// key/attrName divergence for this section).
func TestUsgSection_InterfaceWiring(t *testing.T) {
	sec := usgSection{}
	if sec.key() != "usg" {
		t.Errorf("key() = %q, want %q", sec.key(), "usg")
	}
	if sec.attrName() != "usg" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "usg")
	}

	var found bool
	for _, s := range settingSections {
		if s.key() == "usg" && s.attrName() == "usg" {
			found = true
		}
	}
	if !found {
		t.Errorf("no section in settingSections registry has key()==attrName()==%q", "usg")
	}
}
