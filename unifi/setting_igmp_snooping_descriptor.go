package unifi

// The igmp_snooping section descriptor: a two-of-fifteen-fields hydration,
// replacing the hand-written writeIgmpSnoopingSection /
// readIgmpSnoopingSection (setting_sections.go) and their
// igmpSnoopingModelToSetting / igmpSnoopingSettingToModel mappers (deleted
// from setting_resource.go). The model type and attribute-type map moved
// here too, from setting_resource.go: descriptor_mapping_test.go's
// loadDescriptors reads a descriptor's model tags from the same file the
// Spec literal is in. See setting_mgmt_descriptor.go for the shape every
// section descriptor follows.
//
// settings.IgmpSnooping carries thirteen more fields (querier mode,
// switches, flood options -- advanced UI-only settings) that this schema
// never modelled. The old mapper read the live document first and
// overlaid enabled/network_ids onto it before writing, specifically to
// keep those thirteen fields from being clobbered by a whole-document PUT.
// UpdateSettingFields' field mask makes that read-modify-write
// unnecessary: naming only "enabled" and "network_ids" on the wire leaves
// every other stored field untouched server-side, which
// TestIgmpSnoopingSpecMasksOnlyEnabled pins.
//
// Controller fact, not a provider behaviour (Task 1, measured on 10.4.57):
// enabled only sticks when network_ids names at least one network. A plan
// that sets enabled=true with an empty network_ids reads back false. This
// descriptor sends exactly what the plan configures either way; the
// controller's own conditional acceptance is exercised by
// TestAccSettingResource_igmpSnooping, not asserted here.

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

// settingIgmpSnoopingModel is igmp_snooping's own section model, decoded
// out of settingResourceModel.IgmpSnooping.
type settingIgmpSnoopingModel struct {
	Enabled    types.Bool `tfsdk:"enabled"`
	NetworkIDs types.List `tfsdk:"network_ids"`
}

// igmpSnoopingAttrTypes types igmp_snooping's own object in state; it must
// match the generated schema exactly.
var igmpSnoopingAttrTypes = map[string]attr.Type{
	"enabled":     types.BoolType,
	"network_ids": types.ListType{ElemType: types.StringType},
}

// igmpSnoopingKitSpec maps the two managed attributes of the generated
// igmp_snooping schema (resource_setting/setting_resource_gen.go's
// "igmp_snooping" SingleNestedAttribute) onto settings.IgmpSnooping. Both
// attributes are Optional+Computed with no validator rejecting an empty
// value, so ElideProblems' schema-driven rule demands KeepZero for both --
// enabled is a plain bool anyway (no Elide claim at all), and network_ids'
// KeepZero agrees with the old igmpSnoopingSettingToModel, which never
// nulled an empty read.
func igmpSnoopingKitSpec() resourcekit.Spec[settingIgmpSnoopingModel, settings.IgmpSnooping] {
	return resourcekit.Spec[settingIgmpSnoopingModel, settings.IgmpSnooping]{
		TypeName: "setting_igmp_snooping",
		Subject:  "IGMP Snooping Setting",
		New:      func() *settings.IgmpSnooping { return &settings.IgmpSnooping{} },
		Fields: []resourcekit.Field[settingIgmpSnoopingModel, settings.IgmpSnooping]{
			resourcekit.BoolField[settingIgmpSnoopingModel, settings.IgmpSnooping]{
				Wire:  "enabled",
				Model: func(m *settingIgmpSnoopingModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *settings.IgmpSnooping) *bool { return &s.Enabled },
			},
			resourcekit.StringListField[settingIgmpSnoopingModel, settings.IgmpSnooping]{
				Wire:  "network_ids",
				Model: func(m *settingIgmpSnoopingModel) *types.List { return &m.NetworkIDs },
				SDK:   func(s *settings.IgmpSnooping) *[]string { return &s.NetworkIDs },
				Elide: resourcekit.KeepZero,
			},
		},
	}
}

// igmpSnoopingNestedSchema is the igmp_snooping SingleNestedAttribute's own
// Attributes, wrapped as a schema.Schema so resourcekit's conformance
// checks -- built for a whole resource's top-level schema -- can run
// against one section of unifi_setting instead. Unlike every other section
// migrated so far, igmp_snooping's own top-level attribute is Optional-only
// (not Computed): resourcekit.SpecSection.Configured keys on the object
// being non-null in the plan, which needs no Computed flag to work, and
// TestIgmpSnoopingKitSpecConformance is what confirms the instrument
// accepts that shape.
func igmpSnoopingNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	igmp := built.Attributes["igmp_snooping"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // igmp_snooping is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: igmp.Attributes}
}

// igmpSnoopingKitBackend binds igmpSnoopingKitSpec to a client: Read is
// GetSetting[*IgmpSnooping], UpdateFields is the masked UpdateSettingFields
// -- naming only the fields the plan set instead of the read-modify-write
// whole-document PUT writeIgmpSnoopingSection used to preserve the other
// thirteen fields.
func igmpSnoopingKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.IgmpSnooping] {
	return resourcekit.Backend[settings.IgmpSnooping]{
		Read: func(ctx context.Context, site, _ string) (*settings.IgmpSnooping, error) {
			_, igmp, err := ui.GetSetting[*settings.IgmpSnooping](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return igmp, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.IgmpSnooping, fields ...string,
		) (*settings.IgmpSnooping, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// igmpSnoopingKitSection builds the igmp_snooping entry for settingResource's
// Sections, bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func igmpSnoopingKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := igmpSnoopingKitSpec()
	spec.Backend = igmpSnoopingKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingIgmpSnoopingModel, settings.IgmpSnooping]{
		SectionName: "igmp_snooping",
		Get:         func(m *settingResourceModel) *types.Object { return &m.IgmpSnooping },
		Set:         func(m *settingResourceModel, o types.Object) { m.IgmpSnooping = o },
		AttrTypes:   igmpSnoopingAttrTypes,
		Spec:        spec,
	}
}
