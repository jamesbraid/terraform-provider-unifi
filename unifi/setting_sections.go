package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingSection is one attribute of unifi_setting: whether the plan
// configures it, and how to write and read its document(s). writeSettings
// and readSettings drive every section through this interface, in table
// order, so a section's implementation can be replaced independently.
type settingSection interface {
	Name() string
	Configured(ctx context.Context, plan *settingResourceModel) bool
	Write(ctx context.Context, r *settingResource, site string, plan, state *settingResourceModel, verb string) diag.Diagnostics
	Read(ctx context.Context, r *settingResource, site string, plan, out *settingResourceModel) diag.Diagnostics
}

// settingSectionConfigured is the single place the "plan section non-null and
// known" rule lives; both a section's Write-skip and its Read null/else arm
// call this instead of re-deriving it.
func settingSectionConfigured(o types.Object) bool {
	return !o.IsNull() && !o.IsUnknown()
}

// legacySection adapts one of unifi_setting's hand-written write/read blocks
// (extracted below to named methods on *settingResource) to settingSection.
// write/read are method expressions, not bound values, so a legacySection
// literal stays a genuine package-level value; the live *settingResource is
// threaded through at call time by writeSettings/readSettings.
type legacySection struct {
	name       string
	configured func(plan *settingResourceModel) bool
	write      func(r *settingResource, ctx context.Context, site string, plan, state *settingResourceModel, verb string) diag.Diagnostics
	read       func(r *settingResource, ctx context.Context, site string, plan, out *settingResourceModel) diag.Diagnostics
}

func (l legacySection) Name() string { return l.name }

func (l legacySection) Configured(_ context.Context, plan *settingResourceModel) bool {
	return l.configured(plan)
}

func (l legacySection) Write(
	ctx context.Context, r *settingResource, site string, plan, state *settingResourceModel, verb string,
) diag.Diagnostics {
	return l.write(r, ctx, site, plan, state, verb)
}

func (l legacySection) Read(
	ctx context.Context, r *settingResource, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	return l.read(r, ctx, site, plan, out)
}

// legacySectionAdapter satisfies resourcekit.Section[settingResourceModel] by
// closing over the *settingResource each settingSection.Write/Read needs as
// its receiver: resourcekit.Section carries no client of its own, so this is
// where one gets bound in, once Configure knows it.
type legacySectionAdapter struct {
	r       *settingResource
	section settingSection
}

func (a legacySectionAdapter) Name() string { return a.section.Name() }

func (a legacySectionAdapter) Configured(ctx context.Context, plan *settingResourceModel) bool {
	return a.section.Configured(ctx, plan)
}

func (a legacySectionAdapter) Write(
	ctx context.Context, site string, plan, state *settingResourceModel, verb string,
) diag.Diagnostics {
	return a.section.Write(ctx, a.r, site, plan, state, verb)
}

func (a legacySectionAdapter) Read(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	return a.section.Read(ctx, a.r, site, plan, out)
}

// settingKitSectionEntry is one row of settingKitSectionTable: exactly one
// of Legacy and Kit is set. Legacy adapts a hand-written settingSection; Kit
// builds a resourcekit.SpecSection bound to the client settingKitSections is
// given.
type settingKitSectionEntry struct {
	Legacy settingSection
	Kit    func(client *ui.ApiClient) resourcekit.Section[settingResourceModel]
}

// settingKitSectionTable names all thirteen of unifi_setting's sections, in
// their historical write order, each row either a legacySection or a
// resourcekit.SpecSection kit constructor. It replaces the splice-after-"ips"
// approach settingKitSections used while mgmt was the only section served
// from the kit -- that approach doesn't scale to several sections migrating
// in one task (the R2-B part 1 report flagged this: every further migration
// would need its own named splice point), where an ordered literal just
// names each row once.
var settingKitSectionTable = []settingKitSectionEntry{
	{Kit: autoSpeedtestKitSection},
	{Kit: countryKitSection},
	{Kit: dpiKitSection},
	{Kit: lcmKitSection},
	{Kit: networkOptimizationKitSection},
	{Kit: ntpKitSection},
	{Kit: syslogKitSection},
	{Kit: dohKitSection},
	{Legacy: legacySection{
		name:       "ips",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.Ips) },
		write:      (*settingResource).writeIpsSection,
		read:       (*settingResource).readIpsSection,
	}},
	{Kit: mgmtKitSection},
	{Kit: radiusKitSection},
	{Legacy: legacySection{
		name:       "usg",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.USG) },
		write:      (*settingResource).writeUSGSection,
		read:       (*settingResource).readUSGSection,
	}},
	{Kit: igmpSnoopingKitSection},
}

// settingKitSections adapts settingKitSectionTable to
// []resourcekit.Section[settingResourceModel], bound to r. Called from
// Configure, once r.client exists for both the adapted legacy Write/Read
// calls and every kit section's own backend to use.
func settingKitSections(r *settingResource) []resourcekit.Section[settingResourceModel] {
	sections := make([]resourcekit.Section[settingResourceModel], 0, len(settingKitSectionTable))
	for _, entry := range settingKitSectionTable {
		switch {
		case entry.Kit != nil:
			sections = append(sections, entry.Kit(r.client.ApiClient))
		case entry.Legacy != nil:
			sections = append(sections, legacySectionAdapter{r: r, section: entry.Legacy})
		default:
			panic("settingKitSectionTable row names neither Legacy nor Kit")
		}
	}
	return sections
}

// auto_speedtest, country, dpi, lcm, network_optimization, ntp, syslog and
// doh moved onto resourcekit.SpecSection -- see
// setting_auto_speedtest_descriptor.go, setting_country_descriptor.go,
// setting_dpi_descriptor.go, setting_lcm_descriptor.go,
// setting_network_optimization_descriptor.go, setting_ntp_descriptor.go,
// setting_syslog_descriptor.go and setting_doh_descriptor.go.

// -- ips --

func (r *settingResource) writeIpsSection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var ips settingIpsModel
	diags.Append(plan.Ips.As(ctx, &ips, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	_, currentIps, err := ui.GetSetting[*settings.Ips](r.client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if !errors.As(err, &notFound) {
			diags.AddError("Error Reading IPS Setting", err.Error())
			return diags
		}
		currentIps = &settings.Ips{}
	}

	setting := r.ipsModelToSetting(ctx, &ips, currentIps, &diags)
	if diags.HasError() {
		return diags
	}
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" IPS Setting", err.Error())
		return diags
	}

	r.writeIpsSuppression(ctx, site, &ips, "Creating", &diags)
	if diags.HasError() {
		return diags
	}
	return diags
}

// readIpsSection reads the IPS setting, plus its ips_suppression document.
func (r *settingResource) readIpsSection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.Ips) {
		out.Ips = types.ObjectNull(ipsAttrTypes)
		return diags
	}

	var planIps settingIpsModel
	diags.Append(plan.Ips.As(ctx, &planIps, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	_, ipsSetting, err := ui.GetSetting[*settings.Ips](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading IPS Setting", err.Error())
		return diags
	}

	// Suppression lives in its own setting since UniFi Network 10.x; a site
	// that never configured it has no object, which reads back as null rather than an error.
	_, ipsSuppression, err := ui.GetSetting[*settings.IpsSuppression](
		r.client.ApiClient, ctx, site,
	)
	if err != nil {
		var notFound *ui.NotFoundError
		if !errors.As(err, &notFound) {
			diags.AddError("Error Reading IPS Suppression Setting", err.Error())
			return diags
		}
		ipsSuppression = nil
	}

	ipsModel := r.ipsSettingToModel(ctx, ipsSetting, ipsSuppression, &planIps, &diags)
	objValue, d := types.ObjectValueFrom(ctx, ipsAttrTypes, ipsModel)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.Ips = objValue
	return diags
}

// mgmt and radius moved onto resourcekit.SpecSection -- see
// setting_mgmt_descriptor.go / setting_radius_descriptor.go and
// settingKitSectionTable, which names each back in at its own position.

// -- usg --

func (r *settingResource) writeUSGSection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var usg settingUSGModel
	diags.Append(plan.USG.As(ctx, &usg, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	_, currentUsg, err := ui.GetSetting[*settings.Usg](r.client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if !errors.As(err, &notFound) {
			diags.AddError("Error Reading USG Setting", err.Error())
			return diags
		}
		currentUsg = &settings.Usg{}
	}

	setting := r.usgModelToSetting(ctx, &usg, currentUsg)
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" USG Setting", err.Error())
		return diags
	}

	r.writeUsgGeo(ctx, site, &usg, "Creating", &diags)
	if diags.HasError() {
		return diags
	}
	return diags
}

// readUSGSection reads the USG setting, plus its usg_geo document.
func (r *settingResource) readUSGSection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	usgAttrTypes := map[string]attr.Type{
		"broadcast_ping": types.BoolType,
		"dns_verification": types.ObjectType{
			AttrTypes: map[string]attr.Type{
				"domain":               types.StringType,
				"primary_dns_server":   types.StringType,
				"secondary_dns_server": types.StringType,
				"setting_preference":   types.StringType,
			},
		},
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

	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.USG) {
		out.USG = types.ObjectNull(usgAttrTypes)
		return diags
	}

	var planUSG settingUSGModel
	diags.Append(plan.USG.As(ctx, &planUSG, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	_, usgSetting, err := ui.GetSetting[*settings.Usg](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading USG Setting", err.Error())
		return diags
	}

	// Geo IP filtering lives in its own setting since UniFi Network 10.x; a site
	// that never configured it has no object, which reads back as null rather than an error.
	_, usgGeoSetting, err := ui.GetSetting[*settings.UsgGeo](r.client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if !errors.As(err, &notFound) {
			diags.AddError("Error Reading USG Geo Setting", err.Error())
			return diags
		}
		usgGeoSetting = nil
	}

	usgModel := r.usgSettingToModel(ctx, usgSetting, usgGeoSetting, &planUSG)
	objValue, d := types.ObjectValueFrom(ctx, usgAttrTypes, usgModel)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.USG = objValue
	return diags
}

// igmp_snooping moved onto resourcekit.SpecSection -- see
// setting_igmp_snooping_descriptor.go.
