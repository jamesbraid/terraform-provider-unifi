package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_radius_user "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_radius_user"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type radiusUserKitResource struct {
	resourcekit.Resource[radiusUserKitModel, ui.Account]
}

var (
	_ resource.Resource                = &radiusUserKitResource{}
	_ resource.ResourceWithImportState = &radiusUserKitResource{}
	_ resource.ResourceWithIdentity    = &radiusUserKitResource{}
	_ list.ListResource                = &radiusUserKitResource{}
	_ list.ListResourceWithConfigure   = &radiusUserKitResource{}
)

func newRadiusUserKitResource() *radiusUserKitResource {
	r := &radiusUserKitResource{}
	r.Spec = radiusUserKitSpec()
	r.SchemaSpec = radiusUserKitSchema()
	r.ListSurface = radiusUserKitList()
	return r
}

func NewRadiusUserResource() resource.Resource { return newRadiusUserKitResource() }

func NewRadiusUserListResource() list.ListResource { return newRadiusUserKitResource() }

func (r *radiusUserKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_radius_user.RadiusUserResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *radiusUserKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_radius_user"
}

func (r *radiusUserKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = radiusUserKitBackend(client.ApiClient)
	// Bound here rather than in the spec because it needs the client, the same
	// seam Backend uses.
	r.Spec.BeforeSend = radiusUserDeriveVLAN(client.ApiClient)
	r.DefaultSite = client.Site
}

// radiusUserDeriveVLAN fills in the account's VLAN before the write.
//
// It reads the effective model, not the config: an explicit vlan the plan
// didn't mention still lives in state, and reading config would re-derive it from network_id, silently overriding a pinned VLAN.
func radiusUserDeriveVLAN(
	client *ui.ApiClient,
) func(context.Context, *radiusUserKitModel, *radiusUserKitModel, radiusUserKitModel, *ui.Account, any) diag.Diagnostics {
	return func(
		ctx context.Context,
		_ *radiusUserKitModel,
		effective *radiusUserKitModel,
		_ radiusUserKitModel,
		sdk *ui.Account,
		_ any,
	) diag.Diagnostics {
		site := effective.Site.ValueString()
		vlan, diags := resolveRadiusUserVLAN(ctx, client, effective, site)
		if diags.HasError() {
			return diags
		}
		sdk.VLAN = vlan
		return diags
	}
}

// resolveRadiusUserVLAN determines the VLAN to assign to the account.
//
// The controller leaves `vlan` blank when only networkconf_id is sent, so an
// explicit `vlan` always wins over one inherited via `network_id` (#67); being computed and stable, it won't re-derive on a later network_id change alone.
func resolveRadiusUserVLAN(
	ctx context.Context,
	client *ui.ApiClient,
	model *radiusUserKitModel,
	site string,
) (*int64, diag.Diagnostics) {
	var diags diag.Diagnostics

	if !model.VLAN.IsNull() && !model.VLAN.IsUnknown() {
		return model.VLAN.ValueInt64Pointer(), diags
	}

	networkID := model.NetworkID.ValueString()
	if model.NetworkID.IsNull() || networkID == "" {
		return nil, diags
	}

	network, err := client.GetNetwork(ctx, site, networkID)
	if err != nil {
		diags.AddError(
			"Error Deriving VLAN from network_id",
			"Could not look up network "+networkID+
				" to derive the account VLAN: "+err.Error(),
		)
		return nil, diags
	}

	return network.VLAN, diags
}
