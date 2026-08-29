package unifi

// The usg section descriptor: replaces the hand-written writeUSGSection /
// readUSGSection (setting_sections.go) and their usgModelToSetting /
// usgSettingToModel / usgGeoModelToSetting / writeUsgGeo / usgGeoConfigured
// helpers (deleted from setting_resource.go). The model type and
// attribute-type map moved here too, from setting_resource.go:
// descriptor_mapping_test.go's loadDescriptors reads a descriptor's model
// tags from the same file the Spec literal is in. See
// setting_mgmt_descriptor.go for the shape every section descriptor
// follows, and setting_radius_descriptor.go for the DurationField/
// DurationPtrField precedent this file's twelve timeout fields use.
//
// usg is the first section with a second document. UniFi Network 10.x split
// geo_ip_filtering_* off the `usg` setting into its own `usg_geo` setting
// (settings.UsgGeo, nested one level under IPFiltering). usgGeoKitSpec's
// four Fields address settings.SettingUsgGeoIPFiltering directly, not
// settings.UsgGeo itself: resourcekit.WireNameProblems (jsonOffsets) reads
// only one struct level, so a Field's SDK accessor has to land on a real
// top-level json tag of its own S, and UsgGeo's only top-level tag is
// `ip_filtering` -- its own four members aren't reachable that way. They
// aren't an ObjectField either: that kind needs one types.Object on the
// model, and the schema has four flat sibling attributes
// (geo_ip_filtering_block etc), not one nested object. usgGeoKitBackend
// bridges the two: it reads and writes through settings.UsgGeo (the type
// that actually satisfies settings.Setting), overlaying only the masked
// sub-fields onto whatever the controller already holds before writing the
// whole ip_filtering object back -- the same read-modify-write shape the
// deleted usgGeoModelToSetting/writeUsgGeo used, now scoped to exactly what
// the mask named instead of every field the model declared.
//
// setting_usg.mapping.json declares geo_ip_filtering_block etc managed
// under the same "unifi_setting.usg" surface as usgKitSpec's other 33
// attributes -- Task 2c's generator has no way to record that four of a
// section's managed fields come from a different SDK struct
// (structural_source in the policy input is dropped from the compiled
// mapping). usgGeoKitSpec therefore carries no TypeName: a Spec literal
// with none is excluded from loadDescriptors' own registration
// (descriptor_mapping_test.go's `if desc.TypeName != ""` guard), which is
// what keeps it from needing a setting_usg_geo.mapping.json that does not
// exist and must not be hand-authored. usgKitSpec's own MappedElsewhere
// records the other half: those four wires are real, just carried by this
// sibling document instead of a Fields entry on the primary. The geo Spec's
// own fields are what TestUsgKitSpecConformance and the rest of this file's
// own tests actually verify.

import (
	"context"
	"errors"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// dnsVerificationModel is usg's own dns_verification nested object.
type dnsVerificationModel struct {
	Domain             types.String `tfsdk:"domain"`
	PrimaryDNSServer   types.String `tfsdk:"primary_dns_server"`
	SecondaryDNSServer types.String `tfsdk:"secondary_dns_server"`
	SettingPreference  types.String `tfsdk:"setting_preference"`
}

// settingUSGModel is usg's own section model, decoded out of
// settingResourceModel.USG. Its four geo_ip_filtering_* members are mapped
// by usgGeoKitSpec, not usgKitSpec -- see this file's own top comment.
type settingUSGModel struct {
	BroadcastPing                  types.Bool           `tfsdk:"broadcast_ping"`
	DNSVerification                types.Object         `tfsdk:"dns_verification"`
	FtpModule                      types.Bool           `tfsdk:"ftp_module"`
	GeoIPFilteringBlock            types.String         `tfsdk:"geo_ip_filtering_block"`
	GeoIPFilteringCountries        types.String         `tfsdk:"geo_ip_filtering_countries"`
	GeoIPFilteringEnabled          types.Bool           `tfsdk:"geo_ip_filtering_enabled"`
	GeoIPFilteringTrafficDirection types.String         `tfsdk:"geo_ip_filtering_traffic_direction"`
	GreModule                      types.Bool           `tfsdk:"gre_module"`
	H323Module                     types.Bool           `tfsdk:"h323_module"`
	ICMPTimeout                    timetypes.GoDuration `tfsdk:"icmp_timeout"`
	MssClamp                       types.String         `tfsdk:"mss_clamp"`
	OffloadAccounting              types.Bool           `tfsdk:"offload_accounting"`
	OffloadL2Blocking              types.Bool           `tfsdk:"offload_l2_blocking"`
	OffloadSch                     types.Bool           `tfsdk:"offload_sch"`
	OtherTimeout                   timetypes.GoDuration `tfsdk:"other_timeout"`
	PptpModule                     types.Bool           `tfsdk:"pptp_module"`
	ReceiveRedirects               types.Bool           `tfsdk:"receive_redirects"`
	SendRedirects                  types.Bool           `tfsdk:"send_redirects"`
	SipModule                      types.Bool           `tfsdk:"sip_module"`
	SynCookies                     types.Bool           `tfsdk:"syn_cookies"`
	TCPCloseTimeout                timetypes.GoDuration `tfsdk:"tcp_close_timeout"`
	TCPCloseWaitTimeout            timetypes.GoDuration `tfsdk:"tcp_close_wait_timeout"`
	TCPEstablishedTimeout          timetypes.GoDuration `tfsdk:"tcp_established_timeout"`
	TCPFinWaitTimeout              timetypes.GoDuration `tfsdk:"tcp_fin_wait_timeout"`
	TCPLastAckTimeout              timetypes.GoDuration `tfsdk:"tcp_last_ack_timeout"`
	TCPSynRecvTimeout              timetypes.GoDuration `tfsdk:"tcp_syn_recv_timeout"`
	TCPSynSentTimeout              timetypes.GoDuration `tfsdk:"tcp_syn_sent_timeout"`
	TCPTimeWaitTimeout             timetypes.GoDuration `tfsdk:"tcp_time_wait_timeout"`
	TFTPModule                     types.Bool           `tfsdk:"tftp_module"`
	TimeoutSettingPreference       types.String         `tfsdk:"timeout_setting_preference"`
	UDPOtherTimeout                timetypes.GoDuration `tfsdk:"udp_other_timeout"`
	UDPStreamTimeout               timetypes.GoDuration `tfsdk:"udp_stream_timeout"`
	UnbindWANMonitors              types.Bool           `tfsdk:"unbind_wan_monitors"`
	UPnPEnabled                    types.Bool           `tfsdk:"upnp_enabled"`
	UPnPNATPmpEnabled              types.Bool           `tfsdk:"upnp_nat_pmp_enabled"`
	UPnPSecureMode                 types.Bool           `tfsdk:"upnp_secure_mode"`
	UPnPWANInterface               types.String         `tfsdk:"upnp_wan_interface"`
}

// dnsVerificationAttrTypes and usgAttrTypes type usg's dns_verification
// nested object and usg's own object in state; both must match the
// generated schema exactly.
var (
	dnsVerificationAttrTypes = map[string]attr.Type{
		"domain":               types.StringType,
		"primary_dns_server":   types.StringType,
		"secondary_dns_server": types.StringType,
		"setting_preference":   types.StringType,
	}
	usgAttrTypes = map[string]attr.Type{
		"broadcast_ping":                     types.BoolType,
		"dns_verification":                   types.ObjectType{AttrTypes: dnsVerificationAttrTypes},
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
)

// usgKitSpec maps every non-geo attribute of the generated usg schema
// (resource_setting/setting_resource_gen.go's "usg" SingleNestedAttribute)
// onto settings.Usg. Elide judgments follow resourcekit.ElideProblems'
// schema-driven rule, not a transcription of the old usgSettingToModel:
// mss_clamp, timeout_setting_preference and upnp_wan_interface are
// Optional+Computed strings with no validator rejecting "", so they want
// KeepZero, even though the deleted mapper read each through
// util.StringValueOrNull (a divergence -- see this task's own report).
// icmp_timeout and the eleven other Duration fields carry a CustomType,
// which routes ElideProblems' zeroIsRejected through a check this field
// kind never triggers, so they too want KeepZero regardless of any
// validator. geo_ip_filtering_block/countries/enabled/traffic_direction
// aren't Fields here at all -- see usgGeoKitSpec and this file's own top
// comment.
func usgKitSpec() resourcekit.Spec[settingUSGModel, settings.Usg] {
	return resourcekit.Spec[settingUSGModel, settings.Usg]{
		TypeName: "setting_usg",
		Subject:  "USG Setting",
		New:      func() *settings.Usg { return &settings.Usg{} },
		// setting_usg.mapping.json declares these four managed under this
		// same surface; usgGeoKitSpec (no TypeName of its own) is the Spec
		// that actually carries and verifies them.
		MappedElsewhere: []string{"action", "countries", "enabled", "traffic_direction"},
		Fields: []resourcekit.Field[settingUSGModel, settings.Usg]{
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "broadcast_ping",
				Model: func(m *settingUSGModel) *types.Bool { return &m.BroadcastPing },
				SDK:   func(s *settings.Usg) *bool { return &s.BroadcastPing },
			},
			resourcekit.ObjectField[settingUSGModel, settings.Usg, settings.SettingUsgDNSVerification]{
				Wire:      "dns_verification",
				Model:     func(m *settingUSGModel) *types.Object { return &m.DNSVerification },
				SDK:       func(s *settings.Usg) **settings.SettingUsgDNSVerification { return &s.DNSVerification },
				AttrTypes: dnsVerificationAttrTypes,
				Encode:    dnsVerificationEncode,
				Decode:    dnsVerificationDecode,
				Elide:     resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "ftp_module",
				Model: func(m *settingUSGModel) *types.Bool { return &m.FtpModule },
				SDK:   func(s *settings.Usg) *bool { return &s.FtpModule },
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "gre_module",
				Model: func(m *settingUSGModel) *types.Bool { return &m.GreModule },
				SDK:   func(s *settings.Usg) *bool { return &s.GreModule },
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "h323_module",
				Model: func(m *settingUSGModel) *types.Bool { return &m.H323Module },
				SDK:   func(s *settings.Usg) *bool { return &s.H323Module },
			},
			resourcekit.DurationField[settingUSGModel, settings.Usg]{
				Wire:  "icmp_timeout",
				Model: func(m *settingUSGModel) *timetypes.GoDuration { return &m.ICMPTimeout },
				SDK:   func(s *settings.Usg) *int64 { return &s.ICMPTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingUSGModel, settings.Usg]{
				Wire:  "mss_clamp",
				Model: func(m *settingUSGModel) *types.String { return &m.MssClamp },
				SDK:   func(s *settings.Usg) *string { return &s.MssClamp },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "offload_accounting",
				Model: func(m *settingUSGModel) *types.Bool { return &m.OffloadAccounting },
				SDK:   func(s *settings.Usg) *bool { return &s.OffloadAccounting },
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "offload_l2_blocking",
				Model: func(m *settingUSGModel) *types.Bool { return &m.OffloadL2Blocking },
				SDK:   func(s *settings.Usg) *bool { return &s.OffloadL2Blocking },
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "offload_sch",
				Model: func(m *settingUSGModel) *types.Bool { return &m.OffloadSch },
				SDK:   func(s *settings.Usg) *bool { return &s.OffloadSch },
			},
			resourcekit.DurationField[settingUSGModel, settings.Usg]{
				Wire:  "other_timeout",
				Model: func(m *settingUSGModel) *timetypes.GoDuration { return &m.OtherTimeout },
				SDK:   func(s *settings.Usg) *int64 { return &s.OtherTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "pptp_module",
				Model: func(m *settingUSGModel) *types.Bool { return &m.PptpModule },
				SDK:   func(s *settings.Usg) *bool { return &s.PptpModule },
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "receive_redirects",
				Model: func(m *settingUSGModel) *types.Bool { return &m.ReceiveRedirects },
				SDK:   func(s *settings.Usg) *bool { return &s.ReceiveRedirects },
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "send_redirects",
				Model: func(m *settingUSGModel) *types.Bool { return &m.SendRedirects },
				SDK:   func(s *settings.Usg) *bool { return &s.SendRedirects },
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "sip_module",
				Model: func(m *settingUSGModel) *types.Bool { return &m.SipModule },
				SDK:   func(s *settings.Usg) *bool { return &s.SipModule },
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "syn_cookies",
				Model: func(m *settingUSGModel) *types.Bool { return &m.SynCookies },
				SDK:   func(s *settings.Usg) *bool { return &s.SynCookies },
			},
			resourcekit.DurationField[settingUSGModel, settings.Usg]{
				Wire:  "tcp_close_timeout",
				Model: func(m *settingUSGModel) *timetypes.GoDuration { return &m.TCPCloseTimeout },
				SDK:   func(s *settings.Usg) *int64 { return &s.TCPCloseTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.DurationField[settingUSGModel, settings.Usg]{
				Wire:  "tcp_close_wait_timeout",
				Model: func(m *settingUSGModel) *timetypes.GoDuration { return &m.TCPCloseWaitTimeout },
				SDK:   func(s *settings.Usg) *int64 { return &s.TCPCloseWaitTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.DurationField[settingUSGModel, settings.Usg]{
				Wire:  "tcp_established_timeout",
				Model: func(m *settingUSGModel) *timetypes.GoDuration { return &m.TCPEstablishedTimeout },
				SDK:   func(s *settings.Usg) *int64 { return &s.TCPEstablishedTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.DurationField[settingUSGModel, settings.Usg]{
				Wire:  "tcp_fin_wait_timeout",
				Model: func(m *settingUSGModel) *timetypes.GoDuration { return &m.TCPFinWaitTimeout },
				SDK:   func(s *settings.Usg) *int64 { return &s.TCPFinWaitTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.DurationField[settingUSGModel, settings.Usg]{
				Wire:  "tcp_last_ack_timeout",
				Model: func(m *settingUSGModel) *timetypes.GoDuration { return &m.TCPLastAckTimeout },
				SDK:   func(s *settings.Usg) *int64 { return &s.TCPLastAckTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.DurationField[settingUSGModel, settings.Usg]{
				Wire:  "tcp_syn_recv_timeout",
				Model: func(m *settingUSGModel) *timetypes.GoDuration { return &m.TCPSynRecvTimeout },
				SDK:   func(s *settings.Usg) *int64 { return &s.TCPSynRecvTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.DurationField[settingUSGModel, settings.Usg]{
				Wire:  "tcp_syn_sent_timeout",
				Model: func(m *settingUSGModel) *timetypes.GoDuration { return &m.TCPSynSentTimeout },
				SDK:   func(s *settings.Usg) *int64 { return &s.TCPSynSentTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.DurationField[settingUSGModel, settings.Usg]{
				Wire:  "tcp_time_wait_timeout",
				Model: func(m *settingUSGModel) *timetypes.GoDuration { return &m.TCPTimeWaitTimeout },
				SDK:   func(s *settings.Usg) *int64 { return &s.TCPTimeWaitTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "tftp_module",
				Model: func(m *settingUSGModel) *types.Bool { return &m.TFTPModule },
				SDK:   func(s *settings.Usg) *bool { return &s.TFTPModule },
			},
			resourcekit.StringField[settingUSGModel, settings.Usg]{
				Wire:  "timeout_setting_preference",
				Model: func(m *settingUSGModel) *types.String { return &m.TimeoutSettingPreference },
				SDK:   func(s *settings.Usg) *string { return &s.TimeoutSettingPreference },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.DurationField[settingUSGModel, settings.Usg]{
				Wire:  "udp_other_timeout",
				Model: func(m *settingUSGModel) *timetypes.GoDuration { return &m.UDPOtherTimeout },
				SDK:   func(s *settings.Usg) *int64 { return &s.UDPOtherTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.DurationField[settingUSGModel, settings.Usg]{
				Wire:  "udp_stream_timeout",
				Model: func(m *settingUSGModel) *timetypes.GoDuration { return &m.UDPStreamTimeout },
				SDK:   func(s *settings.Usg) *int64 { return &s.UDPStreamTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "unbind_wan_monitors",
				Model: func(m *settingUSGModel) *types.Bool { return &m.UnbindWANMonitors },
				SDK:   func(s *settings.Usg) *bool { return &s.UnbindWANMonitors },
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "upnp_enabled",
				Model: func(m *settingUSGModel) *types.Bool { return &m.UPnPEnabled },
				SDK:   func(s *settings.Usg) *bool { return &s.UPnPEnabled },
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "upnp_nat_pmp_enabled",
				Model: func(m *settingUSGModel) *types.Bool { return &m.UPnPNATPmpEnabled },
				SDK:   func(s *settings.Usg) *bool { return &s.UPnPNATPmpEnabled },
			},
			resourcekit.BoolField[settingUSGModel, settings.Usg]{
				Wire:  "upnp_secure_mode",
				Model: func(m *settingUSGModel) *types.Bool { return &m.UPnPSecureMode },
				SDK:   func(s *settings.Usg) *bool { return &s.UPnPSecureMode },
			},
			resourcekit.StringField[settingUSGModel, settings.Usg]{
				Wire:  "upnp_wan_interface",
				Model: func(m *settingUSGModel) *types.String { return &m.UPnPWANInterface },
				SDK:   func(s *settings.Usg) *string { return &s.UPnPWANInterface },
				Elide: resourcekit.KeepZero,
			},
		},
	}
}

func dnsVerificationEncode(
	ctx context.Context, object types.Object,
) (*settings.SettingUsgDNSVerification, diag.Diagnostics) {
	var model dnsVerificationModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return &settings.SettingUsgDNSVerification{
		Domain:             model.Domain.ValueString(),
		PrimaryDNSServer:   model.PrimaryDNSServer.ValueString(),
		SecondaryDNSServer: model.SecondaryDNSServer.ValueString(),
		SettingPreference:  model.SettingPreference.ValueString(),
	}, diags
}

func dnsVerificationDecode(
	ctx context.Context, sdk *settings.SettingUsgDNSVerification,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, dnsVerificationAttrTypes, dnsVerificationModel{
		Domain:             types.StringValue(sdk.Domain),
		PrimaryDNSServer:   types.StringValue(sdk.PrimaryDNSServer),
		SecondaryDNSServer: types.StringValue(sdk.SecondaryDNSServer),
		SettingPreference:  types.StringValue(sdk.SettingPreference),
	})
}

// usgAfterReceive reproduces the deleted usgSettingToModel: every one of
// usg's thirty-seven attributes -- the thirty-three usgKitSpec maps plus the
// four geo_ip_filtering_* ones usgGeoKitSpec's own document already
// hydrated into this same shared model by the time this runs -- is
// plan-conditioned: null unless the practitioner's own config (prior) set
// it, so an unmanaged usg attribute never drifts. Run from both Write and
// Read (SpecSection's own contract), and on both paths after every document
// has written or read, never before: usgGeoKitDocument's own response
// merge (spec.ToModel, run over all four geo fields regardless of which the
// plan set) would otherwise re-hydrate a geo field this hook had already
// nulled, on Write, or leave Read's own prior wrongly non-null for a field
// the plan never named.
func usgAfterReceive(
	_ context.Context, _ *settings.Usg, model *settingUSGModel, prior settingUSGModel,
) diag.Diagnostics {
	boolOrNull := func(priorValue, modelValue types.Bool) types.Bool {
		if priorValue.IsNull() || priorValue.IsUnknown() {
			return types.BoolNull()
		}
		return modelValue
	}
	stringOrNull := func(priorValue, modelValue types.String) types.String {
		if priorValue.IsNull() || priorValue.IsUnknown() {
			return types.StringNull()
		}
		return modelValue
	}
	durationOrNull := func(priorValue, modelValue timetypes.GoDuration) timetypes.GoDuration {
		if priorValue.IsNull() || priorValue.IsUnknown() {
			return timetypes.NewGoDurationNull()
		}
		return modelValue
	}

	model.BroadcastPing = boolOrNull(prior.BroadcastPing, model.BroadcastPing)
	model.FtpModule = boolOrNull(prior.FtpModule, model.FtpModule)
	model.GeoIPFilteringEnabled = boolOrNull(prior.GeoIPFilteringEnabled, model.GeoIPFilteringEnabled)
	model.GreModule = boolOrNull(prior.GreModule, model.GreModule)
	model.H323Module = boolOrNull(prior.H323Module, model.H323Module)
	model.OffloadAccounting = boolOrNull(prior.OffloadAccounting, model.OffloadAccounting)
	model.OffloadL2Blocking = boolOrNull(prior.OffloadL2Blocking, model.OffloadL2Blocking)
	model.OffloadSch = boolOrNull(prior.OffloadSch, model.OffloadSch)
	model.PptpModule = boolOrNull(prior.PptpModule, model.PptpModule)
	model.ReceiveRedirects = boolOrNull(prior.ReceiveRedirects, model.ReceiveRedirects)
	model.SendRedirects = boolOrNull(prior.SendRedirects, model.SendRedirects)
	model.SipModule = boolOrNull(prior.SipModule, model.SipModule)
	model.SynCookies = boolOrNull(prior.SynCookies, model.SynCookies)
	model.TFTPModule = boolOrNull(prior.TFTPModule, model.TFTPModule)
	model.UnbindWANMonitors = boolOrNull(prior.UnbindWANMonitors, model.UnbindWANMonitors)
	model.UPnPEnabled = boolOrNull(prior.UPnPEnabled, model.UPnPEnabled)
	model.UPnPNATPmpEnabled = boolOrNull(prior.UPnPNATPmpEnabled, model.UPnPNATPmpEnabled)
	model.UPnPSecureMode = boolOrNull(prior.UPnPSecureMode, model.UPnPSecureMode)

	model.GeoIPFilteringBlock = stringOrNull(prior.GeoIPFilteringBlock, model.GeoIPFilteringBlock)
	model.GeoIPFilteringCountries = stringOrNull(prior.GeoIPFilteringCountries, model.GeoIPFilteringCountries)
	model.GeoIPFilteringTrafficDirection = stringOrNull(
		prior.GeoIPFilteringTrafficDirection, model.GeoIPFilteringTrafficDirection)
	model.MssClamp = stringOrNull(prior.MssClamp, model.MssClamp)
	model.TimeoutSettingPreference = stringOrNull(prior.TimeoutSettingPreference, model.TimeoutSettingPreference)
	model.UPnPWANInterface = stringOrNull(prior.UPnPWANInterface, model.UPnPWANInterface)

	model.ICMPTimeout = durationOrNull(prior.ICMPTimeout, model.ICMPTimeout)
	model.OtherTimeout = durationOrNull(prior.OtherTimeout, model.OtherTimeout)
	model.TCPCloseTimeout = durationOrNull(prior.TCPCloseTimeout, model.TCPCloseTimeout)
	model.TCPCloseWaitTimeout = durationOrNull(prior.TCPCloseWaitTimeout, model.TCPCloseWaitTimeout)
	model.TCPEstablishedTimeout = durationOrNull(prior.TCPEstablishedTimeout, model.TCPEstablishedTimeout)
	model.TCPFinWaitTimeout = durationOrNull(prior.TCPFinWaitTimeout, model.TCPFinWaitTimeout)
	model.TCPLastAckTimeout = durationOrNull(prior.TCPLastAckTimeout, model.TCPLastAckTimeout)
	model.TCPSynRecvTimeout = durationOrNull(prior.TCPSynRecvTimeout, model.TCPSynRecvTimeout)
	model.TCPSynSentTimeout = durationOrNull(prior.TCPSynSentTimeout, model.TCPSynSentTimeout)
	model.TCPTimeWaitTimeout = durationOrNull(prior.TCPTimeWaitTimeout, model.TCPTimeWaitTimeout)
	model.UDPOtherTimeout = durationOrNull(prior.UDPOtherTimeout, model.UDPOtherTimeout)
	model.UDPStreamTimeout = durationOrNull(prior.UDPStreamTimeout, model.UDPStreamTimeout)

	if prior.DNSVerification.IsNull() || prior.DNSVerification.IsUnknown() {
		model.DNSVerification = types.ObjectNull(dnsVerificationAttrTypes)
	}

	return nil
}

// usgNestedSchema is the usg SingleNestedAttribute's own Attributes, wrapped
// as a schema.Schema so resourcekit's conformance checks -- built for a
// whole resource's top-level schema -- can run against one section of
// unifi_setting instead.
func usgNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	usg := built.Attributes["usg"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // usg is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: usg.Attributes}
}

// usgGeoNestedSchema is the subset of usg's own nested schema that
// usgGeoKitSpec maps: resourcekit's conformance checks compare a Spec
// against exactly the schema.Schema it owns, and usg_geo owns four of usg's
// thirty-seven attributes, not the whole SingleNestedAttribute. Keyed by
// terraform name (geo_ip_filtering_block, ...), matching what
// ElideProblems/ZeroReadProblems look up off the model's own tfsdk tags --
// not usgGeoKitSpec's Wire values, which are the SDK's names (action, ...).
func usgGeoNestedSchema(ctx context.Context) schema.Schema {
	usg := usgNestedSchema(ctx)
	attrs := map[string]schema.Attribute{}
	for _, name := range []string{
		"geo_ip_filtering_block",
		"geo_ip_filtering_countries",
		"geo_ip_filtering_enabled",
		"geo_ip_filtering_traffic_direction",
	} {
		attrs[name] = usg.Attributes[name]
	}
	return schema.Schema{Attributes: attrs}
}

// usgKitBackend binds usgKitSpec to a client: Read is GetSetting[*Usg],
// UpdateFields is the masked UpdateSettingFields -- naming only the fields
// the plan set instead of the read-modify-write whole-document PUT
// writeUSGSection used. This is also what retires
// Test_usgForceEmittedFieldCountIsPinned's own worry: the 23 fields
// settings.Usg force-emits (no omitempty) never enter a masked write's body
// unless the plan actually named them, so unlike the deleted
// usgModelToSetting, no base object is needed to protect them --
// TestUsgSpecMasksOnlyTheFieldsThePlanSet pins this directly.
func usgKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Usg] {
	return resourcekit.Backend[settings.Usg]{
		Read: func(ctx context.Context, site, _ string) (*settings.Usg, error) {
			_, usg, err := ui.GetSetting[*settings.Usg](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return usg, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Usg, fields ...string,
		) (*settings.Usg, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// usgGeoKitSpec maps usg's four geo_ip_filtering_* attributes onto
// settings.SettingUsgGeoIPFiltering directly -- see this file's own top
// comment for why S is the nested struct rather than settings.UsgGeo
// itself, and why this carries no TypeName. action's OneOf("block","allow")
// and traffic_direction's OneOf("both","ingress","egress") reject "", so
// they want NullZero; countries' RegexMatches accepts "" (both groups of
// `^([A-Z]{2})?(,[A-Z]{2}){0,149}$` are optional), so it wants KeepZero,
// same as usgKitSpec's own no-validator strings; enabled is a plain bool,
// which carries no Elide at all.
func usgGeoKitSpec() resourcekit.Spec[settingUSGModel, settings.SettingUsgGeoIPFiltering] {
	return resourcekit.Spec[settingUSGModel, settings.SettingUsgGeoIPFiltering]{
		Subject: "USG Geo Setting",
		New:     func() *settings.SettingUsgGeoIPFiltering { return &settings.SettingUsgGeoIPFiltering{} },
		Fields: []resourcekit.Field[settingUSGModel, settings.SettingUsgGeoIPFiltering]{
			resourcekit.StringField[settingUSGModel, settings.SettingUsgGeoIPFiltering]{
				Wire:  "action",
				Model: func(m *settingUSGModel) *types.String { return &m.GeoIPFilteringBlock },
				SDK:   func(s *settings.SettingUsgGeoIPFiltering) *string { return &s.Action },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[settingUSGModel, settings.SettingUsgGeoIPFiltering]{
				Wire:  "countries",
				Model: func(m *settingUSGModel) *types.String { return &m.GeoIPFilteringCountries },
				SDK:   func(s *settings.SettingUsgGeoIPFiltering) *string { return &s.Countries },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingUSGModel, settings.SettingUsgGeoIPFiltering]{
				Wire:  "enabled",
				Model: func(m *settingUSGModel) *types.Bool { return &m.GeoIPFilteringEnabled },
				SDK:   func(s *settings.SettingUsgGeoIPFiltering) *bool { return &s.Enabled },
			},
			resourcekit.StringField[settingUSGModel, settings.SettingUsgGeoIPFiltering]{
				Wire:  "traffic_direction",
				Model: func(m *settingUSGModel) *types.String { return &m.GeoIPFilteringTrafficDirection },
				SDK:   func(s *settings.SettingUsgGeoIPFiltering) *string { return &s.TrafficDirection },
				Elide: resourcekit.NullZero,
			},
		},
	}
}

// usgGeoKitBackend bridges settings.SettingUsgGeoIPFiltering (what
// usgGeoKitSpec's Fields address) to settings.UsgGeo (the type that
// actually satisfies settings.Setting, so it's the one GetSetting and
// UpdateSetting can read and write). UpdateFields reads the controller's
// current usg_geo first -- absent is normal pre-configuration, not an
// error, the same tolerance the deleted writeUsgGeo gave it -- and overlays
// only the masked sub-fields onto it before writing the whole ip_filtering
// object back: ip_filtering has no wire of its own below the whole object
// (UpdateSettingFields' mask only reaches UsgGeo's own top-level json tag,
// ip_filtering, not its four members), so a field the mask didn't name must
// survive by being carried forward from what the controller already held.
func usgGeoKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.SettingUsgGeoIPFiltering] {
	return resourcekit.Backend[settings.SettingUsgGeoIPFiltering]{
		Read: func(ctx context.Context, site, _ string) (*settings.SettingUsgGeoIPFiltering, error) {
			_, geo, err := ui.GetSetting[*settings.UsgGeo](client, ctx, site)
			if err != nil {
				return nil, err
			}
			if geo.IPFiltering == nil {
				return &settings.SettingUsgGeoIPFiltering{}, nil
			}
			return geo.IPFiltering, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.SettingUsgGeoIPFiltering, fields ...string,
		) (*settings.SettingUsgGeoIPFiltering, error) {
			_, current, err := ui.GetSetting[*settings.UsgGeo](client, ctx, site)
			if err != nil {
				var notFound *ui.NotFoundError
				if !errors.As(err, &notFound) {
					return nil, err
				}
				current = &settings.UsgGeo{}
			}
			if current.IPFiltering == nil {
				current.IPFiltering = &settings.SettingUsgGeoIPFiltering{}
			}
			for _, field := range fields {
				switch field {
				case "action":
					current.IPFiltering.Action = in.Action
				case "countries":
					current.IPFiltering.Countries = in.Countries
				case "enabled":
					current.IPFiltering.Enabled = in.Enabled
				case "traffic_direction":
					current.IPFiltering.TrafficDirection = in.TrafficDirection
				}
			}
			if err := client.UpdateSetting(ctx, site, current); err != nil {
				return nil, err
			}
			return current.IPFiltering, nil
		},
	}
}

// usgGeoNotSupportedDiagnostic is the deleted writeUsgGeo's own
// controller-too-old diagnostic, copied verbatim.
func usgGeoNotSupportedDiagnostic(error) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.AddError(
		"Geo IP Filtering Not Supported By This Controller",
		"The `geo_ip_filtering_*` attributes are stored in the `usg_geo` setting, which "+
			"this controller does not expose. UniFi Network 10.x moved them out of the "+
			"`usg` setting. Remove them from the `usg` block, or upgrade the controller.",
	)
	return diags
}

// usgGeoKitDocument builds usg's Extra: written only when the plan sets at
// least one geo_ip_filtering_* attribute -- an empty mask is a no-op that
// SpecDocument's own Write skips before Backend.UpdateFields ever runs,
// which is exactly the predicate the deleted usgGeoConfigured checked by
// hand (any of the four non-null). Read unconditionally, the same as every
// other Extra. OnWriteNotFound reproduces writeUsgGeo's own diagnostic;
// OnReadNotFound is nil, since a read-time absence -- a controller that
// predates the split, or a site that never touched geo filtering -- is
// benign, the same tolerance readUSGSection gave a not-found usg_geo.
func usgGeoKitDocument(client *ui.ApiClient) resourcekit.Document[settingUSGModel] {
	spec := usgGeoKitSpec()
	spec.Backend = usgGeoKitBackend(client)
	return resourcekit.SpecDocument[settingUSGModel, settings.SettingUsgGeoIPFiltering]{
		Spec:            spec,
		OnWriteNotFound: usgGeoNotSupportedDiagnostic,
	}
}

// usgKitSection builds the usg entry for settingResource's Sections, bound to
// client via settingKitSections, which calls it with r.client.ApiClient.
func usgKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := usgKitSpec()
	spec.Backend = usgKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingUSGModel, settings.Usg]{
		SectionName:  "usg",
		Get:          func(m *settingResourceModel) *types.Object { return &m.USG },
		Set:          func(m *settingResourceModel, o types.Object) { m.USG = o },
		AttrTypes:    usgAttrTypes,
		Spec:         spec,
		AfterReceive: usgAfterReceive,
		Extra:        []resourcekit.Document[settingUSGModel]{usgGeoKitDocument(client)},
	}
}
