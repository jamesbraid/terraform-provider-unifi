package unifi

// The device_supervision section descriptor: an unconditional-mirror
// hydration with no specials, shaped like setting_mgmt_descriptor.go. All
// four of settings.DeviceSupervision's own fields are modelled; none is
// omitted.
//
// Each of the three seconds fields carries a controller-published pattern
// in settings.FieldConstraints["SettingDeviceSupervision"]; checked one at
// a time against a literal "0", every one rejects it (their ranges start
// at 60, 60 and 300), so all three Int64PtrFields carry OmitZero -- the
// same per-pattern check setting_netflow_descriptor.go's top comment walks
// through, pinned here by this section's own OmitZeroProblems test. The
// compiler derives no validator from a bounded range, so each field's
// Between is hand-transcribed in provider-codegen/policy/setting.json from
// the same table's Min/Max, like netflow's port and sampling_rate.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// deviceSupervisionKitSpec maps every attribute of the generated
// device_supervision schema (resource_setting/setting_resource_gen.go's
// "device_supervision" SingleNestedAttribute) onto
// settings.DeviceSupervision. global_supervision_enabled is a plain bool,
// which carries no Elide at all; the three Int64PtrFields want KeepZero
// like every other pointer int in this resource, and OmitZero per this
// file's top comment.
func deviceSupervisionKitSpec() resourcekit.Spec[settingDeviceSupervisionModel, settings.DeviceSupervision] {
	return resourcekit.Spec[settingDeviceSupervisionModel, settings.DeviceSupervision]{
		TypeName: "setting_device_supervision",
		Subject:  "Device Supervision Setting",
		New:      func() *settings.DeviceSupervision { return &settings.DeviceSupervision{} },
		Fields:   settingDeviceSupervisionGenFields(),
	}
}

// deviceSupervisionKitBackend binds deviceSupervisionKitSpec to a client:
// Read is GetSetting[*DeviceSupervision], UpdateFields is the masked
// UpdateSettingFields -- naming only the fields the plan set.
func deviceSupervisionKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.DeviceSupervision] {
	return resourcekit.Backend[settings.DeviceSupervision]{
		Read: func(ctx context.Context, site, _ string) (*settings.DeviceSupervision, error) {
			_, deviceSupervision, err := ui.GetSetting[*settings.DeviceSupervision](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return deviceSupervision, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.DeviceSupervision, fields ...string,
		) (*settings.DeviceSupervision, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// deviceSupervisionKitSection builds the device_supervision entry for
// settingResource's Sections, bound to client via settingKitSections,
// which calls it with r.client.ApiClient.
func deviceSupervisionKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := deviceSupervisionKitSpec()
	spec.Backend = deviceSupervisionKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingDeviceSupervisionModel, settings.DeviceSupervision]{
		SectionName: "device_supervision",
		Get:         func(m *settingResourceModel) *types.Object { return &m.DeviceSupervision },
		Set:         func(m *settingResourceModel, o types.Object) { m.DeviceSupervision = o },
		AttrTypes:   deviceSupervisionAttrTypes,
		Spec:        spec,
	}
}
