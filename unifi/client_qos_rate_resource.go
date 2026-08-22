package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_client_qos_rate "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_client_qos_rate"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type clientQosRateKitResource struct {
	resourcekit.Resource[clientQosRateKitModel, ui.ClientGroup]
}

var (
	_ resource.Resource                = &clientQosRateKitResource{}
	_ resource.ResourceWithImportState = &clientQosRateKitResource{}
	_ resource.ResourceWithIdentity    = &clientQosRateKitResource{}
	_ list.ListResource                = &clientQosRateKitResource{}
	_ list.ListResourceWithConfigure   = &clientQosRateKitResource{}
)

func newClientQosRateKitResource() *clientQosRateKitResource {
	r := &clientQosRateKitResource{}
	r.Spec = clientQosRateKitSpec()
	r.SchemaSpec = clientQosRateKitSchema()
	r.ListSurface = clientQosRateKitList()
	return r
}

// Schema STAYS IN THIS PACKAGE, HAND-WRITTEN, for the reason dns_record
// records: internal/schemabehaviour derives what the provider applies by
// parsing unifi/*.go for a receiver whose Metadata names the surface and whose
// Schema it can follow, and promotion from an embedded type is invisible to a
// parser. Moving this into the kit would not fail -- it would go quiet, which
// is worse.
//
// NO VERSION IS SET, and that is a claim rather than an omission: this surface
// has never migrated its state shape, so it stays at 0. dns_record sets 1
// beside a matching upgrader; a version here with no upgrader would be a
// promise nothing keeps.
func (r *clientQosRateKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_client_qos_rate.ClientQosRateResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here for the same reason: the deriver reads the surface name off
// this method, in this package.
func (r *clientQosRateKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_client_qos_rate"
}

// NewClientQosRateResource and NewClientQosRateListResource keep the names
// provider.go registers, so the cutover does not ripple into the registry.
func NewClientQosRateResource() resource.Resource { return newClientQosRateKitResource() }

func NewClientQosRateListResource() list.ListResource { return newClientQosRateKitResource() }

// Configure binds the descriptor to the provider's client, and serves the
// managed and list surfaces alike because they are the same object here.
func (r *clientQosRateKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = clientQosRateKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
