package unifi

// The connectivity section descriptor: an unconditional-mirror hydration
// with no specials, shaped like setting_mgmt_descriptor.go.
//
// The pinned go-unifi SDK's settings.Connectivity defines seven fields of
// its own; five are modelled. x_mesh_essid and x_mesh_psk are deliberately
// NOT: both are x_-prefixed secret candidates, and every secret this
// resource carries (radius.secret, mgmt.ssh_password, snmp's pair,
// guest_access's eighteen) had its read-echo behaviour pinned by a live
// probe before its AfterReceive shape was chosen -- radius-shaped for a
// verbatim echo, mgmt-shaped for a hash. This dispatch's probe
// (2026-09-01) wrote only connectivity.enabled and measured neither
// field's echo, so both are omitted rather than guessed, the same call
// global_switch's acl_device_isolation and radio_ai's default/useXY made.

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

// settingConnectivityModel is connectivity's own section model, decoded out
// of settingResourceModel.Connectivity.
type settingConnectivityModel struct {
	EnableIsolatedWLAN types.Bool   `tfsdk:"enable_isolated_wlan"`
	Enabled            types.Bool   `tfsdk:"enabled"`
	MloMeshEnabled     types.Bool   `tfsdk:"mlo_mesh_enabled"`
	UplinkHost         types.String `tfsdk:"uplink_host"`
	UplinkType         types.String `tfsdk:"uplink_type"`
}

// connectivityAttrTypes types connectivity's own object in state; it must
// match the generated schema exactly.
var connectivityAttrTypes = map[string]attr.Type{
	"enable_isolated_wlan": types.BoolType,
	"enabled":              types.BoolType,
	"mlo_mesh_enabled":     types.BoolType,
	"uplink_host":          types.StringType,
	"uplink_type":          types.StringType,
}

// connectivityKitSpec maps every modelled attribute of the generated
// connectivity schema (resource_setting/setting_resource_gen.go's
// "connectivity" SingleNestedAttribute) onto settings.Connectivity. The
// three bools carry no Elide at all; uplink_host and uplink_type are
// Optional+Computed with no validator rejecting an empty value, so
// resourcekit.ElideProblems' schema-driven rule demands KeepZero for both.
func connectivityKitSpec() resourcekit.Spec[settingConnectivityModel, settings.Connectivity] {
	return resourcekit.Spec[settingConnectivityModel, settings.Connectivity]{
		TypeName: "setting_connectivity",
		Subject:  "Connectivity Setting",
		New:      func() *settings.Connectivity { return &settings.Connectivity{} },
		Fields: []resourcekit.Field[settingConnectivityModel, settings.Connectivity]{
			resourcekit.BoolField[settingConnectivityModel, settings.Connectivity]{
				Wire:  "enable_isolated_wlan",
				Model: func(m *settingConnectivityModel) *types.Bool { return &m.EnableIsolatedWLAN },
				SDK:   func(s *settings.Connectivity) *bool { return &s.EnableIsolatedWLAN },
			},
			resourcekit.BoolField[settingConnectivityModel, settings.Connectivity]{
				Wire:  "enabled",
				Model: func(m *settingConnectivityModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *settings.Connectivity) *bool { return &s.Enabled },
			},
			resourcekit.BoolField[settingConnectivityModel, settings.Connectivity]{
				Wire:  "mlo_mesh_enabled",
				Model: func(m *settingConnectivityModel) *types.Bool { return &m.MloMeshEnabled },
				SDK:   func(s *settings.Connectivity) *bool { return &s.MloMeshEnabled },
			},
			resourcekit.StringField[settingConnectivityModel, settings.Connectivity]{
				Wire:  "uplink_host",
				Model: func(m *settingConnectivityModel) *types.String { return &m.UplinkHost },
				SDK:   func(s *settings.Connectivity) *string { return &s.UplinkHost },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingConnectivityModel, settings.Connectivity]{
				Wire:  "uplink_type",
				Model: func(m *settingConnectivityModel) *types.String { return &m.UplinkType },
				SDK:   func(s *settings.Connectivity) *string { return &s.UplinkType },
				Elide: resourcekit.KeepZero,
			},
		},
	}
}

// connectivityNestedSchema is the connectivity SingleNestedAttribute's own
// Attributes, wrapped as a schema.Schema so resourcekit's conformance
// checks -- built for a whole resource's top-level schema -- can run
// against one section of unifi_setting instead.
func connectivityNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	connectivity := built.Attributes["connectivity"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // connectivity is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: connectivity.Attributes}
}

// connectivityKitBackend binds connectivityKitSpec to a client: Read is
// GetSetting[*Connectivity], UpdateFields is the masked
// UpdateSettingFields -- naming only the fields the plan set.
func connectivityKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Connectivity] {
	return resourcekit.Backend[settings.Connectivity]{
		Read: func(ctx context.Context, site, _ string) (*settings.Connectivity, error) {
			_, connectivity, err := ui.GetSetting[*settings.Connectivity](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return connectivity, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Connectivity, fields ...string,
		) (*settings.Connectivity, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// connectivityKitSection builds the connectivity entry for
// settingResource's Sections, bound to client via settingKitSections,
// which calls it with r.client.ApiClient.
func connectivityKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := connectivityKitSpec()
	spec.Backend = connectivityKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingConnectivityModel, settings.Connectivity]{
		SectionName: "connectivity",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Connectivity },
		Set:         func(m *settingResourceModel, o types.Object) { m.Connectivity = o },
		AttrTypes:   connectivityAttrTypes,
		Spec:        spec,
	}
}
