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
// write/read are method expressions, not bound values, so settingSections
// stays a genuine package-level table; the live *settingResource is threaded
// through at call time by writeSettings/readSettings.
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

// legacySectionsFor adapts every entry of settingSections to
// resourcekit.Section[settingResourceModel], bound to r. Called from
// Configure, once r.client exists for the adapted Write/Read calls to use.
func legacySectionsFor(r *settingResource) []resourcekit.Section[settingResourceModel] {
	sections := make([]resourcekit.Section[settingResourceModel], len(settingSections))
	for i, s := range settingSections {
		sections[i] = legacySectionAdapter{r: r, section: s}
	}
	return sections
}

// settingSections is unifi_setting's 13 sections, in today's write order.
var settingSections = []settingSection{
	legacySection{
		name:       "auto_speedtest",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.AutoSpeedtest) },
		write:      (*settingResource).writeAutoSpeedtestSection,
		read:       (*settingResource).readAutoSpeedtestSection,
	},
	legacySection{
		name:       "country",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.Country) },
		write:      (*settingResource).writeCountrySection,
		read:       (*settingResource).readCountrySection,
	},
	legacySection{
		name:       "dpi",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.Dpi) },
		write:      (*settingResource).writeDpiSection,
		read:       (*settingResource).readDpiSection,
	},
	legacySection{
		name:       "lcm",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.Lcm) },
		write:      (*settingResource).writeLcmSection,
		read:       (*settingResource).readLcmSection,
	},
	legacySection{
		name:       "network_optimization",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.NetworkOpt) },
		write:      (*settingResource).writeNetworkOptimizationSection,
		read:       (*settingResource).readNetworkOptimizationSection,
	},
	legacySection{
		name:       "ntp",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.Ntp) },
		write:      (*settingResource).writeNtpSection,
		read:       (*settingResource).readNtpSection,
	},
	legacySection{
		name:       "syslog",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.Syslog) },
		write:      (*settingResource).writeSyslogSection,
		read:       (*settingResource).readSyslogSection,
	},
	legacySection{
		name:       "doh",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.Doh) },
		write:      (*settingResource).writeDohSection,
		read:       (*settingResource).readDohSection,
	},
	legacySection{
		name:       "ips",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.Ips) },
		write:      (*settingResource).writeIpsSection,
		read:       (*settingResource).readIpsSection,
	},
	legacySection{
		name:       "mgmt",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.Mgmt) },
		write:      (*settingResource).writeMgmtSection,
		read:       (*settingResource).readMgmtSection,
	},
	legacySection{
		name:       "radius",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.Radius) },
		write:      (*settingResource).writeRadiusSection,
		read:       (*settingResource).readRadiusSection,
	},
	legacySection{
		name:       "usg",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.USG) },
		write:      (*settingResource).writeUSGSection,
		read:       (*settingResource).readUSGSection,
	},
	legacySection{
		name:       "igmp_snooping",
		configured: func(plan *settingResourceModel) bool { return settingSectionConfigured(plan.IgmpSnooping) },
		write:      (*settingResource).writeIgmpSnoopingSection,
		read:       (*settingResource).readIgmpSnoopingSection,
	},
}

// -- auto_speedtest --

func (r *settingResource) writeAutoSpeedtestSection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var as settingAutoSpeedtestModel
	diags.Append(plan.AutoSpeedtest.As(ctx, &as, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	setting := r.autoSpeedtestModelToSetting(&as)
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" Auto Speedtest Setting", err.Error())
		return diags
	}
	return diags
}

// readAutoSpeedtestSection reads the auto speedtest setting.
func (r *settingResource) readAutoSpeedtestSection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.AutoSpeedtest) {
		out.AutoSpeedtest = types.ObjectNull(autoSpeedtestAttrTypes)
		return diags
	}
	_, asSetting, err := ui.GetSetting[*settings.AutoSpeedtest](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading Auto Speedtest Setting", err.Error())
		return diags
	}
	objValue, d := types.ObjectValueFrom(
		ctx, autoSpeedtestAttrTypes, r.autoSpeedtestSettingToModel(asSetting),
	)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.AutoSpeedtest = objValue
	return diags
}

// -- country --

func (r *settingResource) writeCountrySection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingCountryModel
	diags.Append(plan.Country.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	setting := r.countryModelToSetting(&m)
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" Country Setting", err.Error())
		return diags
	}
	return diags
}

// readCountrySection reads the country setting.
func (r *settingResource) readCountrySection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.Country) {
		out.Country = types.ObjectNull(countryAttrTypes)
		return diags
	}
	_, s, err := ui.GetSetting[*settings.Country](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading Country Setting", err.Error())
		return diags
	}
	objValue, d := types.ObjectValueFrom(ctx, countryAttrTypes, r.countrySettingToModel(s))
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.Country = objValue
	return diags
}

// -- dpi --

func (r *settingResource) writeDpiSection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingDpiModel
	diags.Append(plan.Dpi.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	setting := r.dpiModelToSetting(&m)
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" DPI Setting", err.Error())
		return diags
	}
	return diags
}

// readDpiSection reads the DPI setting.
func (r *settingResource) readDpiSection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.Dpi) {
		out.Dpi = types.ObjectNull(dpiAttrTypes)
		return diags
	}
	_, s, err := ui.GetSetting[*settings.Dpi](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading DPI Setting", err.Error())
		return diags
	}
	objValue, d := types.ObjectValueFrom(ctx, dpiAttrTypes, r.dpiSettingToModel(s))
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.Dpi = objValue
	return diags
}

// -- lcm --

func (r *settingResource) writeLcmSection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingLcmModel
	diags.Append(plan.Lcm.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	setting := r.lcmModelToSetting(&m)
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" LCM Setting", err.Error())
		return diags
	}
	return diags
}

// readLcmSection reads the LCM setting.
func (r *settingResource) readLcmSection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.Lcm) {
		out.Lcm = types.ObjectNull(lcmAttrTypes)
		return diags
	}
	_, s, err := ui.GetSetting[*settings.Lcm](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading LCM Setting", err.Error())
		return diags
	}
	objValue, d := types.ObjectValueFrom(ctx, lcmAttrTypes, r.lcmSettingToModel(s))
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.Lcm = objValue
	return diags
}

// -- network_optimization --

func (r *settingResource) writeNetworkOptimizationSection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingNetworkOptimizationModel
	diags.Append(plan.NetworkOpt.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	setting := r.networkOptimizationModelToSetting(&m)
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" Network Optimization Setting", err.Error())
		return diags
	}
	return diags
}

// readNetworkOptimizationSection reads the network optimization setting.
func (r *settingResource) readNetworkOptimizationSection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.NetworkOpt) {
		out.NetworkOpt = types.ObjectNull(networkOptimizationAttrTypes)
		return diags
	}
	_, s, err := ui.GetSetting[*settings.NetworkOptimization](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading Network Optimization Setting", err.Error())
		return diags
	}
	objValue, d := types.ObjectValueFrom(
		ctx, networkOptimizationAttrTypes, r.networkOptimizationSettingToModel(s),
	)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.NetworkOpt = objValue
	return diags
}

// -- ntp --

func (r *settingResource) writeNtpSection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingNtpModel
	diags.Append(plan.Ntp.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	setting := r.ntpModelToSetting(&m)
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" NTP Setting", err.Error())
		return diags
	}
	return diags
}

// readNtpSection reads the NTP setting.
func (r *settingResource) readNtpSection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.Ntp) {
		out.Ntp = types.ObjectNull(ntpAttrTypes)
		return diags
	}
	_, s, err := ui.GetSetting[*settings.Ntp](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading NTP Setting", err.Error())
		return diags
	}
	objValue, d := types.ObjectValueFrom(ctx, ntpAttrTypes, r.ntpSettingToModel(s))
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.Ntp = objValue
	return diags
}

// -- syslog --

func (r *settingResource) writeSyslogSection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingSyslogModel
	diags.Append(plan.Syslog.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	setting := r.syslogModelToSetting(ctx, &m, &diags)
	if diags.HasError() {
		return diags
	}
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" Syslog Setting", err.Error())
		return diags
	}
	return diags
}

// readSyslogSection reads the syslog setting.
func (r *settingResource) readSyslogSection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.Syslog) {
		out.Syslog = types.ObjectNull(syslogAttrTypes)
		return diags
	}
	_, s, err := ui.GetSetting[*settings.Rsyslogd](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading Syslog Setting", err.Error())
		return diags
	}
	objValue, d := types.ObjectValueFrom(
		ctx, syslogAttrTypes, r.syslogSettingToModel(ctx, s, &diags),
	)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.Syslog = objValue
	return diags
}

// -- doh --

func (r *settingResource) writeDohSection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var doh settingDohModel
	diags.Append(plan.Doh.As(ctx, &doh, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	setting := r.dohModelToSetting(ctx, &doh, &diags)
	if diags.HasError() {
		return diags
	}
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" DoH Setting", err.Error())
		return diags
	}
	return diags
}

// readDohSection reads the DoH setting.
func (r *settingResource) readDohSection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.Doh) {
		out.Doh = types.ObjectNull(dohAttrTypes)
		return diags
	}

	var planDoh settingDohModel
	diags.Append(plan.Doh.As(ctx, &planDoh, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	_, dohSetting, err := ui.GetSetting[*settings.Doh](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading DoH Setting", err.Error())
		return diags
	}

	dohModel := r.dohSettingToModel(ctx, dohSetting, &planDoh, &diags)
	objValue, d := types.ObjectValueFrom(ctx, dohAttrTypes, dohModel)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.Doh = objValue
	return diags
}

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

// -- mgmt --

func (r *settingResource) writeMgmtSection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var mgmt settingMgmtModel
	diags.Append(plan.Mgmt.As(ctx, &mgmt, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	_, currentMgmt, err := ui.GetSetting[*settings.Mgmt](r.client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if !errors.As(err, &notFound) {
			diags.AddError("Error Reading Mgmt Setting", err.Error())
			return diags
		}
		currentMgmt = &settings.Mgmt{}
	}

	setting := r.mgmtModelToSetting(ctx, &mgmt, currentMgmt)
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" Mgmt Setting", err.Error())
		return diags
	}
	return diags
}

// readMgmtSection reads the mgmt setting.
func (r *settingResource) readMgmtSection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.Mgmt) {
		out.Mgmt = types.ObjectNull(mgmtAttrTypes)
		return diags
	}

	var planMgmt settingMgmtModel
	diags.Append(plan.Mgmt.As(ctx, &planMgmt, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	_, mgmtSetting, err := ui.GetSetting[*settings.Mgmt](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading Mgmt Setting", err.Error())
		return diags
	}

	mgmtModel := r.mgmtSettingToModel(ctx, mgmtSetting, &planMgmt)
	objValue, d := types.ObjectValueFrom(ctx, mgmtAttrTypes, mgmtModel)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.Mgmt = objValue
	return diags
}

// -- radius --

func (r *settingResource) writeRadiusSection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var radius settingRadiusModel
	diags.Append(plan.Radius.As(ctx, &radius, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	_, currentRadius, err := ui.GetSetting[*settings.Radius](r.client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if !errors.As(err, &notFound) {
			diags.AddError("Error Reading Radius Setting", err.Error())
			return diags
		}
		currentRadius = &settings.Radius{}
	}

	setting := r.radiusModelToSetting(ctx, &radius, currentRadius)
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" Radius Setting", err.Error())
		return diags
	}
	return diags
}

// readRadiusSection reads the radius setting.
func (r *settingResource) readRadiusSection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	radiusAttrTypes := map[string]attr.Type{
		"accounting_enabled":      types.BoolType,
		"enabled":                 types.BoolType,
		"acct_port":               types.Int64Type,
		"auth_port":               types.Int64Type,
		"interim_update_interval": timetypes.GoDurationType{},
		"secret":                  types.StringType,
	}

	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.Radius) {
		out.Radius = types.ObjectNull(radiusAttrTypes)
		return diags
	}

	var planRadius settingRadiusModel
	diags.Append(plan.Radius.As(ctx, &planRadius, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	_, radiusSetting, err := ui.GetSetting[*settings.Radius](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading Radius Setting", err.Error())
		return diags
	}

	radiusModel := r.radiusSettingToModel(ctx, radiusSetting, &planRadius)
	objValue, d := types.ObjectValueFrom(ctx, radiusAttrTypes, radiusModel)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.Radius = objValue
	return diags
}

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

// -- igmp_snooping --

func (r *settingResource) writeIgmpSnoopingSection(
	ctx context.Context, site string, plan, _ *settingResourceModel, verb string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var igmp settingIgmpSnoopingModel
	diags.Append(plan.IgmpSnooping.As(ctx, &igmp, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}

	_, currentIgmp, err := ui.GetSetting[*settings.IgmpSnooping](r.client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if !errors.As(err, &notFound) {
			diags.AddError("Error Reading IGMP Snooping Setting", err.Error())
			return diags
		}
		currentIgmp = &settings.IgmpSnooping{}
	}

	setting := r.igmpSnoopingModelToSetting(ctx, &igmp, currentIgmp, &diags)
	if diags.HasError() {
		return diags
	}
	if err := r.client.UpdateSetting(ctx, site, setting); err != nil {
		diags.AddError("Error "+verb+" IGMP Snooping Setting", err.Error())
		return diags
	}
	return diags
}

// readIgmpSnoopingSection reads the site-level IGMP snooping setting.
func (r *settingResource) readIgmpSnoopingSection(
	ctx context.Context, site string, plan, out *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	if !settingSectionConfigured(plan.IgmpSnooping) {
		out.IgmpSnooping = types.ObjectNull(igmpSnoopingAttrTypes)
		return diags
	}
	_, igmpSetting, err := ui.GetSetting[*settings.IgmpSnooping](r.client.ApiClient, ctx, site)
	if err != nil {
		diags.AddError("Error Reading IGMP Snooping Setting", err.Error())
		return diags
	}
	igmpModel := r.igmpSnoopingSettingToModel(ctx, igmpSetting, &diags)
	objValue, d := types.ObjectValueFrom(ctx, igmpSnoopingAttrTypes, igmpModel)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}
	out.IgmpSnooping = objValue
	return diags
}
