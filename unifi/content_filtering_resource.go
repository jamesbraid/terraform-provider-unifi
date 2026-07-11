package unifi

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &contentFilteringResource{}
	_ resource.ResourceWithImportState = &contentFilteringResource{}
	_ resource.ResourceWithIdentity    = &contentFilteringResource{}
)

func NewContentFilteringResource() resource.Resource {
	return &contentFilteringResource{}
}

type contentFilteringResource struct {
	client *Client
}

// contentFilteringResourceModel models a v2 content-filtering policy. The
// shape mirrors the controller object exactly: name, enabled, category
// tokens, targeted clients/networks, allow/block domain lists, safe-search
// enforcement, and a schedule (only mode is modeled; ALWAYS is the only
// observed value).
type contentFilteringResourceModel struct {
	ID         types.String   `tfsdk:"id"`
	Site       types.String   `tfsdk:"site"`
	Name       types.String   `tfsdk:"name"`
	Enabled    types.Bool     `tfsdk:"enabled"`
	Categories types.Set      `tfsdk:"categories"`
	ClientMACs types.Set      `tfsdk:"client_macs"`
	NetworkIDs types.Set      `tfsdk:"network_ids"`
	AllowList  types.Set      `tfsdk:"allow_list"`
	BlockList  types.Set      `tfsdk:"block_list"`
	SafeSearch types.Set      `tfsdk:"safe_search"`
	Schedule   types.Object   `tfsdk:"schedule"`
	Timeouts   timeouts.Value `tfsdk:"timeouts"`
}

type contentFilteringScheduleModel struct {
	Mode types.String `tfsdk:"mode"`
}

var contentFilteringScheduleAttrTypes = map[string]attr.Type{
	"mode": types.StringType,
}

func (r *contentFilteringResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_content_filtering"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *contentFilteringResource) IdentitySchema(
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

func (r *contentFilteringResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a UniFi content-filtering policy (UniFi Network 9.x+, v2 API). " +
			"Content filtering blocks web content by category and by explicit " +
			"domain lists, optionally enforcing safe search, scoped to specific " +
			"clients and/or networks. Shown under Settings → Security → " +
			"Content Filtering in the UniFi UI.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the content-filtering policy.",
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
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the content-filtering policy.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the policy is enabled. Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"categories": schema.SetAttribute{
				MarkdownDescription: "Content category tokens to block (e.g. `FAMILY`, `ADVERTISEMENT`). Category tokens come from the controller's content-filtering category list.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"client_macs": schema.SetAttribute{
				MarkdownDescription: "MAC addresses of the clients the policy applies to. May be empty.",
				Optional:            true,
				Computed:            true,
				// Semantic MAC equality: AA-BB-… and aa:bb:… compare equal.
				ElementType: hwtypes.MACAddressType{},
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"network_ids": schema.SetAttribute{
				MarkdownDescription: "IDs of the networks the policy applies to. May be empty.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"allow_list": schema.SetAttribute{
				MarkdownDescription: "Domains always allowed, overriding category blocks.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"block_list": schema.SetAttribute{
				MarkdownDescription: "Domains always blocked.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"safe_search": schema.SetAttribute{
				MarkdownDescription: "Search providers to enforce safe search for. Observed values: `GOOGLE`, `YOUTUBE`, `BING`.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"schedule": schema.SingleNestedAttribute{
				MarkdownDescription: "When the policy is enforced. Only `mode` is modeled; `ALWAYS` is the only mode observed on live controllers so far.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"mode": schema.StringAttribute{
						MarkdownDescription: "The schedule mode. Defaults to `ALWAYS`.",
						Optional:            true,
						Computed:            true,
						Default:             stringdefault.StaticString("ALWAYS"),
					},
				},
			},
			"timeouts": timeouts.Attributes(
				ctx,
				timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
			),
		},
	}
}

func (r *contentFilteringResource) Configure(
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

func (r *contentFilteringResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan contentFilteringResourceModel
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

	cf, diags := modelToContentFiltering(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateContentFiltering(ctx, site, cf)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Content Filtering Policy",
			"Could not create content filtering policy: "+err.Error(),
		)
		return
	}

	timeoutsValue := plan.Timeouts
	resp.Diagnostics.Append(contentFilteringToModel(ctx, created, &plan, site)...)
	plan.Timeouts = timeoutsValue
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), plan.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *contentFilteringResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state contentFilteringResourceModel
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

	cf, err := r.client.GetContentFiltering(ctx, site, state.ID.ValueString())
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Content Filtering Policy",
			"Could not read content filtering policy "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	timeoutsValue := state.Timeouts
	resp.Diagnostics.Append(contentFilteringToModel(ctx, cf, &state, site)...)
	state.Timeouts = timeoutsValue
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *contentFilteringResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan contentFilteringResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state contentFilteringResourceModel
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

	cf, diags := modelToContentFiltering(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.client.UpdateContentFiltering(ctx, site, cf)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Content Filtering Policy",
			"Could not update content filtering policy "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	timeoutsValue := plan.Timeouts
	resp.Diagnostics.Append(contentFilteringToModel(ctx, updated, &plan, site)...)
	plan.Timeouts = timeoutsValue
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), plan.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *contentFilteringResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state contentFilteringResourceModel
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

	err := r.client.DeleteContentFiltering(ctx, site, state.ID.ValueString())
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); !ok {
			resp.Diagnostics.AddError(
				"Error Deleting Content Filtering Policy",
				"Could not delete content filtering policy "+state.ID.ValueString()+": "+err.Error(),
			)
		}
	}
}

func (r *contentFilteringResource) ImportState(
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

// setElementsOrEmpty decodes a set into a string slice, mapping null/unknown
// to an explicit empty slice: the live content-filtering objects carry every
// list key, so the PUT must too.
func setElementsOrEmpty(
	ctx context.Context,
	set types.Set,
	diags *diag.Diagnostics,
) []string {
	out := []string{}
	if !set.IsNull() && !set.IsUnknown() {
		diags.Append(set.ElementsAs(ctx, &out, false)...)
	}
	return out
}

func modelToContentFiltering(
	ctx context.Context,
	m contentFilteringResourceModel,
) (*unifi.ContentFiltering, diag.Diagnostics) {
	var diags diag.Diagnostics

	cf := &unifi.ContentFiltering{
		ID:         m.ID.ValueString(),
		Name:       m.Name.ValueString(),
		Enabled:    m.Enabled.ValueBool(),
		Categories: setElementsOrEmpty(ctx, m.Categories, &diags),
		ClientMACs: setElementsOrEmpty(ctx, m.ClientMACs, &diags),
		NetworkIDs: setElementsOrEmpty(ctx, m.NetworkIDs, &diags),
		AllowList:  setElementsOrEmpty(ctx, m.AllowList, &diags),
		BlockList:  setElementsOrEmpty(ctx, m.BlockList, &diags),
		SafeSearch: setElementsOrEmpty(ctx, m.SafeSearch, &diags),
	}

	// The controller stores MACs lowercased and colon-separated; normalize on
	// the write path (semantic equality guards the read path).
	for i, mac := range cf.ClientMACs {
		cf.ClientMACs[i] = cleanMAC(mac)
	}

	mode := "ALWAYS"
	if !m.Schedule.IsNull() && !m.Schedule.IsUnknown() {
		var sm contentFilteringScheduleModel
		diags.Append(m.Schedule.As(ctx, &sm, objectAsOptions)...)
		if !sm.Mode.IsNull() && !sm.Mode.IsUnknown() && sm.Mode.ValueString() != "" {
			mode = sm.Mode.ValueString()
		}
	}
	cf.Schedule = &unifi.ContentFilteringSchedule{Mode: mode}

	return cf, diags
}

// stringSetOrEmpty maps a string slice to a set, nil becoming an empty set so
// `x = []` round-trips cleanly instead of an empty-vs-null diff.
func stringSetOrEmpty(
	ctx context.Context,
	vals []string,
	diags *diag.Diagnostics,
) types.Set {
	if vals == nil {
		vals = []string{}
	}
	set, d := types.SetValueFrom(ctx, types.StringType, vals)
	diags.Append(d...)
	return set
}

func contentFilteringToModel(
	ctx context.Context,
	cf *unifi.ContentFiltering,
	m *contentFilteringResourceModel,
	site string,
) diag.Diagnostics {
	var diags diag.Diagnostics

	m.ID = types.StringValue(cf.ID)
	m.Site = types.StringValue(site)
	m.Name = types.StringValue(cf.Name)
	m.Enabled = types.BoolValue(cf.Enabled)
	m.Categories = stringSetOrEmpty(ctx, cf.Categories, &diags)
	m.NetworkIDs = stringSetOrEmpty(ctx, cf.NetworkIDs, &diags)
	m.AllowList = stringSetOrEmpty(ctx, cf.AllowList, &diags)
	m.BlockList = stringSetOrEmpty(ctx, cf.BlockList, &diags)
	m.SafeSearch = stringSetOrEmpty(ctx, cf.SafeSearch, &diags)

	macs := cf.ClientMACs
	if macs == nil {
		macs = []string{}
	}
	macsSet, d := types.SetValueFrom(ctx, hwtypes.MACAddressType{}, macs)
	diags.Append(d...)
	m.ClientMACs = macsSet

	if cf.Schedule != nil {
		obj, d := types.ObjectValueFrom(ctx, contentFilteringScheduleAttrTypes,
			contentFilteringScheduleModel{
				Mode: util.StringValueOrNull(cf.Schedule.Mode),
			})
		diags.Append(d...)
		m.Schedule = obj
	} else {
		m.Schedule = types.ObjectNull(contentFilteringScheduleAttrTypes)
	}

	return diags
}
