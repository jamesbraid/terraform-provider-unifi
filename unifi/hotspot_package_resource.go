package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_hotspot_package "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_hotspot_package"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type hotspotPackageKitResource struct {
	resourcekit.Resource[hotspotPackageKitModel, ui.HotspotPackage]
}

var (
	_ resource.Resource                = &hotspotPackageKitResource{}
	_ resource.ResourceWithImportState = &hotspotPackageKitResource{}
	_ resource.ResourceWithIdentity    = &hotspotPackageKitResource{}
)

func newHotspotPackageKitResource() *hotspotPackageKitResource {
	r := &hotspotPackageKitResource{}
	r.Spec = hotspotPackageKitSpec()
	r.SchemaSpec = hotspotPackageKitSchema()
	return r
}

// NewHotspotPackageResource is the provider registration entry point for
// unifi_hotspot_package.
func NewHotspotPackageResource() resource.Resource { return newHotspotPackageKitResource() }

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *hotspotPackageKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_hotspot_package"
}

func (r *hotspotPackageKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_hotspot_package.HotspotPackageResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

func (r *hotspotPackageKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = hotspotPackageKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
