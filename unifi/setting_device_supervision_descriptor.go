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

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingDeviceSupervisionModel is device_supervision's own section model,
// decoded out of settingResourceModel.DeviceSupervision.
type settingDeviceSupervisionModel struct {
	GlobalSupervisionEnabled types.Bool  `tfsdk:"global_supervision_enabled"`
	HeartbeatIntervalSeconds types.Int64 `tfsdk:"heartbeat_interval_seconds"`
	PowerOffDurationSeconds  types.Int64 `tfsdk:"power_off_duration_seconds"`
	SilenceThresholdSeconds  types.Int64 `tfsdk:"silence_threshold_seconds"`
}

// deviceSupervisionAttrTypes types device_supervision's own object in
// state; it must match the generated schema exactly.
var deviceSupervisionAttrTypes = map[string]attr.Type{
	"global_supervision_enabled": types.BoolType,
	"heartbeat_interval_seconds": types.Int64Type,
	"power_off_duration_seconds": types.Int64Type,
	"silence_threshold_seconds":  types.Int64Type,
}

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
		Fields: []resourcekit.Field[settingDeviceSupervisionModel, settings.DeviceSupervision]{
			resourcekit.BoolField[settingDeviceSupervisionModel, settings.DeviceSupervision]{
				Wire: "global_supervision_enabled",
				Model: func(m *settingDeviceSupervisionModel) *types.Bool {
					return &m.GlobalSupervisionEnabled
				},
				SDK: func(s *settings.DeviceSupervision) *bool { return &s.GlobalSupervisionEnabled },
			},
			resourcekit.Int64PtrField[settingDeviceSupervisionModel, settings.DeviceSupervision]{
				Wire: "heartbeat_interval_seconds",
				Model: func(m *settingDeviceSupervisionModel) *types.Int64 {
					return &m.HeartbeatIntervalSeconds
				},
				SDK:      func(s *settings.DeviceSupervision) **int64 { return &s.HeartbeatIntervalSeconds },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.Int64PtrField[settingDeviceSupervisionModel, settings.DeviceSupervision]{
				Wire: "power_off_duration_seconds",
				Model: func(m *settingDeviceSupervisionModel) *types.Int64 {
					return &m.PowerOffDurationSeconds
				},
				SDK:      func(s *settings.DeviceSupervision) **int64 { return &s.PowerOffDurationSeconds },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.Int64PtrField[settingDeviceSupervisionModel, settings.DeviceSupervision]{
				Wire: "silence_threshold_seconds",
				Model: func(m *settingDeviceSupervisionModel) *types.Int64 {
					return &m.SilenceThresholdSeconds
				},
				SDK:      func(s *settings.DeviceSupervision) **int64 { return &s.SilenceThresholdSeconds },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
		},
	}
}

// deviceSupervisionNestedSchema is the device_supervision
// SingleNestedAttribute's own Attributes, wrapped as a schema.Schema so
// resourcekit's conformance checks -- built for a whole resource's
// top-level schema -- can run against one section of unifi_setting
// instead.
func deviceSupervisionNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	deviceSupervision := built.Attributes["device_supervision"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // device_supervision is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: deviceSupervision.Attributes}
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
