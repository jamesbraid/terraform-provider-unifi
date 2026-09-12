package unifi

// The ntp section descriptor: an unconditional-mirror hydration whose only
// special is what it does NOT do -- an unset NTP server slot round-trips as
// a known "", never null, replacing the hand-written writeNtpSection /
// readNtpSection (setting_sections.go) and their ntpModelToSetting /
// ntpSettingToModel mappers (deleted from setting_resource.go). The model
// type and attribute-type map moved here too, from setting_resource.go:
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

// ntpKitSpec maps every attribute of the generated ntp schema
// (resource_setting/setting_resource_gen.go's "ntp" SingleNestedAttribute)
// onto settings.Ntp. Elide judgments follow resourcekit.ElideProblems'
// schema-driven rule, not a transcription of the old ntpSettingToModel: the
// four server slots are Optional+Computed with no validator rejecting an
// empty value, so KeepZero is what the check demands -- which happens to
// agree with the old mapper's own deliberate choice (round 1's finding that
// the controller persists an unused slot as "", a valid configured value
// distinct from unset). setting_preference carries a OneOf("auto","manual")
// validator that rejects "", so the check demands NullZero there instead,
// again agreeing with the old mapper's util.StringValueOrNull.
func ntpKitSpec() resourcekit.Spec[settingNtpModel, settings.Ntp] {
	return resourcekit.Spec[settingNtpModel, settings.Ntp]{
		TypeName: "setting_ntp",
		Subject:  "NTP Setting",
		New:      func() *settings.Ntp { return &settings.Ntp{} },
		Fields:   settingNtpGenFields(),
	}
}

// ntpKitBackend binds ntpKitSpec to a client: Read is GetSetting[*Ntp],
// UpdateFields is the masked UpdateSettingFields -- naming only the fields
// the plan set instead of the read-modify-write whole-document PUT
// writeNtpSection used.
func ntpKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Ntp] {
	return resourcekit.Backend[settings.Ntp]{
		Read: func(ctx context.Context, site, _ string) (*settings.Ntp, error) {
			_, ntp, err := ui.GetSetting[*settings.Ntp](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return ntp, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Ntp, fields ...string,
		) (*settings.Ntp, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// ntpKitSection builds the ntp entry for settingResource's Sections, bound to
// client via settingKitSections, which calls it with r.client.ApiClient.
func ntpKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := ntpKitSpec()
	spec.Backend = ntpKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingNtpModel, settings.Ntp]{
		SectionName: "ntp",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Ntp },
		Set:         func(m *settingResourceModel, o types.Object) { m.Ntp = o },
		AttrTypes:   ntpAttrTypes,
		Spec:        spec,
	}
}
