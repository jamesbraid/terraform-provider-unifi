package unifi

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	listresource_device "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_device"
	resource_device "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_device"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

type deviceKitResource struct {
	resourcekit.Resource[deviceKitModel, ui.Device]
}

var (
	_ resource.Resource                 = &deviceKitResource{}
	_ resource.ResourceWithImportState  = &deviceKitResource{}
	_ resource.ResourceWithIdentity     = &deviceKitResource{}
	_ resource.ResourceWithUpgradeState = &deviceKitResource{}
	_ list.ListResource                 = &deviceKitResource{}
	_ list.ListResourceWithConfigure    = &deviceKitResource{}
)

func newDeviceKitResource() *deviceKitResource {
	r := &deviceKitResource{}
	r.Spec = deviceKitSpec()
	r.SchemaSpec = deviceKitSchema()
	r.ListSurface = deviceKitList()
	return r
}

func NewDeviceFrameworkResource() resource.Resource { return newDeviceKitResource() }

func NewDeviceListResource() list.ListResource { return newDeviceKitResource() }

func deviceKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_device.DeviceResourceSchema,
		Version:  3,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
		Upgraders: func(
			ctx context.Context, built schema.Schema,
		) map[int64]resource.StateUpgrader {
			schemaType := built.Type().TerraformType(ctx)
			return map[int64]resource.StateUpgrader{
				0: {StateUpgrader: func(
					_ context.Context,
					req resource.UpgradeStateRequest,
					resp *resource.UpgradeStateResponse,
				) {
					if req.RawState == nil {
						return
					}
					dv, err := util.UpgradeDurationRawState(
						schemaType,
						req.RawState.JSON,
						func(state map[string]any) {
							util.SetDurationField(state, "lcm_idle_timeout", time.Second)
							if pos, ok := state["port_override"].([]any); ok {
								for _, p := range pos {
									if pm, ok := p.(map[string]any); ok {
										util.SetDurationField(
											pm, "dot1x_idle_timeout", time.Second,
										)
									}
								}
							}
							dropAssistedRoaming(state)
						},
					)
					if err != nil {
						resp.Diagnostics.AddError(
							"Failed to upgrade device state", err.Error())
						return
					}
					resp.DynamicValue = dv
				}},
				1: {StateUpgrader: func(
					_ context.Context,
					req resource.UpgradeStateRequest,
					resp *resource.UpgradeStateResponse,
				) {
					if req.RawState == nil {
						return
					}
					dv, err := util.UpgradeDurationRawState(
						schemaType, req.RawState.JSON, dropAssistedRoaming,
					)
					if err != nil {
						resp.Diagnostics.AddError(
							"Failed to upgrade device state", err.Error())
						return
					}
					resp.DynamicValue = dv
				}},
				// 2->3: port_override.{excluded,multicast_router,tagged}_networkconf_ids
				// changed from List to Set. A JSON array decodes into either
				// collection, so no field rewrite is needed -- reconcileType
				// (inside UpgradeDurationRawState) reconciles the raw state against
				// schemaType, which is already the current (Set) shape.
				2: {StateUpgrader: func(
					_ context.Context,
					req resource.UpgradeStateRequest,
					resp *resource.UpgradeStateResponse,
				) {
					if req.RawState == nil {
						return
					}
					dv, err := util.UpgradeDurationRawState(
						schemaType, req.RawState.JSON, func(map[string]any) {},
					)
					if err != nil {
						resp.Diagnostics.AddError(
							"Failed to upgrade device state", err.Error())
						return
					}
					resp.DynamicValue = dv
				}},
			}
		},
	}
}

func deviceKitList() resourcekit.ListSpec[ui.Device] {
	return resourcekit.ListSpec[ui.Device]{
		ConfigSchema: listresource_device.DeviceListResourceSchema,
		DisplayName: func(s *ui.Device) string {
			if s.Name != "" {
				return s.Name
			}
			if s.MAC != "" {
				return s.MAC
			}
			return s.ID
		},
		Filters: map[string]func(*ui.Device) string{
			"name":  func(s *ui.Device) string { return s.Name },
			"mac":   func(s *ui.Device) string { return s.MAC },
			"model": func(s *ui.Device) string { return s.Model },
			"type":  func(s *ui.Device) string { return s.Type },
		},
	}
}

func (r *deviceKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_device.DeviceResourceSchema(ctx)
	// v1: lcm_idle_timeout and port_override.dot1x_idle_timeout changed from
	// Int64 (seconds) to GoDuration strings.
	// v2: radio_table.assisted_roaming_enabled and .assisted_roaming_rssi were
	// removed.
	// v3: port_override.{excluded,multicast_router,tagged}_networkconf_ids
	// changed from List to Set -- they are unordered ID sets, and modelling
	// them as a List meant the controller returning the same IDs in a
	// different order produced a spurious diff.
	// See the upgraders in deviceKitSchema.
	//
	// The released schema describes this surface in plain text, but a
	// generated schema always carries a MarkdownDescription (practitioner-
	// visible), so it's stripped rather than licensed as a change -- same as
	// ap_group and port_profile.
	plainDescriptions(&resp.Schema)
	resp.Schema.Version = 3
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *deviceKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_device"
}

func (r *deviceKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = deviceKitBackend(client.ApiClient)
	// BeforeSend needs the client: adoption and the port-override merge both
	// talk to the controller.
	r.Spec.BeforeSend = deviceKitBeforeSend(client.ApiClient)
	r.DefaultSite = client.Site
}
