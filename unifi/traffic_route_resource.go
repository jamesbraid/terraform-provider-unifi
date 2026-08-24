package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_traffic_route "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_traffic_route"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type trafficRouteKitResource struct {
	resourcekit.Resource[trafficRouteKitModel, ui.TrafficRoute]
}

var (
	_ resource.Resource                = &trafficRouteKitResource{}
	_ resource.ResourceWithImportState = &trafficRouteKitResource{}
	_ resource.ResourceWithIdentity    = &trafficRouteKitResource{}
	_ list.ListResource                = &trafficRouteKitResource{}
	_ list.ListResourceWithConfigure   = &trafficRouteKitResource{}
)

func newTrafficRouteKitResource() *trafficRouteKitResource {
	r := &trafficRouteKitResource{}
	r.Spec = trafficRouteKitSpec()
	r.SchemaSpec = trafficRouteKitSchema()
	r.ListSurface = trafficRouteKitList()
	return r
}

func NewTrafficRouteResource() resource.Resource { return newTrafficRouteKitResource() }

func NewTrafficRouteListResource() list.ListResource { return newTrafficRouteKitResource() }

func (r *trafficRouteKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_traffic_route.TrafficRouteResourceSchema(ctx)
	// Grafted rather than generated, as everywhere else: timeouts.Attributes
	// is a call, not a literal, so the code specification cannot carry it.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *trafficRouteKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_traffic_route"
}

func (r *trafficRouteKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = trafficRouteKitBackend(client.ApiClient)
	// Prefetch is bound here, not in the spec, because it needs the client and
	// the spec is built before one exists (Backend, above, is bound the same way).
	r.Spec.Prefetch = trafficRoutePrefetchWANID(client.ApiClient)
	r.DefaultSite = client.Site
}
