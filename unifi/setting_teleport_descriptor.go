package unifi

// The teleport section descriptor: an unconditional-mirror hydration with
// no specials, shaped exactly like setting_global_nat_descriptor.go.
//
// enabled and subnet_cidr are only weakly coupled: settings.Teleport's own
// wire pattern for subnet_cidr already tolerates an empty string
// regardless of enabled (`^(...)\/(...)$|^$`, confirmed by running the
// derived pattern against ""), so there is no "enabled requires subnet_cidr"
// or "subnet_cidr requires enabled" wire-level rule to enforce, and no
// schema-level AlsoRequires is added on top of it -- matching every other
// section in this dispatch, no plan-time cross-field validator is layered
// on a relationship the controller's own regex already resolves.

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

// settingTeleportModel is teleport's own section model, decoded out of
// settingResourceModel.Teleport.
type settingTeleportModel struct {
	Enabled    types.Bool   `tfsdk:"enabled"`
	SubnetCidr types.String `tfsdk:"subnet_cidr"`
}

// teleportAttrTypes types teleport's own object in state; it must match the
// generated schema exactly.
var teleportAttrTypes = map[string]attr.Type{
	"enabled":     types.BoolType,
	"subnet_cidr": types.StringType,
}

// teleportKitSpec maps both attributes of the generated teleport schema
// (resource_setting/setting_resource_gen.go's "teleport"
// SingleNestedAttribute) onto settings.Teleport. subnet_cidr carries a
// RegexMatches validator whose pattern's own top-level alternation
// (`...|^$`) already accepts an empty string -- confirmed by running the
// derived pattern against "" -- so resourcekit.ElideProblems' schema-driven
// rule (which probes the real validator with "") demands KeepZero, not
// NullZero.
func teleportKitSpec() resourcekit.Spec[settingTeleportModel, settings.Teleport] {
	return resourcekit.Spec[settingTeleportModel, settings.Teleport]{
		TypeName: "setting_teleport",
		Subject:  "Teleport Setting",
		New:      func() *settings.Teleport { return &settings.Teleport{} },
		Fields: []resourcekit.Field[settingTeleportModel, settings.Teleport]{
			resourcekit.BoolField[settingTeleportModel, settings.Teleport]{
				Wire:  "enabled",
				Model: func(m *settingTeleportModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *settings.Teleport) *bool { return &s.Enabled },
			},
			resourcekit.StringField[settingTeleportModel, settings.Teleport]{
				Wire:  "subnet_cidr",
				Model: func(m *settingTeleportModel) *types.String { return &m.SubnetCidr },
				SDK:   func(s *settings.Teleport) *string { return &s.SubnetCidr },
				Elide: resourcekit.KeepZero,
			},
		},
	}
}

// teleportNestedSchema is the teleport SingleNestedAttribute's own
// Attributes, wrapped as a schema.Schema so resourcekit's conformance
// checks -- built for a whole resource's top-level schema -- can run
// against one section of unifi_setting instead.
func teleportNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	teleport := built.Attributes["teleport"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // teleport is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: teleport.Attributes}
}

// teleportKitBackend binds teleportKitSpec to a client: Read is
// GetSetting[*Teleport], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set.
func teleportKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Teleport] {
	return resourcekit.Backend[settings.Teleport]{
		Read: func(ctx context.Context, site, _ string) (*settings.Teleport, error) {
			_, teleport, err := ui.GetSetting[*settings.Teleport](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return teleport, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Teleport, fields ...string,
		) (*settings.Teleport, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// teleportKitSection builds the teleport entry for settingResource's
// Sections, bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func teleportKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := teleportKitSpec()
	spec.Backend = teleportKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingTeleportModel, settings.Teleport]{
		SectionName: "teleport",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Teleport },
		Set:         func(m *settingResourceModel, o types.Object) { m.Teleport = o },
		AttrTypes:   teleportAttrTypes,
		Spec:        spec,
	}
}
