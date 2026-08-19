package unifi

import (
	"context"
	"fmt"
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

// Schema STAYS IN THIS PACKAGE, HAND-WRITTEN, because internal/schemabehaviour
// derives what the provider applies by parsing unifi/*.go for a receiver whose
// Metadata names the surface and whose Schema it can follow, and promotion from
// an embedded type is invisible to a parser.
func (r *radiusProfileKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_radius_profile.RadiusProfileResourceSchema(ctx)
	// v1: interim_update_interval changed from Int64 (seconds) to a GoDuration
	// string, which UpgradeState migrates. The specification cannot carry a
	// schema version, so it is re-set here as port_profile does.
	resp.Schema.Version = 1
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// Metadata is here for the same reason: the deriver reads the surface name off
// this method, in this package.
func (r *radiusProfileKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_radius_profile"
}

// UpgradeState migrates v0 state (interim_update_interval stored as integer
// seconds) to v1 (a GoDuration string). Carried across the migration unchanged:
// state written by an older provider does not stop existing because the
// resource moved to the kit.
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
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf(
				"Expected *Client, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}
	r.Spec.Backend = radiusProfileKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
