package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_dns_record "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dns_record"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type dnsRecordKitResource struct {
	resourcekit.Resource[dnsRecordKitModel, ui.DNSRecord]
}

var (
	_ resource.Resource                 = &dnsRecordKitResource{}
	_ resource.ResourceWithImportState  = &dnsRecordKitResource{}
	_ resource.ResourceWithIdentity     = &dnsRecordKitResource{}
	_ resource.ResourceWithUpgradeState = &dnsRecordKitResource{}
	_ list.ListResource                 = &dnsRecordKitResource{}
	_ list.ListResourceWithConfigure    = &dnsRecordKitResource{}
)

func newDNSRecordKitResource() *dnsRecordKitResource {
	r := &dnsRecordKitResource{}
	r.Spec = dnsRecordKitSpec()
	r.SchemaSpec = dnsRecordKitSchema()
	r.ListSurface = dnsRecordKitList()
	return r
}

func (r *dnsRecordKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_dns_record.DnsRecordResourceSchema(ctx)
	// v1: ttl moved from Int64 seconds to a GoDuration string.
	resp.Schema.Version = 1
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *dnsRecordKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_dns_record"
}

func NewDNSRecordFrameworkResource() resource.Resource { return newDNSRecordKitResource() }

func NewDNSRecordListResource() list.ListResource { return newDNSRecordKitResource() }

// Configure binds the descriptor to the provider's client.
func (r *dnsRecordKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = dnsRecordKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
