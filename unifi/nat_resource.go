package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_nat "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_nat"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type natKitResource struct {
	resourcekit.Resource[natKitModel, ui.Nat]
}

var (
	_ resource.Resource                = &natKitResource{}
	_ resource.ResourceWithImportState = &natKitResource{}
	_ resource.ResourceWithIdentity    = &natKitResource{}
)

func newNatKitResource() *natKitResource {
	r := &natKitResource{}
	r.Spec = natKitSpec()
	r.SchemaSpec = natKitSchema()
	return r
}

// NewNatResource is the provider registration entry point for unifi_nat.
func NewNatResource() resource.Resource { return newNatKitResource() }

// natFilterModel is the shape of both source_filter and destination_filter,
// which the SDK carries as two identical-but-distinct structs. The
// controller-internal iid member is left unmodeled (omitted in the policy),
// so it round-trips as the SDK zero.
type natFilterModel struct {
	Address          types.String `tfsdk:"address"`
	FilterType       types.String `tfsdk:"filter_type"`
	FirewallGroupIDs types.List   `tfsdk:"firewall_group_ids"`
	InvertAddress    types.Bool   `tfsdk:"invert_address"`
	InvertPort       types.Bool   `tfsdk:"invert_port"`
	NetworkConfID    types.String `tfsdk:"network_conf_id"`
	Port             types.Int64  `tfsdk:"port"`
}

func (m natFilterModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"address":            types.StringType,
		"filter_type":        types.StringType,
		"firewall_group_ids": types.ListType{ElemType: types.StringType},
		"invert_address":     types.BoolType,
		"invert_port":        types.BoolType,
		"network_conf_id":    types.StringType,
		"port":               types.Int64Type,
	}
}

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *natKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_nat"
}

func (r *natKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_nat.NatResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx, timeouts.Opts{Create: true, Read: true, Update: true, Delete: true})
}

func (r *natKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = natKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}
