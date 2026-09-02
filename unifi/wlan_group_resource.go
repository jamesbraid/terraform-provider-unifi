package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_wlan_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wlan_group"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type wlanGroupKitResource struct {
	resourcekit.Resource[wlanGroupKitModel, ui.WLANGroup]
}

var (
	_ resource.Resource                = &wlanGroupKitResource{}
	_ resource.ResourceWithImportState = &wlanGroupKitResource{}
	_ resource.ResourceWithIdentity    = &wlanGroupKitResource{}
	_ list.ListResource                = &wlanGroupKitResource{}
	_ list.ListResourceWithConfigure   = &wlanGroupKitResource{}
)

func newWLANGroupKitResource() *wlanGroupKitResource {
	r := &wlanGroupKitResource{}
	r.Spec = wlanGroupKitSpec()
	r.SchemaSpec = wlanGroupKitSchema()
	r.ListSurface = wlanGroupKitList()
	return r
}

func (r *wlanGroupKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_wlan_group.WlanGroupResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *wlanGroupKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_wlan_group"
}

func NewWLANGroupResource() resource.Resource { return newWLANGroupKitResource() }

func NewWLANGroupListResource() list.ListResource { return newWLANGroupKitResource() }

func (r *wlanGroupKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = wlanGroupKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
