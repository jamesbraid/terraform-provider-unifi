package unifi

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// usgDNSVerificationAttrTypes is the object attribute-type map for
// dnsVerificationModel, the nested "dns_verification" child object of
// settingUSGModel. Named with a "usg" prefix (rather than the bare
// "dnsVerificationAttrTypes" the brief suggests) because
// setting_golden_test.go already declares a package-level FUNCTION of that
// exact name for its own golden-capture use; a package-level var of the
// same identifier would collide with it. That test file is out of this
// task's scope to modify, so this section defines its own.
var usgDNSVerificationAttrTypes = map[string]attr.Type{
	"domain":               types.StringType,
	"primary_dns_server":   types.StringType,
	"secondary_dns_server": types.StringType,
	"setting_preference":   types.StringType,
}

// usgAttrTypes is the object attribute-type map for settingUSGModel. There
// is no pre-existing package-level var for this section (unlike earlier
// sections): this map matches the inline one built in setting_resource.go's
// Update path (usgModelToSetting/usgSettingToModel call sites,
// setting_resource.go:2183-2230).
var usgAttrTypes = map[string]attr.Type{
	"broadcast_ping":                     types.BoolType,
	"dns_verification":                   types.ObjectType{AttrTypes: usgDNSVerificationAttrTypes},
	"ftp_module":                         types.BoolType,
	"geo_ip_filtering_block":             types.StringType,
	"geo_ip_filtering_countries":         types.StringType,
	"geo_ip_filtering_enabled":           types.BoolType,
	"geo_ip_filtering_traffic_direction": types.StringType,
	"gre_module":                         types.BoolType,
	"h323_module":                        types.BoolType,
	"icmp_timeout":                       timetypes.GoDurationType{},
	"mss_clamp":                          types.StringType,
	"offload_accounting":                 types.BoolType,
	"offload_l2_blocking":                types.BoolType,
	"offload_sch":                        types.BoolType,
	"other_timeout":                      timetypes.GoDurationType{},
	"pptp_module":                        types.BoolType,
	"receive_redirects":                  types.BoolType,
	"send_redirects":                     types.BoolType,
	"sip_module":                         types.BoolType,
	"syn_cookies":                        types.BoolType,
	"tcp_close_timeout":                  timetypes.GoDurationType{},
	"tcp_close_wait_timeout":             timetypes.GoDurationType{},
	"tcp_established_timeout":            timetypes.GoDurationType{},
	"tcp_fin_wait_timeout":               timetypes.GoDurationType{},
	"tcp_last_ack_timeout":               timetypes.GoDurationType{},
	"tcp_syn_recv_timeout":               timetypes.GoDurationType{},
	"tcp_syn_sent_timeout":               timetypes.GoDurationType{},
	"tcp_time_wait_timeout":              timetypes.GoDurationType{},
	"tftp_module":                        types.BoolType,
	"timeout_setting_preference":         types.StringType,
	"udp_other_timeout":                  timetypes.GoDurationType{},
	"udp_stream_timeout":                 timetypes.GoDurationType{},
	"unbind_wan_monitors":                types.BoolType,
	"upnp_enabled":                       types.BoolType,
	"upnp_nat_pmp_enabled":               types.BoolType,
	"upnp_secure_mode":                   types.BoolType,
	"upnp_wan_interface":                 types.StringType,
}

// usgSection is the settingSection implementation for the "usg" settings
// section — the largest section: 37 modeled leaves (18 bool, 6 string, 12
// GoDuration, plus one nested SingleNestedAttribute "dns_verification" with
// 4 string children) and 11 unmodeled always-present fields
// (dhcp_relay_agents_packets, dhcp_relay_server_1..5,
// dhcpd_hostfile_update, dhcpd_use_dnsmasq, dnsmasq_all_servers,
// lldp_enable_all, mdns_enabled) preserved via read-modify-write (RMW) from
// the snapshot's existing section data, matching the igmp_snooping/radius
// RMW pattern. It is the nested-object (SingleNestedAttribute) worked
// template: dns_verification is decoded/overlaid via the generalized
// decodeObject/overlayObject codec helpers, and the 12 conntrack timeout
// leaves use the GoDuration codec helpers with unit = time.Second. All wire
// keys equal their tfsdk names (no remaps); the section has no secret
// leaves, so carryBestEffort is a trivial plan copy.
type usgSection struct{}

func init() {
	registerSection(usgSection{})
}

func (usgSection) key() string      { return "usg" }
func (usgSection) attrName() string { return "usg" }

// schemaAttribute is byte-identical to the inline "usg" block in
// setting_resource.go's schema (setting_resource.go:1018-1244).
func (usgSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "USG settings.",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"broadcast_ping": schema.BoolAttribute{
				MarkdownDescription: "Enable broadcast ping.",
				Optional:            true,
				Computed:            true,
			},
			"dns_verification": schema.SingleNestedAttribute{
				MarkdownDescription: "DNS verification settings.",
				Optional:            true,
				Computed:            true,
				Attributes: map[string]schema.Attribute{
					"domain": schema.StringAttribute{
						MarkdownDescription: "Domain for DNS verification.",
						Optional:            true,
						Computed:            true,
					},
					"primary_dns_server": schema.StringAttribute{
						MarkdownDescription: "Primary DNS server.",
						Optional:            true,
						Computed:            true,
					},
					"secondary_dns_server": schema.StringAttribute{
						MarkdownDescription: "Secondary DNS server.",
						Optional:            true,
						Computed:            true,
					},
					"setting_preference": schema.StringAttribute{
						MarkdownDescription: "Setting preference: auto or manual.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"ftp_module": schema.BoolAttribute{
				MarkdownDescription: "Enable FTP module.",
				Optional:            true,
				Computed:            true,
			},
			"geo_ip_filtering_block": schema.StringAttribute{
				MarkdownDescription: "Geo IP filtering action: block or allow.",
				Optional:            true,
				Computed:            true,
			},
			"geo_ip_filtering_countries": schema.StringAttribute{
				MarkdownDescription: "Comma-separated list of country codes for geo IP filtering.",
				Optional:            true,
				Computed:            true,
			},
			"geo_ip_filtering_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable geo IP filtering.",
				Optional:            true,
				Computed:            true,
			},
			"geo_ip_filtering_traffic_direction": schema.StringAttribute{
				MarkdownDescription: "Geo IP filtering traffic direction: both, ingress, or egress.",
				Optional:            true,
				Computed:            true,
			},
			"gre_module": schema.BoolAttribute{
				MarkdownDescription: "Enable GRE module.",
				Optional:            true,
				Computed:            true,
			},
			"h323_module": schema.BoolAttribute{
				MarkdownDescription: "Enable H.323 module.",
				Optional:            true,
				Computed:            true,
			},
			"icmp_timeout": schema.StringAttribute{
				MarkdownDescription: "ICMP connection timeout, as a Go duration string (e.g. `30s`, `1m`).",
				CustomType:          timetypes.GoDurationType{},
				Optional:            true,
				Computed:            true,
			},
			"mss_clamp": schema.StringAttribute{
				MarkdownDescription: "MSS clamping mode: auto, custom, or disabled.",
				Optional:            true,
				Computed:            true,
			},
			"offload_accounting": schema.BoolAttribute{
				MarkdownDescription: "Enable hardware offload for accounting.",
				Optional:            true,
				Computed:            true,
			},
			"offload_l2_blocking": schema.BoolAttribute{
				MarkdownDescription: "Enable hardware offload for L2 blocking.",
				Optional:            true,
				Computed:            true,
			},
			"offload_sch": schema.BoolAttribute{
				MarkdownDescription: "Enable hardware offload for scheduling.",
				Optional:            true,
				Computed:            true,
			},
			"other_timeout": schema.StringAttribute{
				MarkdownDescription: "Other connections timeout, as a Go duration string (e.g. `600s`, `10m`).",
				CustomType:          timetypes.GoDurationType{},
				Optional:            true,
				Computed:            true,
			},
			"pptp_module": schema.BoolAttribute{
				MarkdownDescription: "Enable PPTP module.",
				Optional:            true,
				Computed:            true,
			},
			"receive_redirects": schema.BoolAttribute{
				MarkdownDescription: "Accept ICMP redirects.",
				Optional:            true,
				Computed:            true,
			},
			"send_redirects": schema.BoolAttribute{
				MarkdownDescription: "Send ICMP redirects.",
				Optional:            true,
				Computed:            true,
			},
			"sip_module": schema.BoolAttribute{
				MarkdownDescription: "Enable SIP module.",
				Optional:            true,
				Computed:            true,
			},
			"syn_cookies": schema.BoolAttribute{
				MarkdownDescription: "Enable SYN cookies.",
				Optional:            true,
				Computed:            true,
			},
			"tcp_close_timeout": schema.StringAttribute{
				MarkdownDescription: "TCP close timeout, as a Go duration string (e.g. `10s`).",
				CustomType:          timetypes.GoDurationType{},
				Optional:            true,
				Computed:            true,
			},
			"tcp_close_wait_timeout": schema.StringAttribute{
				MarkdownDescription: "TCP close wait timeout, as a Go duration string (e.g. `60s`, `1m`).",
				CustomType:          timetypes.GoDurationType{},
				Optional:            true,
				Computed:            true,
			},
			"tcp_established_timeout": schema.StringAttribute{
				MarkdownDescription: "TCP established connection timeout, as a Go duration string (e.g. `7440s`, `2h4m`).",
				CustomType:          timetypes.GoDurationType{},
				Optional:            true,
				Computed:            true,
			},
			"tcp_fin_wait_timeout": schema.StringAttribute{
				MarkdownDescription: "TCP fin wait timeout, as a Go duration string (e.g. `120s`, `2m`).",
				CustomType:          timetypes.GoDurationType{},
				Optional:            true,
				Computed:            true,
			},
			"tcp_last_ack_timeout": schema.StringAttribute{
				MarkdownDescription: "TCP last ACK timeout, as a Go duration string (e.g. `30s`).",
				CustomType:          timetypes.GoDurationType{},
				Optional:            true,
				Computed:            true,
			},
			"tcp_syn_recv_timeout": schema.StringAttribute{
				MarkdownDescription: "TCP SYN received timeout, as a Go duration string (e.g. `60s`, `1m`).",
				CustomType:          timetypes.GoDurationType{},
				Optional:            true,
				Computed:            true,
			},
			"tcp_syn_sent_timeout": schema.StringAttribute{
				MarkdownDescription: "TCP SYN sent timeout, as a Go duration string (e.g. `120s`, `2m`).",
				CustomType:          timetypes.GoDurationType{},
				Optional:            true,
				Computed:            true,
			},
			"tcp_time_wait_timeout": schema.StringAttribute{
				MarkdownDescription: "TCP time wait timeout, as a Go duration string (e.g. `120s`, `2m`).",
				CustomType:          timetypes.GoDurationType{},
				Optional:            true,
				Computed:            true,
			},
			"tftp_module": schema.BoolAttribute{
				MarkdownDescription: "Enable TFTP module.",
				Optional:            true,
				Computed:            true,
			},
			"timeout_setting_preference": schema.StringAttribute{
				MarkdownDescription: "Timeout setting preference: auto or manual.",
				Optional:            true,
				Computed:            true,
			},
			"udp_other_timeout": schema.StringAttribute{
				MarkdownDescription: "UDP other timeout, as a Go duration string (e.g. `30s`).",
				CustomType:          timetypes.GoDurationType{},
				Optional:            true,
				Computed:            true,
			},
			"udp_stream_timeout": schema.StringAttribute{
				MarkdownDescription: "UDP stream timeout, as a Go duration string (e.g. `180s`, `3m`).",
				CustomType:          timetypes.GoDurationType{},
				Optional:            true,
				Computed:            true,
			},
			"unbind_wan_monitors": schema.BoolAttribute{
				MarkdownDescription: "Unbind WAN monitors.",
				Optional:            true,
				Computed:            true,
			},
			"upnp_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable UPnP.",
				Optional:            true,
				Computed:            true,
			},
			"upnp_nat_pmp_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable UPnP NAT-PMP.",
				Optional:            true,
				Computed:            true,
			},
			"upnp_secure_mode": schema.BoolAttribute{
				MarkdownDescription: "Enable UPnP secure mode.",
				Optional:            true,
				Computed:            true,
			},
			"upnp_wan_interface": schema.StringAttribute{
				MarkdownDescription: "UPnP WAN interface (e.g., WAN, WAN2).",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

// decode populates model.USG from snap's "usg" section data. Every leaf
// reads from the API; wire keys equal tfsdk names 1:1 (no remaps). The 12
// conntrack timeout leaves use decodeGoDuration with unit = time.Second; the
// nested dns_verification object uses the generalized decodeObject helper,
// which recurses into its 4 string children.
func (s usgSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingUSGModel
	if !prior.USG.IsNull() && !prior.USG.IsUnknown() {
		diags.Append(prior.USG.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	broadcastPing, d := decodeBool(data, "broadcast_ping", priorModel.BroadcastPing)
	diags.Append(d...)
	ftpModule, d := decodeBool(data, "ftp_module", priorModel.FtpModule)
	diags.Append(d...)
	geoIPFilteringEnabled, d := decodeBool(data, "geo_ip_filtering_enabled", priorModel.GeoIPFilteringEnabled)
	diags.Append(d...)
	greModule, d := decodeBool(data, "gre_module", priorModel.GreModule)
	diags.Append(d...)
	h323Module, d := decodeBool(data, "h323_module", priorModel.H323Module)
	diags.Append(d...)
	offloadAccounting, d := decodeBool(data, "offload_accounting", priorModel.OffloadAccounting)
	diags.Append(d...)
	offloadL2Blocking, d := decodeBool(data, "offload_l2_blocking", priorModel.OffloadL2Blocking)
	diags.Append(d...)
	offloadSch, d := decodeBool(data, "offload_sch", priorModel.OffloadSch)
	diags.Append(d...)
	pptpModule, d := decodeBool(data, "pptp_module", priorModel.PptpModule)
	diags.Append(d...)
	receiveRedirects, d := decodeBool(data, "receive_redirects", priorModel.ReceiveRedirects)
	diags.Append(d...)
	sendRedirects, d := decodeBool(data, "send_redirects", priorModel.SendRedirects)
	diags.Append(d...)
	sipModule, d := decodeBool(data, "sip_module", priorModel.SipModule)
	diags.Append(d...)
	synCookies, d := decodeBool(data, "syn_cookies", priorModel.SynCookies)
	diags.Append(d...)
	tftpModule, d := decodeBool(data, "tftp_module", priorModel.TFTPModule)
	diags.Append(d...)
	unbindWANMonitors, d := decodeBool(data, "unbind_wan_monitors", priorModel.UnbindWANMonitors)
	diags.Append(d...)
	upnpEnabled, d := decodeBool(data, "upnp_enabled", priorModel.UPnPEnabled)
	diags.Append(d...)
	upnpNATPmpEnabled, d := decodeBool(data, "upnp_nat_pmp_enabled", priorModel.UPnPNATPmpEnabled)
	diags.Append(d...)
	upnpSecureMode, d := decodeBool(data, "upnp_secure_mode", priorModel.UPnPSecureMode)
	diags.Append(d...)

	geoIPFilteringBlock, d := decodeString(data, "geo_ip_filtering_block", priorModel.GeoIPFilteringBlock)
	diags.Append(d...)
	geoIPFilteringCountries, d := decodeString(data, "geo_ip_filtering_countries", priorModel.GeoIPFilteringCountries)
	diags.Append(d...)
	geoIPFilteringTrafficDirection, d := decodeString(data, "geo_ip_filtering_traffic_direction", priorModel.GeoIPFilteringTrafficDirection)
	diags.Append(d...)
	mssClamp, d := decodeString(data, "mss_clamp", priorModel.MssClamp)
	diags.Append(d...)
	timeoutSettingPreference, d := decodeString(data, "timeout_setting_preference", priorModel.TimeoutSettingPreference)
	diags.Append(d...)
	upnpWANInterface, d := decodeString(data, "upnp_wan_interface", priorModel.UPnPWANInterface)
	diags.Append(d...)

	icmpTimeout, d := decodeGoDuration(data, "icmp_timeout", priorModel.ICMPTimeout, time.Second)
	diags.Append(d...)
	otherTimeout, d := decodeGoDuration(data, "other_timeout", priorModel.OtherTimeout, time.Second)
	diags.Append(d...)
	tcpCloseTimeout, d := decodeGoDuration(data, "tcp_close_timeout", priorModel.TCPCloseTimeout, time.Second)
	diags.Append(d...)
	tcpCloseWaitTimeout, d := decodeGoDuration(data, "tcp_close_wait_timeout", priorModel.TCPCloseWaitTimeout, time.Second)
	diags.Append(d...)
	tcpEstablishedTimeout, d := decodeGoDuration(data, "tcp_established_timeout", priorModel.TCPEstablishedTimeout, time.Second)
	diags.Append(d...)
	tcpFinWaitTimeout, d := decodeGoDuration(data, "tcp_fin_wait_timeout", priorModel.TCPFinWaitTimeout, time.Second)
	diags.Append(d...)
	tcpLastAckTimeout, d := decodeGoDuration(data, "tcp_last_ack_timeout", priorModel.TCPLastAckTimeout, time.Second)
	diags.Append(d...)
	tcpSynRecvTimeout, d := decodeGoDuration(data, "tcp_syn_recv_timeout", priorModel.TCPSynRecvTimeout, time.Second)
	diags.Append(d...)
	tcpSynSentTimeout, d := decodeGoDuration(data, "tcp_syn_sent_timeout", priorModel.TCPSynSentTimeout, time.Second)
	diags.Append(d...)
	tcpTimeWaitTimeout, d := decodeGoDuration(data, "tcp_time_wait_timeout", priorModel.TCPTimeWaitTimeout, time.Second)
	diags.Append(d...)
	udpOtherTimeout, d := decodeGoDuration(data, "udp_other_timeout", priorModel.UDPOtherTimeout, time.Second)
	diags.Append(d...)
	udpStreamTimeout, d := decodeGoDuration(data, "udp_stream_timeout", priorModel.UDPStreamTimeout, time.Second)
	diags.Append(d...)

	dnsVerification, d := decodeObject(ctx, data, "dns_verification", priorModel.DNSVerification, usgDNSVerificationAttrTypes)
	diags.Append(d...)

	if diags.HasError() {
		return diags
	}

	m := settingUSGModel{
		BroadcastPing:                  broadcastPing,
		DNSVerification:                dnsVerification,
		FtpModule:                      ftpModule,
		GeoIPFilteringBlock:            geoIPFilteringBlock,
		GeoIPFilteringCountries:        geoIPFilteringCountries,
		GeoIPFilteringEnabled:          geoIPFilteringEnabled,
		GeoIPFilteringTrafficDirection: geoIPFilteringTrafficDirection,
		GreModule:                      greModule,
		H323Module:                     h323Module,
		ICMPTimeout:                    icmpTimeout,
		MssClamp:                       mssClamp,
		OffloadAccounting:              offloadAccounting,
		OffloadL2Blocking:              offloadL2Blocking,
		OffloadSch:                     offloadSch,
		OtherTimeout:                   otherTimeout,
		PptpModule:                     pptpModule,
		ReceiveRedirects:               receiveRedirects,
		SendRedirects:                  sendRedirects,
		SipModule:                      sipModule,
		SynCookies:                     synCookies,
		TCPCloseTimeout:                tcpCloseTimeout,
		TCPCloseWaitTimeout:            tcpCloseWaitTimeout,
		TCPEstablishedTimeout:          tcpEstablishedTimeout,
		TCPFinWaitTimeout:              tcpFinWaitTimeout,
		TCPLastAckTimeout:              tcpLastAckTimeout,
		TCPSynRecvTimeout:              tcpSynRecvTimeout,
		TCPSynSentTimeout:              tcpSynSentTimeout,
		TCPTimeWaitTimeout:             tcpTimeWaitTimeout,
		TFTPModule:                     tftpModule,
		TimeoutSettingPreference:       timeoutSettingPreference,
		UDPOtherTimeout:                udpOtherTimeout,
		UDPStreamTimeout:               udpStreamTimeout,
		UnbindWANMonitors:              unbindWANMonitors,
		UPnPEnabled:                    upnpEnabled,
		UPnPNATPmpEnabled:              upnpNATPmpEnabled,
		UPnPSecureMode:                 upnpSecureMode,
		UPnPWANInterface:               upnpWANInterface,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, usgAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.USG = obj
	return diags
}

// overlay computes the "usg" PUT body from model.USG, starting from a deep
// copy of the snapshot's current section data so the 11 unmodeled
// always-present fields (dhcp_relay_agents_packets, dhcp_relay_server_1..5,
// dhcpd_hostfile_update, dhcpd_use_dnsmasq, dnsmasq_all_servers,
// lldp_enable_all, mdns_enabled) survive the merge (RMW). The 12 conntrack
// timeout leaves use overlayGoDuration with unit = time.Second; the nested
// dns_verification object uses the generalized overlayObject helper. Returns
// configured == false (no write) when the section is not configured
// (null/unknown) in model.
func (s usgSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.USG.IsNull() || model.USG.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingUSGModel
	diags.Append(model.USG.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())

	overlayBool(base, "broadcast_ping", m.BroadcastPing)
	overlayBool(base, "ftp_module", m.FtpModule)
	overlayBool(base, "geo_ip_filtering_enabled", m.GeoIPFilteringEnabled)
	overlayBool(base, "gre_module", m.GreModule)
	overlayBool(base, "h323_module", m.H323Module)
	overlayBool(base, "offload_accounting", m.OffloadAccounting)
	overlayBool(base, "offload_l2_blocking", m.OffloadL2Blocking)
	overlayBool(base, "offload_sch", m.OffloadSch)
	overlayBool(base, "pptp_module", m.PptpModule)
	overlayBool(base, "receive_redirects", m.ReceiveRedirects)
	overlayBool(base, "send_redirects", m.SendRedirects)
	overlayBool(base, "sip_module", m.SipModule)
	overlayBool(base, "syn_cookies", m.SynCookies)
	overlayBool(base, "tftp_module", m.TFTPModule)
	overlayBool(base, "unbind_wan_monitors", m.UnbindWANMonitors)
	overlayBool(base, "upnp_enabled", m.UPnPEnabled)
	overlayBool(base, "upnp_nat_pmp_enabled", m.UPnPNATPmpEnabled)
	overlayBool(base, "upnp_secure_mode", m.UPnPSecureMode)

	overlayString(base, "geo_ip_filtering_block", m.GeoIPFilteringBlock)
	overlayString(base, "geo_ip_filtering_countries", m.GeoIPFilteringCountries)
	overlayString(base, "geo_ip_filtering_traffic_direction", m.GeoIPFilteringTrafficDirection)
	overlayString(base, "mss_clamp", m.MssClamp)
	overlayString(base, "timeout_setting_preference", m.TimeoutSettingPreference)
	overlayString(base, "upnp_wan_interface", m.UPnPWANInterface)

	overlayGoDuration(base, "icmp_timeout", m.ICMPTimeout, time.Second)
	overlayGoDuration(base, "other_timeout", m.OtherTimeout, time.Second)
	overlayGoDuration(base, "tcp_close_timeout", m.TCPCloseTimeout, time.Second)
	overlayGoDuration(base, "tcp_close_wait_timeout", m.TCPCloseWaitTimeout, time.Second)
	overlayGoDuration(base, "tcp_established_timeout", m.TCPEstablishedTimeout, time.Second)
	overlayGoDuration(base, "tcp_fin_wait_timeout", m.TCPFinWaitTimeout, time.Second)
	overlayGoDuration(base, "tcp_last_ack_timeout", m.TCPLastAckTimeout, time.Second)
	overlayGoDuration(base, "tcp_syn_recv_timeout", m.TCPSynRecvTimeout, time.Second)
	overlayGoDuration(base, "tcp_syn_sent_timeout", m.TCPSynSentTimeout, time.Second)
	overlayGoDuration(base, "tcp_time_wait_timeout", m.TCPTimeWaitTimeout, time.Second)
	overlayGoDuration(base, "udp_other_timeout", m.UDPOtherTimeout, time.Second)
	overlayGoDuration(base, "udp_stream_timeout", m.UDPStreamTimeout, time.Second)

	diags.Append(overlayObject(ctx, base, "dns_verification", m.DNSVerification)...)

	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

// carryBestEffort copies the plan's usg value onto dst. This section holds
// no secret leaves, so it is a straight copy with no per-leaf plan/prior
// choice needed.
func (usgSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.USG = plan.USG
	return nil
}

// isConfigured reports whether m.USG is set (non-null, non-unknown), gating
// whether Create/Update push this section — all 37 leaves — to the
// controller at all.
func (usgSection) isConfigured(m settingResourceModel) bool {
	return !m.USG.IsNull() && !m.USG.IsUnknown()
}
