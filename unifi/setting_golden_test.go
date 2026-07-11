package unifi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// This file is the MIGRATION ORACLE for the settings-expansion project
// (PR-A Task 9). For each of the 13 legacy settings sections it captures the
// exact JSON PUT body the CURRENT legacy converter (unifi/setting_resource.go,
// methods named "<attr>ModelToSetting") produces for a representative model,
// as a checked-in golden string.
//
// Tasks 10-22 migrate each section onto the new settingSection engine (whose
// overlay() returns a settings.RawSetting, a map-based type). Each of those
// tasks must assert its new overlay reproduces the SAME golden body captured
// here. If a field is dropped, renamed, or defaulted differently during
// migration, the golden catches it byte-for-byte.
//
// The legacy converters remain in unifi/setting_resource.go until Task 24c;
// this file only calls them, it does not modify them.

// mustMarshal marshals v and fails the test on error. Kept trivial and local
// to this file since every golden capture needs it.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

// normalizeSettingJSON is the cross-form comparison oracle shared by this
// file and by Tasks 10-22.
//
// The legacy converters return TYPED structs (e.g. *settings.Mgmt), whose
// json.Marshal emits object keys in Go struct-field-declaration order. The
// new migrated sections (Tasks 10-22) produce a map-based
// settings.RawSetting, whose MarshalJSON emits keys ALPHABETICALLY (Go's
// encoding/json sorts map keys). A naive byte-for-byte comparison of the two
// forms would fail on key ORDER alone even when the two bodies are
// semantically identical, making the golden useless as a migration oracle.
//
// normalizeSettingJSON removes that ordering difference: it unmarshals b
// into a map[string]any and re-marshals it, so both the legacy typed-struct
// output and the new RawSetting output normalize to the same canonical,
// alphabetically-keyed string. Golden constants in this file are stored in
// that normalized form; Tasks 10-22 must call normalizeSettingJSON on their
// overlay()'s marshaled RawSetting before comparing against these goldens.
func normalizeSettingJSON(t *testing.T, b []byte) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("normalizeSettingJSON: unmarshal: %v (input: %s)", err, string(b))
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("normalizeSettingJSON: remarshal: %v", err)
	}
	return string(out)
}

// --- auto_speedtest (scalar) ---

const goldenAutoSpeedtest = `{"cron_expr":"0 0 * * *","enabled":true,"key":""}`

func TestGolden_auto_speedtest(t *testing.T) {
	r := &settingResource{}
	model := &settingAutoSpeedtestModel{
		Enabled:  types.BoolValue(true),
		CronExpr: types.StringValue("0 0 * * *"),
	}

	got := normalizeSettingJSON(t, mustMarshal(t, r.autoSpeedtestModelToSetting(model)))
	if got != goldenAutoSpeedtest {
		t.Errorf("auto_speedtest golden mismatch:\n got:  %s\n want: %s", got, goldenAutoSpeedtest)
	}
}

// --- country (scalar) ---

const goldenCountry = `{"code":840,"key":""}`

func TestGolden_country(t *testing.T) {
	r := &settingResource{}
	model := &settingCountryModel{
		Code: types.Int64Value(840), // ISO 3166-1 numeric for US; neutral example code
	}

	got := normalizeSettingJSON(t, mustMarshal(t, r.countryModelToSetting(model)))
	if got != goldenCountry {
		t.Errorf("country golden mismatch:\n got:  %s\n want: %s", got, goldenCountry)
	}
}

// --- dpi (scalar) ---

const goldenDpi = `{"enabled":true,"fingerprintingEnabled":false,"key":""}`

func TestGolden_dpi(t *testing.T) {
	r := &settingResource{}
	model := &settingDpiModel{
		Enabled:               types.BoolValue(true),
		FingerprintingEnabled: types.BoolValue(false),
	}

	got := normalizeSettingJSON(t, mustMarshal(t, r.dpiModelToSetting(model)))
	if got != goldenDpi {
		t.Errorf("dpi golden mismatch:\n got:  %s\n want: %s", got, goldenDpi)
	}
}

// --- lcm (scalar, with optional int pointers) ---

const goldenLcm = `{"brightness":50,"enabled":true,"idle_timeout":300,"key":"","sync":true,"touch_event":false}`

func TestGolden_lcm(t *testing.T) {
	r := &settingResource{}
	model := &settingLcmModel{
		Enabled:     types.BoolValue(true),
		Brightness:  types.Int64Value(50),
		IdleTimeout: types.Int64Value(300),
		Sync:        types.BoolValue(true),
		TouchEvent:  types.BoolValue(false),
	}

	got := normalizeSettingJSON(t, mustMarshal(t, r.lcmModelToSetting(model)))
	if got != goldenLcm {
		t.Errorf("lcm golden mismatch:\n got:  %s\n want: %s", got, goldenLcm)
	}
}

// --- network_optimization (scalar) ---

const goldenNetworkOptimization = `{"enabled":true,"key":""}`

func TestGolden_network_optimization(t *testing.T) {
	r := &settingResource{}
	model := &settingNetworkOptimizationModel{
		Enabled: types.BoolValue(true),
	}

	got := normalizeSettingJSON(t, mustMarshal(t, r.networkOptimizationModelToSetting(model)))
	if got != goldenNetworkOptimization {
		t.Errorf("network_optimization golden mismatch:\n got:  %s\n want: %s", got, goldenNetworkOptimization)
	}
}

// --- ntp (scalar) ---

const goldenNtp = `{"key":"","ntp_server_1":"ntp1.example.com","ntp_server_2":"ntp2.example.com","setting_preference":"manual"}`

func TestGolden_ntp(t *testing.T) {
	r := &settingResource{}
	model := &settingNtpModel{
		NtpServer1:        types.StringValue("ntp1.example.com"),
		NtpServer2:        types.StringValue("ntp2.example.com"),
		NtpServer3:        types.StringValue(""),
		NtpServer4:        types.StringValue(""),
		SettingPreference: types.StringValue("manual"),
	}

	got := normalizeSettingJSON(t, mustMarshal(t, r.ntpModelToSetting(model)))
	if got != goldenNtp {
		t.Errorf("ntp golden mismatch:\n got:  %s\n want: %s", got, goldenNtp)
	}
}

// --- syslog (list shape: contents []string) ---

const goldenSyslog = `{"contents":["device","client"],"debug":false,"enabled":true,"ip":"192.0.2.10","key":"","log_all_contents":false,"netconsole_enabled":false,"netconsole_port":6514,"port":514,"this_controller":true,"this_controller_encrypted_only":false}`

func TestGolden_syslog(t *testing.T) {
	ctx := context.Background()
	r := &settingResource{}

	contents, diags := types.ListValueFrom(ctx, types.StringType, []string{"device", "client"})
	if diags.HasError() {
		t.Fatalf("building contents list: %v", diags)
	}

	model := &settingSyslogModel{
		Enabled:                     types.BoolValue(true),
		Contents:                    contents,
		Debug:                       types.BoolValue(false),
		IP:                          types.StringValue("192.0.2.10"), // RFC 5737 TEST-NET-1
		Port:                        types.Int64Value(514),
		LogAllContents:              types.BoolValue(false),
		NetconsoleEnabled:           types.BoolValue(false),
		NetconsoleHost:              types.StringValue(""),
		NetconsolePort:              types.Int64Value(6514),
		ThisController:              types.BoolValue(true),
		ThisControllerEncryptedOnly: types.BoolValue(false),
	}

	var d diag.Diagnostics
	got := normalizeSettingJSON(t, mustMarshal(t, r.syslogModelToSetting(ctx, model, &d)))
	if d.HasError() {
		t.Fatalf("syslogModelToSetting diagnostics: %v", d)
	}
	if got != goldenSyslog {
		t.Errorf("syslog golden mismatch:\n got:  %s\n want: %s", got, goldenSyslog)
	}
}

// --- doh (list shape: server_names []string, custom_servers []object) ---

const goldenDoh = `{"custom_servers":[{"enabled":true,"sdns_stamp":"sdns://AQ","server_name":"test-doh-server"}],"key":"","server_names":["cloudflare","google"],"state":"custom"}`

func TestGolden_doh(t *testing.T) {
	ctx := context.Background()
	r := &settingResource{}

	serverNames, diags := types.ListValueFrom(ctx, types.StringType, []string{"cloudflare", "google"})
	if diags.HasError() {
		t.Fatalf("building server_names list: %v", diags)
	}
	customServers, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: dohCustomServerAttrTypes},
		[]settingDohCustomServerModel{{
			Enabled:    types.BoolValue(true),
			SDNSStamp:  types.StringValue("sdns://AQ"),
			ServerName: types.StringValue("test-doh-server"),
		}})
	if diags.HasError() {
		t.Fatalf("building custom_servers list: %v", diags)
	}

	model := &settingDohModel{
		State:         types.StringValue("custom"),
		ServerNames:   serverNames,
		CustomServers: customServers,
	}

	var d diag.Diagnostics
	got := normalizeSettingJSON(t, mustMarshal(t, r.dohModelToSetting(ctx, model, &d)))
	if d.HasError() {
		t.Fatalf("dohModelToSetting diagnostics: %v", d)
	}
	if got != goldenDoh {
		t.Errorf("doh golden mismatch:\n got:  %s\n want: %s", got, goldenDoh)
	}
}

// --- ips (list shape, with nested honeypot/suppression) ---

const goldenIps = `{"advanced_filtering_preference":"manual","content_filtering_blocking_page_enabled":true,"enabled_categories":["botcc","tor"],"enabled_networks":["net-a"],"honeypot":[{"ip_address":"192.0.2.20","network_id":"net-a","version":"v4"}],"honeypot_enabled":true,"ips_mode":"ips","key":"","memory_optimized":false,"restrict_torrents":true,"suppression":{"alerts":[{"category":"malware","gid":1,"id":2001,"signature":"ET MALWARE test signature","tracking":[{"direction":"both","mode":"ip","value":"192.0.2.30"}],"type":"track"}],"whitelist":[{"direction":"src","mode":"ip","value":"192.0.2.40"}]}}`

func TestGolden_ips(t *testing.T) {
	ctx := context.Background()
	r := &settingResource{}

	enabledCategories, diags := types.ListValueFrom(ctx, types.StringType, []string{"botcc", "tor"})
	if diags.HasError() {
		t.Fatalf("building enabled_categories: %v", diags)
	}
	enabledNetworks, diags := types.ListValueFrom(ctx, types.StringType, []string{"net-a"})
	if diags.HasError() {
		t.Fatalf("building enabled_networks: %v", diags)
	}
	honeypot, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ipsHoneypotAttrTypes},
		[]settingIpsHoneypotModel{{
			IPAddress: types.StringValue("192.0.2.20"),
			NetworkID: types.StringValue("net-a"),
			Version:   types.StringValue("v4"),
		}})
	if diags.HasError() {
		t.Fatalf("building honeypot: %v", diags)
	}
	tracking, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ipsTrackingAttrTypes},
		[]settingIpsTrackingModel{{
			Direction: types.StringValue("both"),
			Mode:      types.StringValue("ip"),
			Value:     types.StringValue("192.0.2.30"),
		}})
	if diags.HasError() {
		t.Fatalf("building tracking: %v", diags)
	}
	alerts, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ipsAlertAttrTypes},
		[]settingIpsAlertModel{{
			Category:  types.StringValue("malware"),
			Gid:       types.Int64Value(1),
			ID:        types.Int64Value(2001),
			Signature: types.StringValue("ET MALWARE test signature"),
			Type:      types.StringValue("track"),
			Tracking:  tracking,
		}})
	if diags.HasError() {
		t.Fatalf("building suppression_alerts: %v", diags)
	}
	whitelist, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: ipsWhitelistAttrTypes},
		[]settingIpsWhitelistModel{{
			Direction: types.StringValue("src"),
			Mode:      types.StringValue("ip"),
			Value:     types.StringValue("192.0.2.40"),
		}})
	if diags.HasError() {
		t.Fatalf("building suppression_whitelist: %v", diags)
	}

	model := &settingIpsModel{
		AdvancedFilteringPreference:         types.StringValue("manual"),
		ContentFilteringBlockingPageEnabled: types.BoolValue(true),
		EnabledCategories:                   enabledCategories,
		EnabledNetworks:                     enabledNetworks,
		Honeypot:                            honeypot,
		HoneypotEnabled:                     types.BoolValue(true),
		IPSMode:                             types.StringValue("ips"),
		MemoryOptimized:                     types.BoolValue(false),
		RestrictTorrents:                    types.BoolValue(true),
		SuppressionWhitelist:                whitelist,
		SuppressionAlerts:                   alerts,
	}

	var d diag.Diagnostics
	got := normalizeSettingJSON(t, mustMarshal(t, r.ipsModelToSetting(ctx, model, &d)))
	if d.HasError() {
		t.Fatalf("ipsModelToSetting diagnostics: %v", d)
	}
	if got != goldenIps {
		t.Errorf("ips golden mismatch:\n got:  %s\n want: %s", got, goldenIps)
	}
}

// --- mgmt (nested-list ssh_keys + secret ssh_password; RMW) ---
//
// RMW preservation: base carries AlertEnabled=true and BootSound=true, two
// fields the mgmt model does not expose at all (no schema attribute), plus
// LedEnabled=true, which the model also never sets in this representative
// case. Because mgmtModelToSetting starts from base and only overwrites
// fields the model explicitly sets, those three fields must survive
// unchanged into the golden — proving the legacy RMW-preservation behavior
// that Task 22 must reproduce.
const goldenMgmt = `{"advanced_feature_enabled":true,"alert_enabled":true,"auto_upgrade":true,"auto_upgrade_hour":3,"boot_sound":true,"debug_tools_enabled":false,"direct_connect_enabled":false,"key":"","led_enabled":true,"outdoor_mode_enabled":false,"unifi_idp_enabled":false,"wifiman_enabled":false,"x_ssh_auth_password_enabled":true,"x_ssh_bind_wildcard":false,"x_ssh_enabled":true,"x_ssh_keys":[{"comment":"test key","date":"","fingerprint":"","key":"ssh-ed25519 AAAATESTKEYMATERIAL test-key","name":"test-ssh-key","type":"ssh-ed25519"}],"x_ssh_password":"test-password","x_ssh_username":"testadmin"}`

func TestGolden_mgmt(t *testing.T) {
	ctx := context.Background()
	r := &settingResource{}

	sshKeys, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes},
		[]sshKeyModel{{
			Name:    types.StringValue("test-ssh-key"),
			Type:    types.StringValue("ssh-ed25519"),
			Key:     types.StringValue("ssh-ed25519 AAAATESTKEYMATERIAL test-key"),
			Comment: types.StringValue("test key"),
		}})
	if diags.HasError() {
		t.Fatalf("building ssh_keys: %v", diags)
	}

	model := &settingMgmtModel{
		AutoUpgrade:            types.BoolValue(true),
		AutoUpgradeHour:        types.Int64Value(3),
		SSHEnabled:             types.BoolValue(true),
		SSHKeys:                sshKeys,
		AdvancedFeatureEnabled: types.BoolValue(true),
		DebugToolsEnabled:      types.BoolValue(false),
		DirectConnectEnabled:   types.BoolValue(false),
		UnifiIdpEnabled:        types.BoolValue(false),
		WifimanEnabled:         types.BoolValue(false),
		SSHUsername:            types.StringValue("testadmin"),
		SSHPassword:            types.StringValue("test-password"),
		SSHAuthPasswordEnabled: types.BoolValue(true),
	}

	// base/current carries fields the model never sets (no schema attribute
	// for alert_enabled/boot_sound; led_enabled left unset by this model) so
	// the golden proves they are preserved through the RMW merge.
	base := &settings.Mgmt{
		AlertEnabled: true,
		BootSound:    true,
		LedEnabled:   true,
	}

	got := normalizeSettingJSON(t, mustMarshal(t, r.mgmtModelToSetting(ctx, model, base)))
	if got != goldenMgmt {
		t.Errorf("mgmt golden mismatch:\n got:  %s\n want: %s", got, goldenMgmt)
	}
}

// --- radius (scalar shape; RMW) ---
//
// RMW preservation: base carries ConfigureWholeNetwork=true and
// TunneledReply=true, two fields the radius model does not expose at all.
// The model only sets accounting_enabled/acct_port/secret, leaving
// auth_port and interim_update_interval null/zero on base too — so the
// golden shows both the model's overrides AND the preserved base fields.
const goldenRadius = `{"accounting_enabled":true,"acct_port":1813,"configure_whole_network":true,"enabled":false,"key":"","tunneled_reply":true,"x_secret":"test-radius-secret"}`

func TestGolden_radius(t *testing.T) {
	ctx := context.Background()
	r := &settingResource{}

	model := &settingRadiusModel{
		AccountingEnabled: types.BoolValue(true),
		AcctPort:          types.Int64Value(1813),
		Secret:            types.StringValue("test-radius-secret"),
		// AuthPort and InterimUpdateInterval intentionally left null: not
		// configured by this representative model.
		AuthPort:              types.Int64Null(),
		InterimUpdateInterval: timetypes.NewGoDurationNull(),
	}

	base := &settings.Radius{
		ConfigureWholeNetwork: true,
		TunneledReply:         true,
	}

	got := normalizeSettingJSON(t, mustMarshal(t, r.radiusModelToSetting(ctx, model, base)))
	if got != goldenRadius {
		t.Errorf("radius golden mismatch:\n got:  %s\n want: %s", got, goldenRadius)
	}
}

// --- usg (nested-object dns_verification) ---

const goldenUSG = `{"broadcast_ping":false,"dhcp_relay_agents_packets":"","dhcp_relay_server_1":"","dhcp_relay_server_2":"","dhcp_relay_server_3":"","dhcp_relay_server_4":"","dhcp_relay_server_5":"","dhcpd_hostfile_update":false,"dhcpd_use_dnsmasq":false,"dns_verification":{"domain":"example.com","primary_dns_server":"192.0.2.53","secondary_dns_server":"192.0.2.54","setting_preference":"manual"},"dnsmasq_all_servers":false,"ftp_module":true,"geo_ip_filtering_block":"block","geo_ip_filtering_countries":"US","geo_ip_filtering_enabled":true,"geo_ip_filtering_traffic_direction":"both","gre_module":false,"h323_module":false,"icmp_timeout":30,"key":"","lldp_enable_all":false,"mdns_enabled":false,"mss_clamp":"auto","offload_accounting":false,"offload_l2_blocking":false,"offload_sch":true,"other_timeout":600,"pptp_module":false,"receive_redirects":false,"send_redirects":true,"sip_module":true,"syn_cookies":true,"tcp_close_timeout":10,"tcp_close_wait_timeout":20,"tcp_established_timeout":3600,"tcp_fin_wait_timeout":120,"tcp_last_ack_timeout":30,"tcp_syn_recv_timeout":60,"tcp_syn_sent_timeout":120,"tcp_time_wait_timeout":120,"tftp_module":true,"timeout_setting_preference":"auto","udp_other_timeout":30,"udp_stream_timeout":180,"unbind_wan_monitors":false,"upnp_enabled":true,"upnp_nat_pmp_enabled":false,"upnp_secure_mode":true,"upnp_wan_interface":"WAN"}`

func TestGolden_usg(t *testing.T) {
	ctx := context.Background()
	r := &settingResource{}

	dnsVerif, diags := types.ObjectValueFrom(ctx, dnsVerificationAttrTypes(), dnsVerificationModel{
		Domain:             types.StringValue("example.com"),
		PrimaryDNSServer:   types.StringValue("192.0.2.53"),
		SecondaryDNSServer: types.StringValue("192.0.2.54"),
		SettingPreference:  types.StringValue("manual"),
	})
	if diags.HasError() {
		t.Fatalf("building dns_verification: %v", diags)
	}

	model := &settingUSGModel{
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

	got := normalizeSettingJSON(t, mustMarshal(t, r.usgModelToSetting(ctx, model)))
	if got != goldenUSG {
		t.Errorf("usg golden mismatch:\n got:  %s\n want: %s", got, goldenUSG)
	}
}

// --- igmp_snooping (list shape; RMW) ---
//
// RMW preservation: base carries QuerierMode="auto" and Switches populated,
// two fields the igmp_snooping model does not expose at all (advanced
// querier/flood fields are intentionally out of schema scope per the model's
// doc comment). The golden shows them surviving the merge untouched.
const goldenIgmpSnooping = `{"enabled":true,"flood_known_protocols":false,"forward_unknown_mcast_router_ports":false,"key":"","network_ids":["net-a","net-b"],"querier_mode":"auto","switches":["switch-1"]}`

func TestGolden_igmp_snooping(t *testing.T) {
	ctx := context.Background()
	r := &settingResource{}

	networkIDs, diags := types.ListValueFrom(ctx, types.StringType, []string{"net-a", "net-b"})
	if diags.HasError() {
		t.Fatalf("building network_ids: %v", diags)
	}

	model := &settingIgmpSnoopingModel{
		Enabled:    types.BoolValue(true),
		NetworkIDs: networkIDs,
	}

	base := &settings.IgmpSnooping{
		QuerierMode: "auto",
		Switches:    []string{"switch-1"},
	}

	var d diag.Diagnostics
	got := normalizeSettingJSON(t, mustMarshal(t, r.igmpSnoopingModelToSetting(ctx, model, base, &d)))
	if d.HasError() {
		t.Fatalf("igmpSnoopingModelToSetting diagnostics: %v", d)
	}
	if got != goldenIgmpSnooping {
		t.Errorf("igmp_snooping golden mismatch:\n got:  %s\n want: %s", got, goldenIgmpSnooping)
	}
}

// dnsVerificationAttrTypes mirrors the attr.Type map readSettings (see
// unifi/setting_resource.go's usgSettingToModel call site) uses for the
// usg.dns_verification nested object. Duplicated here (rather than reusing a
// package-level var) because the production code builds this map inline at
// its two call sites instead of exporting a shared package-level map.
func dnsVerificationAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"domain":               types.StringType,
		"primary_dns_server":   types.StringType,
		"secondary_dns_server": types.StringType,
		"setting_preference":   types.StringType,
	}
}
