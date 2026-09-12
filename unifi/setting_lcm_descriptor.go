package unifi

// The lcm section descriptor: an unconditional-mirror hydration whose only
// special is the #288 omit-not-zero guard on brightness/idle_timeout,
// replacing the hand-written writeLcmSection / readLcmSection
// (setting_sections.go) and their lcmModelToSetting / lcmSettingToModel
// mappers (deleted from setting_resource.go). The model type and
// attribute-type map moved here too, from setting_resource.go:
// descriptor_mapping_test.go's loadDescriptors reads a descriptor's model
// tags from the same file the Spec literal is in. See
// setting_mgmt_descriptor.go for the shape every section descriptor
// follows.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// lcmKitSpec maps every attribute of the generated lcm schema
// (resource_setting/setting_resource_gen.go's "lcm" SingleNestedAttribute)
// onto settings.Lcm. Elide judgments follow resourcekit.ElideProblems'
// schema-driven rule: brightness and idle_timeout are Optional+Computed
// Int64 attributes, and ElideProblems' zeroIsRejected only ever inspects a
// StringAttribute's validators, so an Int64 range validator (1-100,
// 10-3600) can't drive it to NullZero the way ntp's setting_preference
// OneOf did -- KeepZero is what the check demands for both, matching the
// old mapper's own Int64PointerValue passthrough (nil stays null, a
// pointer to zero stays zero). OmitZero is the separate, write-side #288
// guard: an unknown (unset Optional+Computed) value's ValueInt64Pointer()
// resolves to a pointer to zero, which the controller rejects as out of
// range, so it must never reach the wire.
func lcmKitSpec() resourcekit.Spec[settingLcmModel, settings.Lcm] {
	return resourcekit.Spec[settingLcmModel, settings.Lcm]{
		TypeName: "setting_lcm",
		Subject:  "LCM Setting",
		New:      func() *settings.Lcm { return &settings.Lcm{} },
		Fields:   settingLcmGenFields(),
	}
}

// lcmKitBackend binds lcmKitSpec to a client: Read is GetSetting[*Lcm],
// UpdateFields is the masked UpdateSettingFields -- naming only the fields
// the plan set instead of the read-modify-write whole-document PUT
// writeLcmSection used.
func lcmKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Lcm] {
	return resourcekit.Backend[settings.Lcm]{
		Read: func(ctx context.Context, site, _ string) (*settings.Lcm, error) {
			_, lcm, err := ui.GetSetting[*settings.Lcm](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return lcm, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Lcm, fields ...string,
		) (*settings.Lcm, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// lcmKitSection builds the lcm entry for settingResource's Sections, bound to
// client via settingKitSections, which calls it with r.client.ApiClient.
func lcmKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := lcmKitSpec()
	spec.Backend = lcmKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingLcmModel, settings.Lcm]{
		SectionName: "lcm",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Lcm },
		Set:         func(m *settingResourceModel, o types.Object) { m.Lcm = o },
		AttrTypes:   lcmAttrTypes,
		Spec:        spec,
	}
}
