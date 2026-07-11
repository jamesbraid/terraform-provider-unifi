package unifi

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// objectAsOptions is the zero ObjectAsOptions used for strict object->struct
// conversion. (Skip this declaration if the package already defines it.)
var objectAsOptions = basetypes.ObjectAsOptions{}

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &natRuleResource{}
	_ resource.ResourceWithImportState = &natRuleResource{}
	_ resource.ResourceWithIdentity    = &natRuleResource{}
)

func NewNatRuleResource() resource.Resource {
	return &natRuleResource{}
}

type natRuleResource struct {
	client *Client
}

// natRuleResourceModel is the Terraform model for a v2 NAT rule.
//
// The controller's attr_hidden/attr_no_delete/attr_no_edit and is_predefined
// fields are intentionally not modeled: they only appear on
// controller-predefined rules, which this resource does not manage.
type natRuleResourceModel struct {
	ID                    types.String   `tfsdk:"id"`
	Site                  types.String   `tfsdk:"site"`
	Type                  types.String   `tfsdk:"type"`
	Description           types.String   `tfsdk:"description"`
	Enabled               types.Bool     `tfsdk:"enabled"`
	Exclude               types.Bool     `tfsdk:"exclude"`
	IPAddress             types.String   `tfsdk:"ip_address"`
	InInterface           types.String   `tfsdk:"in_interface"`
	OutInterface          types.String   `tfsdk:"out_interface"`
	Logging               types.Bool     `tfsdk:"logging"`
	Port                  types.Int64    `tfsdk:"port"`
	PppoeUseBaseInterface types.Bool     `tfsdk:"pppoe_use_base_interface"`
	Protocol              types.String   `tfsdk:"protocol"`
	RuleIndex             types.Int64    `tfsdk:"rule_index"`
	SettingPreference     types.String   `tfsdk:"setting_preference"`
	IPVersion             types.String   `tfsdk:"ip_version"`
	SourceFilter          types.Object   `tfsdk:"source_filter"`
	DestinationFilter     types.Object   `tfsdk:"destination_filter"`
	Timeouts              timeouts.Value `tfsdk:"timeouts"`
}

// natRuleFilterModel is the nested source_filter/destination_filter block.
// filter_type is the discriminator: ADDRESS_AND_PORT uses address/port,
// FIREWALL_GROUPS uses firewall_group_ids, NETWORK_CONF uses network_id.
type natRuleFilterModel struct {
	FilterType       types.String `tfsdk:"filter_type"`
	Address          types.String `tfsdk:"address"`
	FirewallGroupIDs types.Set    `tfsdk:"firewall_group_ids"`
	InvertAddress    types.Bool   `tfsdk:"invert_address"`
	InvertPort       types.Bool   `tfsdk:"invert_port"`
	NetworkID        types.String `tfsdk:"network_id"`
	Port             types.Int64  `tfsdk:"port"`
}

func (natRuleFilterModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"filter_type":        types.StringType,
		"address":            types.StringType,
		"firewall_group_ids": types.SetType{ElemType: types.StringType},
		"invert_address":     types.BoolType,
		"invert_port":        types.BoolType,
		"network_id":         types.StringType,
		"port":               types.Int64Type,
	}
}

func (r *natRuleResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_nat_rule"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *natRuleResource) IdentitySchema(
	_ context.Context,
	_ resource.IdentitySchemaRequest,
	resp *resource.IdentitySchemaResponse,
) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{
				RequiredForImport: true,
			},
		},
	}
}

func (r *natRuleResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	filterAttrs := map[string]schema.Attribute{
		"filter_type": schema.StringAttribute{
			MarkdownDescription: "How to match traffic: `NONE`, `ADDRESS_AND_PORT` " +
				"(match `address`/`port`), `FIREWALL_GROUPS` (match " +
				"`firewall_group_ids`), or `NETWORK_CONF` (match `network_id`).",
			Required: true,
			Validators: []validator.String{
				stringvalidator.OneOf(
					"NONE", "ADDRESS_AND_PORT", "FIREWALL_GROUPS", "NETWORK_CONF",
				),
			},
		},
		"address": schema.StringAttribute{
			MarkdownDescription: "IP address or CIDR to match. Used when `filter_type` is `ADDRESS_AND_PORT`.",
			Optional:            true,
			Computed:            true,
			Default:             stringdefault.StaticString(""),
		},
		"firewall_group_ids": schema.SetAttribute{
			MarkdownDescription: "IDs of `unifi_firewall_group` objects to match. Used when `filter_type` is `FIREWALL_GROUPS`.",
			Optional:            true,
			Computed:            true,
			ElementType:         types.StringType,
			PlanModifiers: []planmodifier.Set{
				setplanmodifier.UseStateForUnknown(),
			},
		},
		"invert_address": schema.BoolAttribute{
			MarkdownDescription: "Match everything except `address`. Defaults to `false`.",
			Optional:            true,
			Computed:            true,
			Default:             booldefault.StaticBool(false),
		},
		"invert_port": schema.BoolAttribute{
			MarkdownDescription: "Match everything except `port`. Defaults to `false`.",
			Optional:            true,
			Computed:            true,
			Default:             booldefault.StaticBool(false),
		},
		"network_id": schema.StringAttribute{
			MarkdownDescription: "UniFi network ID to match. Used when `filter_type` is `NETWORK_CONF`.",
			Optional:            true,
			Computed:            true,
			Default:             stringdefault.StaticString(""),
		},
		"port": schema.Int64Attribute{
			MarkdownDescription: "Port to match. Used when `filter_type` is `ADDRESS_AND_PORT`.",
			Optional:            true,
			Computed:            true,
			Validators: []validator.Int64{
				int64validator.Between(1, 65535),
			},
			PlanModifiers: []planmodifier.Int64{
				int64planmodifier.UseStateForUnknown(),
			},
		},
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a UniFi NAT rule (UniFi Network 9.x+, v2 API). " +
			"NAT rules appear under Settings → Routing → NAT in the UniFi UI and " +
			"cover destination NAT (port-map style DNAT), source NAT, and " +
			"masquerade rules. Controller-predefined rules (`is_predefined`) are " +
			"managed by the firmware and cannot be managed by this resource.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the NAT rule.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"site": schema.StringAttribute{
				MarkdownDescription: "The name of the UniFi site. Defaults to the site configured in the provider.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "The NAT rule type: `DNAT`, `SNAT`, or `MASQUERADE`.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("DNAT", "SNAT", "MASQUERADE"),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description for the rule.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the rule is enabled. Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"exclude": schema.BoolAttribute{
				MarkdownDescription: "When `true` the rule excludes matching traffic from NAT instead of translating it. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"ip_address": schema.StringAttribute{
				MarkdownDescription: "The translation IP address: the internal destination for `DNAT`, the source address for `SNAT`. Not used for `MASQUERADE`.",
				Optional:            true,
			},
			"in_interface": schema.StringAttribute{
				MarkdownDescription: "The inbound interface the rule applies to (e.g. a WAN interface for `DNAT`).",
				Optional:            true,
			},
			"out_interface": schema.StringAttribute{
				MarkdownDescription: "The outbound interface the rule applies to (for `SNAT`/`MASQUERADE`).",
				Optional:            true,
			},
			"logging": schema.BoolAttribute{
				MarkdownDescription: "Whether to log packets matching this rule. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "The translation port (for `DNAT`).",
				Optional:            true,
				Validators: []validator.Int64{
					int64validator.Between(1, 65535),
				},
			},
			"pppoe_use_base_interface": schema.BoolAttribute{
				MarkdownDescription: "Apply the rule to the PPPoE base interface instead of the PPPoE session interface. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"protocol": schema.StringAttribute{
				MarkdownDescription: "The protocol to match: `all`, `tcp`, `udp`, or `tcp_udp`. Assigned by the controller when unset.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("all", "tcp", "udp", "tcp_udp"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"rule_index": schema.Int64Attribute{
				MarkdownDescription: "The ordering index of the rule. Assigned by the controller when unset.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"setting_preference": schema.StringAttribute{
				MarkdownDescription: "Whether the rule is controller-managed (`auto`) or user-managed (`manual`). Assigned by the controller when unset.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "manual"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"ip_version": schema.StringAttribute{
				MarkdownDescription: "The IP version the rule applies to: `IPV4` or `IPV6`. Assigned by the controller when unset.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("IPV4", "IPV6"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"source_filter": schema.SingleNestedAttribute{
				MarkdownDescription: "Filter on the traffic source.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: filterAttrs,
			},
			"destination_filter": schema.SingleNestedAttribute{
				MarkdownDescription: "Filter on the traffic destination.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: filterAttrs,
			},
			"timeouts": timeouts.Attributes(
				ctx,
				timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
			),
		},
	}
}

func (r *natRuleResource) Configure(
	ctx context.Context,
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
			fmt.Sprintf(
				"Expected *Client, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}

	r.client = client
}

func (r *natRuleResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan natRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, timeoutDiags := plan.Timeouts.Create(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	site := plan.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	nat, diags := modelToNat(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateNat(ctx, site, nat)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating NAT Rule",
			"Could not create NAT rule: "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(natToModel(ctx, created, &plan)...)
	plan.Site = types.StringValue(site)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), plan.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *natRuleResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state natRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, timeoutDiags := state.Timeouts.Read(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	// GetNat is list-then-filter in go-unifi: the v2 NAT API has no
	// per-object GET.
	nat, err := r.client.GetNat(ctx, site, state.ID.ValueString())
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading NAT Rule",
			"Could not read NAT rule "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(natToModel(ctx, nat, &state)...)
	state.Site = types.StringValue(site)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *natRuleResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan natRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state natRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, timeoutDiags := plan.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	plan.ID = state.ID

	nat, diags := modelToNat(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.client.UpdateNat(ctx, site, nat)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating NAT Rule",
			"Could not update NAT rule "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(natToModel(ctx, updated, &plan)...)
	plan.Site = types.StringValue(site)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), plan.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *natRuleResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state natRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, timeoutDiags := state.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	err := r.client.DeleteNat(ctx, site, state.ID.ValueString())
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); !ok {
			resp.Diagnostics.AddError(
				"Error Deleting NAT Rule",
				"Could not delete NAT rule "+state.ID.ValueString()+": "+err.Error(),
			)
		}
	}
}

func (r *natRuleResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	idParts, diags := util.ParseImportID(req.ID, 1, 2)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if site := idParts["site"]; site != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), site)...)
	}

	if id := idParts["id"]; id != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	}
}

// ---------------------------------------------------------------------------
// Conversion helpers
// ---------------------------------------------------------------------------

func modelToNat(
	ctx context.Context,
	m natRuleResourceModel,
) (*unifi.Nat, diag.Diagnostics) {
	var diags diag.Diagnostics

	nat := &unifi.Nat{
		ID:                    m.ID.ValueString(),
		Type:                  m.Type.ValueString(),
		Description:           m.Description.ValueString(),
		Enabled:               m.Enabled.ValueBool(),
		Exclude:               m.Exclude.ValueBool(),
		IPAddress:             m.IPAddress.ValueString(),
		InInterface:           m.InInterface.ValueString(),
		OutInterface:          m.OutInterface.ValueString(),
		Logging:               m.Logging.ValueBool(),
		PppoeUseBaseInterface: m.PppoeUseBaseInterface.ValueBool(),
		Protocol:              m.Protocol.ValueString(),
		SettingPreference:     m.SettingPreference.ValueString(),
		Version:               m.IPVersion.ValueString(),
	}

	if !m.Port.IsNull() && !m.Port.IsUnknown() {
		nat.Port = util.Ptr(m.Port.ValueInt64())
	}
	if !m.RuleIndex.IsNull() && !m.RuleIndex.IsUnknown() {
		nat.RuleIndex = util.Ptr(m.RuleIndex.ValueInt64())
	}

	nat.SourceFilter = modelToNatSourceFilter(ctx, m.SourceFilter, &diags)
	nat.DestinationFilter = modelToNatDestinationFilter(ctx, m.DestinationFilter, &diags)

	return nat, diags
}

func modelToNatSourceFilter(
	ctx context.Context,
	obj types.Object,
	diags *diag.Diagnostics,
) *unifi.NatSourceFilter {
	if obj.IsNull() || obj.IsUnknown() {
		return nil
	}
	var fm natRuleFilterModel
	diags.Append(obj.As(ctx, &fm, objectAsOptions)...)
	if diags.HasError() {
		return nil
	}
	f := &unifi.NatSourceFilter{
		FilterType:    fm.FilterType.ValueString(),
		Address:       fm.Address.ValueString(),
		InvertAddress: fm.InvertAddress.ValueBool(),
		InvertPort:    fm.InvertPort.ValueBool(),
		NetworkConfID: fm.NetworkID.ValueString(),
	}
	if !fm.FirewallGroupIDs.IsNull() && !fm.FirewallGroupIDs.IsUnknown() {
		diags.Append(fm.FirewallGroupIDs.ElementsAs(ctx, &f.FirewallGroupIDs, false)...)
	}
	if !fm.Port.IsNull() && !fm.Port.IsUnknown() {
		f.Port = util.Ptr(fm.Port.ValueInt64())
	}
	return f
}

func modelToNatDestinationFilter(
	ctx context.Context,
	obj types.Object,
	diags *diag.Diagnostics,
) *unifi.NatDestinationFilter {
	if obj.IsNull() || obj.IsUnknown() {
		return nil
	}
	var fm natRuleFilterModel
	diags.Append(obj.As(ctx, &fm, objectAsOptions)...)
	if diags.HasError() {
		return nil
	}
	f := &unifi.NatDestinationFilter{
		FilterType:    fm.FilterType.ValueString(),
		Address:       fm.Address.ValueString(),
		InvertAddress: fm.InvertAddress.ValueBool(),
		InvertPort:    fm.InvertPort.ValueBool(),
		NetworkConfID: fm.NetworkID.ValueString(),
	}
	if !fm.FirewallGroupIDs.IsNull() && !fm.FirewallGroupIDs.IsUnknown() {
		diags.Append(fm.FirewallGroupIDs.ElementsAs(ctx, &f.FirewallGroupIDs, false)...)
	}
	if !fm.Port.IsNull() && !fm.Port.IsUnknown() {
		f.Port = util.Ptr(fm.Port.ValueInt64())
	}
	return f
}

// natPortValue maps the API port pointer to a Terraform value. go-unifi's
// UnmarshalJSON maps an empty-string port to *int64(0); both nil and 0 mean
// "no port" and map to null so plans stay clean.
func natPortValue(p *int64) types.Int64 {
	if p == nil || *p == 0 {
		return types.Int64Null()
	}
	return types.Int64Value(*p)
}

func natToModel(
	ctx context.Context,
	nat *unifi.Nat,
	m *natRuleResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics

	m.ID = types.StringValue(nat.ID)
	m.Type = types.StringValue(nat.Type)
	// description has a schema default of "": map "" verbatim, not to null.
	m.Description = types.StringValue(nat.Description)
	m.Enabled = types.BoolValue(nat.Enabled)
	m.Exclude = types.BoolValue(nat.Exclude)
	m.Logging = types.BoolValue(nat.Logging)
	m.PppoeUseBaseInterface = types.BoolValue(nat.PppoeUseBaseInterface)
	// Optional-only strings: "" means unset and maps to null.
	m.IPAddress = util.StringValueOrNull(nat.IPAddress)
	m.InInterface = util.StringValueOrNull(nat.InInterface)
	m.OutInterface = util.StringValueOrNull(nat.OutInterface)
	// Optional+Computed strings: "" also maps to null (Computed absorbs it).
	m.Protocol = util.StringValueOrNull(nat.Protocol)
	m.SettingPreference = util.StringValueOrNull(nat.SettingPreference)
	m.IPVersion = util.StringValueOrNull(nat.Version)
	m.Port = natPortValue(nat.Port)
	if nat.RuleIndex != nil {
		m.RuleIndex = types.Int64Value(*nat.RuleIndex)
	} else {
		m.RuleIndex = types.Int64Null()
	}
	m.SourceFilter = natSourceFilterToModel(ctx, nat.SourceFilter, &diags)
	m.DestinationFilter = natDestinationFilterToModel(ctx, nat.DestinationFilter, &diags)

	return diags
}

func natSourceFilterToModel(
	ctx context.Context,
	f *unifi.NatSourceFilter,
	diags *diag.Diagnostics,
) types.Object {
	attrTypes := natRuleFilterModel{}.AttributeTypes()
	if f == nil {
		return types.ObjectNull(attrTypes)
	}
	groups, d := types.SetValueFrom(ctx, types.StringType, f.FirewallGroupIDs)
	diags.Append(d...)
	m := natRuleFilterModel{
		FilterType:       types.StringValue(f.FilterType),
		Address:          types.StringValue(f.Address),
		FirewallGroupIDs: groups,
		InvertAddress:    types.BoolValue(f.InvertAddress),
		InvertPort:       types.BoolValue(f.InvertPort),
		NetworkID:        types.StringValue(f.NetworkConfID),
		Port:             natPortValue(f.Port),
	}
	obj, d := types.ObjectValueFrom(ctx, attrTypes, m)
	diags.Append(d...)
	return obj
}

func natDestinationFilterToModel(
	ctx context.Context,
	f *unifi.NatDestinationFilter,
	diags *diag.Diagnostics,
) types.Object {
	attrTypes := natRuleFilterModel{}.AttributeTypes()
	if f == nil {
		return types.ObjectNull(attrTypes)
	}
	groups, d := types.SetValueFrom(ctx, types.StringType, f.FirewallGroupIDs)
	diags.Append(d...)
	m := natRuleFilterModel{
		FilterType:       types.StringValue(f.FilterType),
		Address:          types.StringValue(f.Address),
		FirewallGroupIDs: groups,
		InvertAddress:    types.BoolValue(f.InvertAddress),
		InvertPort:       types.BoolValue(f.InvertPort),
		NetworkID:        types.StringValue(f.NetworkConfID),
		Port:             natPortValue(f.Port),
	}
	obj, d := types.ObjectValueFrom(ctx, attrTypes, m)
	diags.Append(d...)
	return obj
}
