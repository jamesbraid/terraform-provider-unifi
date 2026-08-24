package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_ap_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_ap_group"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type apGroupKitResource struct {
	resourcekit.Resource[apGroupKitModel, ui.APGroup]
}

var (
	_ resource.Resource                = &apGroupKitResource{}
	_ resource.ResourceWithImportState = &apGroupKitResource{}
	_ resource.ResourceWithIdentity    = &apGroupKitResource{}
	_ list.ListResource                = &apGroupKitResource{}
	_ list.ListResourceWithConfigure   = &apGroupKitResource{}
)

func newAPGroupKitResource() *apGroupKitResource {
	r := &apGroupKitResource{}
	r.Spec = apGroupKitSpec()
	r.SchemaSpec = apGroupKitSchema()
	r.ListSurface = apGroupKitList()
	return r
}

func NewAPGroupResource() resource.Resource { return newAPGroupKitResource() }

func NewAPGroupListResource() list.ListResource { return newAPGroupKitResource() }

func (r *apGroupKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_ap_group.ApGroupResourceSchema(ctx)
	// The released schema is plain text, not markdown, so strip the MarkdownDescription
	// a generated schema always carries (same call and reason as port_profile).
	plainDescriptions(&resp.Schema)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *apGroupKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_ap_group"
}

func (r *apGroupKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = apGroupKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
