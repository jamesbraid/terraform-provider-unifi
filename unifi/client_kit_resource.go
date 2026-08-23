package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	listresource_client "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_client"
	resource_client "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_client"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type clientKitResource struct {
	resourcekit.Resource[clientModel, ui.Client]

	// api is here for List alone. See the List method: this surface supplies
	// its own, and it needs the two filtered SDK calls the kit's Backend.List
	// does not carry.
	api *ui.ApiClient
}

var (
	_ resource.Resource                = &clientKitResource{}
	_ resource.ResourceWithImportState = &clientKitResource{}
	_ resource.ResourceWithIdentity    = &clientKitResource{}
	_ list.ListResource                = &clientKitResource{}
	_ list.ListResourceWithConfigure   = &clientKitResource{}
)

func newClientKitResource() *clientKitResource {
	r := &clientKitResource{}
	r.Spec = clientKitSpec()
	r.SchemaSpec = clientKitSchema()
	// ONLY ConfigSchema. See the List method below: this surface supplies its
	// own, so the kit's Filters and DisplayName would be configured and never
	// read. ConfigSchema is the one member the kit still consults, from
	// ListResourceConfigSchema.
	r.ListSurface = resourcekit.ListSpec[ui.Client]{
		ConfigSchema: listresource_client.ClientListResourceSchema,
	}
	return r
}

func NewClientResource() resource.Resource { return newClientKitResource() }

func NewClientListResource() list.ListResource { return newClientKitResource() }

func clientKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_client.ClientResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

// Schema STAYS IN THIS PACKAGE, HAND-WRITTEN, because internal/schemabehaviour
// parses unifi/*.go for a receiver whose Metadata names the surface and whose
// Schema it can follow, and promotion from an embedded type is invisible to a
// parser.
func (r *clientKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_client.ClientResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// IdentitySchema ADDS mac TO THE KIT'S DEFAULT "id"-ONLY SCHEMA, because List
// (see List in client_resource.go) sets identity by mac rather than id: a
// listed client's mac is the handle the practitioner already recognizes, an
// id is an opaque one they have not necessarily seen. Writing to an identity
// attribute the schema does not declare is a hard "Resource Identity Write
// Error", not a diff, which is what an unmodified kit schema gave List here.
//
// mac IS OPTIONAL FOR IMPORT, NOT REQUIRED: id alone already resolves every
// import (Create, Read and Update all set it, never mac), and marking mac
// required would demand a value those paths do not write.
func (r *clientKitResource) IdentitySchema(
	_ context.Context,
	_ resource.IdentitySchemaRequest,
	resp *resource.IdentitySchemaResponse,
) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{RequiredForImport: true},
			"mac": identityschema.StringAttribute{
				CustomType:        hwtypes.MACAddressType{},
				OptionalForImport: true,
			},
		},
	}
}

func (r *clientKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_client"
}

func (r *clientKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = clientKitBackend(client.ApiClient)
	r.Spec.Prefetch = clientKitPrefetch(client.ApiClient)
	r.Spec.BeforeSend = clientKitBeforeSend(client.ApiClient)
	r.api = client.ApiClient
	r.DefaultSite = client.Site
}
