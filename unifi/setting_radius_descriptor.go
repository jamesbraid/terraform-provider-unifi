package unifi

// The radius section descriptor: replaces the hand-written writeRadiusSection
// / readRadiusSection (setting_sections.go) and their radiusModelToSetting /
// radiusSettingToModel mappers (deleted from setting_resource.go). The model
// type and attribute-type map moved here too, from setting_resource.go:
// descriptor_mapping_test.go's loadDescriptors reads a descriptor's model
// tags from the same file the Spec literal is in. See
// setting_mgmt_descriptor.go for the shape every section descriptor
// follows.
//
// interim_update_interval is a schema v1 upgrade (settingResource's own
// UpgradeState converts prior integer-seconds state to a GoDuration string);
// DurationPtrField with Units: time.Second matches radius_profile's own
// handling of the same shape (unifi/radius_profile_descriptor.go).
// configure_whole_network and tunneled_reply are settings.Radius members
// with no schema attribute at all -- Unmodelled, same as mgmt's ssh key
// date/fingerprint: a masked write never names them, so they carry no
// Field entry.
//
// secret is Optional+Computed+Sensitive; only its read side is special,
// handled by radiusAfterReceive rather than an unconditional-mirror
// hydration -- see that function's own comment for the exact legacy
// behaviour it pins.

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingRadiusModel is radius's own section model, decoded out of
// settingResourceModel.Radius.
type settingRadiusModel struct {
	AccountingEnabled     types.Bool           `tfsdk:"accounting_enabled"`
	Enabled               types.Bool           `tfsdk:"enabled"`
	AcctPort              types.Int64          `tfsdk:"acct_port"`
	AuthPort              types.Int64          `tfsdk:"auth_port"`
	InterimUpdateInterval timetypes.GoDuration `tfsdk:"interim_update_interval"`
	Secret                types.String         `tfsdk:"secret"`
}

// radiusAttrTypes types radius's own object in state; it must match the
// generated schema exactly.
var radiusAttrTypes = map[string]attr.Type{
	"accounting_enabled":      types.BoolType,
	"enabled":                 types.BoolType,
	"acct_port":               types.Int64Type,
	"auth_port":               types.Int64Type,
	"interim_update_interval": timetypes.GoDurationType{},
	"secret":                  types.StringType,
}

// radiusKitSpec maps every attribute of the generated radius schema
// (resource_setting/setting_resource_gen.go's "radius" SingleNestedAttribute)
// onto settings.Radius. Elide judgments follow resourcekit.ElideProblems'
// schema-driven rule, not a transcription of the old radiusSettingToModel:
// accounting_enabled and enabled are plain bools, which carry no Elide at
// all; acct_port, auth_port and interim_update_interval are Optional+
// Computed with only a Between/no validator ElideProblems' zeroIsRejected
// can act on for a non-string kind, so KeepZero is what the check demands,
// same as every other Int64PtrField/DurationPtrField this repo has (e.g.
// radius_profile's own interim_update_interval). secret carries
// LengthBetween(1, 48), which rejects "", so the check demands NullZero --
// the same rule util.StringValueOrNull applied by hand.
func radiusKitSpec() resourcekit.Spec[settingRadiusModel, settings.Radius] {
	return resourcekit.Spec[settingRadiusModel, settings.Radius]{
		TypeName: "setting_radius",
		Subject:  "Radius Setting",
		New:      func() *settings.Radius { return &settings.Radius{} },
		Fields: []resourcekit.Field[settingRadiusModel, settings.Radius]{
			resourcekit.BoolField[settingRadiusModel, settings.Radius]{
				Wire:  "accounting_enabled",
				Model: func(m *settingRadiusModel) *types.Bool { return &m.AccountingEnabled },
				SDK:   func(s *settings.Radius) *bool { return &s.AccountingEnabled },
			},
			resourcekit.Int64PtrField[settingRadiusModel, settings.Radius]{
				Wire:  "acct_port",
				Model: func(m *settingRadiusModel) *types.Int64 { return &m.AcctPort },
				SDK:   func(s *settings.Radius) **int64 { return &s.AcctPort },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[settingRadiusModel, settings.Radius]{
				Wire:  "auth_port",
				Model: func(m *settingRadiusModel) *types.Int64 { return &m.AuthPort },
				SDK:   func(s *settings.Radius) **int64 { return &s.AuthPort },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingRadiusModel, settings.Radius]{
				Wire:  "enabled",
				Model: func(m *settingRadiusModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *settings.Radius) *bool { return &s.Enabled },
			},
			resourcekit.DurationPtrField[settingRadiusModel, settings.Radius]{
				Wire:  "interim_update_interval",
				Model: func(m *settingRadiusModel) *timetypes.GoDuration { return &m.InterimUpdateInterval },
				SDK:   func(s *settings.Radius) **int64 { return &s.InterimUpdateInterval },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingRadiusModel, settings.Radius]{
				Wire:  "x_secret",
				Model: func(m *settingRadiusModel) *types.String { return &m.Secret },
				SDK:   func(s *settings.Radius) *string { return &s.Secret },
				Elide: resourcekit.NullZero,
			},
		},
	}
}

// radiusAfterReceive reproduces the deleted radiusSettingToModel's one
// special case. Every other field is left exactly as Spec.ToModel already
// decoded it off the wire -- radius has no plan-conditioned nulling for
// accounting_enabled/acct_port/auth_port/enabled/interim_update_interval,
// unlike mgmt's eight bools.
//
// secret is Optional+Computed+Sensitive and the controller echoes back
// whatever it currently holds. The deleted mapper read:
//
//	if !plan.Secret.IsNull() && !plan.Secret.IsUnknown() {
//		model.Secret = util.StringValueOrNull(setting.Secret)
//	} else {
//		model.Secret = types.StringNull()
//	}
//
// which is NOT "keep the plan's own string" (the brief's shorthand) -- a
// named plan/prior still surfaces the controller's own decoded echo, not
// the value the practitioner typed, so a value the controller normalizes on
// write would read back the controller's spelling. Only an unconfigured
// secret is forced null, so it never drifts. The StringField's own
// Elide: NullZero (registered on radiusKitSpec, above) already reproduces
// util.StringValueOrNull's raw==""->null rule as part of Spec.ToModel; this
// hook only adds the plan-conditioned null on top, mirroring mgmt's
// AfterReceive shape. Pinned by
// TestRadiusAfterReceiveKeepsThePlansSecretWhenNamed against the deleted
// mapper's own two unit tests (Test_settingResource_radiusSettingToModel).
func radiusAfterReceive(
	_ context.Context, _ *settings.Radius, model *settingRadiusModel, prior settingRadiusModel,
) diag.Diagnostics {
	if prior.Secret.IsNull() || prior.Secret.IsUnknown() {
		model.Secret = types.StringNull()
	}
	return nil
}

// radiusNestedSchema is the radius SingleNestedAttribute's own Attributes,
// wrapped as a schema.Schema so resourcekit's conformance checks -- built
// for a whole resource's top-level schema -- can run against one section of
// unifi_setting instead.
func radiusNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	radius := built.Attributes["radius"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // radius is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: radius.Attributes}
}

// radiusKitBackend binds radiusKitSpec to a client: Read is
// GetSetting[*Radius], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set instead of the read-modify-write
// whole-document PUT writeRadiusSection used.
func radiusKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Radius] {
	return resourcekit.Backend[settings.Radius]{
		Read: func(ctx context.Context, site, _ string) (*settings.Radius, error) {
			_, radius, err := ui.GetSetting[*settings.Radius](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return radius, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Radius, fields ...string,
		) (*settings.Radius, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// radiusKitSection builds the radius entry for settingResource's Sections,
// bound to client the same way legacySectionsFor binds *settingResource.
func radiusKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := radiusKitSpec()
	spec.Backend = radiusKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingRadiusModel, settings.Radius]{
		SectionName:  "radius",
		Get:          func(m *settingResourceModel) *types.Object { return &m.Radius },
		Set:          func(m *settingResourceModel, o types.Object) { m.Radius = o },
		AttrTypes:    radiusAttrTypes,
		Spec:         spec,
		AfterReceive: radiusAfterReceive,
	}
}
