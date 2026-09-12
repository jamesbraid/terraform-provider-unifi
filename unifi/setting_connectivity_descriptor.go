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

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

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
		Fields:   settingConnectivityGenFields(),
	}
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
