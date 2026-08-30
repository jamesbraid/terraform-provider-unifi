package unifi

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// settingIpsHoneypotModel, settingIpsWhitelistModel, settingIpsTrackingModel,
// settingIpsAlertModel and settingIpsModel moved to setting_ips_descriptor.go,
// alongside the Specs that now own them.

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
	GlobalNat     types.Object   `tfsdk:"global_nat"`
	SslInspection types.Object   `tfsdk:"ssl_inspection"`
	Ipsec         types.Object   `tfsdk:"ipsec"`
	Dashboard     types.Object   `tfsdk:"dashboard"`
	EtherLighting types.Object   `tfsdk:"ether_lighting"`
	GlobalNetwork types.Object   `tfsdk:"global_network"`
	TrafficFlow   types.Object   `tfsdk:"traffic_flow"`
	Mdns          types.Object   `tfsdk:"mdns"`
	Timeouts      timeouts.Value `tfsdk:"timeouts"`
}

// settingIgmpSnoopingModel moved to setting_igmp_snooping_descriptor.go,
// alongside the Spec that now owns it.

// autoSpeedtestAttrTypes, countryAttrTypes, dpiAttrTypes,
// networkOptimizationAttrTypes, mgmtSSHKeyAttrTypes/mgmtAttrTypes,
// lcmAttrTypes, ntpAttrTypes, syslogAttrTypes,
// dohCustomServerAttrTypes/dohAttrTypes, the ips*AttrTypes and
// igmpSnoopingAttrTypes all moved to their own *_descriptor.go files,
// alongside the models and Specs that now own them.

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
	// Re-set by hand: a generated schema can't carry a version, and the policy
	// has no notion of a state upgrader either -- Version and UpgradeState stay
	// hand-written for the same reason.
	resp.Schema.Version = 1
	// Every provider_owned "timeouts" seam across policy/*.json (ap_group.json,
	// bgp.json, this one, ...) is declared generated:false, and the compiler
	// only emits a provider-owned attribute it's told is generated -- so
	// timeouts stays a hand graft here too, the same as every other
	// kit-served resource, not a special case for unifi_setting.
	//
	// Load-bearing for the upgrade path too: UpgradeState derives its prior type
	// from this schema and decodes twelve conntrack durations through it; without this graft that type differs and prior-version state stops decoding.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
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

// ImportState only sets id; the other twelve sections come back null until
// Read populates whichever ones the config claims. A section is managed
// only when the practitioner has configured it, and import has no way to
// know which of the thirteen sections that will be -- hydrating all of
// them here would claim every section as managed, and the next plan would
// diff against attributes the practitioner never wrote.
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
//
// ipsModelToSetting/ipsSettingToModel, ipsSuppressionConfigured/
// ipsSuppressionModelToSetting/writeIpsSuppression moved onto
// resourcekit.SpecSection -- see setting_ips_descriptor.go.
