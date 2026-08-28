package unifi

import (
	"context"
	"errors"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

var (
	_ resource.Resource                 = &settingResource{}
	_ resource.ResourceWithImportState  = &settingResource{}
	_ resource.ResourceWithUpgradeState = &settingResource{}
)

func NewSettingResource() resource.Resource {
	r := &settingResource{}
	r.Site = func(m *settingResourceModel) *types.String { return &m.Site }
	r.ID = func(m *settingResourceModel) *types.String { return &m.ID }
	r.Timeouts = func(m *settingResourceModel) *timeouts.Value { return &m.Timeouts }
	return r
}

// settingResource is served by resourcekit.Composite: unifi_setting is one
// Terraform resource over thirteen independently-written settings documents,
// not one document the way every other kit-served surface is. Metadata,
// Schema, ImportState and UpgradeState stay hand-written below rather than
// promoted from Composite -- see composite.go's own doc comment for why.
type settingResource struct {
	client *Client
	resourcekit.Composite[settingResourceModel]
}

type sshKeyModel struct {
	Name    types.String `tfsdk:"name"`
	Type    types.String `tfsdk:"type"`
	Key     types.String `tfsdk:"key"`
	Comment types.String `tfsdk:"comment"`
}

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

type settingRadiusModel struct {
	AccountingEnabled     types.Bool           `tfsdk:"accounting_enabled"`
	Enabled               types.Bool           `tfsdk:"enabled"`
	AcctPort              types.Int64          `tfsdk:"acct_port"`
	AuthPort              types.Int64          `tfsdk:"auth_port"`
	InterimUpdateInterval timetypes.GoDuration `tfsdk:"interim_update_interval"`
	Secret                types.String         `tfsdk:"secret"`
}

type dnsVerificationModel struct {
	Domain             types.String `tfsdk:"domain"`
	PrimaryDNSServer   types.String `tfsdk:"primary_dns_server"`
	SecondaryDNSServer types.String `tfsdk:"secondary_dns_server"`
	SettingPreference  types.String `tfsdk:"setting_preference"`
}

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

type settingDohCustomServerModel struct {
	Enabled    types.Bool   `tfsdk:"enabled"`
	SDNSStamp  types.String `tfsdk:"sdns_stamp"`
	ServerName types.String `tfsdk:"server_name"`
}

type settingAutoSpeedtestModel struct {
	Enabled  types.Bool   `tfsdk:"enabled"`
	CronExpr types.String `tfsdk:"cron_expr"`
}

type settingCountryModel struct {
	Code types.Int64 `tfsdk:"code"`
}

type settingDpiModel struct {
	Enabled               types.Bool `tfsdk:"enabled"`
	FingerprintingEnabled types.Bool `tfsdk:"fingerprinting_enabled"`
}

type settingLcmModel struct {
	Enabled     types.Bool  `tfsdk:"enabled"`
	Brightness  types.Int64 `tfsdk:"brightness"`
	IdleTimeout types.Int64 `tfsdk:"idle_timeout"`
	Sync        types.Bool  `tfsdk:"sync"`
	TouchEvent  types.Bool  `tfsdk:"touch_event"`
}

type settingNetworkOptimizationModel struct {
	Enabled types.Bool `tfsdk:"enabled"`
}

type settingNtpModel struct {
	NtpServer1        types.String `tfsdk:"ntp_server_1"`
	NtpServer2        types.String `tfsdk:"ntp_server_2"`
	NtpServer3        types.String `tfsdk:"ntp_server_3"`
	NtpServer4        types.String `tfsdk:"ntp_server_4"`
	SettingPreference types.String `tfsdk:"setting_preference"`
}

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

type settingDohModel struct {
	CustomServers types.List   `tfsdk:"custom_servers"`
	ServerNames   types.List   `tfsdk:"server_names"`
	State         types.String `tfsdk:"state"`
}

type settingIpsHoneypotModel struct {
	IPAddress types.String `tfsdk:"ip_address"`
	NetworkID types.String `tfsdk:"network_id"`
	Version   types.String `tfsdk:"version"`
}

type settingIpsWhitelistModel struct {
	Direction types.String `tfsdk:"direction"`
	Mode      types.String `tfsdk:"mode"`
	Value     types.String `tfsdk:"value"`
}

type settingIpsTrackingModel struct {
	Direction types.String `tfsdk:"direction"`
	Mode      types.String `tfsdk:"mode"`
	Value     types.String `tfsdk:"value"`
}

type settingIpsAlertModel struct {
	Category  types.String `tfsdk:"category"`
	Gid       types.Int64  `tfsdk:"gid"`
	ID        types.Int64  `tfsdk:"id"`
	Signature types.String `tfsdk:"signature"`
	Type      types.String `tfsdk:"type"`
	Tracking  types.List   `tfsdk:"tracking"`
}

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
	Timeouts      timeouts.Value `tfsdk:"timeouts"`
}

// settingIgmpSnoopingModel is the nested igmp_snooping block: on UniFi 10.3.x the
// effective toggle moved here from the per-network object (#164); advanced querier/flood fields are preserved via read-modify-write.
type settingIgmpSnoopingModel struct {
	Enabled    types.Bool `tfsdk:"enabled"`
	NetworkIDs types.List `tfsdk:"network_ids"`
}

// Shared attribute-type maps for the doh/ips nested objects, referenced from
// both readSettings and the *SettingToModel helpers -- package level to avoid drift between the two.
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
)

func (r *settingResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_setting"
}

func (r *settingResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_setting.SettingResourceSchema(ctx)
	// v1: radius.interim_update_interval and the usg conntrack timeouts
	// changed from Int64 (seconds) to GoDuration strings. See UpgradeState.
	//
	// Re-set by hand: a generated schema can't carry a version; left at zero,
	// Terraform never runs the upgrader and prior state silently stops migrating.
	resp.Schema.Version = 1
	// Load-bearing for the upgrade path too: UpgradeState derives its prior type
	// from this schema and decodes twelve conntrack durations through it; without this graft that type differs and prior-version state stops decoding.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)

	// Re-set by hand: the generator can't express a PlanModifier, so an omitted
	// ntp slot would replan as unknown on every change despite the controller still reporting its prior value; UseStateForUnknown pins it (#382).
	if ntp, ok := resp.Schema.Attributes["ntp"].(schema.SingleNestedAttribute); ok {
		for _, key := range []string{"ntp_server_1", "ntp_server_2", "ntp_server_3", "ntp_server_4"} {
			if server, ok := ntp.Attributes[key].(schema.StringAttribute); ok {
				server.PlanModifiers = []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				}
				ntp.Attributes[key] = server
			}
		}
		resp.Schema.Attributes["ntp"] = ntp
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

func (r *settingResource) Configure(
	ctx context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}

	r.client = client
	r.DefaultSite = client.Site
	r.Sections = legacySectionsFor(r)
}

func (r *settingResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(
		ctx,
		path.Root("id"),
		req,
		resp,
	)
}

// Mgmt conversion functions.
func (r *settingResource) mgmtModelToSetting(
	ctx context.Context,
	model *settingMgmtModel,
	base *settings.Mgmt,
) *settings.Mgmt {
	setting := base

	if !model.AutoUpgrade.IsNull() && !model.AutoUpgrade.IsUnknown() {
		setting.AutoUpgrade = model.AutoUpgrade.ValueBool()
	}
	if !model.AutoUpgradeHour.IsNull() && !model.AutoUpgradeHour.IsUnknown() {
		setting.AutoUpgradeHour = model.AutoUpgradeHour.ValueInt64Pointer()
	}
	if !model.SSHEnabled.IsNull() && !model.SSHEnabled.IsUnknown() {
		setting.SSHEnabled = model.SSHEnabled.ValueBool()
	}
	if !model.AdvancedFeatureEnabled.IsNull() && !model.AdvancedFeatureEnabled.IsUnknown() {
		setting.AdvancedFeatureEnabled = model.AdvancedFeatureEnabled.ValueBool()
	}
	if !model.DebugToolsEnabled.IsNull() && !model.DebugToolsEnabled.IsUnknown() {
		setting.DebugToolsEnabled = model.DebugToolsEnabled.ValueBool()
	}
	if !model.DirectConnectEnabled.IsNull() && !model.DirectConnectEnabled.IsUnknown() {
		setting.DirectConnectEnabled = model.DirectConnectEnabled.ValueBool()
	}
	if !model.UnifiIdpEnabled.IsNull() && !model.UnifiIdpEnabled.IsUnknown() {
		setting.UniFiIdentityProviderEnabled = model.UnifiIdpEnabled.ValueBool()
	}
	if !model.WifimanEnabled.IsNull() && !model.WifimanEnabled.IsUnknown() {
		setting.WifimanEnabled = model.WifimanEnabled.ValueBool()
	}
	if !model.SSHUsername.IsNull() && !model.SSHUsername.IsUnknown() {
		setting.SSHUsername = model.SSHUsername.ValueString()
	}
	if !model.SSHPassword.IsNull() && !model.SSHPassword.IsUnknown() {
		setting.SSHPassword = model.SSHPassword.ValueString()
	}
	if !model.SSHAuthPasswordEnabled.IsNull() && !model.SSHAuthPasswordEnabled.IsUnknown() {
		setting.SSHAuthPasswordEnabled = model.SSHAuthPasswordEnabled.ValueBool()
	}

	if !model.SSHKeys.IsNull() && !model.SSHKeys.IsUnknown() {
		setting.SSHKeys = nil
		var sshKeys []sshKeyModel
		model.SSHKeys.ElementsAs(ctx, &sshKeys, false)
		for _, sshKey := range sshKeys {
			setting.SSHKeys = append(setting.SSHKeys, settings.SettingMgmtSSHKeys{
				Name:    sshKey.Name.ValueString(),
				KeyType: sshKey.Type.ValueString(),
				Key:     sshKey.Key.ValueString(),
				Comment: sshKey.Comment.ValueString(),
			})
		}
	}

	return setting
}

func (r *settingResource) mgmtSettingToModel(
	ctx context.Context,
	setting *settings.Mgmt,
	plan *settingMgmtModel,
) *settingMgmtModel {
	model := &settingMgmtModel{}

	// Only populate fields that were explicitly configured in the plan, so the
	// resource doesn't report drift on settings the user doesn't manage.
	boolOrNull := func(planVal types.Bool, apiVal bool) types.Bool {
		if !planVal.IsNull() && !planVal.IsUnknown() {
			return types.BoolValue(apiVal)
		}
		return types.BoolNull()
	}

	model.AutoUpgrade = boolOrNull(plan.AutoUpgrade, setting.AutoUpgrade)
	model.SSHEnabled = boolOrNull(plan.SSHEnabled, setting.SSHEnabled)
	model.AdvancedFeatureEnabled = boolOrNull(
		plan.AdvancedFeatureEnabled, setting.AdvancedFeatureEnabled,
	)
	model.DebugToolsEnabled = boolOrNull(plan.DebugToolsEnabled, setting.DebugToolsEnabled)
	model.DirectConnectEnabled = boolOrNull(plan.DirectConnectEnabled, setting.DirectConnectEnabled)
	model.UnifiIdpEnabled = boolOrNull(plan.UnifiIdpEnabled, setting.UniFiIdentityProviderEnabled)
	model.WifimanEnabled = boolOrNull(plan.WifimanEnabled, setting.WifimanEnabled)
	model.SSHAuthPasswordEnabled = boolOrNull(
		plan.SSHAuthPasswordEnabled, setting.SSHAuthPasswordEnabled,
	)

	if !plan.AutoUpgradeHour.IsNull() && !plan.AutoUpgradeHour.IsUnknown() {
		model.AutoUpgradeHour = types.Int64PointerValue(setting.AutoUpgradeHour)
	} else {
		model.AutoUpgradeHour = types.Int64Null()
	}

	if !plan.SSHUsername.IsNull() && !plan.SSHUsername.IsUnknown() {
		model.SSHUsername = util.StringValueOrNull(setting.SSHUsername)
	} else {
		model.SSHUsername = types.StringNull()
	}

	// The controller never returns the plaintext SSH password (only hashes), so
	// preserve the configured value to avoid a perpetual diff.
	model.SSHPassword = plan.SSHPassword

	if !plan.SSHKeys.IsNull() && !plan.SSHKeys.IsUnknown() {
		if len(setting.SSHKeys) > 0 {
			var sshKeys []sshKeyModel
			for _, sshKey := range setting.SSHKeys {
				sshKeys = append(sshKeys, sshKeyModel{
					Name:    types.StringValue(sshKey.Name),
					Type:    types.StringValue(sshKey.KeyType),
					Key:     types.StringValue(sshKey.Key),
					Comment: types.StringValue(sshKey.Comment),
				})
			}
			listValue, _ := types.ListValueFrom(
				ctx, types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes}, sshKeys,
			)
			model.SSHKeys = listValue
		} else {
			model.SSHKeys = types.ListNull(types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes})
		}
	} else {
		model.SSHKeys = types.ListNull(types.ObjectType{AttrTypes: mgmtSSHKeyAttrTypes})
	}

	return model
}

// Radius conversion functions.
func (r *settingResource) radiusModelToSetting(
	_ context.Context,
	model *settingRadiusModel,
	base *settings.Radius,
) *settings.Radius {
	setting := base

	if !model.AccountingEnabled.IsNull() && !model.AccountingEnabled.IsUnknown() {
		setting.AccountingEnabled = model.AccountingEnabled.ValueBool()
	}
	if !model.Enabled.IsNull() && !model.Enabled.IsUnknown() {
		setting.Enabled = model.Enabled.ValueBool()
	}
	if !model.AcctPort.IsNull() && !model.AcctPort.IsUnknown() {
		setting.AcctPort = model.AcctPort.ValueInt64Pointer()
	}
	if !model.AuthPort.IsNull() && !model.AuthPort.IsUnknown() {
		setting.AuthPort = model.AuthPort.ValueInt64Pointer()
	}
	if !model.InterimUpdateInterval.IsNull() && !model.InterimUpdateInterval.IsUnknown() {
		setting.InterimUpdateInterval = util.DurationUnitsPtr(
			model.InterimUpdateInterval,
			time.Second,
		)
	}
	if !model.Secret.IsNull() && !model.Secret.IsUnknown() {
		setting.Secret = model.Secret.ValueString()
	}

	return setting
}

func (r *settingResource) radiusSettingToModel(
	_ context.Context,
	setting *settings.Radius,
	plan *settingRadiusModel,
) *settingRadiusModel {
	model := &settingRadiusModel{}

	model.AccountingEnabled = types.BoolValue(setting.AccountingEnabled)

	model.Enabled = types.BoolValue(setting.Enabled)

	model.AcctPort = types.Int64PointerValue(setting.AcctPort)

	model.AuthPort = types.Int64PointerValue(setting.AuthPort)

	model.InterimUpdateInterval = util.DurationPtrValue(setting.InterimUpdateInterval, time.Second)

	if !plan.Secret.IsNull() && !plan.Secret.IsUnknown() {
		model.Secret = util.StringValueOrNull(setting.Secret)
	} else {
		model.Secret = types.StringNull()
	}

	return model
}

// USG conversion functions.
// usgGeoConfigured reports whether the practitioner manages any geo IP filtering
// attribute; these moved off `usg` onto a separate `usg_geo` object in UniFi
// Network 10.x, written only when configured so an unconditional write can't clobber an out-of-Terraform geo config.
func usgGeoConfigured(model *settingUSGModel) bool {
	for _, v := range []attr.Value{
		model.GeoIPFilteringBlock,
		model.GeoIPFilteringCountries,
		model.GeoIPFilteringEnabled,
		model.GeoIPFilteringTrafficDirection,
	} {
		if !v.IsNull() && !v.IsUnknown() {
			return true
		}
	}
	return false
}

// usgGeoModelToSetting overlays configured geo IP filtering attributes onto what
// the controller stores; usg_geo's `enabled` has no omitempty, so unconfigured fields are carried over rather than reset to zero.
func (r *settingResource) usgGeoModelToSetting(
	model *settingUSGModel,
	current *settings.UsgGeo,
) *settings.UsgGeo {
	setting := current
	if setting == nil {
		setting = &settings.UsgGeo{}
	}
	if setting.IPFiltering == nil {
		setting.IPFiltering = &settings.SettingUsgGeoIPFiltering{}
	}

	if !model.GeoIPFilteringBlock.IsNull() {
		setting.IPFiltering.Action = model.GeoIPFilteringBlock.ValueString()
	}
	if !model.GeoIPFilteringCountries.IsNull() {
		setting.IPFiltering.Countries = model.GeoIPFilteringCountries.ValueString()
	}
	if !model.GeoIPFilteringEnabled.IsNull() {
		setting.IPFiltering.Enabled = model.GeoIPFilteringEnabled.ValueBool()
	}
	if !model.GeoIPFilteringTrafficDirection.IsNull() {
		setting.IPFiltering.TrafficDirection = model.GeoIPFilteringTrafficDirection.ValueString()
	}

	return setting
}

// writeUsgGeo writes the usg_geo setting, but only when the practitioner manages
// at least one geo IP filtering attribute; verb ("Creating"/"Updating") is for the error message.
func (r *settingResource) writeUsgGeo(
	ctx context.Context,
	site string,
	model *settingUSGModel,
	verb string,
	diags *diag.Diagnostics,
) {
	if !usgGeoConfigured(model) {
		return
	}

	// Reads the stored object as the base so unmanaged fields survive the write;
	// absent is normal pre-configuration, so start from empty here -- if the controller truly lacks the endpoint, the write below says so.
	_, current, err := ui.GetSetting[*settings.UsgGeo](r.client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if !errors.As(err, &notFound) {
			diags.AddError("Error Reading USG Geo Setting", err.Error())
			return
		}
		current = &settings.UsgGeo{}
	}

	if err := r.client.UpdateSetting(
		ctx,
		site,
		r.usgGeoModelToSetting(model, current),
	); err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			diags.AddError(
				"Geo IP Filtering Not Supported By This Controller",
				"The `geo_ip_filtering_*` attributes are stored in the `usg_geo` setting, which "+
					"this controller does not expose. UniFi Network 10.x moved them out of the "+
					"`usg` setting. Remove them from the `usg` block, or upgrade the controller.",
			)
			return
		}
		diags.AddError("Error "+verb+" USG Geo Setting", err.Error())
	}
}

// usgModelToSetting overlays the model onto the setting the controller already
// holds, for the same reason mgmt, radius and igmpSnooping take a base: 23 of
// settings.Usg's 46 fields carry no omitempty, so a freshly built struct force-emits
// a Go zero for every field the schema doesn't declare. No mask is available (the
// SDK's UpdateSetting takes a settings.Setting interface, not per-field updates), so
// every assignment below is guarded on the model declaring a value, changing nothing for unmanaged fields.
func (r *settingResource) usgModelToSetting(
	ctx context.Context,
	model *settingUSGModel,
	base *settings.Usg,
) *settings.Usg {
	setting := base

	if !model.BroadcastPing.IsNull() {
		setting.BroadcastPing = model.BroadcastPing.ValueBool()
	}
	if !model.DNSVerification.IsNull() && !model.DNSVerification.IsUnknown() {
		var dnsVerif dnsVerificationModel
		model.DNSVerification.As(ctx, &dnsVerif, basetypes.ObjectAsOptions{})
		setting.DNSVerification = &settings.SettingUsgDNSVerification{
			Domain:             dnsVerif.Domain.ValueString(),
			PrimaryDNSServer:   dnsVerif.PrimaryDNSServer.ValueString(),
			SecondaryDNSServer: dnsVerif.SecondaryDNSServer.ValueString(),
			SettingPreference:  dnsVerif.SettingPreference.ValueString(),
		}
	}
	if !model.FtpModule.IsNull() {
		setting.FtpModule = model.FtpModule.ValueBool()
	}
	if !model.GreModule.IsNull() {
		setting.GreModule = model.GreModule.ValueBool()
	}
	if !model.H323Module.IsNull() {
		setting.H323Module = model.H323Module.ValueBool()
	}
	if !model.ICMPTimeout.IsNull() && !model.ICMPTimeout.IsUnknown() {
		setting.ICMPTimeout = util.DurationUnits(model.ICMPTimeout, time.Second)
	}
	if !model.MssClamp.IsNull() {
		setting.MssClamp = model.MssClamp.ValueString()
	}
	if !model.OffloadAccounting.IsNull() {
		setting.OffloadAccounting = model.OffloadAccounting.ValueBool()
	}
	if !model.OffloadL2Blocking.IsNull() {
		setting.OffloadL2Blocking = model.OffloadL2Blocking.ValueBool()
	}
	if !model.OffloadSch.IsNull() {
		setting.OffloadSch = model.OffloadSch.ValueBool()
	}
	if !model.OtherTimeout.IsNull() && !model.OtherTimeout.IsUnknown() {
		setting.OtherTimeout = util.DurationUnits(model.OtherTimeout, time.Second)
	}
	if !model.PptpModule.IsNull() {
		setting.PptpModule = model.PptpModule.ValueBool()
	}
	if !model.ReceiveRedirects.IsNull() {
		setting.ReceiveRedirects = model.ReceiveRedirects.ValueBool()
	}
	if !model.SendRedirects.IsNull() {
		setting.SendRedirects = model.SendRedirects.ValueBool()
	}
	if !model.SipModule.IsNull() {
		setting.SipModule = model.SipModule.ValueBool()
	}
	if !model.SynCookies.IsNull() {
		setting.SynCookies = model.SynCookies.ValueBool()
	}
	if !model.TCPCloseTimeout.IsNull() && !model.TCPCloseTimeout.IsUnknown() {
		setting.TCPCloseTimeout = util.DurationUnits(model.TCPCloseTimeout, time.Second)
	}
	if !model.TCPCloseWaitTimeout.IsNull() && !model.TCPCloseWaitTimeout.IsUnknown() {
		setting.TCPCloseWaitTimeout = util.DurationUnits(model.TCPCloseWaitTimeout, time.Second)
	}
	if !model.TCPEstablishedTimeout.IsNull() && !model.TCPEstablishedTimeout.IsUnknown() {
		setting.TCPEstablishedTimeout = util.DurationUnits(model.TCPEstablishedTimeout, time.Second)
	}
	if !model.TCPFinWaitTimeout.IsNull() && !model.TCPFinWaitTimeout.IsUnknown() {
		setting.TCPFinWaitTimeout = util.DurationUnits(model.TCPFinWaitTimeout, time.Second)
	}
	if !model.TCPLastAckTimeout.IsNull() && !model.TCPLastAckTimeout.IsUnknown() {
		setting.TCPLastAckTimeout = util.DurationUnits(model.TCPLastAckTimeout, time.Second)
	}
	if !model.TCPSynRecvTimeout.IsNull() && !model.TCPSynRecvTimeout.IsUnknown() {
		setting.TCPSynRecvTimeout = util.DurationUnits(model.TCPSynRecvTimeout, time.Second)
	}
	if !model.TCPSynSentTimeout.IsNull() && !model.TCPSynSentTimeout.IsUnknown() {
		setting.TCPSynSentTimeout = util.DurationUnits(model.TCPSynSentTimeout, time.Second)
	}
	if !model.TCPTimeWaitTimeout.IsNull() && !model.TCPTimeWaitTimeout.IsUnknown() {
		setting.TCPTimeWaitTimeout = util.DurationUnits(model.TCPTimeWaitTimeout, time.Second)
	}
	if !model.TFTPModule.IsNull() {
		setting.TFTPModule = model.TFTPModule.ValueBool()
	}
	if !model.TimeoutSettingPreference.IsNull() {
		setting.TimeoutSettingPreference = model.TimeoutSettingPreference.ValueString()
	}
	if !model.UDPOtherTimeout.IsNull() && !model.UDPOtherTimeout.IsUnknown() {
		setting.UDPOtherTimeout = util.DurationUnits(model.UDPOtherTimeout, time.Second)
	}
	if !model.UDPStreamTimeout.IsNull() && !model.UDPStreamTimeout.IsUnknown() {
		setting.UDPStreamTimeout = util.DurationUnits(model.UDPStreamTimeout, time.Second)
	}
	if !model.UnbindWANMonitors.IsNull() {
		setting.UnbindWANMonitors = model.UnbindWANMonitors.ValueBool()
	}
	if !model.UPnPEnabled.IsNull() {
		setting.UPnPEnabled = model.UPnPEnabled.ValueBool()
	}
	if !model.UPnPNATPmpEnabled.IsNull() {
		setting.UPnPNATPmpEnabled = model.UPnPNATPmpEnabled.ValueBool()
	}
	if !model.UPnPSecureMode.IsNull() {
		setting.UPnPSecureMode = model.UPnPSecureMode.ValueBool()
	}
	if !model.UPnPWANInterface.IsNull() {
		setting.UPnPWANInterface = model.UPnPWANInterface.ValueString()
	}

	return setting
}

func (r *settingResource) usgSettingToModel(
	ctx context.Context,
	setting *settings.Usg,
	geo *settings.UsgGeo,
	plan *settingUSGModel,
) *settingUSGModel {
	model := &settingUSGModel{}

	// usg_geo may be absent on controllers that predate the split, and its
	// IPFiltering object is only present once geo filtering has been touched.
	var geoFilter settings.SettingUsgGeoIPFiltering
	if geo != nil && geo.IPFiltering != nil {
		geoFilter = *geo.IPFiltering
	}

	// Only populate fields that were explicitly configured in the plan
	if !plan.BroadcastPing.IsNull() && !plan.BroadcastPing.IsUnknown() {
		model.BroadcastPing = types.BoolValue(setting.BroadcastPing)
	} else {
		model.BroadcastPing = types.BoolNull()
	}

	if !plan.DNSVerification.IsNull() && !plan.DNSVerification.IsUnknown() {
		dnsVerif := dnsVerificationModel{
			Domain:             types.StringValue(setting.DNSVerification.Domain),
			PrimaryDNSServer:   types.StringValue(setting.DNSVerification.PrimaryDNSServer),
			SecondaryDNSServer: types.StringValue(setting.DNSVerification.SecondaryDNSServer),
			SettingPreference:  types.StringValue(setting.DNSVerification.SettingPreference),
		}
		objValue, _ := types.ObjectValueFrom(ctx, map[string]attr.Type{
			"domain":               types.StringType,
			"primary_dns_server":   types.StringType,
			"secondary_dns_server": types.StringType,
			"setting_preference":   types.StringType,
		}, dnsVerif)
		model.DNSVerification = objValue
	} else {
		model.DNSVerification = types.ObjectNull(map[string]attr.Type{
			"domain":               types.StringType,
			"primary_dns_server":   types.StringType,
			"secondary_dns_server": types.StringType,
			"setting_preference":   types.StringType,
		})
	}

	if !plan.FtpModule.IsNull() && !plan.FtpModule.IsUnknown() {
		model.FtpModule = types.BoolValue(setting.FtpModule)
	} else {
		model.FtpModule = types.BoolNull()
	}

	if !plan.GeoIPFilteringBlock.IsNull() && !plan.GeoIPFilteringBlock.IsUnknown() {
		model.GeoIPFilteringBlock = util.StringValueOrNull(geoFilter.Action)
	} else {
		model.GeoIPFilteringBlock = types.StringNull()
	}

	if !plan.GeoIPFilteringCountries.IsNull() && !plan.GeoIPFilteringCountries.IsUnknown() {
		model.GeoIPFilteringCountries = util.StringValueOrNull(geoFilter.Countries)
	} else {
		model.GeoIPFilteringCountries = types.StringNull()
	}

	if !plan.GeoIPFilteringEnabled.IsNull() && !plan.GeoIPFilteringEnabled.IsUnknown() {
		model.GeoIPFilteringEnabled = types.BoolValue(geoFilter.Enabled)
	} else {
		model.GeoIPFilteringEnabled = types.BoolNull()
	}

	if !plan.GeoIPFilteringTrafficDirection.IsNull() &&
		!plan.GeoIPFilteringTrafficDirection.IsUnknown() {
		model.GeoIPFilteringTrafficDirection = util.StringValueOrNull(geoFilter.TrafficDirection)
	} else {
		model.GeoIPFilteringTrafficDirection = types.StringNull()
	}

	if !plan.GreModule.IsNull() && !plan.GreModule.IsUnknown() {
		model.GreModule = types.BoolValue(setting.GreModule)
	} else {
		model.GreModule = types.BoolNull()
	}

	if !plan.H323Module.IsNull() && !plan.H323Module.IsUnknown() {
		model.H323Module = types.BoolValue(setting.H323Module)
	} else {
		model.H323Module = types.BoolNull()
	}

	if !plan.ICMPTimeout.IsNull() && !plan.ICMPTimeout.IsUnknown() {
		model.ICMPTimeout = util.DurationValue(setting.ICMPTimeout, time.Second)
	} else {
		model.ICMPTimeout = timetypes.NewGoDurationNull()
	}

	if !plan.MssClamp.IsNull() && !plan.MssClamp.IsUnknown() {
		model.MssClamp = util.StringValueOrNull(setting.MssClamp)
	} else {
		model.MssClamp = types.StringNull()
	}

	if !plan.OffloadAccounting.IsNull() && !plan.OffloadAccounting.IsUnknown() {
		model.OffloadAccounting = types.BoolValue(setting.OffloadAccounting)
	} else {
		model.OffloadAccounting = types.BoolNull()
	}

	if !plan.OffloadL2Blocking.IsNull() && !plan.OffloadL2Blocking.IsUnknown() {
		model.OffloadL2Blocking = types.BoolValue(setting.OffloadL2Blocking)
	} else {
		model.OffloadL2Blocking = types.BoolNull()
	}

	if !plan.OffloadSch.IsNull() && !plan.OffloadSch.IsUnknown() {
		model.OffloadSch = types.BoolValue(setting.OffloadSch)
	} else {
		model.OffloadSch = types.BoolNull()
	}

	if !plan.OtherTimeout.IsNull() && !plan.OtherTimeout.IsUnknown() {
		model.OtherTimeout = util.DurationValue(setting.OtherTimeout, time.Second)
	} else {
		model.OtherTimeout = timetypes.NewGoDurationNull()
	}

	if !plan.PptpModule.IsNull() && !plan.PptpModule.IsUnknown() {
		model.PptpModule = types.BoolValue(setting.PptpModule)
	} else {
		model.PptpModule = types.BoolNull()
	}

	if !plan.ReceiveRedirects.IsNull() && !plan.ReceiveRedirects.IsUnknown() {
		model.ReceiveRedirects = types.BoolValue(setting.ReceiveRedirects)
	} else {
		model.ReceiveRedirects = types.BoolNull()
	}

	if !plan.SendRedirects.IsNull() && !plan.SendRedirects.IsUnknown() {
		model.SendRedirects = types.BoolValue(setting.SendRedirects)
	} else {
		model.SendRedirects = types.BoolNull()
	}

	if !plan.SipModule.IsNull() && !plan.SipModule.IsUnknown() {
		model.SipModule = types.BoolValue(setting.SipModule)
	} else {
		model.SipModule = types.BoolNull()
	}

	if !plan.SynCookies.IsNull() && !plan.SynCookies.IsUnknown() {
		model.SynCookies = types.BoolValue(setting.SynCookies)
	} else {
		model.SynCookies = types.BoolNull()
	}

	if !plan.TCPCloseTimeout.IsNull() && !plan.TCPCloseTimeout.IsUnknown() {
		model.TCPCloseTimeout = util.DurationValue(setting.TCPCloseTimeout, time.Second)
	} else {
		model.TCPCloseTimeout = timetypes.NewGoDurationNull()
	}

	if !plan.TCPCloseWaitTimeout.IsNull() && !plan.TCPCloseWaitTimeout.IsUnknown() {
		model.TCPCloseWaitTimeout = util.DurationValue(setting.TCPCloseWaitTimeout, time.Second)
	} else {
		model.TCPCloseWaitTimeout = timetypes.NewGoDurationNull()
	}

	if !plan.TCPEstablishedTimeout.IsNull() && !plan.TCPEstablishedTimeout.IsUnknown() {
		model.TCPEstablishedTimeout = util.DurationValue(setting.TCPEstablishedTimeout, time.Second)
	} else {
		model.TCPEstablishedTimeout = timetypes.NewGoDurationNull()
	}

	if !plan.TCPFinWaitTimeout.IsNull() && !plan.TCPFinWaitTimeout.IsUnknown() {
		model.TCPFinWaitTimeout = util.DurationValue(setting.TCPFinWaitTimeout, time.Second)
	} else {
		model.TCPFinWaitTimeout = timetypes.NewGoDurationNull()
	}

	if !plan.TCPLastAckTimeout.IsNull() && !plan.TCPLastAckTimeout.IsUnknown() {
		model.TCPLastAckTimeout = util.DurationValue(setting.TCPLastAckTimeout, time.Second)
	} else {
		model.TCPLastAckTimeout = timetypes.NewGoDurationNull()
	}

	if !plan.TCPSynRecvTimeout.IsNull() && !plan.TCPSynRecvTimeout.IsUnknown() {
		model.TCPSynRecvTimeout = util.DurationValue(setting.TCPSynRecvTimeout, time.Second)
	} else {
		model.TCPSynRecvTimeout = timetypes.NewGoDurationNull()
	}

	if !plan.TCPSynSentTimeout.IsNull() && !plan.TCPSynSentTimeout.IsUnknown() {
		model.TCPSynSentTimeout = util.DurationValue(setting.TCPSynSentTimeout, time.Second)
	} else {
		model.TCPSynSentTimeout = timetypes.NewGoDurationNull()
	}

	if !plan.TCPTimeWaitTimeout.IsNull() && !plan.TCPTimeWaitTimeout.IsUnknown() {
		model.TCPTimeWaitTimeout = util.DurationValue(setting.TCPTimeWaitTimeout, time.Second)
	} else {
		model.TCPTimeWaitTimeout = timetypes.NewGoDurationNull()
	}

	if !plan.TFTPModule.IsNull() && !plan.TFTPModule.IsUnknown() {
		model.TFTPModule = types.BoolValue(setting.TFTPModule)
	} else {
		model.TFTPModule = types.BoolNull()
	}

	if !plan.TimeoutSettingPreference.IsNull() && !plan.TimeoutSettingPreference.IsUnknown() {
		model.TimeoutSettingPreference = util.StringValueOrNull(setting.TimeoutSettingPreference)
	} else {
		model.TimeoutSettingPreference = types.StringNull()
	}

	if !plan.UDPOtherTimeout.IsNull() && !plan.UDPOtherTimeout.IsUnknown() {
		model.UDPOtherTimeout = util.DurationValue(setting.UDPOtherTimeout, time.Second)
	} else {
		model.UDPOtherTimeout = timetypes.NewGoDurationNull()
	}

	if !plan.UDPStreamTimeout.IsNull() && !plan.UDPStreamTimeout.IsUnknown() {
		model.UDPStreamTimeout = util.DurationValue(setting.UDPStreamTimeout, time.Second)
	} else {
		model.UDPStreamTimeout = timetypes.NewGoDurationNull()
	}

	if !plan.UnbindWANMonitors.IsNull() && !plan.UnbindWANMonitors.IsUnknown() {
		model.UnbindWANMonitors = types.BoolValue(setting.UnbindWANMonitors)
	} else {
		model.UnbindWANMonitors = types.BoolNull()
	}

	if !plan.UPnPEnabled.IsNull() && !plan.UPnPEnabled.IsUnknown() {
		model.UPnPEnabled = types.BoolValue(setting.UPnPEnabled)
	} else {
		model.UPnPEnabled = types.BoolNull()
	}

	if !plan.UPnPNATPmpEnabled.IsNull() && !plan.UPnPNATPmpEnabled.IsUnknown() {
		model.UPnPNATPmpEnabled = types.BoolValue(setting.UPnPNATPmpEnabled)
	} else {
		model.UPnPNATPmpEnabled = types.BoolNull()
	}

	if !plan.UPnPSecureMode.IsNull() && !plan.UPnPSecureMode.IsUnknown() {
		model.UPnPSecureMode = types.BoolValue(setting.UPnPSecureMode)
	} else {
		model.UPnPSecureMode = types.BoolNull()
	}

	if !plan.UPnPWANInterface.IsNull() && !plan.UPnPWANInterface.IsUnknown() {
		model.UPnPWANInterface = util.StringValueOrNull(setting.UPnPWANInterface)
	} else {
		model.UPnPWANInterface = types.StringNull()
	}

	return model
}

// IGMP snooping conversion functions.

// igmpSnoopingModelToSetting overlays the user-set fields (enabled, network_ids)
// onto the current remote setting (base) so advanced querier/flood options are
// preserved across updates.
func (r *settingResource) igmpSnoopingModelToSetting(
	ctx context.Context,
	model *settingIgmpSnoopingModel,
	base *settings.IgmpSnooping,
	diags *diag.Diagnostics,
) *settings.IgmpSnooping {
	setting := base
	if !model.Enabled.IsNull() && !model.Enabled.IsUnknown() {
		setting.Enabled = model.Enabled.ValueBool()
	}
	if !model.NetworkIDs.IsNull() && !model.NetworkIDs.IsUnknown() {
		var ids []string
		diags.Append(model.NetworkIDs.ElementsAs(ctx, &ids, false)...)
		setting.NetworkIDs = ids
	}
	return setting
}

func (r *settingResource) igmpSnoopingSettingToModel(
	ctx context.Context,
	setting *settings.IgmpSnooping,
	diags *diag.Diagnostics,
) *settingIgmpSnoopingModel {
	model := &settingIgmpSnoopingModel{
		Enabled: types.BoolValue(setting.Enabled),
	}
	ids, d := types.ListValueFrom(ctx, types.StringType, setting.NetworkIDs)
	diags.Append(d...)
	model.NetworkIDs = ids
	return model
}

// Simple 1:1 conversion functions: auto-speedtest, country, DPI, LCM,
// network-optimization, NTP and syslog.
func (r *settingResource) autoSpeedtestModelToSetting(
	model *settingAutoSpeedtestModel,
) *settings.AutoSpeedtest {
	setting := &settings.AutoSpeedtest{}
	if !model.Enabled.IsNull() && !model.Enabled.IsUnknown() {
		setting.Enabled = model.Enabled.ValueBool()
	}
	if !model.CronExpr.IsNull() && !model.CronExpr.IsUnknown() {
		setting.CronExpr = model.CronExpr.ValueString()
	}
	return setting
}

func (r *settingResource) autoSpeedtestSettingToModel(
	setting *settings.AutoSpeedtest,
) settingAutoSpeedtestModel {
	return settingAutoSpeedtestModel{
		Enabled:  types.BoolValue(setting.Enabled),
		CronExpr: util.StringValueOrNull(setting.CronExpr),
	}
}

func (r *settingResource) countryModelToSetting(m *settingCountryModel) *settings.Country {
	return &settings.Country{Code: m.Code.ValueInt64Pointer()}
}

func (r *settingResource) countrySettingToModel(s *settings.Country) settingCountryModel {
	return settingCountryModel{Code: types.Int64PointerValue(s.Code)}
}

func (r *settingResource) dpiModelToSetting(m *settingDpiModel) *settings.Dpi {
	return &settings.Dpi{
		Enabled:               m.Enabled.ValueBool(),
		FingerprintingEnabled: m.FingerprintingEnabled.ValueBool(),
	}
}

func (r *settingResource) dpiSettingToModel(s *settings.Dpi) settingDpiModel {
	return settingDpiModel{
		Enabled:               types.BoolValue(s.Enabled),
		FingerprintingEnabled: types.BoolValue(s.FingerprintingEnabled),
	}
}

func (r *settingResource) lcmModelToSetting(m *settingLcmModel) *settings.Lcm {
	setting := &settings.Lcm{
		Enabled:    m.Enabled.ValueBool(),
		Sync:       m.Sync.ValueBool(),
		TouchEvent: m.TouchEvent.ValueBool(),
	}
	// Guard the optional ints: an unknown (unset Optional+Computed) value yields a
	// 0 pointer, which the controller rejects as out of range (cf. #288/#303).
	if !m.Brightness.IsNull() && !m.Brightness.IsUnknown() {
		setting.Brightness = m.Brightness.ValueInt64Pointer()
	}
	if !m.IdleTimeout.IsNull() && !m.IdleTimeout.IsUnknown() {
		setting.IDleTimeout = m.IdleTimeout.ValueInt64Pointer()
	}
	return setting
}

func (r *settingResource) lcmSettingToModel(s *settings.Lcm) settingLcmModel {
	return settingLcmModel{
		Enabled:     types.BoolValue(s.Enabled),
		Brightness:  types.Int64PointerValue(s.Brightness),
		IdleTimeout: types.Int64PointerValue(s.IDleTimeout),
		Sync:        types.BoolValue(s.Sync),
		TouchEvent:  types.BoolValue(s.TouchEvent),
	}
}

func (r *settingResource) networkOptimizationModelToSetting(
	m *settingNetworkOptimizationModel,
) *settings.NetworkOptimization {
	return &settings.NetworkOptimization{Enabled: m.Enabled.ValueBool()}
}

func (r *settingResource) networkOptimizationSettingToModel(
	s *settings.NetworkOptimization,
) settingNetworkOptimizationModel {
	return settingNetworkOptimizationModel{Enabled: types.BoolValue(s.Enabled)}
}

func (r *settingResource) ntpModelToSetting(m *settingNtpModel) *settings.Ntp {
	return &settings.Ntp{
		NtpServer1:        m.NtpServer1.ValueString(),
		NtpServer2:        m.NtpServer2.ValueString(),
		NtpServer3:        m.NtpServer3.ValueString(),
		NtpServer4:        m.NtpServer4.ValueString(),
		SettingPreference: m.SettingPreference.ValueString(),
	}
}

func (r *settingResource) ntpSettingToModel(s *settings.Ntp) settingNtpModel {
	// The controller persists unused server slots as "", a valid configured value
	// distinct from unset; rewriting it to null (as StringValueOrNull did) collided with an explicit "" and destabilized state.
	return settingNtpModel{
		NtpServer1:        types.StringValue(s.NtpServer1),
		NtpServer2:        types.StringValue(s.NtpServer2),
		NtpServer3:        types.StringValue(s.NtpServer3),
		NtpServer4:        types.StringValue(s.NtpServer4),
		SettingPreference: util.StringValueOrNull(s.SettingPreference),
	}
}

func (r *settingResource) syslogModelToSetting(
	ctx context.Context,
	m *settingSyslogModel,
	diags *diag.Diagnostics,
) *settings.Rsyslogd {
	setting := &settings.Rsyslogd{
		Enabled:                     m.Enabled.ValueBool(),
		Debug:                       m.Debug.ValueBool(),
		IP:                          m.IP.ValueString(),
		LogAllContents:              m.LogAllContents.ValueBool(),
		NetconsoleEnabled:           m.NetconsoleEnabled.ValueBool(),
		NetconsoleHost:              m.NetconsoleHost.ValueString(),
		ThisController:              m.ThisController.ValueBool(),
		ThisControllerEncryptedOnly: m.ThisControllerEncryptedOnly.ValueBool(),
	}
	// Guard the optional ports: an unknown (unset Optional+Computed) value yields a
	// 0 pointer, which the controller rejects as an out-of-range port (#303, cf. #288).
	if !m.Port.IsNull() && !m.Port.IsUnknown() {
		setting.Port = m.Port.ValueInt64Pointer()
	}
	if !m.NetconsolePort.IsNull() && !m.NetconsolePort.IsUnknown() {
		setting.NetconsolePort = m.NetconsolePort.ValueInt64Pointer()
	}
	if !m.Contents.IsNull() && !m.Contents.IsUnknown() {
		diags.Append(m.Contents.ElementsAs(ctx, &setting.Contents, false)...)
	}
	return setting
}

func (r *settingResource) syslogSettingToModel(
	ctx context.Context,
	s *settings.Rsyslogd,
	diags *diag.Diagnostics,
) settingSyslogModel {
	contents, d := types.ListValueFrom(ctx, types.StringType, s.Contents)
	diags.Append(d...)
	return settingSyslogModel{
		Enabled:                     types.BoolValue(s.Enabled),
		Contents:                    contents,
		Debug:                       types.BoolValue(s.Debug),
		IP:                          util.StringValueOrNull(s.IP),
		Port:                        types.Int64PointerValue(s.Port),
		LogAllContents:              types.BoolValue(s.LogAllContents),
		NetconsoleEnabled:           types.BoolValue(s.NetconsoleEnabled),
		NetconsoleHost:              util.StringValueOrNull(s.NetconsoleHost),
		NetconsolePort:              types.Int64PointerValue(s.NetconsolePort),
		ThisController:              types.BoolValue(s.ThisController),
		ThisControllerEncryptedOnly: types.BoolValue(s.ThisControllerEncryptedOnly),
	}
}

// DoH conversion functions.
func (r *settingResource) dohModelToSetting(
	ctx context.Context,
	model *settingDohModel,
	diags *diag.Diagnostics,
) *settings.Doh {
	setting := &settings.Doh{}

	if !model.State.IsNull() && !model.State.IsUnknown() {
		setting.State = model.State.ValueString()
	}
	if !model.ServerNames.IsNull() && !model.ServerNames.IsUnknown() {
		diags.Append(model.ServerNames.ElementsAs(ctx, &setting.ServerNames, false)...)
		if diags.HasError() {
			return setting
		}
	}
	if !model.CustomServers.IsNull() && !model.CustomServers.IsUnknown() {
		var servers []settingDohCustomServerModel
		diags.Append(model.CustomServers.ElementsAs(ctx, &servers, false)...)
		if diags.HasError() {
			return setting
		}
		for _, s := range servers {
			enabled := true
			if !s.Enabled.IsNull() && !s.Enabled.IsUnknown() {
				enabled = s.Enabled.ValueBool()
			}
			setting.CustomServers = append(setting.CustomServers, settings.SettingDohCustomServers{
				Enabled:    enabled,
				SdnsStamp:  s.SDNSStamp.ValueString(),
				ServerName: s.ServerName.ValueString(),
			})
		}
	}

	return setting
}

func (r *settingResource) dohSettingToModel(
	ctx context.Context,
	setting *settings.Doh,
	plan *settingDohModel,
	diags *diag.Diagnostics,
) *settingDohModel {
	model := &settingDohModel{}

	if !plan.State.IsNull() && !plan.State.IsUnknown() {
		model.State = util.StringValueOrNull(setting.State)
	} else {
		model.State = types.StringNull()
	}

	// Configured (plan known) mirrors the remote value as a list, empty list
	// included; ListNull here would differ from a planned []value and trip
	// "inconsistent result after apply" -- it's reserved for not-configured/unknown.
	if !plan.ServerNames.IsNull() && !plan.ServerNames.IsUnknown() {
		listVal, d := types.ListValueFrom(ctx, types.StringType, setting.ServerNames)
		diags.Append(d...)
		model.ServerNames = listVal
	} else {
		model.ServerNames = types.ListNull(types.StringType)
	}

	customServersType := types.ObjectType{AttrTypes: dohCustomServerAttrTypes}
	if !plan.CustomServers.IsNull() && !plan.CustomServers.IsUnknown() {
		servers := make([]settingDohCustomServerModel, 0, len(setting.CustomServers))
		for _, s := range setting.CustomServers {
			servers = append(servers, settingDohCustomServerModel{
				Enabled:    types.BoolValue(s.Enabled),
				SDNSStamp:  types.StringValue(s.SdnsStamp),
				ServerName: types.StringValue(s.ServerName),
			})
		}
		listVal, d := types.ListValueFrom(ctx, customServersType, servers)
		diags.Append(d...)
		model.CustomServers = listVal
	} else {
		model.CustomServers = types.ListNull(customServersType)
	}

	return model
}

// IPS conversion functions.
// ipsModelToSetting overlays the user-set fields onto the current remote setting,
// as mgmt, radius, usg and igmp_snooping already do: four fields carry no omitempty,
// so a partial config force-emitted false over the controller's value. A mask
// wouldn't catch this (it only asks whether a field is assigned, not on every path), so a base fixes the whole class.
func (r *settingResource) ipsModelToSetting(
	ctx context.Context,
	model *settingIpsModel,
	base *settings.Ips,
	diags *diag.Diagnostics,
) *settings.Ips {
	setting := base

	if !model.IPSMode.IsNull() && !model.IPSMode.IsUnknown() {
		setting.IPsMode = model.IPSMode.ValueString()
	}
	if !model.HoneypotEnabled.IsNull() && !model.HoneypotEnabled.IsUnknown() {
		setting.HoneypotEnabled = model.HoneypotEnabled.ValueBool()
	}
	if !model.RestrictTorrents.IsNull() && !model.RestrictTorrents.IsUnknown() {
		setting.RestrictTorrents = model.RestrictTorrents.ValueBool()
	}
	if !model.ContentFilteringBlockingPageEnabled.IsNull() &&
		!model.ContentFilteringBlockingPageEnabled.IsUnknown() {
		setting.ContentFilteringBlockingPageEnabled = model.ContentFilteringBlockingPageEnabled.ValueBool()
	}
	if !model.MemoryOptimized.IsNull() && !model.MemoryOptimized.IsUnknown() {
		setting.MemoryOptimized = model.MemoryOptimized.ValueBool()
	}
	if !model.AdvancedFilteringPreference.IsNull() &&
		!model.AdvancedFilteringPreference.IsUnknown() {
		setting.AdvancedFilteringPreference = model.AdvancedFilteringPreference.ValueString()
	}
	if !model.EnabledCategories.IsNull() && !model.EnabledCategories.IsUnknown() {
		diags.Append(model.EnabledCategories.ElementsAs(ctx, &setting.EnabledCategories, false)...)
		if diags.HasError() {
			return setting
		}
	}
	if !model.EnabledNetworks.IsNull() && !model.EnabledNetworks.IsUnknown() {
		diags.Append(model.EnabledNetworks.ElementsAs(ctx, &setting.EnabledNetworks, false)...)
		if diags.HasError() {
			return setting
		}
	}
	if !model.Honeypot.IsNull() && !model.Honeypot.IsUnknown() {
		var honeypots []settingIpsHoneypotModel
		diags.Append(model.Honeypot.ElementsAs(ctx, &honeypots, false)...)
		if diags.HasError() {
			return setting
		}
		// Replace rather than extend: the loop appends, and base now carries the
		// controller's list, so without nilling first a configured list would double up with the remote one on every apply.
		setting.Honeypot = nil
		for _, h := range honeypots {
			setting.Honeypot = append(setting.Honeypot, settings.SettingIpsHoneypot{
				IPAddress: h.IPAddress.ValueString(),
				NetworkID: h.NetworkID.ValueString(),
				Version:   h.Version.ValueString(),
			})
		}
	}
	return setting
}

// ipsSuppressionConfigured reports whether the practitioner manages either
// suppression list; UniFi Network 10.x promoted this off `ips` into its own `ips_suppression` object, written separately and only when configured.
func ipsSuppressionConfigured(model *settingIpsModel) bool {
	for _, v := range []attr.Value{model.SuppressionWhitelist, model.SuppressionAlerts} {
		if !v.IsNull() && !v.IsUnknown() {
			return true
		}
	}
	return false
}

// ipsSuppressionModelToSetting builds the ips_suppression object; only configured
// lists are populated, the rest stay nil so omitempty keeps them off the wire (matching the nested object's behavior before the split).
func (r *settingResource) ipsSuppressionModelToSetting(
	ctx context.Context,
	model *settingIpsModel,
	diags *diag.Diagnostics,
) *settings.IpsSuppression {
	setting := &settings.IpsSuppression{}

	if !model.SuppressionWhitelist.IsNull() && !model.SuppressionWhitelist.IsUnknown() {
		var whitelist []settingIpsWhitelistModel
		diags.Append(model.SuppressionWhitelist.ElementsAs(ctx, &whitelist, false)...)
		if diags.HasError() {
			return setting
		}
		for _, w := range whitelist {
			setting.Whitelist = append(
				setting.Whitelist,
				settings.SettingIpsSuppressionWhitelist{
					Direction: w.Direction.ValueString(),
					Mode:      w.Mode.ValueString(),
					Value:     w.Value.ValueString(),
				},
			)
		}
	}
	if !model.SuppressionAlerts.IsNull() && !model.SuppressionAlerts.IsUnknown() {
		var alerts []settingIpsAlertModel
		diags.Append(model.SuppressionAlerts.ElementsAs(ctx, &alerts, false)...)
		if diags.HasError() {
			return setting
		}
		for _, a := range alerts {
			alert := settings.SettingIpsSuppressionAlerts{
				Category:  a.Category.ValueString(),
				Signature: a.Signature.ValueString(),
				Type:      a.Type.ValueString(),
			}
			// Omit gid/id when unset rather than sending 0 (cf. #303).
			if !a.Gid.IsNull() && !a.Gid.IsUnknown() {
				alert.Gid = a.Gid.ValueInt64Pointer()
			}
			if !a.ID.IsNull() && !a.ID.IsUnknown() {
				alert.ID = a.ID.ValueInt64Pointer()
			}
			if !a.Tracking.IsNull() && !a.Tracking.IsUnknown() {
				var tracking []settingIpsTrackingModel
				diags.Append(a.Tracking.ElementsAs(ctx, &tracking, false)...)
				for _, t := range tracking {
					alert.Tracking = append(alert.Tracking, settings.SettingIpsSuppressionTracking{
						Direction: t.Direction.ValueString(),
						Mode:      t.Mode.ValueString(),
						Value:     t.Value.ValueString(),
					})
				}
			}
			setting.Alerts = append(setting.Alerts, alert)
		}
	}

	return setting
}

// writeIpsSuppression writes the ips_suppression setting, but only when the
// practitioner manages at least one suppression list; verb ("Creating"/"Updating") is for the error message.
func (r *settingResource) writeIpsSuppression(
	ctx context.Context,
	site string,
	model *settingIpsModel,
	verb string,
	diags *diag.Diagnostics,
) {
	if !ipsSuppressionConfigured(model) {
		return
	}

	setting := r.ipsSuppressionModelToSetting(ctx, model, diags)
	if diags.HasError() {
		return
	}

	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			diags.AddError(
				"IPS Suppression Not Supported By This Controller",
				"The `suppression_alerts` and `suppression_whitelist` attributes are stored in "+
					"the `ips_suppression` setting, which this controller does not expose. UniFi "+
					"Network 10.x moved them out of the `ips` setting. Remove them from the `ips` "+
					"block, or upgrade the controller.",
			)
			return
		}
		diags.AddError("Error "+verb+" IPS Suppression Setting", err.Error())
	}
}

func (r *settingResource) ipsSettingToModel(
	ctx context.Context,
	setting *settings.Ips,
	suppression *settings.IpsSuppression,
	plan *settingIpsModel,
	diags *diag.Diagnostics,
) *settingIpsModel {
	model := &settingIpsModel{}

	if !plan.IPSMode.IsNull() && !plan.IPSMode.IsUnknown() {
		model.IPSMode = util.StringValueOrNull(setting.IPsMode)
	} else {
		model.IPSMode = types.StringNull()
	}

	if !plan.HoneypotEnabled.IsNull() && !plan.HoneypotEnabled.IsUnknown() {
		model.HoneypotEnabled = types.BoolValue(setting.HoneypotEnabled)
	} else {
		model.HoneypotEnabled = types.BoolNull()
	}

	if !plan.RestrictTorrents.IsNull() && !plan.RestrictTorrents.IsUnknown() {
		model.RestrictTorrents = types.BoolValue(setting.RestrictTorrents)
	} else {
		model.RestrictTorrents = types.BoolNull()
	}

	if !plan.ContentFilteringBlockingPageEnabled.IsNull() &&
		!plan.ContentFilteringBlockingPageEnabled.IsUnknown() {
		model.ContentFilteringBlockingPageEnabled = types.BoolValue(
			setting.ContentFilteringBlockingPageEnabled,
		)
	} else {
		model.ContentFilteringBlockingPageEnabled = types.BoolNull()
	}

	if !plan.MemoryOptimized.IsNull() && !plan.MemoryOptimized.IsUnknown() {
		model.MemoryOptimized = types.BoolValue(setting.MemoryOptimized)
	} else {
		model.MemoryOptimized = types.BoolNull()
	}

	if !plan.AdvancedFilteringPreference.IsNull() && !plan.AdvancedFilteringPreference.IsUnknown() {
		model.AdvancedFilteringPreference = util.StringValueOrNull(setting.AdvancedFilteringPreference)
	} else {
		model.AdvancedFilteringPreference = types.StringNull()
	}

	// Configured lists mirror the remote value (empty list included); ListNull is
	// reserved for the not-configured/unknown case (see dohSettingToModel for why a configured-but-empty list must not become ListNull).
	if !plan.EnabledCategories.IsNull() && !plan.EnabledCategories.IsUnknown() {
		listVal, d := types.ListValueFrom(ctx, types.StringType, setting.EnabledCategories)
		diags.Append(d...)
		model.EnabledCategories = listVal
	} else {
		model.EnabledCategories = types.ListNull(types.StringType)
	}

	if !plan.EnabledNetworks.IsNull() && !plan.EnabledNetworks.IsUnknown() {
		listVal, d := types.ListValueFrom(ctx, types.StringType, setting.EnabledNetworks)
		diags.Append(d...)
		model.EnabledNetworks = listVal
	} else {
		model.EnabledNetworks = types.ListNull(types.StringType)
	}

	honeypotType := types.ObjectType{AttrTypes: ipsHoneypotAttrTypes}
	if !plan.Honeypot.IsNull() && !plan.Honeypot.IsUnknown() {
		honeypots := make([]settingIpsHoneypotModel, 0, len(setting.Honeypot))
		for _, h := range setting.Honeypot {
			honeypots = append(honeypots, settingIpsHoneypotModel{
				IPAddress: types.StringValue(h.IPAddress),
				NetworkID: types.StringValue(h.NetworkID),
				Version:   types.StringValue(h.Version),
			})
		}
		listVal, d := types.ListValueFrom(ctx, honeypotType, honeypots)
		diags.Append(d...)
		model.Honeypot = listVal
	} else {
		model.Honeypot = types.ListNull(honeypotType)
	}

	whitelistType := types.ObjectType{AttrTypes: ipsWhitelistAttrTypes}
	if !plan.SuppressionWhitelist.IsNull() && !plan.SuppressionWhitelist.IsUnknown() {
		var whitelist []settings.SettingIpsSuppressionWhitelist
		if suppression != nil {
			whitelist = suppression.Whitelist
		}
		entries := make([]settingIpsWhitelistModel, 0, len(whitelist))
		for _, w := range whitelist {
			entries = append(entries, settingIpsWhitelistModel{
				Direction: types.StringValue(w.Direction),
				Mode:      types.StringValue(w.Mode),
				Value:     types.StringValue(w.Value),
			})
		}
		listVal, d := types.ListValueFrom(ctx, whitelistType, entries)
		diags.Append(d...)
		model.SuppressionWhitelist = listVal
	} else {
		model.SuppressionWhitelist = types.ListNull(whitelistType)
	}

	trackingType := types.ObjectType{AttrTypes: ipsTrackingAttrTypes}
	alertType := types.ObjectType{AttrTypes: ipsAlertAttrTypes}
	if !plan.SuppressionAlerts.IsNull() && !plan.SuppressionAlerts.IsUnknown() {
		var alerts []settings.SettingIpsSuppressionAlerts
		if suppression != nil {
			alerts = suppression.Alerts
		}
		entries := make([]settingIpsAlertModel, 0, len(alerts))
		for _, a := range alerts {
			tracking := make([]settingIpsTrackingModel, 0, len(a.Tracking))
			for _, t := range a.Tracking {
				tracking = append(tracking, settingIpsTrackingModel{
					Direction: types.StringValue(t.Direction),
					Mode:      types.StringValue(t.Mode),
					Value:     types.StringValue(t.Value),
				})
			}
			trackingList, d := types.ListValueFrom(ctx, trackingType, tracking)
			diags.Append(d...)
			entries = append(entries, settingIpsAlertModel{
				Category:  util.StringValueOrNull(a.Category),
				Gid:       types.Int64PointerValue(a.Gid),
				ID:        types.Int64PointerValue(a.ID),
				Signature: util.StringValueOrNull(a.Signature),
				Type:      util.StringValueOrNull(a.Type),
				Tracking:  trackingList,
			})
		}
		listVal, d := types.ListValueFrom(ctx, alertType, entries)
		diags.Append(d...)
		model.SuppressionAlerts = listVal
	} else {
		model.SuppressionAlerts = types.ListNull(alertType)
	}

	return model
}
