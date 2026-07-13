package unifi

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

var (
	_ resource.Resource                 = &firewallPolicyResource{}
	_ resource.ResourceWithImportState  = &firewallPolicyResource{}
	_ resource.ResourceWithIdentity     = &firewallPolicyResource{}
	_ resource.ResourceWithUpgradeState = &firewallPolicyResource{}
)

// Ensure provider defined types fully satisfy list interfaces.
var (
	_ list.ListResource              = &firewallPolicyResource{}
	_ list.ListResourceWithConfigure = &firewallPolicyResource{}
)

func NewFirewallPolicyResource() resource.Resource {
	return &firewallPolicyResource{}
}

func NewFirewallPolicyListResource() list.ListResource {
	return &firewallPolicyResource{}
}

// firewallPolicyListConfigModel describes the list configuration model.
type firewallPolicyListConfigModel struct {
	Site   types.String `tfsdk:"site"`
	Filter types.List   `tfsdk:"filter"`
}

// firewallPolicyListFilterModel represents a single name/value filter entry.
type firewallPolicyListFilterModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

type firewallPolicyResource struct {
	client *Client
}

// firewallPolicyModel is the Terraform resource model.
type firewallPolicyModel struct {
	ID                 types.String `tfsdk:"id"`
	Site               types.String `tfsdk:"site"`
	Name               types.String `tfsdk:"name"`
	Action             types.String `tfsdk:"action"`
	Enabled            types.Bool   `tfsdk:"enabled"`
	Protocol           types.String `tfsdk:"protocol"`
	Description        types.String `tfsdk:"description"`
	Logging            types.Bool   `tfsdk:"logging"`
	Index              types.Int64  `tfsdk:"index"`
	CreateAllowRespond types.Bool   `tfsdk:"create_allow_respond"`
	IPVersion          types.String `tfsdk:"ip_version"`
	// Firmware-managed fields the controller requires back on every PUT. They are
	// not user-settable; the provider round-trips them so updates don't drop them
	// (an omitted connection_state_type/icmp_typename makes the PUT fail HTTP 400).
	ConnectionStateType types.String   `tfsdk:"connection_state_type"`
	ConnectionStates    types.List     `tfsdk:"connection_states"`
	ICMPTypename        types.String   `tfsdk:"icmp_typename"`
	ICMPV6Typename      types.String   `tfsdk:"icmp_v6_typename"`
	Source              types.Object   `tfsdk:"source"`
	Destination         types.Object   `tfsdk:"destination"`
	Timeouts            timeouts.Value `tfsdk:"timeouts"`
}

// firewallPolicyEndpointModel is the nested source/destination block model.
type firewallPolicyEndpointModel struct {
	ZoneID           types.String `tfsdk:"zone_id"`
	MatchingTarget   types.String `tfsdk:"matching_target"`
	NetworkIDs       types.List   `tfsdk:"network_ids"`
	ClientMACs       types.List   `tfsdk:"client_macs"`
	IPs              types.List   `tfsdk:"ips"`
	WebDomains       types.List   `tfsdk:"web_domains"`
	Port             types.String `tfsdk:"port"`
	PortGroupID      types.String `tfsdk:"port_group_id"`
	IPGroupID        types.String `tfsdk:"ip_group_id"`
	PortMatchingType types.String `tfsdk:"port_matching_type"`
	// Firmware-managed; round-tripped so updates keep it (a PUT that omits
	// source/destination matching_target_type is rejected with HTTP 400).
	MatchingTargetType types.String `tfsdk:"matching_target_type"`
}

func (m firewallPolicyEndpointModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"zone_id":              types.StringType,
		"matching_target":      types.StringType,
		"network_ids":          types.ListType{ElemType: types.StringType},
		"client_macs":          types.ListType{ElemType: types.StringType},
		"ips":                  types.ListType{ElemType: types.StringType},
		"web_domains":          types.ListType{ElemType: types.StringType},
		"port":                 types.StringType,
		"port_group_id":        types.StringType,
		"ip_group_id":          types.StringType,
		"port_matching_type":   types.StringType,
		"matching_target_type": types.StringType,
	}
}

func (r *firewallPolicyResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_firewall_policy"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *firewallPolicyResource) IdentitySchema(
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

func (r *firewallPolicyResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	endpointAttrs := map[string]schema.Attribute{
		"zone_id": schema.StringAttribute{
			MarkdownDescription: "The ID of the firewall zone this endpoint belongs to. Use the `unifi_firewall_zone` data source to look up zone IDs by name.",
			Required:            true,
		},
		"matching_target": schema.StringAttribute{
			MarkdownDescription: "What to match: `ANY`, `NETWORK`, `CLIENT`, `IP`, `DEVICE`, `MAC`, or `WEB` (domains/FQDN).",
			Required:            true,
			Validators: []validator.String{
				stringvalidator.OneOf("ANY", "NETWORK", "CLIENT", "IP", "DEVICE", "MAC", "WEB"),
			},
		},
		"network_ids": schema.ListAttribute{
			MarkdownDescription: "List of UniFi network IDs to match. Used when `matching_target` is `NETWORK`.",
			Optional:            true,
			Computed:            true,
			ElementType:         types.StringType,
			PlanModifiers: []planmodifier.List{
				listplanmodifier.UseStateForUnknown(),
			},
		},
		"client_macs": schema.ListAttribute{
			MarkdownDescription: "List of client MAC addresses to match. Used when `matching_target` is `CLIENT`.",
			Optional:            true,
			Computed:            true,
			ElementType:         types.StringType,
			PlanModifiers: []planmodifier.List{
				listplanmodifier.UseStateForUnknown(),
			},
		},
		"ips": schema.ListAttribute{
			MarkdownDescription: "List of IP addresses or CIDR ranges to match. Used when `matching_target` is `IP`.",
			Optional:            true,
			Computed:            true,
			ElementType:         types.StringType,
			PlanModifiers: []planmodifier.List{
				listplanmodifier.UseStateForUnknown(),
			},
		},
		"web_domains": schema.ListAttribute{
			MarkdownDescription: "List of domains/FQDNs to match. Used when `matching_target` is `WEB`.",
			Optional:            true,
			Computed:            true,
			ElementType:         types.StringType,
			PlanModifiers: []planmodifier.List{
				listplanmodifier.UseStateForUnknown(),
			},
		},
		"port": schema.StringAttribute{
			MarkdownDescription: "Port(s) to match when `port_matching_type` is `SPECIFIC`. " +
				"A single port (`161`) or a comma-separated list of ports/ranges " +
				"(`80,443`, `8000-8100`). Leave unset for no port match.",
			Optional: true,
			Computed: true,
			Validators: []validator.String{
				stringvalidator.RegexMatches(
					regexp.MustCompile(`^[0-9]{1,5}(-[0-9]{1,5})?(,[0-9]{1,5}(-[0-9]{1,5})?)*$`),
					"must be a port number or a comma-separated list of ports/ranges "+
						`(e.g. "80,443" or "8000-8100")`,
				),
			},
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		},
		"port_group_id": schema.StringAttribute{
			MarkdownDescription: "ID of a `unifi_firewall_group` (port-group type) to match. Used when `port_matching_type` is `OBJECT`.",
			Optional:            true,
			Computed:            true,
			Default:             stringdefault.StaticString(""),
		},
		"ip_group_id": schema.StringAttribute{
			MarkdownDescription: "ID of a `unifi_firewall_group` (address-group type) to match. Used when `matching_target` is `IP` with `matching_target_type = OBJECT`.",
			Optional:            true,
			Computed:            true,
			Default:             stringdefault.StaticString(""),
		},
		"port_matching_type": schema.StringAttribute{
			MarkdownDescription: "How to match ports: `ANY`, `SPECIFIC`, or `OBJECT` (port group).",
			Optional:            true,
			Computed:            true,
			Default:             stringdefault.StaticString("ANY"),
			Validators: []validator.String{
				stringvalidator.OneOf("ANY", "SPECIFIC", "OBJECT"),
			},
		},
		"matching_target_type": schema.StringAttribute{
			MarkdownDescription: "How the matching target is specified (`ANY`, `SPECIFIC`, `LIST`, `OBJECT`). Managed by the UniFi controller; the provider round-trips it so updates are accepted.",
			Computed:            true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		},
	}

	resp.Schema = schema.Schema{
		Version: 1,
		MarkdownDescription: "Manages a UniFi zone-based firewall policy (UniFi Network 8.x+). " +
			"Zone-based firewall policies replace the legacy firewall rules and are displayed " +
			"under Settings → Security → Firewall Policies in the UniFi UI.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the firewall policy.",
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
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the firewall policy.",
				Required:            true,
			},
			"action": schema.StringAttribute{
				MarkdownDescription: "The action to take when the policy matches: `ALLOW`, `BLOCK`, or `REJECT`.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("ALLOW", "BLOCK", "REJECT"),
				},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the policy is enabled. Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"protocol": schema.StringAttribute{
				MarkdownDescription: "The protocol to match: `all`, `tcp`, `udp`, `tcp_udp`, " +
					"`icmp`, or `icmpv6`. Defaults to `all`. Note: for `icmp`/`icmpv6` " +
					"policies the controller rejects `create_allow_respond = true` " +
					"(`FirewallPolicyCreateRespondTrafficPolicyNotAllowed`) — keep it " +
					"`false` and add an explicit reverse policy if you need the reply.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("all"),
				Validators: []validator.String{
					stringvalidator.OneOf("all", "tcp", "udp", "tcp_udp", "icmp", "icmpv6"),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "A description for the policy.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
			},
			"logging": schema.BoolAttribute{
				MarkdownDescription: "Whether to log packets matching this policy. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"index": schema.Int64Attribute{
				MarkdownDescription: "The ordering index of the policy within its zone-pair, " +
					"assigned by the controller. **Read-only:** UniFi does not accept a " +
					"client-supplied index on create or update (the policy is always appended " +
					"to the end of its source/destination zone-pair), and the supported API " +
					"exposes no reorder operation, so policy ordering cannot be managed through " +
					"this provider. Reorder policies in the UniFi UI if needed.",
				Computed: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"create_allow_respond": schema.BoolAttribute{
				MarkdownDescription: "When `true`, UniFi automatically creates a matching rule to allow established/related return traffic. Recommended for `ALLOW` policies. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"ip_version": schema.StringAttribute{
				MarkdownDescription: "The IP version to match: `BOTH`, `IPV4`, or `IPV6`. Defaults to `IPV4`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("IPV4"),
				Validators: []validator.String{
					stringvalidator.OneOf("BOTH", "IPV4", "IPV6"),
				},
			},
			"connection_state_type": schema.StringAttribute{
				MarkdownDescription: "Connection-state matching mode: `ALL` (any state), `RESPOND_ONLY` (established/related returns), or `CUSTOM` (match the states listed in `connection_states`). Optional: if omitted the controller assigns it (defaults to `ALL`) and the provider round-trips the value so updates are accepted.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("ALL", "RESPOND_ONLY", "CUSTOM"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"connection_states": schema.ListAttribute{
				MarkdownDescription: "Connection states matched when `connection_state_type` is `CUSTOM` (`NEW`, `ESTABLISHED`, `RELATED`, `INVALID`). Optional: leave unset for `ALL`/`RESPOND_ONLY` and the controller manages it; the provider round-trips the value so a `CUSTOM` policy's states are not dropped on update (which the firmware rejects with HTTP 400).",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				Validators: []validator.List{
					listvalidator.ValueStringsAre(
						stringvalidator.OneOf("NEW", "ESTABLISHED", "RELATED", "INVALID"),
					),
				},
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"icmp_typename": schema.StringAttribute{
				MarkdownDescription: "ICMP type matching mode. Managed by the UniFi controller; the provider round-trips it so updates are accepted.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"icmp_v6_typename": schema.StringAttribute{
				MarkdownDescription: "ICMPv6 type matching mode. Managed by the UniFi controller; the provider round-trips it so updates are accepted.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"source": schema.SingleNestedAttribute{
				MarkdownDescription: "The source endpoint of the policy.",
				Required:            true,
				Attributes:          endpointAttrs,
				PlanModifiers: []planmodifier.Object{
					endpointDiscriminatorPlanModifier{},
				},
				Validators: []validator.Object{
					endpointDiscriminatorValidator{},
				},
			},
			"destination": schema.SingleNestedAttribute{
				MarkdownDescription: "The destination endpoint of the policy.",
				Required:            true,
				Attributes:          endpointAttrs,
				PlanModifiers: []planmodifier.Object{
					endpointDiscriminatorPlanModifier{},
				},
				Validators: []validator.Object{
					endpointDiscriminatorValidator{},
				},
			},
			"timeouts": timeouts.Attributes(
				ctx,
				timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
			),
		},
	}
}

func (r *firewallPolicyResource) Configure(
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

func (r *firewallPolicyResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan firewallPolicyModel
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

	fp, diags := modelToFirewallPolicy(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateFirewallPolicy(ctx, site, fp)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Firewall Policy",
			"Could not create firewall policy: "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(firewallPolicyToModel(ctx, created, &plan)...)
	plan.Site = types.StringValue(site)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), plan.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *firewallPolicyResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state firewallPolicyModel
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

	fp, err := r.client.GetFirewallPolicy(ctx, site, state.ID.ValueString())
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Firewall Policy",
			"Could not read firewall policy "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(firewallPolicyToModel(ctx, fp, &state)...)
	state.Site = types.StringValue(site)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *firewallPolicyResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan firewallPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state firewallPolicyModel
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

	fp, diags := modelToFirewallPolicy(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// matching_target_type is firmware-derived: the controller (and the
	// provider's own firewallPolicyMatchingTargetType helper) may set it to a
	// concrete value during the PUT (e.g. "" -> "SPECIFIC" for a non-ANY match),
	// which the planned value cannot anticipate. It is Computed +
	// UseStateForUnknown, so the planned value is the prior-state value; capture
	// it now and re-assert it on the post-apply state so Terraform's
	// "inconsistent result after apply" check passes for policies whose state
	// still carries an empty type (#324). The next Read reconciles state with the
	// controller's value.
	plannedSrcMTT := endpointMatchingTargetType(ctx, plan.Source, &resp.Diagnostics)
	plannedDstMTT := endpointMatchingTargetType(ctx, plan.Destination, &resp.Diagnostics)

	updated, err := r.client.UpdateFirewallPolicy(ctx, site, fp)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Firewall Policy",
			"Could not update firewall policy "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(firewallPolicyToModel(ctx, updated, &plan)...)

	// Only re-assert when the plan carried a known value: if it was unknown
	// (an in-block field changed), the attribute is known-after-apply and the
	// controller's value is accepted as-is.
	if !plannedSrcMTT.IsNull() && !plannedSrcMTT.IsUnknown() {
		plan.Source = withMatchingTargetType(ctx, plan.Source, plannedSrcMTT, &resp.Diagnostics)
	}
	if !plannedDstMTT.IsNull() && !plannedDstMTT.IsUnknown() {
		plan.Destination = withMatchingTargetType(
			ctx,
			plan.Destination,
			plannedDstMTT,
			&resp.Diagnostics,
		)
	}

	plan.Site = types.StringValue(site)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), plan.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *firewallPolicyResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state firewallPolicyModel
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

	err := r.client.DeleteFirewallPolicy(ctx, site, state.ID.ValueString())
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); !ok {
			resp.Diagnostics.AddError(
				"Error Deleting Firewall Policy",
				"Could not delete firewall policy "+state.ID.ValueString()+": "+err.Error(),
			)
		}
	}
}

func (r *firewallPolicyResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	idParts := strings.Split(req.ID, ":")
	if len(idParts) == 2 {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), idParts[0])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), idParts[1])...)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// ---------------------------------------------------------------------------
// State upgrade (schema v0 -> v1: port int64 -> string)
// ---------------------------------------------------------------------------

// firewallPolicyEndpointModelV0 mirrors firewallPolicyEndpointModel but with the
// pre-v1 integer `port`. It exists only to decode prior state during upgrade.
type firewallPolicyEndpointModelV0 struct {
	ZoneID             types.String `tfsdk:"zone_id"`
	MatchingTarget     types.String `tfsdk:"matching_target"`
	NetworkIDs         types.List   `tfsdk:"network_ids"`
	ClientMACs         types.List   `tfsdk:"client_macs"`
	IPs                types.List   `tfsdk:"ips"`
	WebDomains         types.List   `tfsdk:"web_domains"`
	Port               types.Int64  `tfsdk:"port"`
	PortGroupID        types.String `tfsdk:"port_group_id"`
	IPGroupID          types.String `tfsdk:"ip_group_id"`
	PortMatchingType   types.String `tfsdk:"port_matching_type"`
	MatchingTargetType types.String `tfsdk:"matching_target_type"`
}

func (r *firewallPolicyResource) UpgradeState(
	ctx context.Context,
) map[int64]resource.StateUpgrader {
	// Build the prior (v0) schema from the current one and swap the
	// source/destination `port` back to an integer — that is the only
	// structural difference. Deriving it from the live schema keeps the
	// upgrader correct as the rest of the schema evolves.
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	priorSchema := schemaResp.Schema
	priorSchema.Version = 0
	for _, key := range []string{"source", "destination"} {
		nested, ok := priorSchema.Attributes[key].(schema.SingleNestedAttribute)
		if !ok {
			continue
		}
		attrs := make(map[string]schema.Attribute, len(nested.Attributes))
		for k, v := range nested.Attributes {
			attrs[k] = v
		}
		attrs["port"] = schema.Int64Attribute{Optional: true, Computed: true}
		nested.Attributes = attrs
		priorSchema.Attributes[key] = nested
	}

	return map[int64]resource.StateUpgrader{
		// v0 modeled `port` as an integer, which both dropped multi-port values
		// (#286) and serialized portless endpoints as the invalid "0" (#288).
		// v1 models it as a string; convert the stored number, treating 0/null
		// as "no port".
		0: {
			PriorSchema: &priorSchema,
			StateUpgrader: func(
				ctx context.Context,
				req resource.UpgradeStateRequest,
				resp *resource.UpgradeStateResponse,
			) {
				var state firewallPolicyModel
				resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
				if resp.Diagnostics.HasError() {
					return
				}

				state.Source = upgradeFirewallPolicyEndpointV0(
					ctx, state.Source, &resp.Diagnostics,
				)
				state.Destination = upgradeFirewallPolicyEndpointV0(
					ctx, state.Destination, &resp.Diagnostics,
				)
				if resp.Diagnostics.HasError() {
					return
				}

				resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			},
		},
	}
}

func upgradeFirewallPolicyEndpointV0(
	ctx context.Context,
	obj types.Object,
	diags *diag.Diagnostics,
) types.Object {
	newTypes := firewallPolicyEndpointModel{}.AttributeTypes()
	if obj.IsNull() {
		return types.ObjectNull(newTypes)
	}
	if obj.IsUnknown() {
		return types.ObjectUnknown(newTypes)
	}

	var v0 firewallPolicyEndpointModelV0
	diags.Append(obj.As(ctx, &v0, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return obj
	}

	port := types.StringNull()
	if !v0.Port.IsNull() && !v0.Port.IsUnknown() && v0.Port.ValueInt64() != 0 {
		port = types.StringValue(strconv.FormatInt(v0.Port.ValueInt64(), 10))
	}

	upgraded := firewallPolicyEndpointModel{
		ZoneID:             v0.ZoneID,
		MatchingTarget:     v0.MatchingTarget,
		NetworkIDs:         v0.NetworkIDs,
		ClientMACs:         v0.ClientMACs,
		IPs:                v0.IPs,
		WebDomains:         v0.WebDomains,
		Port:               port,
		PortGroupID:        v0.PortGroupID,
		IPGroupID:          v0.IPGroupID,
		PortMatchingType:   v0.PortMatchingType,
		MatchingTargetType: v0.MatchingTargetType,
	}

	newObj, d := types.ObjectValueFrom(ctx, newTypes, upgraded)
	diags.Append(d...)
	return newObj
}

// portToStringValue maps the API port string to a Terraform value. The API
// returns "" for a portless endpoint and historically "0" for policies created
// by older provider versions (#288); both map to null so plans stay clean.
func portToStringValue(p string) types.String {
	if p == "" || p == "0" {
		return types.StringNull()
	}
	return types.StringValue(p)
}

// ---------------------------------------------------------------------------
// Conversion helpers
// ---------------------------------------------------------------------------

func modelToFirewallPolicy(
	ctx context.Context,
	model firewallPolicyModel,
) (*unifi.FirewallPolicy, diag.Diagnostics) {
	var diags diag.Diagnostics

	fp := &unifi.FirewallPolicy{
		ID:                  model.ID.ValueString(),
		Name:                model.Name.ValueString(),
		Action:              model.Action.ValueString(),
		Enabled:             model.Enabled.ValueBool(),
		Protocol:            model.Protocol.ValueString(),
		Description:         model.Description.ValueString(),
		Logging:             model.Logging.ValueBool(),
		CreateAllowRespond:  model.CreateAllowRespond.ValueBool(),
		Version:             model.IPVersion.ValueString(),
		ConnectionStateType: model.ConnectionStateType.ValueString(),
		ICMPTypename:        model.ICMPTypename.ValueString(),
		ICMPV6Typename:      model.ICMPV6Typename.ValueString(),
		ConnectionStates:    []string{},
		Schedule: &unifi.FirewallPolicySchedule{
			Mode: "ALWAYS",
		},
	}

	// Round-trip the connection states (e.g. ["NEW"]) the controller reported.
	// Omitting them makes a CUSTOM-state policy's PUT fail with HTTP 400 (#227).
	if !model.ConnectionStates.IsNull() && !model.ConnectionStates.IsUnknown() {
		diags.Append(model.ConnectionStates.ElementsAs(ctx, &fp.ConnectionStates, false)...)
	}

	// index is controller-assigned and read-only: UniFi ignores a client-supplied
	// value on create/update (the policy is appended to the end of its zone-pair) and
	// the supported API exposes no reorder operation, so we never send it (#348).

	var srcModel firewallPolicyEndpointModel
	diags.Append(model.Source.As(ctx, &srcModel, basetypes.ObjectAsOptions{})...)
	if !diags.HasError() {
		fp.Source = endpointModelToSource(ctx, srcModel, &diags)
	}

	var dstModel firewallPolicyEndpointModel
	diags.Append(model.Destination.As(ctx, &dstModel, basetypes.ObjectAsOptions{})...)
	if !diags.HasError() {
		fp.Destination = endpointModelToDestination(ctx, dstModel, &diags)
	}

	return fp, diags
}

// firewallPolicyMatchingTargetType ensures a concrete matching_target_type is
// sent for a specific (non-ANY) match. The controller rejects an IP/NETWORK/etc.
// match whose matching_target_type is empty (#293,
// api.err.MissingFirewallPolicySourceMatchingTargetType) — which happens when a
// source is switched from ANY to a specific target, leaving the round-tripped
// type empty or a stale "ANY". A match that references an IP group via
// ip_group_id (#316) requires "OBJECT" instead: the controller rejects a group
// reference sent with "SPECIFIC" (api.err.EmptyFirewallDestinationIps), and on
// create the type is never controller-assigned, so a group reference derives
// "OBJECT" — overriding a stale ""/"ANY"/"SPECIFIC" from state (e.g. when a
// policy is switched from literal ips to a group). A controller-assigned
// "OBJECT"/"LIST" is preserved.
//
// A stale "OBJECT" is only ever meaningful under matching_target == "IP" (the
// only target ip_group_id can be owned by — see endpointOwnsSelector); if the
// caller has already transitioned matching_target away from IP (so ipGroupID
// is correctly empty per that gating) but currentType still carries a stale
// "OBJECT" left over from the prior IP-group state, it is demoted and
// re-derived by the SPECIFIC rule below instead of being preserved (design
// doc §4.3's ip_group_id correctness note: a cleared ip_group_id must not
// leave the type stuck at OBJECT). "LIST" has no such dependency on
// ip_group_id and is always preserved.
func firewallPolicyMatchingTargetType(matchingTarget, currentType, ipGroupID string) string {
	if ipGroupID != "" && currentType != "OBJECT" && currentType != "LIST" {
		return "OBJECT"
	}
	if currentType == "OBJECT" && matchingTarget != "IP" {
		currentType = ""
	}
	if matchingTarget != "" && matchingTarget != "ANY" &&
		(currentType == "" || currentType == "ANY") {
		return "SPECIFIC"
	}
	return currentType
}

// endpointOwnsSelector reports whether the given matching_target owns the
// named selector field. A firewall policy endpoint's matching_target is a
// discriminator: exactly one selector field is active for each non-ANY
// value (network_ids for NETWORK, client_macs for CLIENT, ips and
// ip_group_id for IP, web_domains for WEB); every other selector is
// inactive and must not be sent to, or read back from, the controller
// (design doc §4.3). ip_group_id is owned by IP alongside ips — it is the
// object-reference form of the same IP match, not a separate discriminator
// value (design doc's ip_group_id correctness note: leaving it unconditional
// lets a stale ip_group_id force matching_target_type back to OBJECT after
// a transition away from IP).
func endpointOwnsSelector(matchingTarget, field string) bool {
	switch field {
	case "network_ids":
		return matchingTarget == "NETWORK"
	case "client_macs":
		return matchingTarget == "CLIENT"
	case "ips", "ip_group_id":
		return matchingTarget == "IP"
	case "web_domains":
		return matchingTarget == "WEB"
	default:
		return false
	}
}

// endpointOwnsPortField reports whether the given port_matching_type owns
// the named port field: SPECIFIC owns port, OBJECT owns port_group_id, ANY
// owns neither.
func endpointOwnsPortField(portMatchingType, field string) bool {
	switch field {
	case "port":
		return portMatchingType == "SPECIFIC"
	case "port_group_id":
		return portMatchingType == "OBJECT"
	default:
		return false
	}
}

// endpointDiscriminatorPlanModifier rebuilds the planned source/destination
// object so every selector/port field the active (planned) matching_target/
// port_matching_type does not own is forced to null, before the
// endpointDiscriminatorValidator or Terraform's plan/apply consistency check
// ever see it.
//
// This runs after UseStateForUnknown (which is what leaves a stale child
// resolved into the plan in the first place — see design doc §4.3 item 3)
// and before validation. It shares endpointOwnsSelector/endpointOwnsPortField
// with the codec functions (endpointModelToSource/Destination,
// apiSourceToEndpointModel/apiDestinationToEndpointModel) and
// endpointDiscriminatorValidator, so there is exactly one ownership
// definition, not three that could drift.
//
// If matching_target or port_matching_type is itself null or unknown in the
// plan, this modifier makes no changes at all: nulling children based on a
// guessed discriminator would itself create a plan/apply mismatch once the
// real value resolves at apply time. This mirrors the deferral rule
// endpointDiscriminatorValidator follows.
type endpointDiscriminatorPlanModifier struct{}

func (m endpointDiscriminatorPlanModifier) Description(context.Context) string {
	return "nulls source/destination selector and port fields the active matching_target/port_matching_type does not own"
}

func (m endpointDiscriminatorPlanModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m endpointDiscriminatorPlanModifier) PlanModifyObject(
	ctx context.Context,
	req planmodifier.ObjectRequest,
	resp *planmodifier.ObjectResponse,
) {
	// Nothing to rebuild if there's no planned value, or if it's already
	// unknown (e.g. an entirely-unresolved endpoint) — leave resp.PlanValue
	// as whatever prior plan-modifier stages (UseStateForUnknown) produced.
	if resp.PlanValue.IsNull() || resp.PlanValue.IsUnknown() {
		return
	}

	var planned firewallPolicyEndpointModel
	resp.Diagnostics.Append(resp.PlanValue.As(ctx, &planned, basetypes.ObjectAsOptions{
		UnhandledUnknownAsEmpty: false,
	})...)
	if resp.Diagnostics.HasError() {
		return
	}

	matchingTarget := planned.MatchingTarget
	portMatchingType := planned.PortMatchingType

	// A null/unknown discriminator defers entirely to apply — see the
	// doc comment above and design doc §4.3 item 4 (the validator follows
	// the identical rule). Guessing here would null out a child that turns
	// out to be valid once the discriminator resolves.
	matchingTargetKnown := !matchingTarget.IsNull() && !matchingTarget.IsUnknown()
	portMatchingTypeKnown := !portMatchingType.IsNull() && !portMatchingType.IsUnknown()

	if !matchingTargetKnown && !portMatchingTypeKnown {
		// Neither discriminator is known yet: nothing to null, avoid the
		// round-trip entirely so we don't risk re-encoding a value we
		// didn't need to touch.
		return
	}

	if matchingTargetKnown {
		mt := matchingTarget.ValueString()
		if !endpointOwnsSelector(mt, "network_ids") {
			planned.NetworkIDs = types.ListNull(types.StringType)
		}
		if !endpointOwnsSelector(mt, "client_macs") {
			planned.ClientMACs = types.ListNull(types.StringType)
		}
		if !endpointOwnsSelector(mt, "ips") {
			planned.IPs = types.ListNull(types.StringType)
		}
		if !endpointOwnsSelector(mt, "web_domains") {
			planned.WebDomains = types.ListNull(types.StringType)
		}
		if !endpointOwnsSelector(mt, "ip_group_id") {
			planned.IPGroupID = types.StringNull()
		}
	}
	if portMatchingTypeKnown {
		pmt := portMatchingType.ValueString()
		if !endpointOwnsPortField(pmt, "port") {
			planned.Port = types.StringNull()
		}
		if !endpointOwnsPortField(pmt, "port_group_id") {
			planned.PortGroupID = types.StringNull()
		}
	}

	newObj, diags := types.ObjectValueFrom(ctx, firewallPolicyEndpointModel{}.AttributeTypes(), planned)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.PlanValue = newObj
}

// endpointDiscriminatorValidator rejects a firewall policy endpoint
// (source/destination) config that sets a selector or port field the
// active matching_target/port_matching_type does not own — the plan-time
// half of the discriminator contract (design doc §4.3): a contradictory
// config is a validation error, not a silently-dropped value.
//
// Correctness requirement (design doc §4.3 item 4): when matching_target or
// port_matching_type is itself null or unknown, this validator skips the
// corresponding ownership checks entirely rather than treating the
// discriminator as "". An unresolved discriminator defers child validation
// to apply, not to a guessed default — treating unknown as "" would make
// every non-empty selector look "unowned" and reject configs that are
// actually valid once the discriminator resolves.
type endpointDiscriminatorValidator struct{}

func (v endpointDiscriminatorValidator) Description(context.Context) string {
	return "matching_target and port_matching_type must not have a configured selector for an inactive discriminator value"
}

func (v endpointDiscriminatorValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v endpointDiscriminatorValidator) ValidateObject(
	ctx context.Context,
	req validator.ObjectRequest,
	resp *validator.ObjectResponse,
) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	var m firewallPolicyEndpointModel
	resp.Diagnostics.Append(req.ConfigValue.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Deferral rule: an unknown/null discriminator skips its ownership
	// checks entirely rather than being treated as "" (design doc §4.3
	// item 4). Each discriminator is gated independently, since
	// matching_target and port_matching_type resolve independently.
	if !m.MatchingTarget.IsNull() && !m.MatchingTarget.IsUnknown() {
		matchingTarget := m.MatchingTarget.ValueString()
		selectors := map[string]types.List{
			"network_ids": m.NetworkIDs,
			"client_macs": m.ClientMACs,
			"ips":         m.IPs,
			"web_domains": m.WebDomains,
		}
		for field, val := range selectors {
			if val.IsNull() || val.IsUnknown() || len(val.Elements()) == 0 {
				continue
			}
			if !endpointOwnsSelector(matchingTarget, field) {
				resp.Diagnostics.AddAttributeError(
					req.Path.AtName(field),
					"Inactive Selector Configured",
					fmt.Sprintf(
						"%q is configured but matching_target is %q, which does not use it. "+
							"Remove %q or change matching_target.",
						field, matchingTarget, field,
					),
				)
			}
		}
		if !m.IPGroupID.IsNull() && !m.IPGroupID.IsUnknown() && m.IPGroupID.ValueString() != "" &&
			!endpointOwnsSelector(matchingTarget, "ip_group_id") {
			resp.Diagnostics.AddAttributeError(
				req.Path.AtName("ip_group_id"),
				"Inactive Selector Configured",
				fmt.Sprintf(
					"%q is configured but matching_target is %q, which does not use it (ip_group_id is only valid under IP). "+
						"Remove %q or change matching_target.",
					"ip_group_id", matchingTarget, "ip_group_id",
				),
			)
		}
	}

	if !m.PortMatchingType.IsNull() && !m.PortMatchingType.IsUnknown() {
		portMatchingType := m.PortMatchingType.ValueString()
		ports := map[string]types.String{
			"port":          m.Port,
			"port_group_id": m.PortGroupID,
		}
		for field, val := range ports {
			if val.IsNull() || val.IsUnknown() || val.ValueString() == "" {
				continue
			}
			if !endpointOwnsPortField(portMatchingType, field) {
				resp.Diagnostics.AddAttributeError(
					req.Path.AtName(field),
					"Inactive Port Field Configured",
					fmt.Sprintf(
						"%q is configured but port_matching_type is %q, which does not use it. "+
							"Remove %q or change port_matching_type.",
						field, portMatchingType, field,
					),
				)
			}
		}
	}
}

func endpointModelToSource(
	ctx context.Context,
	m firewallPolicyEndpointModel,
	diags *diag.Diagnostics,
) *unifi.FirewallPolicySource {
	matchingTarget := m.MatchingTarget.ValueString()
	portMatchingType := m.PortMatchingType.ValueString()

	// ip_group_id is gated the same as the other selectors (owned by IP
	// only) BEFORE it is handed to firewallPolicyMatchingTargetType, so a
	// stale group reference from a prior IP-targeted state cannot force
	// matching_target_type back to OBJECT on an unrelated transition
	// (design doc's ip_group_id correctness note).
	ipGroupID := ""
	if endpointOwnsSelector(matchingTarget, "ip_group_id") {
		ipGroupID = m.IPGroupID.ValueString()
	}

	ep := &unifi.FirewallPolicySource{
		ZoneID:         m.ZoneID.ValueString(),
		MatchingTarget: matchingTarget,
		MatchingTargetType: firewallPolicyMatchingTargetType(
			matchingTarget, m.MatchingTargetType.ValueString(),
			ipGroupID,
		),
		PortMatchingType: portMatchingType,
		IPGroupID:        ipGroupID,
	}
	// Only the active port_matching_type's field is sent; the other is
	// cleared regardless of what is left over in the model from a prior
	// discriminator value (#4.3).
	if endpointOwnsPortField(portMatchingType, "port") {
		ep.Port = m.Port.ValueString()
	}
	if endpointOwnsPortField(portMatchingType, "port_group_id") {
		ep.PortGroupID = m.PortGroupID.ValueString()
	}

	// Only the active matching_target's selector is sent; every other
	// selector is cleared regardless of what is left over in the model
	// from a prior discriminator value (#4.3).
	if endpointOwnsSelector(matchingTarget, "ips") &&
		!m.IPs.IsNull() && !m.IPs.IsUnknown() {
		diags.Append(m.IPs.ElementsAs(ctx, &ep.IPs, false)...)
	}
	if endpointOwnsSelector(matchingTarget, "network_ids") &&
		!m.NetworkIDs.IsNull() && !m.NetworkIDs.IsUnknown() {
		diags.Append(m.NetworkIDs.ElementsAs(ctx, &ep.NetworkIDs, false)...)
	}
	if endpointOwnsSelector(matchingTarget, "client_macs") &&
		!m.ClientMACs.IsNull() && !m.ClientMACs.IsUnknown() {
		diags.Append(m.ClientMACs.ElementsAs(ctx, &ep.ClientMACs, false)...)
	}
	if endpointOwnsSelector(matchingTarget, "web_domains") &&
		!m.WebDomains.IsNull() && !m.WebDomains.IsUnknown() {
		diags.Append(m.WebDomains.ElementsAs(ctx, &ep.WebDomains, false)...)
	}
	return ep
}

func endpointModelToDestination(
	ctx context.Context,
	m firewallPolicyEndpointModel,
	diags *diag.Diagnostics,
) *unifi.FirewallPolicyDestination {
	matchingTarget := m.MatchingTarget.ValueString()
	portMatchingType := m.PortMatchingType.ValueString()

	// See endpointModelToSource: ip_group_id is gated identically before
	// being handed to firewallPolicyMatchingTargetType.
	ipGroupID := ""
	if endpointOwnsSelector(matchingTarget, "ip_group_id") {
		ipGroupID = m.IPGroupID.ValueString()
	}

	ep := &unifi.FirewallPolicyDestination{
		ZoneID:         m.ZoneID.ValueString(),
		MatchingTarget: matchingTarget,
		MatchingTargetType: firewallPolicyMatchingTargetType(
			matchingTarget, m.MatchingTargetType.ValueString(),
			ipGroupID,
		),
		PortMatchingType: portMatchingType,
		IPGroupID:        ipGroupID,
	}
	if endpointOwnsPortField(portMatchingType, "port") {
		ep.Port = m.Port.ValueString()
	}
	if endpointOwnsPortField(portMatchingType, "port_group_id") {
		ep.PortGroupID = m.PortGroupID.ValueString()
	}

	if endpointOwnsSelector(matchingTarget, "ips") &&
		!m.IPs.IsNull() && !m.IPs.IsUnknown() {
		diags.Append(m.IPs.ElementsAs(ctx, &ep.IPs, false)...)
	}
	if endpointOwnsSelector(matchingTarget, "network_ids") &&
		!m.NetworkIDs.IsNull() && !m.NetworkIDs.IsUnknown() {
		diags.Append(m.NetworkIDs.ElementsAs(ctx, &ep.NetworkIDs, false)...)
	}
	if endpointOwnsSelector(matchingTarget, "client_macs") &&
		!m.ClientMACs.IsNull() && !m.ClientMACs.IsUnknown() {
		diags.Append(m.ClientMACs.ElementsAs(ctx, &ep.ClientMACs, false)...)
	}
	if endpointOwnsSelector(matchingTarget, "web_domains") &&
		!m.WebDomains.IsNull() && !m.WebDomains.IsUnknown() {
		diags.Append(m.WebDomains.ElementsAs(ctx, &ep.WebDomains, false)...)
	}
	return ep
}

// endpointMatchingTargetType extracts the matching_target_type out of a
// source/destination object, or a null string if the object is null/unknown.
func endpointMatchingTargetType(
	ctx context.Context,
	obj types.Object,
	diags *diag.Diagnostics,
) types.String {
	if obj.IsNull() || obj.IsUnknown() {
		return types.StringNull()
	}
	var m firewallPolicyEndpointModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	return m.MatchingTargetType
}

// withMatchingTargetType returns obj with its matching_target_type replaced by
// mtt, leaving every other attribute untouched.
func withMatchingTargetType(
	ctx context.Context,
	obj types.Object,
	mtt types.String,
	diags *diag.Diagnostics,
) types.Object {
	if obj.IsNull() || obj.IsUnknown() {
		return obj
	}
	var m firewallPolicyEndpointModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	m.MatchingTargetType = mtt
	newObj, d := types.ObjectValueFrom(
		ctx,
		firewallPolicyEndpointModel{}.AttributeTypes(),
		m,
	)
	diags.Append(d...)
	return newObj
}

func firewallPolicyToModel(
	ctx context.Context,
	fp *unifi.FirewallPolicy,
	model *firewallPolicyModel,
) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(fp.ID)
	model.Name = types.StringValue(fp.Name)
	model.Action = types.StringValue(fp.Action)
	model.Enabled = types.BoolValue(fp.Enabled)
	model.Protocol = types.StringValue(fp.Protocol)
	model.Description = types.StringValue(fp.Description)
	model.Logging = types.BoolValue(fp.Logging)
	model.CreateAllowRespond = types.BoolValue(fp.CreateAllowRespond)
	model.IPVersion = types.StringValue(fp.Version)
	model.ConnectionStateType = types.StringValue(fp.ConnectionStateType)
	connStates, csDiags := types.ListValueFrom(ctx, types.StringType, fp.ConnectionStates)
	diags.Append(csDiags...)
	model.ConnectionStates = connStates
	model.ICMPTypename = types.StringValue(fp.ICMPTypename)
	model.ICMPV6Typename = types.StringValue(fp.ICMPV6Typename)

	if fp.Index != nil {
		model.Index = types.Int64Value(*fp.Index)
	}

	if fp.Source != nil {
		srcModel := apiSourceToEndpointModel(ctx, fp.Source, &diags)
		srcObj, d := types.ObjectValueFrom(
			ctx,
			firewallPolicyEndpointModel{}.AttributeTypes(),
			srcModel,
		)
		diags.Append(d...)
		model.Source = srcObj
	}

	if fp.Destination != nil {
		dstModel := apiDestinationToEndpointModel(ctx, fp.Destination, &diags)
		dstObj, d := types.ObjectValueFrom(
			ctx,
			firewallPolicyEndpointModel{}.AttributeTypes(),
			dstModel,
		)
		diags.Append(d...)
		model.Destination = dstObj
	}

	return diags
}

func apiSourceToEndpointModel(
	ctx context.Context,
	src *unifi.FirewallPolicySource,
	diags *diag.Diagnostics,
) firewallPolicyEndpointModel {
	m := firewallPolicyEndpointModel{
		ZoneID:             types.StringValue(src.ZoneID),
		MatchingTarget:     types.StringValue(src.MatchingTarget),
		MatchingTargetType: types.StringValue(src.MatchingTargetType),
		PortGroupID:        types.StringValue(src.PortGroupID),
		PortMatchingType:   types.StringValue(src.PortMatchingType),
	}
	if endpointOwnsPortField(src.PortMatchingType, "port") {
		m.Port = portToStringValue(src.Port)
	} else {
		m.Port = types.StringNull()
	}
	if endpointOwnsSelector(src.MatchingTarget, "ip_group_id") {
		m.IPGroupID = types.StringValue(src.IPGroupID)
	} else {
		m.IPGroupID = types.StringValue("")
	}

	var networkIDs []string
	if endpointOwnsSelector(src.MatchingTarget, "network_ids") {
		networkIDs = src.NetworkIDs
	}
	nl, nd := types.ListValueFrom(ctx, types.StringType, networkIDs)
	diags.Append(nd...)
	m.NetworkIDs = nl

	var clientMACs []string
	if endpointOwnsSelector(src.MatchingTarget, "client_macs") {
		clientMACs = src.ClientMACs
	}
	cl, cd := types.ListValueFrom(ctx, types.StringType, clientMACs)
	diags.Append(cd...)
	m.ClientMACs = cl

	var ips []string
	if endpointOwnsSelector(src.MatchingTarget, "ips") {
		ips = src.IPs
	}
	il, id := types.ListValueFrom(ctx, types.StringType, ips)
	diags.Append(id...)
	m.IPs = il

	var webDomains []string
	if endpointOwnsSelector(src.MatchingTarget, "web_domains") {
		webDomains = src.WebDomains
	}
	wl, wd := types.ListValueFrom(ctx, types.StringType, webDomains)
	diags.Append(wd...)
	m.WebDomains = wl

	return m
}

func apiDestinationToEndpointModel(
	ctx context.Context,
	dst *unifi.FirewallPolicyDestination,
	diags *diag.Diagnostics,
) firewallPolicyEndpointModel {
	m := firewallPolicyEndpointModel{
		ZoneID:             types.StringValue(dst.ZoneID),
		MatchingTarget:     types.StringValue(dst.MatchingTarget),
		MatchingTargetType: types.StringValue(dst.MatchingTargetType),
		PortGroupID:        types.StringValue(dst.PortGroupID),
		PortMatchingType:   types.StringValue(dst.PortMatchingType),
	}
	if endpointOwnsPortField(dst.PortMatchingType, "port") {
		m.Port = portToStringValue(dst.Port)
	} else {
		m.Port = types.StringNull()
	}
	if endpointOwnsSelector(dst.MatchingTarget, "ip_group_id") {
		m.IPGroupID = types.StringValue(dst.IPGroupID)
	} else {
		m.IPGroupID = types.StringValue("")
	}

	var networkIDs []string
	if endpointOwnsSelector(dst.MatchingTarget, "network_ids") {
		networkIDs = dst.NetworkIDs
	}
	nl, nd := types.ListValueFrom(ctx, types.StringType, networkIDs)
	diags.Append(nd...)
	m.NetworkIDs = nl

	var clientMACs []string
	if endpointOwnsSelector(dst.MatchingTarget, "client_macs") {
		clientMACs = dst.ClientMACs
	}
	cl, cd := types.ListValueFrom(ctx, types.StringType, clientMACs)
	diags.Append(cd...)
	m.ClientMACs = cl

	var ips []string
	if endpointOwnsSelector(dst.MatchingTarget, "ips") {
		ips = dst.IPs
	}
	il, id := types.ListValueFrom(ctx, types.StringType, ips)
	diags.Append(id...)
	m.IPs = il

	var webDomains []string
	if endpointOwnsSelector(dst.MatchingTarget, "web_domains") {
		webDomains = dst.WebDomains
	}
	wl, wd := types.ListValueFrom(ctx, types.StringType, webDomains)
	diags.Append(wd...)
	m.WebDomains = wl

	return m
}

// ---------------------------------------------------------------------------
// List resource
// ---------------------------------------------------------------------------

// firewallPolicyListToModel populates the model's schema fields directly from
// the API struct for listing. It reuses the nil-safe firewallPolicyToModel
// flatten helper (which faithfully maps the source/destination nested objects)
// and sets the site so the listed resource is self-contained.
func (r *firewallPolicyResource) firewallPolicyListToModel(
	ctx context.Context,
	api *unifi.FirewallPolicy,
	model *firewallPolicyModel,
	site string,
) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(firewallPolicyToModel(ctx, api, model)...)
	model.Site = types.StringValue(site)
	return diags
}

// ListResourceConfigSchema implements [list.ListResource].
func (r *firewallPolicyResource) ListResourceConfigSchema(
	_ context.Context,
	_ list.ListResourceSchemaRequest,
	resp *list.ListResourceSchemaResponse,
) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "List firewall policies in a site.",
		Attributes: map[string]listschema.Attribute{
			"site": listschema.StringAttribute{
				MarkdownDescription: "The name of the site to list firewall policies from.",
				Optional:            true,
			},
		},
		Blocks: map[string]listschema.Block{
			"filter": listschema.ListNestedBlock{
				NestedObject: listschema.NestedBlockObject{
					Attributes: map[string]listschema.Attribute{
						"name": listschema.StringAttribute{
							MarkdownDescription: "The name of the filter to apply. Supported values are: `name`, `action`, `enabled`.",
							Required:            true,
						},
						"value": listschema.StringAttribute{
							MarkdownDescription: "The value to filter by.",
							Required:            true,
						},
					},
				},
			},
		},
	}
}

// List implements [list.ListResource].
func (r *firewallPolicyResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config firewallPolicyListConfigModel

	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	site := config.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	// Process filter blocks.
	var filters []firewallPolicyListFilterModel
	if !config.Filter.IsNull() && !config.Filter.IsUnknown() {
		config.Filter.ElementsAs(ctx, &filters, false)
	}

	postFilters := make(map[string]string)
	for _, f := range filters {
		postFilters[f.Name.ValueString()] = f.Value.ValueString()
	}

	policies, err := r.client.ListFirewallPolicy(ctx, site)
	if err != nil {
		var d diag.Diagnostics
		d.AddError(
			"Error Listing Firewall Policies",
			"Could not list firewall policies: "+err.Error(),
		)
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for _, policy := range policies {
			// Apply name filter.
			if val, ok := postFilters["name"]; ok {
				if policy.Name != val {
					continue
				}
			}

			// Apply action filter.
			if val, ok := postFilters["action"]; ok {
				if policy.Action != val {
					continue
				}
			}

			// Apply enabled filter.
			if val, ok := postFilters["enabled"]; ok {
				enabled := fmt.Sprintf("%t", policy.Enabled)
				if enabled != val {
					continue
				}
			}

			result := req.NewListResult(ctx)

			// Display name: prefer name, fall back to ID.
			if policy.Name != "" {
				result.DisplayName = policy.Name
			} else {
				result.DisplayName = policy.ID
			}

			// Set identity.
			result.Diagnostics.Append(
				result.Identity.SetAttribute(
					ctx,
					path.Root("id"),
					types.StringValue(policy.ID),
				)...,
			)

			// Convert to model.
			p := policy
			var model firewallPolicyModel
			result.Diagnostics.Append(r.firewallPolicyListToModel(ctx, &p, &model, site)...)
			if !result.Diagnostics.HasError() {
				model.Timeouts = timeoutsNullValue()
				result.Diagnostics.Append(result.Resource.Set(ctx, model)...)
			}

			if !push(result) {
				return
			}
		}
	}
}
