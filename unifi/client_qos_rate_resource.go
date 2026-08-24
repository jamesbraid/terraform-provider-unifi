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

// No version is set: this surface has never migrated its state shape, so it
// stays at 0 rather than claiming an upgrader (contrast dns_record's v1) that doesn't exist.
func (r *clientQosRateKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_client_qos_rate.ClientQosRateResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *clientQosRateKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_client_qos_rate"
}

func NewClientQosRateResource() resource.Resource { return newClientQosRateKitResource() }

func NewClientQosRateListResource() list.ListResource { return newClientQosRateKitResource() }

// Configure binds the descriptor to the provider's client.
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
