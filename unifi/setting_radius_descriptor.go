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
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

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
		Fields: resourcekit.Override(settingRadiusGenFields(), []resourcekit.Field[settingRadiusModel, settings.Radius]{
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
			resourcekit.DurationPtrField[settingRadiusModel, settings.Radius]{
				Wire:  "interim_update_interval",
				Model: func(m *settingRadiusModel) *timetypes.GoDuration { return &m.InterimUpdateInterval },
				SDK:   func(s *settings.Radius) **int64 { return &s.InterimUpdateInterval },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
		}),
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
// bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
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
