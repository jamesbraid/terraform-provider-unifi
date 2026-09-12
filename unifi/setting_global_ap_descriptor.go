package unifi

// The global_ap section descriptor: an unconditional-mirror hydration with
// no specials, shaped like setting_global_switch_descriptor.go -- one
// StringListField (ap_exclusions) beside scalar radio fields.
//
// The pinned go-unifi SDK's settings.GlobalAp defines ten fields of its
// own; seven are modelled. The 6 GHz trio (6e_channel_size, 6e_tx_power,
// 6e_tx_power_mode) is deliberately NOT: each wire name begins with a
// digit, which no Terraform attribute name may, so exposing them would
// mean inventing a rename the controller's definition doesn't carry --
// omitted rather than renamed, recorded in
// provider-codegen/policy/setting.json's omitted list.
//
// Field constraints here ARE compiler-derived (unlike the batch
// setting_global_switch_descriptor.go's own comment describes, which
// predates cmd/sdk-bootstrap's "Setting"-prefixed FieldConstraints
// fallback): the channel-size OneOfs and the tx-power-mode OneOfs come
// straight from settings.FieldConstraints["SettingGlobalAp"]. The two
// hand-transcribed validators in policy/setting.json are the kinds the
// compiler does not derive: ap_exclusions' per-element MAC pattern (a
// list) and the tx powers' Between (a bounded range).

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// globalApKitSpec maps every modelled attribute of the generated global_ap
// schema (resource_setting/setting_resource_gen.go's "global_ap"
// SingleNestedAttribute) onto settings.GlobalAp. The channel sizes'
// Int64Values sets ({20, 40, 80, 160} and {20, 40}) exclude 0, so both
// carry OmitZero; the tx powers' bounds (0-49) admit a literal 0, so
// neither does -- the same per-pattern check
// setting_netflow_descriptor.go's top comment walks through. The two mode
// strings' derived OneOf rejects "", so both want NullZero by
// resourcekit.ElideProblems' schema-driven rule; ap_exclusions and the
// Int64PtrFields want KeepZero like every other list and pointer int in
// this resource.
func globalApKitSpec() resourcekit.Spec[settingGlobalApModel, settings.GlobalAp] {
	return resourcekit.Spec[settingGlobalApModel, settings.GlobalAp]{
		TypeName: "setting_global_ap",
		Subject:  "Global AP Setting",
		New:      func() *settings.GlobalAp { return &settings.GlobalAp{} },
		Fields:   settingGlobalApGenFields(),
	}
}

// globalApKitBackend binds globalApKitSpec to a client: Read is
// GetSetting[*GlobalAp], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set.
func globalApKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.GlobalAp] {
	return resourcekit.Backend[settings.GlobalAp]{
		Read: func(ctx context.Context, site, _ string) (*settings.GlobalAp, error) {
			_, globalAp, err := ui.GetSetting[*settings.GlobalAp](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return globalAp, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.GlobalAp, fields ...string,
		) (*settings.GlobalAp, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// globalApKitSection builds the global_ap entry for settingResource's
// Sections, bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func globalApKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := globalApKitSpec()
	spec.Backend = globalApKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingGlobalApModel, settings.GlobalAp]{
		SectionName: "global_ap",
		Get:         func(m *settingResourceModel) *types.Object { return &m.GlobalAp },
		Set:         func(m *settingResourceModel, o types.Object) { m.GlobalAp = o },
		AttrTypes:   globalApAttrTypes,
		Spec:        spec,
	}
}
