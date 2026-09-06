package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_dynamic_dns "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dynamic_dns"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type dynamicDNSKitResource struct {
	resourcekit.Resource[dynamicDNSKitModel, ui.DynamicDNS]
}

var (
	_ resource.Resource                = &dynamicDNSKitResource{}
	_ resource.ResourceWithImportState = &dynamicDNSKitResource{}
	_ resource.ResourceWithIdentity    = &dynamicDNSKitResource{}
	_ list.ListResource                = &dynamicDNSKitResource{}
	_ list.ListResourceWithConfigure   = &dynamicDNSKitResource{}
)

func newDynamicDNSKitResource() *dynamicDNSKitResource {
	r := &dynamicDNSKitResource{}
	r.Spec = dynamicDNSKitSpec()
	r.SchemaSpec = dynamicDNSKitSchema()
	r.ListSurface = dynamicDNSKitList()
	return r
}

func (r *dynamicDNSKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_dynamic_dns.DynamicDnsResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *dynamicDNSKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_dynamic_dns"
}

func NewDynamicDNSResource() resource.Resource { return newDynamicDNSKitResource() }

func NewDynamicDNSListResource() list.ListResource { return newDynamicDNSKitResource() }

func (r *dynamicDNSKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = dynamicDNSKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}

// IdentitySchema keeps the two-attribute identity this resource has shipped
// with since before the kit: id plus site. States created under it store
// both, and a stored identity is decoded against the schema served here, so
// narrowing to the kit's id-only default would orphan every one of them.
func (r *dynamicDNSKitResource) IdentitySchema(
	_ context.Context,
	_ resource.IdentitySchemaRequest,
	resp *resource.IdentitySchemaResponse,
) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{
				RequiredForImport: true,
			},
			"site": identityschema.StringAttribute{
				OptionalForImport: true,
			},
		},
	}
}

// Create, Read and Update delegate to the kit and then stamp the site into
// the response identity, which the kit (writing id alone) would leave null.
// The hand-written resource stored both, so the states already out there
// carry both.
func (r *dynamicDNSKitResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	r.Resource.Create(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	var site types.String
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("site"), &site)...)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("site"), site)...)
}

func (r *dynamicDNSKitResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	r.Resource.Read(ctx, req, resp)
	if resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
		return
	}
	var site types.String
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("site"), &site)...)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("site"), site)...)
}

func (r *dynamicDNSKitResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	r.Resource.Update(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		return
	}
	var site types.String
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("site"), &site)...)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("site"), site)...)
}

// ImportState accepts what the kit accepts ("site:id" or "id") and keeps the
// one thing the hand resource did beyond that: an import block whose
// identity names a site reads from that site rather than the provider
// default.
func (r *dynamicDNSKitResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	r.Resource.ImportState(ctx, req, resp)
	if resp.Diagnostics.HasError() || req.ID != "" || req.Identity == nil {
		return
	}
	var site types.String
	resp.Diagnostics.Append(req.Identity.GetAttribute(ctx, path.Root("site"), &site)...)
	if resp.Diagnostics.HasError() || site.ValueString() == "" {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), site)...)
	if resp.Identity != nil {
		resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("site"), site)...)
	}
}

// List wraps the kit's stream to stamp the site into each result's identity,
// matching what Create, Read and Update store there.
func (r *dynamicDNSKitResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	r.Resource.List(ctx, req, stream)
	site := r.DefaultSite
	var config resourcekit.ListConfig
	if diags := req.Config.Get(ctx, &config); !diags.HasError() && config.Site.ValueString() != "" {
		site = config.Site.ValueString()
	}
	inner := stream.Results
	if inner == nil {
		return
	}
	stream.Results = func(push func(list.ListResult) bool) {
		inner(func(result list.ListResult) bool {
			if result.Identity != nil {
				result.Diagnostics.Append(result.Identity.SetAttribute(
					ctx, path.Root("site"), types.StringValue(site))...)
			}
			return push(result)
		})
	}
}
