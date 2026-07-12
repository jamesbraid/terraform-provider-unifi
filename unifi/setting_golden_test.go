package unifi

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// This file is the MIGRATION ORACLE for the settings-expansion project
// (PR-A Task 9). For each of the 13 legacy settings sections it pins the exact
// JSON PUT body a representative model must produce, as a checked-in golden
// string constant.
//
// Task 24c cut over the SOLE PRODUCER of those bodies from the (now-deleted)
// legacy per-section converters to each section's settingSection.overlay().
// The golden CONSTANTS are immutable — they remain the expected bodies; the
// section overlays are now their only producer. Every TestGolden_<section>
// below drives its section's overlay() and asserts the result matches the
// constant via assertPUTBodyMatchesGolden (which strips the routing "key"
// field: overlay() sets RawSetting.Key=<section> while the legacy constants
// carry "key":"").
//
// After the 24c repoint each TestGolden_<section> is essentially identical to
// the section's own Test<Xxx>Section_GoldenReproduction (in
// setting_section_<x>_test.go), from which its representative model / seeded
// RMW snapshot base was copied. This centralized golden file remains the
// oracle and TestMigrationInventoryCoversAllSections enforces (via go/parser)
// that a TestGolden_<section> exists for every one of the 13 sections — so
// these funcs must not be deleted, only kept in sync with their constants.

// --- auto_speedtest (scalar) ---

const goldenAutoSpeedtest = `{"cron_expr":"0 0 * * *","enabled":true,"key":""}`

func TestGolden_auto_speedtest(t *testing.T) {
	ctx := context.Background()
	m := settingAutoSpeedtestModel{
		Enabled:  types.BoolValue(true),
		CronExpr: types.StringValue("0 0 * * *"),
	}
	obj, diags := types.ObjectValueFrom(ctx, autoSpeedtestAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building auto_speedtest object: %v", diags)
	}
	rs, _, oDiags := autoSpeedtestSection{}.overlay(ctx, settingResourceModel{AutoSpeedtest: obj}, settingResourceModel{}, newRawSettings(nil))
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenAutoSpeedtest)
}

// --- country (scalar) ---

const goldenCountry = `{"code":840,"key":""}`

func TestGolden_country(t *testing.T) {
	ctx := context.Background()
	m := settingCountryModel{
		Code: types.Int64Value(840), // ISO 3166-1 numeric for US; neutral example code
	}
	obj, diags := types.ObjectValueFrom(ctx, countryAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building country object: %v", diags)
	}
	rs, _, oDiags := countrySection{}.overlay(ctx, settingResourceModel{Country: obj}, settingResourceModel{}, newRawSettings(nil))
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenCountry)
}

// --- dpi (scalar) ---

const goldenDpi = `{"enabled":true,"fingerprintingEnabled":false,"key":""}`

func TestGolden_dpi(t *testing.T) {
	ctx := context.Background()
	m := settingDpiModel{
		Enabled:               types.BoolValue(true),
		FingerprintingEnabled: types.BoolValue(false),
	}
	obj, diags := types.ObjectValueFrom(ctx, dpiAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building dpi object: %v", diags)
	}
	rs, _, oDiags := dpiSection{}.overlay(ctx, settingResourceModel{Dpi: obj}, settingResourceModel{}, newRawSettings(nil))
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenDpi)
}

// --- lcm (scalar, with optional int pointers) ---

const goldenLcm = `{"brightness":50,"enabled":true,"idle_timeout":300,"key":"","sync":true,"touch_event":false}`

func TestGolden_lcm(t *testing.T) {
	ctx := context.Background()
	m := settingLcmModel{
		Enabled:     types.BoolValue(true),
		Brightness:  types.Int64Value(50),
		IdleTimeout: types.Int64Value(300),
		Sync:        types.BoolValue(true),
		TouchEvent:  types.BoolValue(false),
	}
	obj, diags := types.ObjectValueFrom(ctx, lcmAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building lcm object: %v", diags)
	}
	rs, _, oDiags := lcmSection{}.overlay(ctx, settingResourceModel{Lcm: obj}, settingResourceModel{}, newRawSettings(nil))
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenLcm)
}

// --- network_optimization (scalar) ---

const goldenNetworkOptimization = `{"enabled":true,"key":""}`

func TestGolden_network_optimization(t *testing.T) {
	ctx := context.Background()
	m := settingNetworkOptimizationModel{
		Enabled: types.BoolValue(true),
	}
	obj, diags := types.ObjectValueFrom(ctx, networkOptimizationAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building network_optimization object: %v", diags)
	}
	rs, _, oDiags := networkOptimizationSection{}.overlay(ctx, settingResourceModel{NetworkOpt: obj}, settingResourceModel{}, newRawSettings(nil))
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenNetworkOptimization)
}

// --- ntp (scalar) ---

const goldenNtp = `{"key":"","ntp_server_1":"ntp1.example.com","ntp_server_2":"ntp2.example.com","setting_preference":"manual"}`

func TestGolden_ntp(t *testing.T) {
	ctx := context.Background()
	m := settingNtpModel{
		NtpServer1:        types.StringValue("ntp1.example.com"),
		NtpServer2:        types.StringValue("ntp2.example.com"),
		NtpServer3:        types.StringNull(),
		NtpServer4:        types.StringNull(),
		SettingPreference: types.StringValue("manual"),
	}
	obj, diags := types.ObjectValueFrom(ctx, ntpAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building ntp object: %v", diags)
	}
	rs, _, oDiags := ntpSection{}.overlay(ctx, settingResourceModel{Ntp: obj}, settingResourceModel{}, newRawSettings(nil))
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenNtp)
}

// --- syslog (list shape: contents []string) ---

const goldenSyslog = `{"contents":["device","client"],"debug":false,"enabled":true,"ip":"192.0.2.10","key":"","log_all_contents":false,"netconsole_enabled":false,"netconsole_port":6514,"port":514,"this_controller":true,"this_controller_encrypted_only":false}`

func TestGolden_syslog(t *testing.T) {
	ctx := context.Background()

	contents, diags := types.ListValueFrom(ctx, types.StringType, []string{"device", "client"})
	if diags.HasError() {
		t.Fatalf("building contents list: %v", diags)
	}

	m := settingSyslogModel{
		Enabled:                     types.BoolValue(true),
		Contents:                    contents,
		Debug:                       types.BoolValue(false),
		IP:                          types.StringValue("192.0.2.10"), // RFC 5737 TEST-NET-1
		Port:                        types.Int64Value(514),
		LogAllContents:              types.BoolValue(false),
		NetconsoleEnabled:           types.BoolValue(false),
		NetconsoleHost:              types.StringNull(),
		NetconsolePort:              types.Int64Value(6514),
		ThisController:              types.BoolValue(true),
		ThisControllerEncryptedOnly: types.BoolValue(false),
	}
	obj, objDiags := types.ObjectValueFrom(ctx, syslogAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building syslog object: %v", objDiags)
	}
	rs, _, oDiags := syslogSection{}.overlay(ctx, settingResourceModel{Syslog: obj}, settingResourceModel{}, newRawSettings(nil))
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenSyslog)
}

// --- doh (list shape: server_names []string, custom_servers []object) ---

const goldenDoh = `{"custom_servers":[{"enabled":true,"sdns_stamp":"sdns://AQ","server_name":"test-doh-server"}],"key":"","server_names":["cloudflare","google"],"state":"custom"}`

func TestGolden_doh(t *testing.T) {
	ctx := context.Background()

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

	m := settingDohModel{
		State:         types.StringValue("custom"),
		ServerNames:   serverNames,
		CustomServers: customServers,
	}
	obj, objDiags := types.ObjectValueFrom(ctx, dohAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building doh object: %v", objDiags)
	}
	rs, _, oDiags := dohSection{}.overlay(ctx, settingResourceModel{Doh: obj}, settingResourceModel{}, newRawSettings(nil))
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenDoh)
}

// --- ips (list shape, with nested honeypot/suppression) ---

const goldenIps = `{"advanced_filtering_preference":"manual","content_filtering_blocking_page_enabled":true,"enabled_categories":["botcc","tor"],"enabled_networks":["net-a"],"honeypot":[{"ip_address":"192.0.2.20","network_id":"net-a","version":"v4"}],"honeypot_enabled":true,"ips_mode":"ips","key":"","memory_optimized":false,"restrict_torrents":true,"suppression":{"alerts":[{"category":"malware","gid":1,"id":2001,"signature":"ET MALWARE test signature","tracking":[{"direction":"both","mode":"ip","value":"192.0.2.30"}],"type":"track"}],"whitelist":[{"direction":"src","mode":"ip","value":"192.0.2.40"}]}}`

func TestGolden_ips(t *testing.T) {
	ctx := context.Background()

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

	m := settingIpsModel{
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
	obj, objDiags := types.ObjectValueFrom(ctx, ipsAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building ips object: %v", objDiags)
	}
	rs, _, oDiags := ipsSection{}.overlay(ctx, settingResourceModel{Ips: obj}, settingResourceModel{}, newRawSettings(nil))
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenIps)
}

// --- mgmt (nested-list ssh_keys + secret ssh_password; RMW) ---
//
// RMW preservation: the seeded snapshot base carries alert_enabled=true and
// boot_sound=true, two fields the mgmt model does not expose at all (no schema
// attribute), plus led_enabled=true, which the model also never sets in this
// representative case. Because overlay() starts from the snapshot and only
// overwrites fields the model explicitly sets, those three fields must survive
// unchanged into the golden — proving the RMW-preservation behavior. The
// snapshot base mirrors TestMgmtSection_GoldenReproduction.
const goldenMgmt = `{"advanced_feature_enabled":true,"alert_enabled":true,"auto_upgrade":true,"auto_upgrade_hour":3,"boot_sound":true,"debug_tools_enabled":false,"direct_connect_enabled":false,"key":"","led_enabled":true,"outdoor_mode_enabled":false,"unifi_idp_enabled":false,"wifiman_enabled":false,"x_ssh_auth_password_enabled":true,"x_ssh_bind_wildcard":false,"x_ssh_enabled":true,"x_ssh_keys":[{"comment":"test key","date":"","fingerprint":"","key":"ssh-ed25519 AAAATESTKEYMATERIAL test-key","name":"test-ssh-key","type":"ssh-ed25519"}],"x_ssh_password":"test-password","x_ssh_username":"testadmin"}`

func TestGolden_mgmt(t *testing.T) {
	ctx := context.Background()

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

	m := settingMgmtModel{
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
	obj, objDiags := types.ObjectValueFrom(ctx, mgmtAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building mgmt object: %v", objDiags)
	}

	// Seeded snapshot base carries fields the model never sets (no schema
	// attribute for alert_enabled/boot_sound; led_enabled left unset by this
	// model) so the golden proves they are preserved through the RMW merge.
	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "mgmt"},
		Data: map[string]any{
			"alert_enabled":        true,
			"boot_sound":           true,
			"led_enabled":          true,
			"outdoor_mode_enabled": false,
			"x_ssh_bind_wildcard":  false,
			"x_ssh_keys": []any{
				map[string]any{"date": "", "fingerprint": ""},
			},
		},
	}})

	rs, _, oDiags := mgmtSection{}.overlay(ctx, settingResourceModel{Mgmt: obj}, settingResourceModel{}, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenMgmt)
}

// --- radius (scalar shape; RMW) ---
//
// RMW preservation: the seeded snapshot base carries configure_whole_network=
// true and tunneled_reply=true, two fields the radius model does not expose at
// all. The model only sets accounting_enabled/acct_port/secret, leaving
// auth_port and interim_update_interval null on the model — so the golden shows
// both the model's overrides AND the preserved base fields. Snapshot base
// mirrors TestRadiusSection_GoldenReproduction.
const goldenRadius = `{"accounting_enabled":true,"acct_port":1813,"configure_whole_network":true,"enabled":false,"key":"","tunneled_reply":true,"x_secret":"test-radius-secret"}`

func TestGolden_radius(t *testing.T) {
	ctx := context.Background()

	m := settingRadiusModel{
		AccountingEnabled: types.BoolValue(true),
		AcctPort:          types.Int64Value(1813),
		Secret:            types.StringValue("test-radius-secret"),
		// AuthPort and InterimUpdateInterval intentionally left null: not
		// configured by this representative model.
		AuthPort:              types.Int64Null(),
		InterimUpdateInterval: timetypes.NewGoDurationNull(),
	}
	obj, diags := types.ObjectValueFrom(ctx, radiusAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building radius object: %v", diags)
	}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "radius"},
		Data: map[string]any{
			"configure_whole_network": true,
			"tunneled_reply":          true,
			"enabled":                 false,
		},
	}})

	rs, _, oDiags := radiusSection{}.overlay(ctx, settingResourceModel{Radius: obj}, settingResourceModel{}, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenRadius)
}

// --- usg (nested-object dns_verification; RMW) ---

const goldenUSG = `{"broadcast_ping":false,"dhcp_relay_agents_packets":"","dhcp_relay_server_1":"","dhcp_relay_server_2":"","dhcp_relay_server_3":"","dhcp_relay_server_4":"","dhcp_relay_server_5":"","dhcpd_hostfile_update":false,"dhcpd_use_dnsmasq":false,"dns_verification":{"domain":"example.com","primary_dns_server":"192.0.2.53","secondary_dns_server":"192.0.2.54","setting_preference":"manual"},"dnsmasq_all_servers":false,"ftp_module":true,"geo_ip_filtering_block":"block","geo_ip_filtering_countries":"US","geo_ip_filtering_enabled":true,"geo_ip_filtering_traffic_direction":"both","gre_module":false,"h323_module":false,"icmp_timeout":30,"key":"","lldp_enable_all":false,"mdns_enabled":false,"mss_clamp":"auto","offload_accounting":false,"offload_l2_blocking":false,"offload_sch":true,"other_timeout":600,"pptp_module":false,"receive_redirects":false,"send_redirects":true,"sip_module":true,"syn_cookies":true,"tcp_close_timeout":10,"tcp_close_wait_timeout":20,"tcp_established_timeout":3600,"tcp_fin_wait_timeout":120,"tcp_last_ack_timeout":30,"tcp_syn_recv_timeout":60,"tcp_syn_sent_timeout":120,"tcp_time_wait_timeout":120,"tftp_module":true,"timeout_setting_preference":"auto","udp_other_timeout":30,"udp_stream_timeout":180,"unbind_wan_monitors":false,"upnp_enabled":true,"upnp_nat_pmp_enabled":false,"upnp_secure_mode":true,"upnp_wan_interface":"WAN"}`

func TestGolden_usg(t *testing.T) {
	ctx := context.Background()

	dnsVerif, diags := types.ObjectValueFrom(ctx, usgDNSVerificationAttrTypes, dnsVerificationModel{
		Domain:             types.StringValue("example.com"),
		PrimaryDNSServer:   types.StringValue("192.0.2.53"),
		SecondaryDNSServer: types.StringValue("192.0.2.54"),
		SettingPreference:  types.StringValue("manual"),
	})
	if diags.HasError() {
		t.Fatalf("building dns_verification: %v", diags)
	}

	m := settingUSGModel{
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
	obj, objDiags := types.ObjectValueFrom(ctx, usgAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building usg object: %v", objDiags)
	}

	// Seeded snapshot base carries the always-emitted-but-unmodeled scalar
	// fields (dhcp relay / dnsmasq / lldp / mdns) the golden expects, mirroring
	// TestUsgSection_GoldenReproduction.
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

	rs, _, oDiags := usgSection{}.overlay(ctx, settingResourceModel{USG: obj}, settingResourceModel{}, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenUSG)
}

// --- igmp_snooping (list shape; RMW) ---
//
// RMW preservation: the seeded snapshot base carries querier_mode="auto" and
// switches populated, two fields the igmp_snooping model does not expose at all
// (advanced querier/flood fields are intentionally out of schema scope per the
// model's doc comment). The golden shows them surviving the merge untouched.
// Snapshot base mirrors TestIgmpSnoopingSection_GoldenReproduction.
const goldenIgmpSnooping = `{"enabled":true,"flood_known_protocols":false,"forward_unknown_mcast_router_ports":false,"key":"","network_ids":["net-a","net-b"],"querier_mode":"auto","switches":["switch-1"]}`

func TestGolden_igmp_snooping(t *testing.T) {
	ctx := context.Background()

	networkIDs, diags := types.ListValueFrom(ctx, types.StringType, []string{"net-a", "net-b"})
	if diags.HasError() {
		t.Fatalf("building network_ids: %v", diags)
	}

	m := settingIgmpSnoopingModel{
		Enabled:    types.BoolValue(true),
		NetworkIDs: networkIDs,
	}
	obj, objDiags := types.ObjectValueFrom(ctx, igmpSnoopingAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building igmp_snooping object: %v", objDiags)
	}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "igmp_snooping"},
		Data: map[string]any{
			"querier_mode":                       "auto",
			"switches":                           []any{"switch-1"},
			"flood_known_protocols":              false,
			"forward_unknown_mcast_router_ports": false,
		},
	}})

	rs, _, oDiags := igmpSnoopingSection{}.overlay(ctx, settingResourceModel{IgmpSnooping: obj}, settingResourceModel{}, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay: %v", oDiags)
	}
	assertPUTBodyMatchesGolden(t, rs, goldenIgmpSnooping)
}
