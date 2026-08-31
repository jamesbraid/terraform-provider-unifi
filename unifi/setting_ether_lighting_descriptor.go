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

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingEtherLightingNetworkOverrideModel is one element of
// ether_lighting's network_overrides list.
type settingEtherLightingNetworkOverrideModel struct {
	Key         types.String `tfsdk:"key"`
	RawColorHex types.String `tfsdk:"raw_color_hex"`
}

// settingEtherLightingSpeedOverrideModel is one element of
// ether_lighting's speed_overrides list.
type settingEtherLightingSpeedOverrideModel struct {
	Key         types.String `tfsdk:"key"`
	RawColorHex types.String `tfsdk:"raw_color_hex"`
}

// settingEtherLightingModel is ether_lighting's own section model, decoded
// out of settingResourceModel.EtherLighting.
type settingEtherLightingModel struct {
	NetworkOverrides types.List `tfsdk:"network_overrides"`
	SpeedOverrides   types.List `tfsdk:"speed_overrides"`
}

// etherLightingNetworkOverrideAttrTypes, etherLightingSpeedOverrideAttrTypes
// and etherLightingAttrTypes type ether_lighting's two override lists'
// elements and ether_lighting's own object in state; all three must match
// the generated schema exactly.
var (
	etherLightingNetworkOverrideAttrTypes = map[string]attr.Type{
		"key":           types.StringType,
		"raw_color_hex": types.StringType,
	}
	etherLightingSpeedOverrideAttrTypes = map[string]attr.Type{
		"key":           types.StringType,
		"raw_color_hex": types.StringType,
	}
	etherLightingAttrTypes = map[string]attr.Type{
		"network_overrides": types.ListType{
			ElemType: types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes},
		},
		"speed_overrides": types.ListType{
			ElemType: types.ObjectType{AttrTypes: etherLightingSpeedOverrideAttrTypes},
		},
	}
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
		Fields: []resourcekit.Field[settingEtherLightingModel, settings.EtherLighting]{
			resourcekit.ObjectListField[
				settingEtherLightingModel, settings.EtherLighting, settings.SettingEtherLightingNetworkOverrides,
			]{
				Wire:  "network_overrides",
				Model: func(m *settingEtherLightingModel) *types.List { return &m.NetworkOverrides },
				SDK: func(s *settings.EtherLighting) *[]settings.SettingEtherLightingNetworkOverrides {
					return &s.NetworkOverrides
				},
				AttrTypes: etherLightingNetworkOverrideAttrTypes,
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
				AttrTypes: etherLightingSpeedOverrideAttrTypes,
				Encode:    etherLightingSpeedOverrideEncode,
				Decode:    etherLightingSpeedOverrideDecode,
				Elide:     resourcekit.KeepZero,
			},
		},
	}
}

func etherLightingNetworkOverrideEncode(
	ctx context.Context, object types.Object,
) (settings.SettingEtherLightingNetworkOverrides, diag.Diagnostics) {
	var model settingEtherLightingNetworkOverrideModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingEtherLightingNetworkOverrides{
		Key:         model.Key.ValueString(),
		RawColorHex: model.RawColorHex.ValueString(),
	}, diags
}

func etherLightingNetworkOverrideDecode(
	ctx context.Context, element settings.SettingEtherLightingNetworkOverrides,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, etherLightingNetworkOverrideAttrTypes, settingEtherLightingNetworkOverrideModel{
		Key:         types.StringValue(element.Key),
		RawColorHex: types.StringValue(element.RawColorHex),
	})
}

func etherLightingSpeedOverrideEncode(
	ctx context.Context, object types.Object,
) (settings.SettingEtherLightingSpeedOverrides, diag.Diagnostics) {
	var model settingEtherLightingSpeedOverrideModel
	diags := object.As(ctx, &model, basetypes.ObjectAsOptions{})
	return settings.SettingEtherLightingSpeedOverrides{
		Key:         model.Key.ValueString(),
		RawColorHex: model.RawColorHex.ValueString(),
	}, diags
}

func etherLightingSpeedOverrideDecode(
	ctx context.Context, element settings.SettingEtherLightingSpeedOverrides,
) (types.Object, diag.Diagnostics) {
	return types.ObjectValueFrom(ctx, etherLightingSpeedOverrideAttrTypes, settingEtherLightingSpeedOverrideModel{
		Key:         types.StringValue(element.Key),
		RawColorHex: types.StringValue(element.RawColorHex),
	})
}

// etherLightingNestedSchema is the ether_lighting SingleNestedAttribute's
// own Attributes, wrapped as a schema.Schema so resourcekit's conformance
// checks -- built for a whole resource's top-level schema -- can run
// against one section of unifi_setting instead.
func etherLightingNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	etherLighting := built.Attributes["ether_lighting"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // ether_lighting is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: etherLighting.Attributes}
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
