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

// Schema STAYS IN THIS PACKAGE, HAND-WRITTEN, because internal/schemabehaviour
// derives what the provider applies by parsing unifi/*.go for a receiver whose
// Metadata names the surface and whose Schema it can follow, and promotion from
// an embedded type is invisible to a parser. Moving it into the kit would not
// fail; it would go quiet.
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

// Metadata is here for the same reason: the deriver reads the surface name off
// this method, in this package.
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

// THE VALIDATOR STAYS HERE, AND IT IS THE POINT OF KEEPING A SURFACE FILE AT
// ALL. Every line below is about static routes specifically: that network and
// next_hop have to name the same IP family, and that an IPv4-mapped IPv6
// address counts as IPv4. Nothing in it is shaped like any other surface's
// validation, so there is nothing here for the kit to hold.
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

	// Only validate when both are known and next_hop is set.
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

// ipVersionsMatch reports whether a CIDR prefix and an address use the same IP
// family. Unmap collapses IPv4-mapped IPv6 addresses (::ffff:a.b.c.d) to IPv4 so
// they compare as the v4 family.
func ipVersionsMatch(prefix netip.Prefix, hop netip.Addr) bool {
	return prefix.Addr().Unmap().Is4() == hop.Unmap().Is4()
}

// validateIPVersionMatch returns an error if network (CIDR) and nextHop (IP) use different IP versions.
func validateIPVersionMatch(network, nextHop string) error {
	// Invalid network/next_hop are already reported by their field validators;
	// an invalid (zero) value here just means "nothing to compare".
	prefix, _ := netip.ParsePrefix(network)
	hop, _ := netip.ParseAddr(nextHop)
	if !prefix.IsValid() || !hop.IsValid() {
		return nil
	}

	if !ipVersionsMatch(prefix, hop) {
		return fmt.Errorf(
			"network %q and next_hop %q must use the same IP version",
			network,
			nextHop,
		)
	}
	return nil
}

// Ensure staticRouteIPVersionValidator satisfies the resource.ConfigValidator interface.
var _ resource.ConfigValidator = &staticRouteIPVersionValidator{}
