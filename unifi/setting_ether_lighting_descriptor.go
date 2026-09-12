package unifi

// The ether_lighting section descriptor: an unconditional-mirror hydration
// with no specials; network_overrides and speed_overrides are
// ether_lighting's own ObjectListFields, the same kind doh's
// custom_servers and mgmt's ssh_keys use. See setting_mgmt_descriptor.go
// for the shape every section descriptor follows, and
// setting_doh_descriptor.go for the ObjectListField pattern this one
// repeats twice.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// etherLightingKitSpec maps every attribute of the generated ether_lighting
// schema (resource_setting/setting_resource_gen.go's "ether_lighting"
// SingleNestedAttribute) onto settings.EtherLighting. Both lists are
// Optional+Computed with no zero-rejecting validator of their own kind, so
// they want KeepZero, same as doh's custom_servers.
func etherLightingKitSpec() resourcekit.Spec[settingEtherLightingModel, settings.EtherLighting] {
	return resourcekit.Spec[settingEtherLightingModel, settings.EtherLighting]{
		TypeName: "setting_ether_lighting",
		Subject:  "Ethernet Lighting Setting",
		New:      func() *settings.EtherLighting { return &settings.EtherLighting{} },
		Fields: resourcekit.Override(settingEtherLightingGenFields(), []resourcekit.Field[settingEtherLightingModel, settings.EtherLighting]{
			resourcekit.ObjectListField[
				settingEtherLightingModel, settings.EtherLighting, settings.SettingEtherLightingNetworkOverrides,
			]{
				Wire:  "network_overrides",
				Model: func(m *settingEtherLightingModel) *types.List { return &m.NetworkOverrides },
				SDK: func(s *settings.EtherLighting) *[]settings.SettingEtherLightingNetworkOverrides {
					return &s.NetworkOverrides
				},
				AttrTypes: etherLightingNetworkOverridesAttrTypes,
				Encode:    etherLightingNetworkOverrideEncode,
				Decode:    etherLightingNetworkOverrideDecode,
				Elide:     resourcekit.KeepZero,
			},
			resourcekit.ObjectListField[
				settingEtherLightingModel, settings.EtherLighting, settings.SettingEtherLightingSpeedOverrides,
			]{
				Wire:  "speed_overrides",
				Model: func(m *settingEtherLightingModel) *types.List { return &m.SpeedOverrides },
				SDK: func(s *settings.EtherLighting) *[]settings.SettingEtherLightingSpeedOverrides {
					return &s.SpeedOverrides
				},
				AttrTypes: etherLightingSpeedOverridesAttrTypes,
				Encode:    etherLightingSpeedOverrideEncode,
				Decode:    etherLightingSpeedOverrideDecode,
				Elide:     resourcekit.KeepZero,
			},
		}),
	}
}

func etherLightingNetworkOverrideEncode(
	ctx context.Context, object types.Object,
) (settings.SettingEtherLightingNetworkOverrides, diag.Diagnostics) {
	var model etherLightingNetworkOverridesModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingEtherLightingNetworkOverrides{
		Key:         model.Key.ValueString(),
		RawColorHex: model.RawColorHex.ValueString(),
	}, diags
}

func etherLightingNetworkOverrideDecode(
	ctx context.Context, element settings.SettingEtherLightingNetworkOverrides,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, etherLightingNetworkOverridesAttrTypes, etherLightingNetworkOverridesModel{
		Key:         types.StringValue(element.Key),
		RawColorHex: types.StringValue(element.RawColorHex),
	})
}

func etherLightingSpeedOverrideEncode(
	ctx context.Context, object types.Object,
) (settings.SettingEtherLightingSpeedOverrides, diag.Diagnostics) {
	var model etherLightingSpeedOverridesModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingEtherLightingSpeedOverrides{
		Key:         model.Key.ValueString(),
		RawColorHex: model.RawColorHex.ValueString(),
	}, diags
}

func etherLightingSpeedOverrideDecode(
	ctx context.Context, element settings.SettingEtherLightingSpeedOverrides,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, etherLightingSpeedOverridesAttrTypes, etherLightingSpeedOverridesModel{
		Key:         types.StringValue(element.Key),
		RawColorHex: types.StringValue(element.RawColorHex),
	})
}

// etherLightingKitBackend binds etherLightingKitSpec to a client: Read is
// GetSetting[*EtherLighting], UpdateFields is the masked
// UpdateSettingFields -- naming only the fields the plan set.
func etherLightingKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.EtherLighting] {
	return resourcekit.Backend[settings.EtherLighting]{
		Read: func(ctx context.Context, site, _ string) (*settings.EtherLighting, error) {
			_, etherLighting, err := ui.GetSetting[*settings.EtherLighting](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return etherLighting, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.EtherLighting, fields ...string,
		) (*settings.EtherLighting, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// etherLightingKitSection builds the ether_lighting entry for
// settingResource's Sections, bound to client via settingKitSections,
// which calls it with r.client.ApiClient.
func etherLightingKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := etherLightingKitSpec()
	spec.Backend = etherLightingKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingEtherLightingModel, settings.EtherLighting]{
		SectionName: "ether_lighting",
		Get:         func(m *settingResourceModel) *types.Object { return &m.EtherLighting },
		Set:         func(m *settingResourceModel, o types.Object) { m.EtherLighting = o },
		AttrTypes:   etherLightingAttrTypes,
		Spec:        spec,
	}
}
