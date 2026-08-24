package unifi

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_static_route"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type staticRouteKitResource struct {
	resourcekit.Resource[staticRouteKitModel, ui.Routing]
}

var (
	_ resource.Resource                     = &staticRouteKitResource{}
	_ resource.ResourceWithImportState      = &staticRouteKitResource{}
	_ resource.ResourceWithConfigValidators = &staticRouteKitResource{}
	_ resource.ResourceWithIdentity         = &staticRouteKitResource{}
	_ list.ListResource                     = &staticRouteKitResource{}
	_ list.ListResourceWithConfigure        = &staticRouteKitResource{}
)

func newStaticRouteKitResource() *staticRouteKitResource {
	r := &staticRouteKitResource{}
	r.Spec = staticRouteKitSpec()
	r.SchemaSpec = staticRouteKitSchema()
	r.ListSurface = staticRouteKitList()
	return r
}

func (r *staticRouteKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_static_route.StaticRouteResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *staticRouteKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_static_route"
}

func NewStaticRouteFrameworkResource() resource.Resource { return newStaticRouteKitResource() }

func NewStaticRouteListResource() list.ListResource { return newStaticRouteKitResource() }

func (r *staticRouteKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = staticRouteKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}

// This validator is the reason a surface file exists here at all: the
// network/next_hop IP-family rule is static-route-specific, nothing the kit could hold.
func (r *staticRouteKitResource) ConfigValidators(
	_ context.Context,
) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		&staticRouteIPVersionValidator{},
	}
}

// staticRouteIPVersionValidator ensures network and next_hop use the same IP version.
type staticRouteIPVersionValidator struct{}

func (v *staticRouteIPVersionValidator) Description(_ context.Context) string {
	return "network and next_hop must use the same IP version (both IPv4 or both IPv6)"
}

func (v *staticRouteIPVersionValidator) MarkdownDescription(_ context.Context) string {
	return "network and next_hop must use the same IP version (both IPv4 or both IPv6)"
}

func (v *staticRouteIPVersionValidator) ValidateResource(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	// next_hop uses the iptypes.IPAddress custom type, so it must be read into a
	// matching value — reading it into types.String fails config conversion.
	var network types.String
	var nextHop iptypes.IPAddress
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("network"), &network)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("next_hop"), &nextHop)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if network.IsNull() || network.IsUnknown() || nextHop.IsNull() || nextHop.IsUnknown() {
		return
	}

	// Convert next_hop via the custom type's built-in netip.Addr conversion
	// rather than re-parsing the raw string.
	hopAddr, diags := nextHop.ValueIPAddress()
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// network is a CIDR string (already shape-validated by CIDRValidator); parse
	// it to a netip.Prefix so both sides are compared as netip values.
	prefix, err := netip.ParsePrefix(network.ValueString())
	if err != nil {
		return // malformed CIDR is already reported by the network attribute validator
	}

	if !ipVersionsMatch(prefix, hopAddr) {
		resp.Diagnostics.AddAttributeError(
			path.Root("next_hop"),
			"IP Version Mismatch",
			fmt.Sprintf(
				"network %q and next_hop %q must use the same IP version (both IPv4 or both IPv6)",
				network.ValueString(),
				hopAddr.String(),
			),
		)
	}
}

// ipVersionsMatch reports whether a CIDR prefix and address share an IP family;
// Unmap collapses IPv4-mapped IPv6 (::ffff:a.b.c.d) to IPv4 so both compare as v4.
func ipVersionsMatch(prefix netip.Prefix, hop netip.Addr) bool {
	return prefix.Addr().Unmap().Is4() == hop.Unmap().Is4()
}

// Ensure staticRouteIPVersionValidator satisfies the resource.ConfigValidator interface.
var _ resource.ConfigValidator = &staticRouteIPVersionValidator{}
