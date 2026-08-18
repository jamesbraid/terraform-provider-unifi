package unifi

// The dns_record resource, assembled from its descriptor.
//
// THE FILE KEEPS ITS NAME AND THAT IS LOAD-BEARING. internal/catalogparity's
// evidence inventory reads unifi/<surface>_resource.go BY PATH and records its
// digest as that surface's runtime evidence. Naming this file for its new
// implementation broke cmd/catalog-evidence, two catalogparity tests and the
// committed inventory -- a coupling between this lane and the evidence layer
// that nothing declares and that only deleting the file reveals.
//
// The file still IS the dns_record resource. Only the implementation moved,
// which is what a digest is for.
//
// EVERYTHING BELOW IS WIRING, and it is the whole of what a resource needs
// beyond its descriptor: a constructor per registered surface, and a Configure
// that turns the provider's client into the descriptor's backend. Configure
// cannot live in the kit because it names *Client, and the kit is imported BY
// this package rather than the other way round.

import (
	"context"
	"fmt"

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

// Schema STAYS IN THIS PACKAGE, HAND-WRITTEN, AND A LIVE CHECK IS WHY.
//
// internal/schemabehaviour derives what the provider applies -- defaults, plan
// modifiers, validators -- by parsing unifi/*.go for a receiver with a Metadata
// method naming the surface and a Schema method it can follow into the
// generated package. It handles the delegation; what it cannot do is find a
// Schema method that is not declared here, because promotion from an embedded
// type is invisible to a parser.
//
// Moving this into the kit blinded it: 15 behaviours on this surface alone went
// from derived to unread, and the deriver is the only thing that compares a
// SERVED schema against the runtime model that carries its values -- the class
// that produced 54 controller regressions.
//
// So twelve lines stay per resource against 838 that go. That is the trade, and
// it is worth naming rather than discovering again at resource nineteen.
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

// Metadata is here for the same reason: the deriver reads the surface name off
// this method, in this package.
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
//
// ONE METHOD SERVES BOTH SURFACES. list.ListResourceWithConfigure requires a
// Configure with the same signature as resource.ResourceWithConfigure, so the
// framework calls this for the managed resource and for the list resource
// alike -- which matters because they are the same object here.
//
// THE BACKEND CLOSURES ARE BUILT HERE, NOT IN THE DESCRIPTOR, because they need
// a client and the descriptor is a pure value that a test can build without
// one. That split is what lets the differentials and the contract check run
// with no provider configured at all.
func (r *dnsRecordKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *Client, got: %T. Please report this issue to the provider developers.",
				req.ProviderData),
		)
		return
	}
	r.Spec.Backend = dnsRecordKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
