package unifi

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_power_supervisor"
	resource_power_supervisor "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_power_supervisor"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

type powerSupervisorKitModel struct {
	ID                  types.String         `tfsdk:"id"`
	Site                types.String         `tfsdk:"site"`
	DeviceMAC           hwtypes.MACAddress   `tfsdk:"device_mac"`
	Enabled             types.Bool           `tfsdk:"enabled"`
	HeartbeatInterval   timetypes.GoDuration `tfsdk:"heartbeat_interval"`
	SilenceThreshold    timetypes.GoDuration `tfsdk:"silence_threshold"`
	PowerOffDuration    timetypes.GoDuration `tfsdk:"power_off_duration"`
	ConsecutiveFailures types.Int64          `tfsdk:"consecutive_failures"`
	PowerSources        types.List           `tfsdk:"power_sources"`
	Timeouts            timeouts.Value       `tfsdk:"timeouts"`
}

// powerSourceAttrTypes is the object schema of a resolved upstream power source.
func powerSourceAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"client_psu_index":   types.Int64Type,
		"power_source_index": types.Int64Type,
		"power_source_mac":   types.StringType,
		"power_source_type":  types.StringType,
	}
}

func powerSupervisorKitSpec() resourcekit.Spec[powerSupervisorKitModel, ui.PowerSupervisor] {
	return resourcekit.Spec[powerSupervisorKitModel, ui.PowerSupervisor]{
		TypeName: "power_supervisor",
		Subject:  "Power Supervisor",
		IDWire:   "id",
		New:      func() *ui.PowerSupervisor { return &ui.PowerSupervisor{} },
		ID:       func(m *powerSupervisorKitModel) *types.String { return &m.ID },
		Site:     func(m *powerSupervisorKitModel) *types.String { return &m.Site },
		Timeouts: func(m *powerSupervisorKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[powerSupervisorKitModel, ui.PowerSupervisor]{
			resourcekit.StringLikeField[powerSupervisorKitModel, ui.PowerSupervisor, hwtypes.MACAddress]{
				Wire:  "client_mac",
				Model: func(m *powerSupervisorKitModel) *hwtypes.MACAddress { return &m.DeviceMAC },
				SDK:   func(s *ui.PowerSupervisor) *string { return &s.ClientMAC },
				New: func(v basetypes.StringValue) hwtypes.MACAddress {
					return hwtypes.MACAddress{StringValue: v}
				},
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[powerSupervisorKitModel, ui.PowerSupervisor]{
				Wire:  "enabled",
				Model: func(m *powerSupervisorKitModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *ui.PowerSupervisor) *bool { return &s.Enabled },
			},
			// The controller resolves the upstream PoE source itself; the
			// resource never writes it, so the write side stays with the hook
			// below (an empty slice, the shape the hand mapper always sent).
			resourcekit.ReadOnly[powerSupervisorKitModel, ui.PowerSupervisor](
				resourcekit.ObjectListField[powerSupervisorKitModel, ui.PowerSupervisor, ui.PowerSupervisorSource]{
					Wire:      "power_sources",
					Model:     func(m *powerSupervisorKitModel) *types.List { return &m.PowerSources },
					SDK:       func(s *ui.PowerSupervisor) *[]ui.PowerSupervisorSource { return &s.PowerSources },
					AttrTypes: powerSourceAttrTypes(),
					Decode: func(_ context.Context, src ui.PowerSupervisorSource) (types.Object, diag.Diagnostics) {
						return types.ObjectValue(powerSourceAttrTypes(), map[string]attr.Value{
							"client_psu_index":   types.Int64Value(int64(src.ClientPsuIndex)),
							"power_source_index": types.Int64Value(int64(src.PowerSourceIndex)),
							"power_source_mac":   types.StringValue(src.PowerSourceMAC),
							"power_source_type":  types.StringValue(src.PowerSourceType),
						})
					},
					Elide: resourcekit.KeepZero,
				}),
		},
		// The three durations and consecutive_failures are plain ints on the
		// SDK struct -- the recorded sdk-int blocker -- and the durations
		// also live inside the nested settings document, which the masked
		// write can only address whole ("settings.heartbeat_interval" is a
		// mapping name, not a maskable wire). So the hooks carry all four,
		// exactly as the hand mapper did, and "settings" joins the mask on
		// every write.
		BeforeSend:   powerSupervisorKitBeforeSend,
		AfterReceive: powerSupervisorKitAfterReceive,
		AlwaysWire:   []string{"settings"},
		MappedElsewhere: []string{
			"settings.heartbeat_interval",
			"settings.power_off_duration",
			"settings.silence_threshold",
			"consecutive_failures",
		},
		// Seeded here as well as in powerSupervisorKitBackend, because
		// Configure binds the real Backend and a unit test calling ToModel on
		// an unconfigured spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.PowerSupervisor]{
			GetID: func(s *ui.PowerSupervisor) string { return s.ID },
			SetID: func(s *ui.PowerSupervisor, id string) { s.ID = id },
		},
	}
}

// powerSupervisorKitBeforeSend renders the three duration attributes into the
// nested settings document and sends power_sources empty, both exactly as the
// hand mapper did: the controller resolves the upstream source itself.
func powerSupervisorKitBeforeSend(
	_ context.Context,
	_, effective *powerSupervisorKitModel,
	_ powerSupervisorKitModel,
	sdk *ui.PowerSupervisor,
	_ any,
) diag.Diagnostics {
	sdk.Settings = ui.PowerSupervisorSettings{
		HeartbeatInterval: int(util.DurationUnits(effective.HeartbeatInterval, time.Second)),
		SilenceThreshold:  int(util.DurationUnits(effective.SilenceThreshold, time.Second)),
		PowerOffDuration:  int(util.DurationUnits(effective.PowerOffDuration, time.Second)),
	}
	sdk.PowerSources = []ui.PowerSupervisorSource{}
	return nil
}

// powerSupervisorKitAfterReceive reads back the four attributes the field
// list cannot express (see the Spec comment).
func powerSupervisorKitAfterReceive(
	_ context.Context,
	sdk *ui.PowerSupervisor,
	model *powerSupervisorKitModel,
	_ powerSupervisorKitModel,
	_ any,
) diag.Diagnostics {
	model.HeartbeatInterval = util.DurationValue(int64(sdk.Settings.HeartbeatInterval), time.Second)
	model.SilenceThreshold = util.DurationValue(int64(sdk.Settings.SilenceThreshold), time.Second)
	model.PowerOffDuration = util.DurationValue(int64(sdk.Settings.PowerOffDuration), time.Second)
	model.ConsecutiveFailures = types.Int64Value(int64(sdk.ConsecutiveFailures))
	return nil
}

func powerSupervisorKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_power_supervisor.PowerSupervisorResourceSchema,
		// v1: the three settings durations changed from Int64 seconds to
		// GoDuration strings.
		Version:  1,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
		Upgraders: func(_ context.Context, built schema.Schema) map[int64]resource.StateUpgrader {
			return map[int64]resource.StateUpgrader{
				0: {StateUpgrader: func(
					ctx context.Context,
					req resource.UpgradeStateRequest,
					resp *resource.UpgradeStateResponse,
				) {
					if req.RawState == nil {
						return
					}
					dv, err := util.UpgradeDurationRawState(
						built.Type().TerraformType(ctx),
						req.RawState.JSON,
						func(state map[string]any) {
							util.SetDurationField(state, "heartbeat_interval", time.Second)
							util.SetDurationField(state, "silence_threshold", time.Second)
							util.SetDurationField(state, "power_off_duration", time.Second)
						},
					)
					if err != nil {
						resp.Diagnostics.AddError(
							"Failed to upgrade power supervisor state", resourcekit.DiagErrorText(err))
						return
					}
					resp.DynamicValue = dv
				}},
			}
		},
	}
}

func powerSupervisorKitList() resourcekit.ListSpec[ui.PowerSupervisor] {
	return resourcekit.ListSpec[ui.PowerSupervisor]{
		ConfigSchema: listresource_power_supervisor.PowerSupervisorListResourceSchema,
		DisplayName: func(s *ui.PowerSupervisor) string {
			if s.ClientMAC != "" {
				return s.ClientMAC
			}
			return s.ID
		},
		Filters: map[string]func(*ui.PowerSupervisor) string{
			"device_mac": func(s *ui.PowerSupervisor) string { return s.ClientMAC },
		},
	}
}

func powerSupervisorKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.PowerSupervisor] {
	return resourcekit.Backend[ui.PowerSupervisor]{
		Create: func(ctx context.Context, site string, in *ui.PowerSupervisor) (*ui.PowerSupervisor, error) {
			return client.CreatePowerSupervisor(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.PowerSupervisor, error) {
			return client.GetPowerSupervisor(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.PowerSupervisor, fields ...string,
		) (*ui.PowerSupervisor, error) {
			return client.UpdatePowerSupervisorFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeletePowerSupervisor(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.PowerSupervisor, error) {
			return client.ListPowerSupervisors(ctx, site)
		},
		GetID: func(s *ui.PowerSupervisor) string { return s.ID },
		SetID: func(s *ui.PowerSupervisor, id string) { s.ID = id },
	}
}
