package unifi

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_radius_profile "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_radius_profile"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

type radiusProfileKitResource struct {
	resourcekit.Resource[radiusProfileKitModel, ui.RADIUSProfile]
}

var (
	_ resource.Resource                 = &radiusProfileKitResource{}
	_ resource.ResourceWithImportState  = &radiusProfileKitResource{}
	_ resource.ResourceWithIdentity     = &radiusProfileKitResource{}
	_ resource.ResourceWithUpgradeState = &radiusProfileKitResource{}
	_ list.ListResource                 = &radiusProfileKitResource{}
	_ list.ListResourceWithConfigure    = &radiusProfileKitResource{}
)

func newRadiusProfileKitResource() *radiusProfileKitResource {
	r := &radiusProfileKitResource{}
	r.Spec = radiusProfileKitSpec()
	r.SchemaSpec = radiusProfileKitSchema()
	r.ListSurface = radiusProfileKitList()
	return r
}

func NewRadiusProfileResource() resource.Resource { return newRadiusProfileKitResource() }

func NewRadiusProfileListResource() list.ListResource { return newRadiusProfileKitResource() }

func (r *radiusProfileKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_radius_profile.RadiusProfileResourceSchema(ctx)
	// v1: interim_update_interval changed from Int64 (seconds) to a GoDuration string.
	resp.Schema.Version = 1
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *radiusProfileKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_radius_profile"
}

// UpgradeState migrates v0 state (interim_update_interval stored as integer
// seconds) to v1 (a GoDuration string).
func (r *radiusProfileKitResource) UpgradeState(
	ctx context.Context,
) map[int64]resource.StateUpgrader {
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	schemaType := schemaResp.Schema.Type().TerraformType(ctx)

	return map[int64]resource.StateUpgrader{
		0: {
			StateUpgrader: func(
				ctx context.Context,
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
						util.SetDurationField(state, "interim_update_interval", time.Second)
					},
				)
				if err != nil {
					resp.Diagnostics.AddError("Failed to upgrade RADIUS profile state", err.Error())
					return
				}
				resp.DynamicValue = dv
			},
		},
	}
}

func (r *radiusProfileKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = radiusProfileKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
