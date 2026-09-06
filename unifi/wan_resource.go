package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// The nested models below stay in this package: they are the schema's shape,
// not the kit's -- the descriptor's Encode/Decode and tests read them directly.

// vlanModel describes the VLAN configuration.
type vlanModel struct {
	Enabled types.Bool  `tfsdk:"enabled"`
	ID      types.Int64 `tfsdk:"id"`
}

func (m vlanModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled": types.BoolType,
		"id":      types.Int64Type,
	}
}

// egressQosModel describes the Egress QoS configuration.
type egressQosModel struct {
	Enabled  types.Bool  `tfsdk:"enabled"`
	Priority types.Int64 `tfsdk:"priority"`
}

func (m egressQosModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled":  types.BoolType,
		"priority": types.Int64Type,
	}
}

// smartqModel describes the Smart Queue configuration.
type smartqModel struct {
	Enabled  types.Bool  `tfsdk:"enabled"`
	UpRate   types.Int64 `tfsdk:"up_rate"`
	DownRate types.Int64 `tfsdk:"down_rate"`
}

func (m smartqModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled":   types.BoolType,
		"up_rate":   types.Int64Type,
		"down_rate": types.Int64Type,
	}
}

// providerCapabilitiesModel describes the provider capabilities nested object.
type providerCapabilitiesModel struct {
	DownloadKbps types.Int64 `tfsdk:"download_kilobits_per_second"`
	UploadKbps   types.Int64 `tfsdk:"upload_kilobits_per_second"`
}

func (m providerCapabilitiesModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"download_kilobits_per_second": types.Int64Type,
		"upload_kilobits_per_second":   types.Int64Type,
	}
}

// dhcpOptionModel describes one DHCP or DHCPv6 option.
type dhcpOptionModel struct {
	OptionNumber types.Int64  `tfsdk:"option_number"`
	Value        types.String `tfsdk:"value"`
}

func (m dhcpOptionModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"option_number": types.Int64Type,
		"value":         types.StringType,
	}
}

// dnsModel describes the DNS configuration nested object.
type dnsModel struct {
	Primary        types.String `tfsdk:"primary"`
	Secondary      types.String `tfsdk:"secondary"`
	IPv6Primary    types.String `tfsdk:"ipv6_primary"`
	IPv6Secondary  types.String `tfsdk:"ipv6_secondary"`
	Preference     types.String `tfsdk:"preference"`
	IPv6Preference types.String `tfsdk:"ipv6_preference"`
}

func (m dnsModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"primary":         types.StringType,
		"secondary":       types.StringType,
		"ipv6_primary":    types.StringType,
		"ipv6_secondary":  types.StringType,
		"preference":      types.StringType,
		"ipv6_preference": types.StringType,
	}
}

// upnpModel describes the UPnP configuration nested object.
type upnpModel struct {
	Enabled       types.Bool   `tfsdk:"enabled"`
	WANInterface  types.String `tfsdk:"wan_interface"`
	NatPMPEnabled types.Bool   `tfsdk:"nat_pmp_enabled"`
	SecureMode    types.Bool   `tfsdk:"secure_mode"`
}

func (m upnpModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled":         types.BoolType,
		"wan_interface":   types.StringType,
		"nat_pmp_enabled": types.BoolType,
		"secure_mode":     types.BoolType,
	}
}

// loadBalanceModel describes the load balance configuration nested object.
type loadBalanceModel struct {
	Type             types.String `tfsdk:"type"`
	Weight           types.Int64  `tfsdk:"weight"`
	FailoverPriority types.Int64  `tfsdk:"failover_priority"`
}

func (m loadBalanceModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"type":              types.StringType,
		"weight":            types.Int64Type,
		"failover_priority": types.Int64Type,
	}
}

// igmpProxyModel describes the IGMP proxy configuration nested object.
type igmpProxyModel struct {
	Downstream types.String `tfsdk:"downstream"`
	Upstream   types.Bool   `tfsdk:"upstream"`
}

func (m igmpProxyModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"downstream": types.StringType,
		"upstream":   types.BoolType,
	}
}

// dhcpv6WanModel describes the DHCPv6 WAN configuration nested object.
type dhcpv6WanModel struct {
	CoS            types.Int64  `tfsdk:"cos"`
	PDSize         types.Int64  `tfsdk:"pd_size"`
	PDSizeAuto     types.Bool   `tfsdk:"pd_size_auto"`
	Options        types.List   `tfsdk:"options"`
	DelegationType types.String `tfsdk:"wan_delegation_type"`
}

func (m dhcpv6WanModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"cos":          types.Int64Type,
		"pd_size":      types.Int64Type,
		"pd_size_auto": types.BoolType,
		"options": types.ListType{
			ElemType: types.ObjectType{AttrTypes: dhcpOptionModel{}.AttributeTypes()},
		},
		"wan_delegation_type": types.StringType,
	}
}

// dhcpWanModel describes the DHCP WAN configuration nested object.
type dhcpWanModel struct {
	CoS     types.Int64 `tfsdk:"cos"`
	Options types.List  `tfsdk:"options"`
}

func (m dhcpWanModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"cos": types.Int64Type,
		"options": types.ListType{
			ElemType: types.ObjectType{AttrTypes: dhcpOptionModel{}.AttributeTypes()},
		},
	}
}

type wanKitResource struct {
	resourcekit.Resource[wanKitModel, ui.Network]
}

var (
	_ resource.Resource                = &wanKitResource{}
	_ resource.ResourceWithImportState = &wanKitResource{}
	_ resource.ResourceWithIdentity    = &wanKitResource{}
	// The assertion is the guard, not decoration: the framework calls
	// ValidateConfig only if the type satisfies this interface, so a mistyped
	// signature would mean the warning is simply never raised, with nothing
	// failing to say so. This makes that a compile error.
	_ resource.ResourceWithValidateConfig = &wanKitResource{}
	_ list.ListResource                   = &wanKitResource{}
	_ list.ListResourceWithConfigure      = &wanKitResource{}
)

func newWANKitResource() *wanKitResource {
	r := &wanKitResource{}
	r.Spec = wanKitSpec()
	r.SchemaSpec = wanKitSchema()
	r.ListSurface = wanKitList()
	return r
}

func NewWANResource() resource.Resource { return newWANKitResource() }

func NewWANListResource() list.ListResource { return newWANKitResource() }

// Metadata is here, not promoted from an embedded type: descriptor_policy_test.go's
// kitServedSurfaces resolves each surface's TypeName by parsing this method.
func (r *wanKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_wan"
}

func (r *wanKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = wanKitBackend(client.ApiClient)
	r.DefaultSite = client.Site
}

// ValidateConfig warns when the configuration sets a value the controller
// will not receive for a WAN: go-unifi serializes a Network through one of
// seven per-purpose structs, and any field the WAN one omits is discarded
// with no diagnostic at any layer. At plan time, so a still-unknown attribute
// goes unreported (a miss, not a false alarm).
func (r *wanKitResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var model wanKitModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Stopgap carried over from the hand resource: the model has no
	// ip/netmask/gateway attributes and the descriptor has no matching wires,
	// so a static WAN would plan clean and write an unaddressable one.
	if !model.Type.IsNull() && !model.Type.IsUnknown() && model.Type.ValueString() == "static" {
		resp.Diagnostics.AddAttributeError(
			path.Root("type"),
			"Static WAN Addressing Not Supported",
			"This provider has no attributes for static WAN addressing (IP, netmask, gateway) yet, "+
				`so type = "static" would plan clean and write an unaddressable WAN. `+
				"Use type = \"dhcp\" or configure the WAN's static address in the UniFi controller directly.",
		)
	}
	network, diags := r.Spec.ToSDK(ctx, &model)
	// A configuration this mapper can't build is a problem the apply will
	// report properly; warning about its fields here would just be noise on
	// top of a real error.
	if diags.HasError() || network == nil {
		return
	}
	// The two ReadOnly fields never reach ToSDK's output, so they are set
	// here for the warning's benefit: droppedOnWrite reporting them is the
	// whole reason they are safe to leave in the schema.
	if !model.MACOverrideEnabled.IsNull() && !model.MACOverrideEnabled.IsUnknown() {
		network.MACOverrideEnabled = model.MACOverrideEnabled.ValueBool()
	}
	if !model.SingleNetworkLAN.IsNull() && !model.SingleNetworkLAN.IsUnknown() {
		network.SingleNetworkLan = model.SingleNetworkLAN.ValueStringPointer()
	}
	resp.Diagnostics.Append(droppedOnWrite("WAN", network)...)
}
