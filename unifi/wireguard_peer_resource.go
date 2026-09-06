package unifi

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_wireguard_peer "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wireguard_peer"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// wireguardPeerKitResource is the one parent-scoped surface: every SDK call
// for a peer names the WireGuard server network it lives under. Create and
// update carry that key on the object, but the kit's Read, Delete and List
// closures only receive (site, id), so this wrapper overrides those three --
// Read and Delete peek network_id out of state and rebind the backend before
// delegating, and List decodes its own scoped config (as client's does).
type wireguardPeerKitResource struct {
	resourcekit.Resource[wireguardPeerKitModel, ui.WireGuardPeer]

	// scopedBackend builds the backend for one parent network. A factory
	// rather than a client field, so the overrides' rebinding stays
	// injectable: a test can hand in a factory returning a fake and still
	// see exactly which network the resource asked for.
	scopedBackend func(networkID string) resourcekit.Backend[ui.WireGuardPeer]
}

var (
	_ resource.Resource                = &wireguardPeerKitResource{}
	_ resource.ResourceWithImportState = &wireguardPeerKitResource{}
	_ resource.ResourceWithIdentity    = &wireguardPeerKitResource{}
	_ list.ListResource                = &wireguardPeerKitResource{}
	_ list.ListResourceWithConfigure   = &wireguardPeerKitResource{}
)

func newWireguardPeerKitResource() *wireguardPeerKitResource {
	r := &wireguardPeerKitResource{}
	r.Spec = wireguardPeerKitSpec()
	r.SchemaSpec = wireguardPeerKitSchema()
	r.ListSurface = wireguardPeerKitList()
	return r
}

func NewWireguardPeerResource() resource.Resource { return newWireguardPeerKitResource() }

func NewWireguardPeerListResource() list.ListResource { return newWireguardPeerKitResource() }

func (r *wireguardPeerKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_wireguard_peer.WireguardPeerResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *wireguardPeerKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_wireguard_peer"
}

func (r *wireguardPeerKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.scopedBackend = func(networkID string) resourcekit.Backend[ui.WireGuardPeer] {
		return wireguardPeerKitBackend(client.ApiClient, networkID)
	}
	// Bound without a parent network: Create and UpdateFields read it off
	// the object, and the Read/Delete overrides rebind with the real one.
	r.Spec.Backend = r.scopedBackend("")
	r.DefaultSite = client.Site
}

// Read rebinds the backend to the network the peer belongs to, then runs the
// kit's Read. network_id is Required and RequiresReplace, so state always
// holds it -- an import supplies it through the import handle.
func (r *wireguardPeerKitResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var networkID types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("network_id"), &networkID)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.Spec.Backend = r.scopedBackend(networkID.ValueString())
	r.Resource.Read(ctx, req, resp)
}

// Delete rebinds the same way Read does.
func (r *wireguardPeerKitResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var networkID types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("network_id"), &networkID)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.Spec.Backend = r.scopedBackend(networkID.ValueString())
	r.Resource.Delete(ctx, req, resp)
}

// ImportState accepts "site:network_id:id" or "network_id:id" for the
// default site: the parent network is part of a peer's address, so the
// kit's uniform "site:id" form cannot name one.
func (r *wireguardPeerKitResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	idParts := strings.Split(req.ID, ":")

	var handle string
	switch len(idParts) {
	case 3:
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), idParts[0])...)
		resp.Diagnostics.Append(
			resp.State.SetAttribute(ctx, path.Root("network_id"), idParts[1])...)
		handle = idParts[2]
	case 2:
		resp.Diagnostics.Append(
			resp.State.SetAttribute(ctx, path.Root("network_id"), idParts[0])...)
		handle = idParts[1]
	default:
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"Import ID must be in format 'site:network_id:id' or 'network_id:id'",
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), handle)...)
	// Identity is set here too, not only in Read: the framework pre-populates
	// the post-import read's identity from this response, and leaving it null
	// turns a clean not-found into "Missing Resource Identity After Read".
	if resp.Identity != nil {
		resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), handle)...)
	}
}

// wireguardPeerListConfigModel is resourcekit.ListConfig plus the parent
// network: peers can only be listed within a WireGuard server network, so
// `network_id` is required.
type wireguardPeerListConfigModel struct {
	Site      types.String `tfsdk:"site"`
	NetworkID types.String `tfsdk:"network_id"`
	Filter    types.List   `tfsdk:"filter"`
}

// List implements [list.ListResource]. The kit's List decodes the uniform
// two-attribute config, which this surface's network_id does not fit, so the
// walk lives here -- same shape, scoped fetch.
func (r *wireguardPeerKitResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config wireguardPeerListConfigModel
	if diags := req.Config.Get(ctx, &config); diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	site := config.Site.ValueString()
	if site == "" {
		site = r.DefaultSite
	}

	wanted := map[string]string{}
	if !config.Filter.IsNull() && !config.Filter.IsUnknown() {
		var filters []resourcekit.ListFilter
		if diags := config.Filter.ElementsAs(ctx, &filters, false); diags.HasError() {
			stream.Results = list.ListResultsStreamDiagnostics(diags)
			return
		}
		for _, f := range filters {
			wanted[f.Name.ValueString()] = f.Value.ValueString()
		}
	}

	// The same refusal the kit's List makes: a filter naming no field would
	// otherwise match everything, which reads as "nothing matched" rather
	// than as the practitioner's mistake.
	var unknown diag.Diagnostics
	for name := range wanted {
		if name != "name" {
			unknown.AddError("Unknown filter",
				"This resource has no filterable field named "+name+
					". A filter that names nothing would match everything.")
		}
	}
	if unknown.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(unknown)
		return
	}

	peers, err := r.scopedBackend(config.NetworkID.ValueString()).List(ctx, site)
	if err != nil {
		var d diag.Diagnostics
		d.AddError("Error Listing WireGuard Peers", resourcekit.DiagErrorText(err))
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for i := range peers {
			peer := &peers[i]
			if value, ok := wanted["name"]; ok && peer.Name != value {
				continue
			}

			result := req.NewListResult(ctx)
			if peer.Name != "" {
				result.DisplayName = peer.Name
			} else {
				result.DisplayName = peer.ID
			}
			result.Diagnostics.Append(result.Identity.SetAttribute(
				ctx, path.Root("id"), types.StringValue(peer.ID))...)

			var model wireguardPeerKitModel
			result.Diagnostics.Append(r.Spec.ToModel(ctx, peer, &model, site)...)
			model.Timeouts = timeoutsNullValue()
			result.Diagnostics.Append(result.Resource.Set(ctx, model)...)

			if !push(result) {
				return
			}
		}
	}
}
