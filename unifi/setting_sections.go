package unifi

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingSectionConfigured is the single place the "plan section non-null and
// known" rule lives; setting_syslog_validate.go's own ValidateConfig calls
// this instead of re-deriving it. Every resourcekit.SpecSection has its own
// equivalent check built in (Configured), so this one free function is what
// remains once every section is kit-served.
func settingSectionConfigured(o types.Object) bool {
	return !o.IsNull() && !o.IsUnknown()
}

// settingKitSectionTable names all of unifi_setting's sections, in their
// historical write order (the original thirteen, then each later addition
// in the order it landed), each a resourcekit.SpecSection kit constructor
// bound to the client settingKitSections is given. It replaces the
// splice-after-"ips" approach
// settingKitSections used while mgmt was the only section served from the
// kit -- that approach doesn't scale to several sections migrating in one
// task (the R2-B part 1 report flagged this: every further migration would
// need its own named splice point), where an ordered literal just names
// each row once. ips (Task 5c) was the last of the original thirteen still
// hand-written; the legacySection/settingSection/legacySectionAdapter
// machinery that adapted a hand-written section's Write/Read to
// resourcekit.Section existed only to serve rows this table no longer has,
// and was removed with it.
var settingKitSectionTable = []func(client *ui.ApiClient) resourcekit.Section[settingResourceModel]{
	autoSpeedtestKitSection,
	countryKitSection,
	dpiKitSection,
	lcmKitSection,
	networkOptimizationKitSection,
	ntpKitSection,
	syslogKitSection,
	dohKitSection,
	ipsKitSection,
	mgmtKitSection,
	radiusKitSection,
	usgKitSection,
	igmpSnoopingKitSection,
	localeKitSection,
	globalNatKitSection,
	sslInspectionKitSection,
	ipsecKitSection,
	dashboardKitSection,
	etherLightingKitSection,
}

// settingKitSections adapts settingKitSectionTable to
// []resourcekit.Section[settingResourceModel], bound to r.client. Called
// from Configure, once r.client exists for every kit section's own backend
// to use.
func settingKitSections(r *settingResource) []resourcekit.Section[settingResourceModel] {
	sections := make([]resourcekit.Section[settingResourceModel], 0, len(settingKitSectionTable))
	for _, kit := range settingKitSectionTable {
		sections = append(sections, kit(r.client.ApiClient))
	}
	return sections
}

// auto_speedtest, country, dpi, lcm, network_optimization, ntp, syslog, doh,
// ips, mgmt, radius, usg and igmp_snooping all moved onto
// resourcekit.SpecSection -- see setting_auto_speedtest_descriptor.go,
// setting_country_descriptor.go, setting_dpi_descriptor.go,
// setting_lcm_descriptor.go, setting_network_optimization_descriptor.go,
// setting_ntp_descriptor.go, setting_syslog_descriptor.go,
// setting_doh_descriptor.go, setting_ips_descriptor.go,
// setting_mgmt_descriptor.go, setting_radius_descriptor.go,
// setting_usg_descriptor.go and setting_igmp_snooping_descriptor.go.
// ips_suppression (ips's own Extra) and usg_geo (usg's own Extra) moved the
// same way -- see setting_ips_descriptor.go's ipsSuppressionKitSpec/
// ipsSuppressionKitBackend and setting_usg_descriptor.go's usgGeoKitSpec/
// usgGeoKitBackend.
//
// locale, global_nat, ssl_inspection, ipsec, dashboard and ether_lighting
// moved the same way too, each from the controller's own
// Locale/GlobalNat/SslInspection/Ipsec/Dashboard/EtherLighting definition --
// see setting_locale_descriptor.go, setting_global_nat_descriptor.go,
// setting_ssl_inspection_descriptor.go, setting_ipsec_descriptor.go,
// setting_dashboard_descriptor.go and setting_ether_lighting_descriptor.go.
