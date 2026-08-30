package unifi

// The global_network section descriptor: an unconditional-mirror hydration
// with no specials, shaped exactly like setting_ipsec_descriptor.go.
//
// settings.GlobalNetwork is hand-maintained in the SDK rather than
// generated from the locked field spec (the controller exposes it at
// /api/s/<site>/{get,set}/setting/global_network on newer releases, ahead
// of the spec capture) -- see its own doc comment. It is still one of the
// settings GetSettingKey recognises, so it derives from the controller the
// same way every generated section does.

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

// settingGlobalNetworkModel is global_network's own section model, decoded
// out of settingResourceModel.GlobalNetwork.
type settingGlobalNetworkModel struct {
	DefaultSecurityPosture types.String `tfsdk:"default_security_posture"`
}

// globalNetworkAttrTypes types global_network's own object in state; it must
// match the generated schema exactly.
var globalNetworkAttrTypes = map[string]attr.Type{
	"default_security_posture": types.StringType,
}

// globalNetworkKitSpec maps the one attribute of the generated
// global_network schema (resource_setting/setting_resource_gen.go's
// "global_network" SingleNestedAttribute) onto settings.GlobalNetwork.
// default_security_posture is Optional+Computed with no validator rejecting
// an empty value -- the SDK records only one observed value, not a formal
// enum -- so resourcekit.ElideProblems' schema-driven rule demands
// KeepZero.
func globalNetworkKitSpec() resourcekit.Spec[settingGlobalNetworkModel, settings.GlobalNetwork] {
	return resourcekit.Spec[settingGlobalNetworkModel, settings.GlobalNetwork]{
		TypeName: "setting_global_network",
		Subject:  "Global Network Setting",
		New:      func() *settings.GlobalNetwork { return &settings.GlobalNetwork{} },
		Fields: []resourcekit.Field[settingGlobalNetworkModel, settings.GlobalNetwork]{
			resourcekit.StringField[settingGlobalNetworkModel, settings.GlobalNetwork]{
				Wire:  "default_security_posture",
				Model: func(m *settingGlobalNetworkModel) *types.String { return &m.DefaultSecurityPosture },
				SDK:   func(s *settings.GlobalNetwork) *string { return &s.DefaultSecurityPosture },
				Elide: resourcekit.KeepZero,
			},
		},
	}
}

// globalNetworkNestedSchema is the global_network SingleNestedAttribute's
// own Attributes, wrapped as a schema.Schema so resourcekit's conformance
// checks -- built for a whole resource's top-level schema -- can run
// against one section of unifi_setting instead.
func globalNetworkNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	globalNetwork := built.Attributes["global_network"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // global_network is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: globalNetwork.Attributes}
}

// globalNetworkKitBackend binds globalNetworkKitSpec to a client: Read is
// GetSetting[*GlobalNetwork], UpdateFields is the masked
// UpdateSettingFields -- naming only the fields the plan set.
func globalNetworkKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.GlobalNetwork] {
	return resourcekit.Backend[settings.GlobalNetwork]{
		Read: func(ctx context.Context, site, _ string) (*settings.GlobalNetwork, error) {
			_, globalNetwork, err := ui.GetSetting[*settings.GlobalNetwork](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return globalNetwork, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.GlobalNetwork, fields ...string,
		) (*settings.GlobalNetwork, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// globalNetworkKitSection builds the global_network entry for
// settingResource's Sections, bound to client via settingKitSections, which
// calls it with r.client.ApiClient.
func globalNetworkKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := globalNetworkKitSpec()
	spec.Backend = globalNetworkKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingGlobalNetworkModel, settings.GlobalNetwork]{
		SectionName: "global_network",
		Get:         func(m *settingResourceModel) *types.Object { return &m.GlobalNetwork },
		Set:         func(m *settingResourceModel, o types.Object) { m.GlobalNetwork = o },
		AttrTypes:   globalNetworkAttrTypes,
		Spec:        spec,
	}
}
