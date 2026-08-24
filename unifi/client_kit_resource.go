package unifi

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// IdentitySchema adds mac to the kit's default id-only schema: List sets
// identity by mac rather than id (a listed client's mac is the handle the
// practitioner already recognizes), and writing to an undeclared identity
// attribute is a hard "Resource Identity Write Error", not a diff.
//
// Neither attribute is RequiredForImport: both optional lets a practitioner
// supply either alone. Marking one required would make the other's import
// block invalid on its own -- an `identity = { mac = "..." }` block would
// fail core's own validation before the provider ever saw it.
func (r *clientKitResource) IdentitySchema(
	_ context.Context,
	_ resource.IdentitySchemaRequest,
	resp *resource.IdentitySchemaResponse,
) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{OptionalForImport: true},
			"mac": identityschema.StringAttribute{
				CustomType:        hwtypes.MACAddressType{},
				OptionalForImport: true,
			},
		},
	}
}

// clientMACPattern is the shape of a MAC address, not a controller id: exactly
// five colon-separated pairs. A "site:id" import handle has at most one colon,
// and a bare id has none, so this disambiguates without needing to know which
// kind of handle a practitioner wrote.
var clientMACPattern = regexp.MustCompile(`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`)

// clientImportHandle decides what ImportState routes on, including the two
// ways it refuses to.
//
// req.ID carries the handle for every import shape but one: the CLI's
// `terraform import <addr> <handle>` and an import block's `id = "<handle>"`
// both land in req.ID; only an import block's `identity = {...}` (Terraform
// 1.12+) leaves req.ID empty and puts the handle in req.Identity instead. That
// block may carry an id, or just a mac, so id is tried first and mac is the
// fallback.
func clientImportHandle(ctx context.Context, req resource.ImportStateRequest) (string, bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	handle := req.ID
	if handle == "" && req.Identity != nil {
		var identityID types.String
		diags.Append(req.Identity.GetAttribute(ctx, path.Root("id"), &identityID)...)
		if diags.HasError() {
			return "", false, diags
		}
		// ValueString() returns "" for both null and unknown, so comparing it
		// to "" already covers those states without a separate IsNull() check.
		if identityID.ValueString() != "" {
			handle = identityID.ValueString()
		} else {
			var identityMAC hwtypes.MACAddress
			diags.Append(req.Identity.GetAttribute(ctx, path.Root("mac"), &identityMAC)...)
			if diags.HasError() {
				return "", false, diags
			}
			handle = identityMAC.ValueString()
		}
	}

	if handle == "" {
		// An unguarded empty handle becomes id="" in state; GetClient(site, "")
		// then hits the LIST endpoint, silently importing the site's lone client.
		diags.AddError("Error Importing Client",
			"Either id or mac must be supplied to import a client.")
		return "", false, diags
	}

	if clientMACPattern.MatchString(handle) {
		return handle, true, diags
	}

	if _, rest, ok := strings.Cut(handle, ":"); ok && clientMACPattern.MatchString(rest) {
		// "site:mac" isn't supported: mac import always uses the provider's
		// own site, with no per-import override. Left to the kit's generic
		// routing, this would fall through to a generic "must be site:id or
		// id" error -- true, but silent about mac, which is what this
		// practitioner needs to hear instead.
		diags.AddError("Error Importing Client",
			`A site-prefixed mac ("site:mac") is not supported for import; `+
				`use a bare mac (its own site is always used), a bare id, `+
				`or "site:id".`)
		return "", false, diags
	}

	return handle, false, diags
}

// ImportState resolves a mac-shaped handle to an id before delegating, rather
// than teaching Read a second lookup: the kit's generic Read only ever looks
// a client up by id, so a mac has to become one before Read runs, and this is
// the one place that's true for every import shape.
func (r *clientKitResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	handle, isMAC, diags := clientImportHandle(ctx, req)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if isMAC {
		existing, err := r.api.GetClientByMAC(ctx, r.DefaultSite, handle)
		if err != nil {
			resp.Diagnostics.AddError("Error Importing Client",
				fmt.Sprintf("Could not find a client with MAC %q: %s", handle, err.Error()))
			return
		}
		handle = existing.ID
	}
	req.ID = handle
	r.Resource.ImportState(ctx, req, resp)
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
	r.Spec.BeforeSend = clientKitBeforeSend(client.ApiClient, client.Site, &client.clientGroupMu)
	r.api = client.ApiClient
	r.DefaultSite = client.Site
}
