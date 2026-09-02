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

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingGlobalApModel is global_ap's own section model, decoded out of
// settingResourceModel.GlobalAp.
type settingGlobalApModel struct {
	ApExclusions  types.List   `tfsdk:"ap_exclusions"`
	NaChannelSize types.Int64  `tfsdk:"na_channel_size"`
	NaTxPower     types.Int64  `tfsdk:"na_tx_power"`
	NaTxPowerMode types.String `tfsdk:"na_tx_power_mode"`
	NgChannelSize types.Int64  `tfsdk:"ng_channel_size"`
	NgTxPower     types.Int64  `tfsdk:"ng_tx_power"`
	NgTxPowerMode types.String `tfsdk:"ng_tx_power_mode"`
}

// globalApAttrTypes types global_ap's own object in state; it must match
// the generated schema exactly.
var globalApAttrTypes = map[string]attr.Type{
	"ap_exclusions":    types.ListType{ElemType: types.StringType},
	"na_channel_size":  types.Int64Type,
	"na_tx_power":      types.Int64Type,
	"na_tx_power_mode": types.StringType,
	"ng_channel_size":  types.Int64Type,
	"ng_tx_power":      types.Int64Type,
	"ng_tx_power_mode": types.StringType,
}

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
		Fields: []resourcekit.Field[settingGlobalApModel, settings.GlobalAp]{
			resourcekit.StringListField[settingGlobalApModel, settings.GlobalAp]{
				Wire:  "ap_exclusions",
				Model: func(m *settingGlobalApModel) *types.List { return &m.ApExclusions },
				SDK:   func(s *settings.GlobalAp) *[]string { return &s.ApExclusions },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[settingGlobalApModel, settings.GlobalAp]{
				Wire:     "na_channel_size",
				Model:    func(m *settingGlobalApModel) *types.Int64 { return &m.NaChannelSize },
				SDK:      func(s *settings.GlobalAp) **int64 { return &s.NaChannelSize },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.Int64PtrField[settingGlobalApModel, settings.GlobalAp]{
				Wire:  "na_tx_power",
				Model: func(m *settingGlobalApModel) *types.Int64 { return &m.NaTxPower },
				SDK:   func(s *settings.GlobalAp) **int64 { return &s.NaTxPower },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGlobalApModel, settings.GlobalAp]{
				Wire:  "na_tx_power_mode",
				Model: func(m *settingGlobalApModel) *types.String { return &m.NaTxPowerMode },
				SDK:   func(s *settings.GlobalAp) *string { return &s.NaTxPowerMode },
				Elide: resourcekit.NullZero,
			},
			resourcekit.Int64PtrField[settingGlobalApModel, settings.GlobalAp]{
				Wire:     "ng_channel_size",
				Model:    func(m *settingGlobalApModel) *types.Int64 { return &m.NgChannelSize },
				SDK:      func(s *settings.GlobalAp) **int64 { return &s.NgChannelSize },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.Int64PtrField[settingGlobalApModel, settings.GlobalAp]{
				Wire:  "ng_tx_power",
				Model: func(m *settingGlobalApModel) *types.Int64 { return &m.NgTxPower },
				SDK:   func(s *settings.GlobalAp) **int64 { return &s.NgTxPower },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingGlobalApModel, settings.GlobalAp]{
				Wire:  "ng_tx_power_mode",
				Model: func(m *settingGlobalApModel) *types.String { return &m.NgTxPowerMode },
				SDK:   func(s *settings.GlobalAp) *string { return &s.NgTxPowerMode },
				Elide: resourcekit.NullZero,
			},
		},
	}
}

// globalApNestedSchema is the global_ap SingleNestedAttribute's own
// Attributes, wrapped as a schema.Schema so resourcekit's conformance
// checks -- built for a whole resource's top-level schema -- can run
// against one section of unifi_setting instead.
func globalApNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	globalAp := built.Attributes["global_ap"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // global_ap is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: globalAp.Attributes}
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
