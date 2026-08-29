package unifi

import (
	"context"
	"errors"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// sshKeyModel and settingMgmtModel moved to setting_mgmt_descriptor.go,
// alongside the Spec that now owns them: descriptor_mapping_test.go's
// loadDescriptors reads a descriptor's model tags from the same file, so a
// model declared elsewhere reads as undeclared.

// settingRadiusModel moved to setting_radius_descriptor.go, alongside the
// Spec that now owns it.

// dnsVerificationModel and settingUSGModel moved to setting_usg_descriptor.go,
// alongside the Specs that now own them.

// settingAutoSpeedtestModel, settingCountryModel, settingDpiModel,
// settingNetworkOptimizationModel, settingLcmModel, settingNtpModel,
// settingSyslogModel, settingDohCustomServerModel and settingDohModel moved
// to their own *_descriptor.go files, alongside the Specs that now own
// them: descriptor_mapping_test.go's loadDescriptors reads a descriptor's
// model tags from the same file.

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

// settingIgmpSnoopingModel moved to setting_igmp_snooping_descriptor.go,
// alongside the Spec that now owns it.

// Shared attribute-type maps for the doh/ips nested objects, referenced from
// both readSettings and the *SettingToModel helpers -- package level to avoid drift between the two.
var (
	// autoSpeedtestAttrTypes, countryAttrTypes, dpiAttrTypes and
	// networkOptimizationAttrTypes moved to their own *_descriptor.go files,
	// alongside the models and Specs that now own them.
	// mgmtSSHKeyAttrTypes and mgmtAttrTypes moved to
	// setting_mgmt_descriptor.go, alongside sshKeyModel/settingMgmtModel.
	// lcmAttrTypes moved to setting_lcm_descriptor.go, alongside
	// settingLcmModel and the Spec that now owns it.
	// ntpAttrTypes moved to setting_ntp_descriptor.go, alongside
	// settingNtpModel and the Spec that now owns it.
	// syslogAttrTypes moved to setting_syslog_descriptor.go, alongside
	// settingSyslogModel and the Spec that now owns it.
	// dohCustomServerAttrTypes and dohAttrTypes moved to
	// setting_doh_descriptor.go, alongside settingDohCustomServerModel,
	// settingDohModel and the Spec that now owns them.
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
	// igmpSnoopingAttrTypes moved to setting_igmp_snooping_descriptor.go,
	// alongside settingIgmpSnoopingModel and the Spec that now owns it.
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
	r.Sections = settingKitSections(r)
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

// mgmt's mapper functions moved onto resourcekit.SpecSection -- see
// setting_mgmt_descriptor.go's mgmtKitSpec and mgmtAfterReceive. radius's
// moved the same way -- see setting_radius_descriptor.go's radiusKitSpec
// and radiusAfterReceive.

// usg's mapper functions moved onto resourcekit.SpecSection -- see
// setting_usg_descriptor.go's usgKitSpec, usgAfterReceive, usgGeoKitSpec and
// usgGeoKitBackend.

// igmpSnoopingModelToSetting/igmpSnoopingSettingToModel moved onto
// resourcekit.SpecSection -- see setting_igmp_snooping_descriptor.go.
//
// autoSpeedtestModelToSetting/autoSpeedtestSettingToModel,
// countryModelToSetting/countrySettingToModel,
// dpiModelToSetting/dpiSettingToModel and
// networkOptimizationModelToSetting/networkOptimizationSettingToModel moved
// onto resourcekit.SpecSection -- see setting_auto_speedtest_descriptor.go,
// setting_country_descriptor.go, setting_dpi_descriptor.go and
// setting_network_optimization_descriptor.go.
//
// lcmModelToSetting/lcmSettingToModel, ntpModelToSetting/ntpSettingToModel,
// syslogModelToSetting/syslogSettingToModel and
// dohModelToSetting/dohSettingToModel moved onto resourcekit.SpecSection --
// see setting_lcm_descriptor.go, setting_ntp_descriptor.go,
// setting_syslog_descriptor.go and setting_doh_descriptor.go.

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
	// reserved for the not-configured/unknown case -- a configured-but-empty list must not become ListNull (see dohAfterReceive, setting_doh_descriptor.go, for the same rule on doh's own lists).
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
