package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_content_filtering "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_content_filtering"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type contentFilteringKitResource struct {
	resourcekit.Resource[contentFilteringKitModel, ui.ContentFiltering]
}

var (
	_ resource.Resource                = &contentFilteringKitResource{}
	_ resource.ResourceWithImportState = &contentFilteringKitResource{}
	_ resource.ResourceWithIdentity    = &contentFilteringKitResource{}
)

func newContentFilteringKitResource() *contentFilteringKitResource {
	r := &contentFilteringKitResource{}
	r.Spec = contentFilteringKitSpec()
	r.SchemaSpec = contentFilteringKitSchema()
	return r
}

// NewContentFilteringResource is the provider registration entry point for
// unifi_content_filtering.
func NewContentFilteringResource() resource.Resource { return newContentFilteringKitResource() }

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *contentFilteringKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_content_filtering"
}

func (r *contentFilteringKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_content_filtering.ContentFilteringResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

func (r *contentFilteringKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = contentFilteringKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
