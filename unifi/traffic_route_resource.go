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

// Schema STAYS IN THIS PACKAGE, HAND-WRITTEN, because internal/schemabehaviour
// derives what the provider applies by parsing unifi/*.go for a receiver whose
// Metadata names the surface and whose Schema it can follow, and promotion from
// an embedded type is invisible to a parser. Moving it into the kit would not
// fail; it would go quiet.
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

// Metadata is here for the same reason: the deriver reads the surface name off
// this method, in this package.
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
	// PREFETCH IS BOUND HERE, NOT IN THE SPEC, because it needs the client and
	// the spec is built before one exists. Backend is bound the same way for
	// the same reason.
	r.Spec.Prefetch = trafficRoutePrefetchWANID(client.ApiClient)
	r.DefaultSite = client.Site
}
