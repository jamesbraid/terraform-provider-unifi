package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_schedule_task "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_schedule_task"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type scheduleTaskKitResource struct {
	resourcekit.Resource[scheduleTaskKitModel, ui.ScheduleTask]
}

var (
	_ resource.Resource                = &scheduleTaskKitResource{}
	_ resource.ResourceWithImportState = &scheduleTaskKitResource{}
	_ resource.ResourceWithIdentity    = &scheduleTaskKitResource{}
	_ list.ListResource                = &scheduleTaskKitResource{}
	_ list.ListResourceWithConfigure   = &scheduleTaskKitResource{}
)

func newScheduleTaskKitResource() *scheduleTaskKitResource {
	r := &scheduleTaskKitResource{}
	r.Spec = scheduleTaskKitSpec()
	r.SchemaSpec = scheduleTaskKitSchema()
	r.ListSurface = scheduleTaskKitList()
	return r
}

func (r *scheduleTaskKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_schedule_task.ScheduleTaskResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *scheduleTaskKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_schedule_task"
}

func NewScheduleTaskResource() resource.Resource { return newScheduleTaskKitResource() }

func NewScheduleTaskListResource() list.ListResource { return newScheduleTaskKitResource() }

func (r *scheduleTaskKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = scheduleTaskKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
