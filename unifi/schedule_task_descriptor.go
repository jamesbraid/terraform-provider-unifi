package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_schedule_task"
	resource_schedule_task "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_schedule_task"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// scheduleTaskTargetModel describes one nested upgrade_targets entry.
type scheduleTaskTargetModel struct {
	MAC types.String `tfsdk:"mac"`
}

func (m scheduleTaskTargetModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"mac": types.StringType,
	}
}

// scheduleTaskKitSpec maps the ScheduleTask struct. action is the sole enum
// value the SDK records ("upgrade") and defaults to it, so a minimal config
// is a cron, a once/repeat flag and the devices to upgrade. The four attr_*
// controller internals stay unmapped, so the derived mask can never offer
// them back on a write.
func scheduleTaskKitSpec() resourcekit.Spec[scheduleTaskKitModel, ui.ScheduleTask] {
	return resourcekit.Spec[scheduleTaskKitModel, ui.ScheduleTask]{
		TypeName: "schedule_task",
		Subject:  "Schedule Task",
		IDWire:   "_id",
		New:      func() *ui.ScheduleTask { return &ui.ScheduleTask{} },
		ID:       func(m *scheduleTaskKitModel) *types.String { return &m.ID },
		Site:     func(m *scheduleTaskKitModel) *types.String { return &m.Site },
		Timeouts: func(m *scheduleTaskKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: resourcekit.Override(scheduleTaskGenFields(), []resourcekit.Field[scheduleTaskKitModel, ui.ScheduleTask]{
			resourcekit.ObjectListField[scheduleTaskKitModel, ui.ScheduleTask, ui.ScheduleTaskUpgradeTargets]{
				Wire:      "upgrade_targets",
				Model:     func(m *scheduleTaskKitModel) *types.List { return &m.UpgradeTargets },
				SDK:       func(s *ui.ScheduleTask) *[]ui.ScheduleTaskUpgradeTargets { return &s.UpgradeTargets },
				AttrTypes: scheduleTaskTargetModel{}.AttributeTypes(),
				Encode:    scheduleTaskTargetToAPI,
				Decode:    scheduleTaskTargetFromAPI,
				Elide:     resourcekit.KeepZero,
			},
		}),
		// Seeded here as well as in scheduleTaskKitBackend, because Configure
		// binds the real Backend and a unit test calling ToModel on an
		// unconfigured spec still needs the identity accessors.
		Backend: resourcekit.Backend[ui.ScheduleTask]{
			GetID: func(s *ui.ScheduleTask) string { return s.ID },
			SetID: func(s *ui.ScheduleTask, id string) { s.ID = id },
		},
	}
}

// scheduleTaskTargetToAPI encodes one upgrade_targets element.
func scheduleTaskTargetToAPI(ctx context.Context, object types.Object) (ui.ScheduleTaskUpgradeTargets, diag.Diagnostics) {
	var diags diag.Diagnostics
	var m scheduleTaskTargetModel
	diags.Append(object.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return ui.ScheduleTaskUpgradeTargets{}, diags
	}
	return ui.ScheduleTaskUpgradeTargets{MAC: m.MAC.ValueString()}, diags
}

// scheduleTaskTargetFromAPI decodes one upgrade_targets element.
func scheduleTaskTargetFromAPI(_ context.Context, e ui.ScheduleTaskUpgradeTargets) (types.Object, diag.Diagnostics) {
	return types.ObjectValue(scheduleTaskTargetModel{}.AttributeTypes(), map[string]attr.Value{
		"mac": types.StringValue(e.MAC),
	})
}

func scheduleTaskKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_schedule_task.ScheduleTaskResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func scheduleTaskKitList() resourcekit.ListSpec[ui.ScheduleTask] {
	return resourcekit.ListSpec[ui.ScheduleTask]{
		ConfigSchema: listresource_schedule_task.ScheduleTaskListResourceSchema,
		DisplayName: func(s *ui.ScheduleTask) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.ScheduleTask) string{
			"name": func(s *ui.ScheduleTask) string { return s.Name },
		},
	}
}

func scheduleTaskKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.ScheduleTask] {
	return resourcekit.Backend[ui.ScheduleTask]{
		Create: func(ctx context.Context, site string, in *ui.ScheduleTask) (*ui.ScheduleTask, error) {
			return client.CreateScheduleTask(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.ScheduleTask, error) {
			return client.GetScheduleTask(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.ScheduleTask, fields ...string,
		) (*ui.ScheduleTask, error) {
			return client.UpdateScheduleTaskFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteScheduleTask(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.ScheduleTask, error) {
			return client.ListScheduleTask(ctx, site)
		},
		GetID: func(s *ui.ScheduleTask) string { return s.ID },
		SetID: func(s *ui.ScheduleTask, id string) { s.ID = id },
	}
}
