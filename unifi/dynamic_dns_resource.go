package unifi

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_dynamic_dns"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dynamic_dns"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                = &dynamicDNSResource{}
	_ resource.ResourceWithImportState = &dynamicDNSResource{}
	_ resource.ResourceWithIdentity    = &dynamicDNSResource{}
)

// Ensure provider defined types fully satisfy list interfaces.
var (
	_ list.ListResource              = &dynamicDNSResource{}
	_ list.ListResourceWithConfigure = &dynamicDNSResource{}
)

func NewDynamicDNSResource() resource.Resource {
	return &dynamicDNSResource{}
}

func NewDynamicDNSListResource() list.ListResource {
	return &dynamicDNSResource{}
}

// dynamicDNSResource defines the resource implementation.
type dynamicDNSResource struct {
	client *Client
}

// dynamicDNSResourceModel describes the resource data model.
type dynamicDNSResourceModel struct {
	ID        types.String   `tfsdk:"id"`
	Site      types.String   `tfsdk:"site"`
	Interface types.String   `tfsdk:"interface"`
	Service   types.String   `tfsdk:"service"`
	HostName  types.String   `tfsdk:"host_name"`
	Server    types.String   `tfsdk:"server"`
	Login     types.String   `tfsdk:"login"`
	Password  types.String   `tfsdk:"password"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

// dynamicDNSResourceIdentityModel describes the resource identity data model.
type dynamicDNSResourceIdentityModel struct {
	ID   types.String `tfsdk:"id"`
	Site types.String `tfsdk:"site"`
}

// dynamicDNSListConfigModel describes the list configuration model.
type dynamicDNSListConfigModel struct {
	Site   types.String `tfsdk:"site"`
	Filter types.List   `tfsdk:"filter"`
}

// dynamicDNSListFilterModel represents a single name/value filter entry.
type dynamicDNSListFilterModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

func (r *dynamicDNSResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_dynamic_dns"
}

func (r *dynamicDNSResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_dynamic_dns.DynamicDnsResourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

func (r *dynamicDNSResource) IdentitySchema(
	ctx context.Context,
	req resource.IdentitySchemaRequest,
	resp *resource.IdentitySchemaResponse,
) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{
				RequiredForImport: true,
			},
			"site": identityschema.StringAttribute{
				OptionalForImport: true,
			},
		},
	}
}

func (r *dynamicDNSResource) Configure(
	ctx context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}

	r.client = client
}

func (r *dynamicDNSResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data dynamicDNSResourceModel

	if resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...); resp.Diagnostics.HasError() {
		return
	}

	createTimeout, timeoutDiags := data.Timeouts.Create(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	site := r.client.Site
	if !data.Site.IsNull() && !data.Site.IsUnknown() {
		site = data.Site.ValueString()
	}

	dynamicDNS := r.modelToDynamicDNS(ctx, &data)

	createdDynamicDNS, err := r.client.CreateDynamicDNS(ctx, site, dynamicDNS)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Dynamic DNS",
			err.Error(),
		)
		return
	}

	r.dynamicDNSToModel(ctx, createdDynamicDNS, &data, site)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

	identity := dynamicDNSResourceIdentityModel{
		ID:   types.StringValue(createdDynamicDNS.ID),
		Site: types.StringValue(site),
	}
	resp.Diagnostics.Append(resp.Identity.Set(ctx, identity)...)
}

func (r *dynamicDNSResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data dynamicDNSResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, timeoutDiags := data.Timeouts.Read(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	// Read identity, falling back to state for resources created before identity support
	var identity dynamicDNSResourceIdentityModel
	if !req.Identity.Raw.IsNull() {
		resp.Diagnostics.Append(req.Identity.Get(ctx, &identity)...)
		if resp.Diagnostics.HasError() {
			return
		}
	} else {
		identity.ID = data.ID
		identity.Site = data.Site
	}

	id := identity.ID.ValueString()
	site := identity.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	dynamicDNS, err := r.client.GetDynamicDNS(ctx, site, id)
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Dynamic DNS",
			"Could not read dynamic DNS with ID "+id+": "+err.Error(),
		)
		return
	}

	r.dynamicDNSToModel(ctx, dynamicDNS, &data, site)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

	// Re-set identity (should be unchanged).
	resp.Diagnostics.Append(resp.Identity.Set(ctx, identity)...)
}

func (r *dynamicDNSResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var state dynamicDNSResourceModel
	var plan dynamicDNSResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
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

	// Read identity, falling back to state for resources created before identity support
	var identity dynamicDNSResourceIdentityModel
	if !req.Identity.Raw.IsNull() {
		resp.Diagnostics.Append(req.Identity.Get(ctx, &identity)...)
		if resp.Diagnostics.HasError() {
			return
		}
	} else {
		identity.ID = state.ID
		identity.Site = state.Site
	}

	r.applyPlanToState(ctx, &plan, &state)

	id := identity.ID.ValueString()
	site := identity.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	dynamicDNS := r.modelToDynamicDNS(ctx, &state)
	dynamicDNS.ID = id
	dynamicDNS.SiteID = site

	updatedDynamicDNS, err := r.client.UpdateDynamicDNS(ctx, site, dynamicDNS)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Dynamic DNS",
			err.Error(),
		)
		return
	}

	r.dynamicDNSToModel(ctx, updatedDynamicDNS, &state, site)

	state.Timeouts = plan.Timeouts

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)

	// Identity should not change during update.
	resp.Diagnostics.Append(resp.Identity.Set(ctx, identity)...)
}

func (r *dynamicDNSResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data dynamicDNSResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, timeoutDiags := data.Timeouts.Delete(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	// Read identity, falling back to state for resources created before identity support
	var identity dynamicDNSResourceIdentityModel
	if !req.Identity.Raw.IsNull() {
		resp.Diagnostics.Append(req.Identity.Get(ctx, &identity)...)
		if resp.Diagnostics.HasError() {
			return
		}
	} else {
		identity.ID = data.ID
		identity.Site = data.Site
	}

	id := identity.ID.ValueString()
	site := identity.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	err := r.client.DeleteDynamicDNS(ctx, site, id)
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting Dynamic DNS",
			err.Error(),
		)
		return
	}
}

func (r *dynamicDNSResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughWithIdentity(
		ctx,
		path.Root("id"),
		path.Root("id"),
		req,
		resp,
	)
}

// applyPlanToState merges plan values into state, preserving state values where plan is null/unknown.
func (r *dynamicDNSResource) applyPlanToState(
	_ context.Context,
	plan *dynamicDNSResourceModel,
	state *dynamicDNSResourceModel,
) {
	// Apply plan values to state, but only if plan value is not null/unknown
	if !plan.Interface.IsNull() && !plan.Interface.IsUnknown() {
		state.Interface = plan.Interface
	}
	if !plan.Service.IsNull() && !plan.Service.IsUnknown() {
		state.Service = plan.Service
	}
	if !plan.HostName.IsNull() && !plan.HostName.IsUnknown() {
		state.HostName = plan.HostName
	}
	if !plan.Server.IsNull() && !plan.Server.IsUnknown() {
		state.Server = plan.Server
	}
	if !plan.Login.IsNull() && !plan.Login.IsUnknown() {
		state.Login = plan.Login
	}
	if !plan.Password.IsNull() && !plan.Password.IsUnknown() {
		state.Password = plan.Password
	}
}

// modelToDynamicDNS converts the Terraform model to the API struct.
func (r *dynamicDNSResource) modelToDynamicDNS(
	_ context.Context,
	model *dynamicDNSResourceModel,
) *unifi.DynamicDNS {
	dynamicDNS := &unifi.DynamicDNS{
		ID:        model.ID.ValueString(),
		Interface: model.Interface.ValueString(),
		Service:   model.Service.ValueString(),
		HostName:  model.HostName.ValueString(),
	}

	if !model.Server.IsNull() {
		dynamicDNS.Server = model.Server.ValueString()
	}
	if !model.Login.IsNull() {
		dynamicDNS.Login = model.Login.ValueString()
	}
	if !model.Password.IsNull() {
		dynamicDNS.Password = model.Password.ValueString()
	}

	return dynamicDNS
}

// dynamicDNSToModel converts the API struct to the Terraform model.
func (r *dynamicDNSResource) dynamicDNSToModel(
	_ context.Context,
	dynamicDNS *unifi.DynamicDNS,
	model *dynamicDNSResourceModel,
	site string,
) {
	model.ID = types.StringValue(dynamicDNS.ID)
	model.Interface = types.StringValue(dynamicDNS.Interface)
	model.Service = types.StringValue(dynamicDNS.Service)
	model.HostName = types.StringValue(dynamicDNS.HostName)

	model.Site = util.StringValueOrNull(site)
	model.Server = util.StringValueOrNull(dynamicDNS.Server)
	model.Login = util.StringValueOrNull(dynamicDNS.Login)
	model.Password = util.StringValueOrNull(dynamicDNS.Password)
}

// ListResourceConfigSchema implements [list.ListResource].
func (r *dynamicDNSResource) ListResourceConfigSchema(
	ctx context.Context,
	_ list.ListResourceSchemaRequest,
	resp *list.ListResourceSchemaResponse,
) {
	resp.Schema = listresource_dynamic_dns.DynamicDnsListResourceSchema(ctx)
}

// List implements [list.ListResource].
func (r *dynamicDNSResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config dynamicDNSListConfigModel

	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	site := config.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	var filters []dynamicDNSListFilterModel
	if !config.Filter.IsNull() && !config.Filter.IsUnknown() {
		config.Filter.ElementsAs(ctx, &filters, false)
	}

	postFilters := make(map[string]string)
	for _, f := range filters {
		postFilters[f.Name.ValueString()] = f.Value.ValueString()
	}

	entries, err := r.client.ListDynamicDNS(ctx, site)
	if err != nil {
		var d diag.Diagnostics
		d.AddError(
			"Error Listing Dynamic DNS",
			"Could not list dynamic DNS configurations: "+err.Error(),
		)
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for _, entry := range entries {
			if val, ok := postFilters["host_name"]; ok {
				if entry.HostName != val {
					continue
				}
			}

			if val, ok := postFilters["service"]; ok {
				if entry.Service != val {
					continue
				}
			}

			result := req.NewListResult(ctx)

			if entry.HostName != "" {
				result.DisplayName = entry.HostName
			} else {
				result.DisplayName = entry.ID
			}

			result.Diagnostics.Append(
				result.Identity.SetAttribute(
					ctx,
					path.Root("id"),
					types.StringValue(entry.ID),
				)...,
			)
			result.Diagnostics.Append(
				result.Identity.SetAttribute(
					ctx,
					path.Root("site"),
					types.StringValue(site),
				)...,
			)

			var model dynamicDNSResourceModel
			r.dynamicDNSToModel(ctx, &entry, &model, site)
			model.Timeouts = timeoutsNullValue()
			result.Diagnostics.Append(result.Resource.Set(ctx, model)...)

			if !push(result) {
				return
			}
		}
	}
}
