package unifi

// The netflow section descriptor: an unconditional-mirror hydration with no
// specials, shaped like setting_mgmt_descriptor.go. All eleven of
// settings.Netflow's own fields are modelled; none is omitted.
//
// Like every other top-level settings document in this batch, Netflow's
// bootstrap fields carry no captured FieldConstraints (the lookup key is
// "SettingNetflow", not the bare "Netflow" the bootstrap walks a document
// under -- see setting_global_switch_descriptor.go's own comment for the
// mechanism), so sampling_mode's OneOf, port/sampling_rate's Between and
// version's OneOf are hand-transcribed in provider-codegen/policy/setting.json
// rather than compiler-derived.
//
// Five of netflow's six Int64PtrFields carry a controller-published pattern;
// checked one at a time against a literal "0":
//   - engine_id: `^$|[1-9][0-9]*` -- rejects "0" (the digit class starts at
//     1-9), needs OmitZero.
//   - export_frequency: no FieldConstraints entry captured at all -- nothing
//     to check, left without OmitZero (absence of a captured pattern is not
//     evidence 0 is safe, just evidence nobody has measured it either way).
//   - port: `HasBounds` 1024-65535 -- 0 is out of range, needs OmitZero.
//   - refresh_rate: no FieldConstraints entry captured -- same as
//     export_frequency.
//   - sampling_rate: `HasBounds` 2-16383 -- 0 is out of range, needs
//     OmitZero.
//   - version: `Int64Values` {5, 9, 10} -- 0 is not a member, needs
//     OmitZero.
//
// server's own pattern (`.{0,252}[^\.]$`) requires at least one non-dot
// character -- confirmed by running the anchored pattern against "" -- so
// unlike teleport.subnet_cidr (whose pattern has its own `|^$` escape
// hatch) an empty server rejects at plan time, and the field wants
// NullZero rather than KeepZero.
import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// netflowKitSpec maps every attribute of the generated netflow schema
// (resource_setting/setting_resource_gen.go's "netflow"
// SingleNestedAttribute) onto settings.Netflow. See this file's own top
// comment for the OmitZero and Elide reasoning behind each field.
func netflowKitSpec() resourcekit.Spec[settingNetflowModel, settings.Netflow] {
	return resourcekit.Spec[settingNetflowModel, settings.Netflow]{
		TypeName: "setting_netflow",
		Subject:  "NetFlow Setting",
		New:      func() *settings.Netflow { return &settings.Netflow{} },
		Fields:   settingNetflowGenFields(),
	}
}

// netflowKitBackend binds netflowKitSpec to a client: Read is
// GetSetting[*Netflow], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set.
func netflowKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Netflow] {
	return resourcekit.Backend[settings.Netflow]{
		Read: func(ctx context.Context, site, _ string) (*settings.Netflow, error) {
			_, netflow, err := ui.GetSetting[*settings.Netflow](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return netflow, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Netflow, fields ...string,
		) (*settings.Netflow, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// netflowKitSection builds the netflow entry for settingResource's
// Sections, bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func netflowKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := netflowKitSpec()
	spec.Backend = netflowKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingNetflowModel, settings.Netflow]{
		SectionName: "netflow",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Netflow },
		Set:         func(m *settingResourceModel, o types.Object) { m.Netflow = o },
		AttrTypes:   netflowAttrTypes,
		Spec:        spec,
	}
}
