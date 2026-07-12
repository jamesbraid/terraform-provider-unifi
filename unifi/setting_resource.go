package unifi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

var (
	_ resource.Resource                 = &settingResource{}
	_ resource.ResourceWithImportState  = &settingResource{}
	_ resource.ResourceWithUpgradeState = &settingResource{}
)

// NewSettingResource returns a new instance of the unifi_setting resource,
// which manages settings for a UniFi site.
func NewSettingResource() resource.Resource {
	return &settingResource{}
}

// settingResource implements the unifi_setting Terraform resource. It holds
// only the provider client; all section-specific behavior (schema, decode,
// overlay) is delegated to the registered settingSections rather than kept
// here, so adding a settings section does not require touching this struct.
type settingResource struct {
	client *Client
}

// sshKeyModel is the nested per-key element of settingMgmtModel.SSHKeys
// (mgmt.ssh_keys). It models only the public-key fields a user configures;
// the controller-assigned date/fingerprint metadata on each wire element is
// deliberately not modeled and is blanked on every apply rather than carried
// by list position (see blankSSHKeyControllerMetadata).
type sshKeyModel struct {
	Name    types.String `tfsdk:"name"`
	Type    types.String `tfsdk:"type"`
	Key     types.String `tfsdk:"key"`
	Comment types.String `tfsdk:"comment"`
}

// settingMgmtModel is the Terraform model for the "mgmt" section
// (settingResourceModel.Mgmt). SSHPassword is write-only: the controller
// never echoes secret values back, so decode always carries the prior
// state's value forward instead of reading the wire's masked placeholder.
type settingMgmtModel struct {
	AutoUpgrade            types.Bool   `tfsdk:"auto_upgrade"`
	AutoUpgradeHour        types.Int64  `tfsdk:"auto_upgrade_hour"`
	SSHEnabled             types.Bool   `tfsdk:"ssh_enabled"`
	SSHKeys                types.List   `tfsdk:"ssh_keys"`
	AdvancedFeatureEnabled types.Bool   `tfsdk:"advanced_feature_enabled"`
	DebugToolsEnabled      types.Bool   `tfsdk:"debug_tools_enabled"`
	DirectConnectEnabled   types.Bool   `tfsdk:"direct_connect_enabled"`
	UnifiIdpEnabled        types.Bool   `tfsdk:"unifi_idp_enabled"`
	WifimanEnabled         types.Bool   `tfsdk:"wifiman_enabled"`
	SSHUsername            types.String `tfsdk:"ssh_username"`
	SSHPassword            types.String `tfsdk:"ssh_password"`
	SSHAuthPasswordEnabled types.Bool   `tfsdk:"ssh_auth_password_enabled"`
}

// settingRadiusModel is the Terraform model for the "radius" section
// (settingResourceModel.Radius). Secret is write-only, carried forward from
// prior state on decode rather than read from the controller's masked wire
// value, and InterimUpdateInterval is a GoDuration string (seconds on the
// wire) so v0->v1 state upgrades convert it via UpgradeState.
type settingRadiusModel struct {
	AccountingEnabled     types.Bool           `tfsdk:"accounting_enabled"`
	AcctPort              types.Int64          `tfsdk:"acct_port"`
	AuthPort              types.Int64          `tfsdk:"auth_port"`
	InterimUpdateInterval timetypes.GoDuration `tfsdk:"interim_update_interval"`
	Secret                types.String         `tfsdk:"secret"`
}

// dnsVerificationModel is the nested "dns_verification" child object of
// settingUSGModel, decoded/overlaid through the generalized single-object
// codec (decodeObject/overlayObject) rather than by hand.
type dnsVerificationModel struct {
	Domain             types.String `tfsdk:"domain"`
	PrimaryDNSServer   types.String `tfsdk:"primary_dns_server"`
	SecondaryDNSServer types.String `tfsdk:"secondary_dns_server"`
	SettingPreference  types.String `tfsdk:"setting_preference"`
}

// settingUSGModel is the Terraform model for the "usg" section
// (settingResourceModel.USG), the largest section: 37 leaves including 12
// conntrack timeouts stored as GoDuration strings (seconds on the wire, so
// v0->v1 state upgrades convert them via UpgradeState) and one nested
// dns_verification object. All other leaves map 1:1 to their wire keys.
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

// settingDohCustomServerModel is the nested per-server element of
// settingDohModel.CustomServers (doh.custom_servers), one custom resolver
// specified via a DNS stamp (sdns://).
type settingDohCustomServerModel struct {
	Enabled    types.Bool   `tfsdk:"enabled"`
	SDNSStamp  types.String `tfsdk:"sdns_stamp"`
	ServerName types.String `tfsdk:"server_name"`
}

// settingAutoSpeedtestModel is the Terraform model for the "auto_speedtest"
// section (settingResourceModel.AutoSpeedtest): a flat scalar-only section
// with no nested objects/lists and no secrets, used as the section template
// other flat sections copy.
type settingAutoSpeedtestModel struct {
	Enabled  types.Bool   `tfsdk:"enabled"`
	CronExpr types.String `tfsdk:"cron_expr"`
}

// settingCountryModel is the Terraform model for the "country" section
// (settingResourceModel.Country): a single required regulatory country code
// (ISO 3166-1 numeric).
type settingCountryModel struct {
	Code types.Int64 `tfsdk:"code"`
}

// settingDpiModel is the Terraform model for the "dpi" section
// (settingResourceModel.Dpi). FingerprintingEnabled's wire key is the
// camelCase "fingerprintingEnabled" (go-unifi's settings.Dpi json tag),
// unlike its snake_case tfsdk name, so decode/overlay must remap it by hand.
type settingDpiModel struct {
	Enabled               types.Bool `tfsdk:"enabled"`
	FingerprintingEnabled types.Bool `tfsdk:"fingerprinting_enabled"`
}

// settingLcmModel is the Terraform model for the "lcm" section
// (settingResourceModel.Lcm): device LCD/display settings, a flat
// scalar-only section with no nested objects/lists and no secrets.
type settingLcmModel struct {
	Enabled     types.Bool  `tfsdk:"enabled"`
	Brightness  types.Int64 `tfsdk:"brightness"`
	IdleTimeout types.Int64 `tfsdk:"idle_timeout"`
	Sync        types.Bool  `tfsdk:"sync"`
	TouchEvent  types.Bool  `tfsdk:"touch_event"`
}

// settingNetworkOptimizationModel is the Terraform model for the
// "network_optimization" section (settingResourceModel.NetworkOpt): a single
// managed toggle, no nested objects/lists and no secrets.
type settingNetworkOptimizationModel struct {
	Enabled types.Bool `tfsdk:"enabled"`
}

// settingNtpModel is the Terraform model for the "ntp" section
// (settingResourceModel.Ntp): up to four time-server addresses plus a mode
// preference, all plain strings with no nested objects/lists and no
// secrets.
type settingNtpModel struct {
	NtpServer1        types.String `tfsdk:"ntp_server_1"`
	NtpServer2        types.String `tfsdk:"ntp_server_2"`
	NtpServer3        types.String `tfsdk:"ntp_server_3"`
	NtpServer4        types.String `tfsdk:"ntp_server_4"`
	SettingPreference types.String `tfsdk:"setting_preference"`
}

// settingSyslogModel is the Terraform model for the "syslog" section
// (settingResourceModel.Syslog). The controller stores this section under
// the wire key "rsyslogd" while the Terraform attribute is "syslog" — the
// only section where key() and attrName() diverge.
type settingSyslogModel struct {
	Enabled                     types.Bool   `tfsdk:"enabled"`
	Contents                    types.List   `tfsdk:"contents"`
	Debug                       types.Bool   `tfsdk:"debug"`
	IP                          types.String `tfsdk:"ip"`
	Port                        types.Int64  `tfsdk:"port"`
	LogAllContents              types.Bool   `tfsdk:"log_all_contents"`
	NetconsoleEnabled           types.Bool   `tfsdk:"netconsole_enabled"`
	NetconsoleHost              types.String `tfsdk:"netconsole_host"`
	NetconsolePort              types.Int64  `tfsdk:"netconsole_port"`
	ThisController              types.Bool   `tfsdk:"this_controller"`
	ThisControllerEncryptedOnly types.Bool   `tfsdk:"this_controller_encrypted_only"`
}

// settingDohModel is the Terraform model for the "doh" (Encrypted DNS /
// DNS-over-HTTPS) section (settingResourceModel.Doh). CustomServers is
// decoded/overlaid through the generalized nested-object-list codec
// (decodeObjectList/overlayObjectList).
type settingDohModel struct {
	CustomServers types.List   `tfsdk:"custom_servers"`
	ServerNames   types.List   `tfsdk:"server_names"`
	State         types.String `tfsdk:"state"`
}

// settingIpsHoneypotModel is the nested per-entry element of
// settingIpsModel.Honeypot (ips.honeypot): one IP address, on one network,
// used as an IPS honeypot to detect internal port scans.
type settingIpsHoneypotModel struct {
	IPAddress types.String `tfsdk:"ip_address"`
	NetworkID types.String `tfsdk:"network_id"`
	Version   types.String `tfsdk:"version"`
}

// settingIpsWhitelistModel is the nested per-entry element of
// settingIpsModel.SuppressionWhitelist (ips.suppression_whitelist), a
// source/destination excluded from IPS inspection entirely. On the wire it
// is unwrapped by hand from the controller's "suppression.whitelist" object
// (see ipsSection's decode/overlay).
type settingIpsWhitelistModel struct {
	Direction types.String `tfsdk:"direction"`
	Mode      types.String `tfsdk:"mode"`
	Value     types.String `tfsdk:"value"`
}

// settingIpsTrackingModel is the nested per-entry element of
// settingIpsAlertModel.Tracking (ips.suppression_alerts[].tracking), a
// source/destination match used when the parent alert's Type is "track".
// The generalized nested codec recurses into this list automatically since
// it is nested inside settingIpsAlertModel.
type settingIpsTrackingModel struct {
	Direction types.String `tfsdk:"direction"`
	Mode      types.String `tfsdk:"mode"`
	Value     types.String `tfsdk:"value"`
}

// settingIpsAlertModel is the nested per-entry element of
// settingIpsModel.SuppressionAlerts (ips.suppression_alerts), one signature
// or category suppressed either everywhere (Type "all") or only for the
// sources/destinations listed in Tracking (Type "track"). On the wire it is
// unwrapped by hand from the controller's "suppression.alerts" object (see
// ipsSection's decode/overlay).
type settingIpsAlertModel struct {
	Category  types.String `tfsdk:"category"`
	Gid       types.Int64  `tfsdk:"gid"`
	ID        types.Int64  `tfsdk:"id"`
	Signature types.String `tfsdk:"signature"`
	Type      types.String `tfsdk:"type"`
	Tracking  types.List   `tfsdk:"tracking"`
}

// settingIpsModel is the Terraform model for the "ips" (Intrusion Prevention
// System) section (settingResourceModel.Ips), the deepest section: three
// object lists (Honeypot, SuppressionAlerts, SuppressionWhitelist), one of
// which (SuppressionAlerts) nests a further object list (Tracking) of its
// own. SuppressionAlerts/SuppressionWhitelist are flattened to top-level
// attributes here even though the controller wire nests both under a
// "suppression" object — ipsSection's decode/overlay glue that wrapper by
// hand.
type settingIpsModel struct {
	AdvancedFilteringPreference         types.String `tfsdk:"advanced_filtering_preference"`
	ContentFilteringBlockingPageEnabled types.Bool   `tfsdk:"content_filtering_blocking_page_enabled"`
	EnabledCategories                   types.List   `tfsdk:"enabled_categories"`
	EnabledNetworks                     types.List   `tfsdk:"enabled_networks"`
	Honeypot                            types.List   `tfsdk:"honeypot"`
	HoneypotEnabled                     types.Bool   `tfsdk:"honeypot_enabled"`
	IPSMode                             types.String `tfsdk:"ips_mode"`
	MemoryOptimized                     types.Bool   `tfsdk:"memory_optimized"`
	RestrictTorrents                    types.Bool   `tfsdk:"restrict_torrents"`
	SuppressionWhitelist                types.List   `tfsdk:"suppression_whitelist"`
	SuppressionAlerts                   types.List   `tfsdk:"suppression_alerts"`
}

// settingResourceModel is the top-level Terraform state model for
// unifi_setting. Each section field (AutoSpeedtest, Country, ...) is a
// types.Object that is null when the user has not configured that section
// in HCL; settingSection.isConfigured reads that null/unknown state to
// decide whether Create/Update pushes the section to the controller at all.
type settingResourceModel struct {
	ID            types.String   `tfsdk:"id"`
	Site          types.String   `tfsdk:"site"`
	AutoSpeedtest types.Object   `tfsdk:"auto_speedtest"`
	Country       types.Object   `tfsdk:"country"`
	Dpi           types.Object   `tfsdk:"dpi"`
	Lcm           types.Object   `tfsdk:"lcm"`
	NetworkOpt    types.Object   `tfsdk:"network_optimization"`
	Ntp           types.Object   `tfsdk:"ntp"`
	Syslog        types.Object   `tfsdk:"syslog"`
	Doh           types.Object   `tfsdk:"doh"`
	Ips           types.Object   `tfsdk:"ips"`
	Mgmt          types.Object   `tfsdk:"mgmt"`
	Radius        types.Object   `tfsdk:"radius"`
	USG           types.Object   `tfsdk:"usg"`
	IgmpSnooping  types.Object   `tfsdk:"igmp_snooping"`
	Locale        types.Object   `tfsdk:"locale"`
	Timeouts      timeouts.Value `tfsdk:"timeouts"`
}

// settingLocaleModel is the Terraform model for the "locale" settings
// section (settingResourceModel.Locale): the site's configured timezone.
type settingLocaleModel struct {
	Timezone types.String `tfsdk:"timezone"`
}

// settingIgmpSnoopingModel is the nested igmp_snooping block. On UniFi 10.3.x the
// effective IGMP snooping toggle moved from the per-network object to this site
// setting (#164). Only the common fields are exposed; advanced querier/flood
// fields are preserved across updates via a read-modify-write merge.
type settingIgmpSnoopingModel struct {
	Enabled    types.Bool `tfsdk:"enabled"`
	NetworkIDs types.List `tfsdk:"network_ids"`
}

// Shared attribute-type maps for the doh/ips nested objects and lists. These
// are referenced from both readSettings and the *SettingToModel conversion
// helpers, so they live at package level to avoid drift between the two.
var (
	autoSpeedtestAttrTypes = map[string]attr.Type{
		"enabled":   types.BoolType,
		"cron_expr": types.StringType,
	}
	mgmtSSHKeyAttrTypes = map[string]attr.Type{
		"name":    types.StringType,
		"type":    types.StringType,
		"key":     types.StringType,
		"comment": types.StringType,
	}
	mgmtAttrTypes = map[string]attr.Type{
		"auto_upgrade":      types.BoolType,
		"auto_upgrade_hour": types.Int64Type,
		"ssh_enabled":       types.BoolType,
		"ssh_keys": types.ListType{
			ElemType: types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes},
		},
		"advanced_feature_enabled":  types.BoolType,
		"debug_tools_enabled":       types.BoolType,
		"direct_connect_enabled":    types.BoolType,
		"unifi_idp_enabled":         types.BoolType,
		"wifiman_enabled":           types.BoolType,
		"ssh_username":              types.StringType,
		"ssh_password":              types.StringType,
		"ssh_auth_password_enabled": types.BoolType,
	}
	countryAttrTypes = map[string]attr.Type{
		"code": types.Int64Type,
	}
	dpiAttrTypes = map[string]attr.Type{
		"enabled":                types.BoolType,
		"fingerprinting_enabled": types.BoolType,
	}
	lcmAttrTypes = map[string]attr.Type{
		"enabled":      types.BoolType,
		"brightness":   types.Int64Type,
		"idle_timeout": types.Int64Type,
		"sync":         types.BoolType,
		"touch_event":  types.BoolType,
	}
	networkOptimizationAttrTypes = map[string]attr.Type{
		"enabled": types.BoolType,
	}
	ntpAttrTypes = map[string]attr.Type{
		"ntp_server_1":       types.StringType,
		"ntp_server_2":       types.StringType,
		"ntp_server_3":       types.StringType,
		"ntp_server_4":       types.StringType,
		"setting_preference": types.StringType,
	}
	syslogAttrTypes = map[string]attr.Type{
		"enabled":                        types.BoolType,
		"contents":                       types.ListType{ElemType: types.StringType},
		"debug":                          types.BoolType,
		"ip":                             types.StringType,
		"port":                           types.Int64Type,
		"log_all_contents":               types.BoolType,
		"netconsole_enabled":             types.BoolType,
		"netconsole_host":                types.StringType,
		"netconsole_port":                types.Int64Type,
		"this_controller":                types.BoolType,
		"this_controller_encrypted_only": types.BoolType,
	}
	dohCustomServerAttrTypes = map[string]attr.Type{
		"enabled":     types.BoolType,
		"sdns_stamp":  types.StringType,
		"server_name": types.StringType,
	}
	dohAttrTypes = map[string]attr.Type{
		"state":        types.StringType,
		"server_names": types.ListType{ElemType: types.StringType},
		"custom_servers": types.ListType{
			ElemType: types.ObjectType{AttrTypes: dohCustomServerAttrTypes},
		},
	}
	ipsHoneypotAttrTypes = map[string]attr.Type{
		"ip_address": types.StringType,
		"network_id": types.StringType,
		"version":    types.StringType,
	}
	ipsWhitelistAttrTypes = map[string]attr.Type{
		"direction": types.StringType,
		"mode":      types.StringType,
		"value":     types.StringType,
	}
	ipsTrackingAttrTypes = map[string]attr.Type{
		"direction": types.StringType,
		"mode":      types.StringType,
		"value":     types.StringType,
	}
	ipsAlertAttrTypes = map[string]attr.Type{
		"category":  types.StringType,
		"gid":       types.Int64Type,
		"id":        types.Int64Type,
		"signature": types.StringType,
		"type":      types.StringType,
		"tracking":  types.ListType{ElemType: types.ObjectType{AttrTypes: ipsTrackingAttrTypes}},
	}
	ipsAttrTypes = map[string]attr.Type{
		"advanced_filtering_preference":           types.StringType,
		"content_filtering_blocking_page_enabled": types.BoolType,
		"enabled_categories":                      types.ListType{ElemType: types.StringType},
		"enabled_networks":                        types.ListType{ElemType: types.StringType},
		"honeypot_enabled":                        types.BoolType,
		"honeypot": types.ListType{
			ElemType: types.ObjectType{AttrTypes: ipsHoneypotAttrTypes},
		},
		"ips_mode":          types.StringType,
		"memory_optimized":  types.BoolType,
		"restrict_torrents": types.BoolType,
		"suppression_whitelist": types.ListType{
			ElemType: types.ObjectType{AttrTypes: ipsWhitelistAttrTypes},
		},
		"suppression_alerts": types.ListType{
			ElemType: types.ObjectType{AttrTypes: ipsAlertAttrTypes},
		},
	}
	igmpSnoopingAttrTypes = map[string]attr.Type{
		"enabled":     types.BoolType,
		"network_ids": types.ListType{ElemType: types.StringType},
	}
	localeAttrTypes = map[string]attr.Type{
		"timezone": types.StringType,
	}
)

// Metadata sets the resource's Terraform type name to "<provider>_setting".
func (r *settingResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_setting"
}

// Schema builds the resource schema by combining the fixed id/site/timeouts
// attributes with one attribute per registered settingSection, in
// registration order (orderedSections). Adding a new section therefore
// extends the schema automatically without an edit here.
func (r *settingResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			MarkdownDescription: "The ID of the settings.",
			Computed:            true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		},
		"site": schema.StringAttribute{
			MarkdownDescription: "The name of the site to associate the settings with.",
			Optional:            true,
			Computed:            true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
				stringplanmodifier.UseStateForUnknown(),
			},
		},
	}
	for _, s := range orderedSections(settingSections) {
		attrs[s.attrName()] = s.schemaAttribute()
	}
	attrs["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)

	resp.Schema = schema.Schema{
		// v1: radius.interim_update_interval and the usg conntrack timeouts
		// changed from Int64 (seconds) to GoDuration strings. See UpgradeState.
		Version:             1,
		MarkdownDescription: "Manages settings for a UniFi site. Configure only the settings you need by providing the corresponding nested object.",
		Attributes:          attrs,
	}
}

// UpgradeState migrates v0 state to v1: radius.interim_update_interval and the
// usg conntrack timeouts changed from integer seconds to GoDuration strings.
func (r *settingResource) UpgradeState(
	ctx context.Context,
) map[int64]resource.StateUpgrader {
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	schemaType := schemaResp.Schema.Type().TerraformType(ctx)

	conntrack := []string{
		"icmp_timeout", "other_timeout",
		"tcp_close_timeout", "tcp_close_wait_timeout", "tcp_established_timeout",
		"tcp_fin_wait_timeout", "tcp_last_ack_timeout", "tcp_syn_recv_timeout",
		"tcp_syn_sent_timeout", "tcp_time_wait_timeout",
		"udp_other_timeout", "udp_stream_timeout",
	}

	return map[int64]resource.StateUpgrader{
		0: {
			StateUpgrader: func(
				ctx context.Context,
				req resource.UpgradeStateRequest,
				resp *resource.UpgradeStateResponse,
			) {
				if req.RawState == nil {
					return
				}
				dv, err := util.UpgradeDurationRawState(
					schemaType,
					req.RawState.JSON,
					func(state map[string]any) {
						if radius, ok := state["radius"].(map[string]any); ok {
							util.SetDurationField(radius, "interim_update_interval", time.Second)
						}
						if usg, ok := state["usg"].(map[string]any); ok {
							for _, n := range conntrack {
								util.SetDurationField(usg, n, time.Second)
							}
						}
					},
				)
				if err != nil {
					resp.Diagnostics.AddError("Failed to upgrade settings state", err.Error())
					return
				}
				resp.DynamicValue = dv
			},
		},
	}
}

// Configure wires the provider's shared *Client into the resource. It is a
// no-op when ProviderData is nil (the resource is being validated without a
// configured provider) and errors if ProviderData is not a *Client.
func (r *settingResource) Configure(
	ctx context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf(
				"Expected *Client, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}

	r.client = client
}

// Create applies every section configured in req.Config against a zero
// prior model — since nothing exists yet, each configured section is
// treated as new. It scopes the write to configuredSections(config) rather
// than plan (see configuredSections for why) and persists the best-effort
// resulting state before appending any apply diagnostics, so a partial
// failure still leaves Terraform with the best-known state.
func (r *settingResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data settingResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, timeoutDiags := data.Timeouts.Create(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	var config settingResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client := realSettingsClient{r.client}
	site := resolveSite(data.Site.ValueString(), r.client.Site)

	// Nothing exists yet: the prior state for the engine's reconcile is a
	// zero model, so every configured section in data is treated as new.
	// The section SET passed to applySections is derived from CONFIG (the
	// authoritative user-configured signal), not plan — see
	// configuredSections. plan is still used for the payload VALUES.
	newModel, applyDiags := applySections(ctx, configuredSections(config), client, site, data, settingResourceModel{})

	// applySections/readSections only populate the 13 section fields. id,
	// site, and timeouts are resource-level, not section fields, so they
	// must be set explicitly here — this mirrors what legacy readSettings
	// did at id/site assignment (settings are site-level: id == site).
	newModel.ID = types.StringValue(site)
	newModel.Site = types.StringValue(site)
	newModel.Timeouts = data.Timeouts

	// Framework-state conformance (Task 8): persist best-effort state
	// BEFORE appending the apply diagnostics, so a partial-apply error
	// still leaves Terraform with the best-known state rather than none.
	resp.Diagnostics.Append(resp.State.Set(ctx, &newModel)...)
	resp.Diagnostics.Append(applyDiags...)
}

// allSectionAttrsNull reports whether every registered section attribute is
// null in m — the shape produced by ImportState (before the first Read
// hydrates them). When true, Read hydrates ALL sections (onlyConfigured=
// false) so an imported resource observes every section as Computed
// (UseStateForUnknown keeps them stable -> a subsequent no-config plan is
// clean). Otherwise Read refreshes only the configured sections.
//
// This is an explicit 13-field check, not derived from the section registry
// — if a 14th section is ever added, it must be updated here too (acceptable
// for PR-A's fixed 13; a future refactor could derive it from the registry
// if a section gains a model-field accessor).
func allSectionAttrsNull(m settingResourceModel) bool {
	return m.AutoSpeedtest.IsNull() && m.Country.IsNull() && m.Dpi.IsNull() &&
		m.Lcm.IsNull() && m.NetworkOpt.IsNull() && m.Ntp.IsNull() &&
		m.Syslog.IsNull() && m.Doh.IsNull() && m.Ips.IsNull() &&
		m.Mgmt.IsNull() && m.Radius.IsNull() && m.USG.IsNull() &&
		m.IgmpSnooping.IsNull() && m.Locale.IsNull()
}

// configuredSections returns the registered sections the user configured in
// m (its object attribute is non-null and known) — used by Create/Update to
// scope applySections to the WRITE path's authoritative signal. Callers must
// pass the CONFIG model here, not plan: after a site-name import, Read
// hydrates every supported section as Computed and UseStateForUnknown fills
// the plan with those hydrated blocks even though the user never configured
// them in HCL, so isConfigured(plan) would over-select. Config has no such
// fill — a section the user omitted stays null in config regardless of what
// Read hydrated into state/plan. Read's own state-based filter is unrelated
// and unchanged: Read OBSERVES all sections in state, only the WRITE path
// (Create/Update) must be config-scoped.
func configuredSections(m settingResourceModel) []settingSection {
	out := make([]settingSection, 0, len(settingSections))
	for _, s := range settingSections {
		if s.isConfigured(m) {
			out = append(out, s)
		}
	}
	return out
}

// Read re-fetches the raw settings snapshot and overlays it onto state. If
// state has every section attribute null (the shape ImportState produces),
// it hydrates every registered section as Computed so a freshly imported
// resource gets a full picture; otherwise it refreshes only the sections
// the user has configured, matching legacy behavior and avoiding a
// fail-closed error for a section absent on this controller that the user
// never asked for.
func (r *settingResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data settingResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, timeoutDiags := data.Timeouts.Read(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	client := realSettingsClient{r.client}
	site := resolveSite(data.Site.ValueString(), r.client.Site)

	// Hydration gate: state with every section attribute null is exactly the
	// shape ImportState produces (a bare site name, no configured sections
	// yet) — in that case hydrate ALL registered sections as Computed
	// (onlyConfigured=false); that path already skips any section this
	// controller doesn't support rather than erroring, so it hydrates
	// whatever is actually available. UseStateForUnknown then holds every
	// hydrated section stable, giving a clean subsequent no-config plan.
	//
	// Any other state (at least one section already populated, i.e. normal
	// steady-state refresh) refreshes only the sections the user actually
	// configured (isConfigured), matching legacy readSettings' behavior and
	// avoiding a spurious fail-closed error for a section the user never
	// configured that happens to be absent from this controller (e.g.
	// radius/usg on a gateway-less UDM). A configured-but-absent section
	// stays in that filtered set, so it still fails closed — the user
	// asserted it should exist.
	if allSectionAttrsNull(data) {
		resp.Diagnostics.Append(readSections(ctx, settingSections, client, site, data, &data, false)...)
	} else {
		configured := make([]settingSection, 0, len(settingSections))
		for _, s := range settingSections {
			if s.isConfigured(data) {
				configured = append(configured, s)
			}
		}
		resp.Diagnostics.Append(readSections(ctx, configured, client, site, data, &data, true)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// id/site are resource-level, not section fields — readSections does
	// not touch them, so set them explicitly (mirrors legacy readSettings:
	// settings are site-level, id == site).
	data.ID = types.StringValue(site)
	data.Site = types.StringValue(site)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update applies every section configured in req.Config, scoped the same
// way as Create (configuredSections(config), not plan — see
// configuredSections), using plan for the payload values and state as the
// prior model for read-modify-write merges. Like Create, it persists the
// best-effort resulting state before appending any apply diagnostics.
func (r *settingResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var state settingResourceModel
	var plan settingResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var config settingResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, timeoutDiags := plan.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	client := realSettingsClient{r.client}
	site := resolveSite(state.Site.ValueString(), r.client.Site)

	// The section SET passed to applySections is derived from CONFIG (the
	// authoritative user-configured signal), not plan: after a site-name
	// import, Read hydrates every supported section as Computed and
	// UseStateForUnknown fills the plan with those hydrated blocks even
	// though the user never configured them in HCL. Scoping to config keeps
	// an update from over-managing (re-PUTting, or fail-closing on) sections
	// the user never wrote — see configuredSections. plan is still used for
	// the payload VALUES within each configured section.
	newModel, applyDiags := applySections(ctx, configuredSections(config), client, site, plan, state)

	// applySections/readSections only populate the 13 section fields. id,
	// site, and timeouts are resource-level, not section fields, so they
	// must be set explicitly here — this mirrors what legacy readSettings
	// did at id/site assignment (settings are site-level: id == site).
	newModel.ID = types.StringValue(site)
	newModel.Site = types.StringValue(site)
	newModel.Timeouts = plan.Timeouts

	// Framework-state conformance (Task 8): persist best-effort state
	// BEFORE appending the apply diagnostics, so a partial-apply error
	// still leaves Terraform with the best-known state rather than none.
	resp.Diagnostics.Append(resp.State.Set(ctx, &newModel)...)
	resp.Diagnostics.Append(applyDiags...)
}

// Delete only removes the resource from Terraform state. Settings cannot be
// deleted on the controller, only reset to defaults, so no API call is made.
func (r *settingResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	// Settings cannot be deleted, only reset to defaults
	// Just remove from state
}

// ImportState accepts a bare site name (e.g. "default"), NOT the "site:id"
// composite ImportStatePassthroughID (and the NAT/CF resources) use —
// unifi_setting is site-level, so the site name alone fully identifies it.
// id and site are both set to that name; all 13 section attributes are left
// null (the imported shape), and the first Read hydrates them in full — see
// the hydration gate in Read and allSectionAttrsNull below.
func (r *settingResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	siteName := strings.TrimSpace(req.ID)
	if siteName == "" || strings.Contains(siteName, ":") {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf(
				"unifi_setting import ID must be a bare site name (e.g. %q), not a composite %q — settings are site-level.",
				"default",
				req.ID,
			),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), siteName)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), siteName)...)
	// All 13 section attributes are left null (the imported shape); the first
	// Read hydrates them (see the Read hydration gate above).
}
