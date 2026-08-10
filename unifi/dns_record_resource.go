package unifi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_dns_record"
	resource_dns_record "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dns_record"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                 = &dnsRecordFrameworkResource{}
	_ resource.ResourceWithImportState  = &dnsRecordFrameworkResource{}
	_ resource.ResourceWithIdentity     = &dnsRecordFrameworkResource{}
	_ resource.ResourceWithUpgradeState = &dnsRecordFrameworkResource{}
)

// Ensure provider defined types fully satisfy list interfaces.
var (
	_ list.ListResource              = &dnsRecordFrameworkResource{}
	_ list.ListResourceWithConfigure = &dnsRecordFrameworkResource{}
)

func NewDNSRecordFrameworkResource() resource.Resource {
	return &dnsRecordFrameworkResource{}
}

func NewDNSRecordListResource() list.ListResource {
	return &dnsRecordFrameworkResource{}
}

// dnsRecordFrameworkResource defines the resource implementation.
type dnsRecordFrameworkResource struct {
	backend     dnsRecordBackend
	defaultSite string
}

// dnsRecordFrameworkResourceModel describes the resource data model.
type dnsRecordFrameworkResourceModel struct {
	ID         types.String         `tfsdk:"id"`
	Site       types.String         `tfsdk:"site"`
	Name       types.String         `tfsdk:"name"`
	Enabled    types.Bool           `tfsdk:"enabled"`
	Port       types.Int64          `tfsdk:"port"`
	Priority   types.Int64          `tfsdk:"priority"`
	RecordType types.String         `tfsdk:"record_type"`
	TTL        timetypes.GoDuration `tfsdk:"ttl"`
	Value      types.String         `tfsdk:"value"`
	Weight     types.Int64          `tfsdk:"weight"`
	Timeouts   timeouts.Value       `tfsdk:"timeouts"`
}

// dnsRecordFrameworkListConfigModel describes the list configuration model.
type dnsRecordFrameworkListConfigModel struct {
	Site   types.String `tfsdk:"site"`
	Filter types.List   `tfsdk:"filter"`
}

// dnsRecordFrameworkListFilterModel represents a single name/value filter entry.
type dnsRecordFrameworkListFilterModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

func (r *dnsRecordFrameworkResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_dns_record"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *dnsRecordFrameworkResource) IdentitySchema(
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

func (r *dnsRecordFrameworkResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_dns_record.DnsRecordResourceSchema(ctx)
	// v1: ttl changed from Int64 (seconds) to a GoDuration string.
	resp.Schema.Version = 1
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// UpgradeState migrates v0 state (ttl stored as integer seconds) to v1
// (a GoDuration string).
func (r *dnsRecordFrameworkResource) UpgradeState(
	ctx context.Context,
) map[int64]resource.StateUpgrader {
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	schemaType := schemaResp.Schema.Type().TerraformType(ctx)

	return map[int64]resource.StateUpgrader{
		0: {
			StateUpgrader: func(
				ctx context.Context,
				req resource.UpgradeStateRequest,
				resp *resource.UpgradeStateResponse,
			) {
				if req.RawState == nil {
					return
				}
				dv, err := util.UpgradeDurationRawState(
					schemaType,
					req.RawState.JSON,
					func(state map[string]any) {
						util.SetDurationField(state, "ttl", time.Second)
					},
				)
				if err != nil {
					resp.Diagnostics.AddError("Failed to upgrade DNS record state", err.Error())
					return
				}
				resp.DynamicValue = dv
			},
		},
	}
}

func (r *dnsRecordFrameworkResource) Configure(
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

	r.backend = newPrivateDNSRecordBackend(client.ApiClient)
	r.defaultSite = client.Site
}

func (r *dnsRecordFrameworkResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data dnsRecordFrameworkResourceModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, timeoutDiags := data.Timeouts.Create(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	site := data.Site.ValueString()
	if site == "" {
		site = r.defaultSite
	}

	// Create the DNS record
	createdDNSRecord, err := r.backend.Create(ctx, site, r.modelToDNSRecordIntent(ctx, &data))
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Dns Record",
			err.Error(),
		)
		return
	}

	// Convert back to model
	r.dnsRecordToModel(ctx, createdDNSRecord, &data, site)

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), data.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *dnsRecordFrameworkResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data dnsRecordFrameworkResourceModel

	// Read Terraform prior state data into the model
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

	site := data.Site.ValueString()
	if site == "" {
		site = r.defaultSite
	}

	// Get the DNS record from the API
	dnsRecord, err := r.backend.Read(ctx, site, data.ID.ValueString())
	if err != nil {
		var notFound *unifi.NotFoundError
		if errors.As(err, &notFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Dns Record",
			"Could not read DNS record with ID "+data.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	// Convert to model
	r.dnsRecordToModel(ctx, dnsRecord, &data, site)

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), data.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *dnsRecordFrameworkResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var state dnsRecordFrameworkResourceModel
	var plan dnsRecordFrameworkResourceModel

	// Step 1: Read the current state (which already contains API values from previous reads)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read the plan data
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

	// Step 2: Apply the plan changes to the state object
	r.applyPlanToState(ctx, &plan, &state)

	site := state.Site.ValueString()
	if site == "" {
		site = r.defaultSite
	}

	// Step 3: Send only the managed fields present in the plan.
	patch := r.modelToDNSRecordPatch(ctx, &plan, &state)
	updatedDNSRecord, err := r.backend.Update(ctx, site, patch)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Dns Record",
			err.Error(),
		)
		return
	}

	// Step 4: Update state with API response
	r.dnsRecordToModel(ctx, updatedDNSRecord, &state, site)

	state.Timeouts = plan.Timeouts

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dnsRecordFrameworkResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data dnsRecordFrameworkResourceModel

	// Read Terraform prior state data into the model
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

	site := data.Site.ValueString()
	if site == "" {
		site = r.defaultSite
	}

	// Delete the DNS record
	err := r.backend.Delete(ctx, site, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Deleting Dns Record",
			err.Error(),
		)
		return
	}
}

func (r *dnsRecordFrameworkResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	// Import format: "site:id" or just "id" for default site
	idParts := strings.Split(req.ID, ":")

	if len(idParts) == 2 {
		// site:id format
		site := idParts[0]
		id := idParts[1]

		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), site)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
		return
	}

	if len(idParts) == 1 {
		// Just id, use default site
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
		return
	}

	resp.Diagnostics.AddError(
		"Invalid Import ID",
		"Import ID must be in format 'site:id' or 'id'",
	)
}

// applyPlanToState merges plan values into state, preserving state values where plan is null/unknown.
func (r *dnsRecordFrameworkResource) applyPlanToState(
	_ context.Context,
	plan *dnsRecordFrameworkResourceModel,
	state *dnsRecordFrameworkResourceModel,
) {
	// Apply plan values to state, but only if plan value is not null/unknown
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		state.Name = plan.Name
	}
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		state.Enabled = plan.Enabled
	}
	if !plan.Port.IsNull() && !plan.Port.IsUnknown() {
		state.Port = plan.Port
	}
	if !plan.Priority.IsNull() && !plan.Priority.IsUnknown() {
		state.Priority = plan.Priority
	}
	if !plan.RecordType.IsNull() && !plan.RecordType.IsUnknown() {
		state.RecordType = plan.RecordType
	}
	if !plan.TTL.IsNull() && !plan.TTL.IsUnknown() {
		state.TTL = plan.TTL
	}
	if !plan.Value.IsNull() && !plan.Value.IsUnknown() {
		state.Value = plan.Value
	}
	if !plan.Weight.IsNull() && !plan.Weight.IsUnknown() {
		state.Weight = plan.Weight
	}
}

// modelToDNSRecordIntent converts the Terraform model to provider-owned API intent.
func (r *dnsRecordFrameworkResource) modelToDNSRecordIntent(
	_ context.Context,
	model *dnsRecordFrameworkResourceModel,
) dnsRecordIntent {
	intent := dnsRecordIntent{
		Name:  model.Name.ValueString(),
		Value: model.Value.ValueString(),
	}

	if !model.Enabled.IsNull() && !model.Enabled.IsUnknown() {
		intent.Enabled = model.Enabled.ValueBool()
	}

	intent.Port = model.Port.ValueInt64Pointer()

	if !model.Priority.IsNull() && !model.Priority.IsUnknown() {
		intent.Priority = model.Priority.ValueInt64()
	}

	if !model.RecordType.IsNull() && !model.RecordType.IsUnknown() {
		intent.RecordType = model.RecordType.ValueString()
	}

	if !model.TTL.IsNull() && !model.TTL.IsUnknown() {
		intent.TTL = util.DurationUnits(model.TTL, time.Second)
	}

	if !model.Weight.IsNull() && !model.Weight.IsUnknown() {
		intent.Weight = model.Weight.ValueInt64()
	}

	return intent
}

func (r *dnsRecordFrameworkResource) modelToDNSRecordPatch(
	ctx context.Context,
	plan *dnsRecordFrameworkResourceModel,
	state *dnsRecordFrameworkResourceModel,
) dnsRecordPatch {
	patch := dnsRecordPatch{
		ID:     state.ID.ValueString(),
		Values: r.modelToDNSRecordIntent(ctx, state),
		Fields: make([]dnsRecordField, 0, 8),
	}
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		patch.Fields = append(patch.Fields, dnsRecordFieldEnabled)
	}
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		patch.Fields = append(patch.Fields, dnsRecordFieldName)
	}
	if !plan.Port.IsNull() && !plan.Port.IsUnknown() {
		patch.Fields = append(patch.Fields, dnsRecordFieldPort)
	}
	if !plan.Priority.IsNull() && !plan.Priority.IsUnknown() {
		patch.Fields = append(patch.Fields, dnsRecordFieldPriority)
	}
	if !plan.RecordType.IsNull() && !plan.RecordType.IsUnknown() {
		patch.Fields = append(patch.Fields, dnsRecordFieldRecordType)
	}
	if !plan.TTL.IsNull() && !plan.TTL.IsUnknown() {
		patch.Fields = append(patch.Fields, dnsRecordFieldTTL)
	}
	if !plan.Value.IsNull() && !plan.Value.IsUnknown() {
		patch.Fields = append(patch.Fields, dnsRecordFieldValue)
	}
	if !plan.Weight.IsNull() && !plan.Weight.IsUnknown() {
		patch.Fields = append(patch.Fields, dnsRecordFieldWeight)
	}
	return patch
}

// dnsRecordToModel converts normalized controller state to the Terraform model.
func (r *dnsRecordFrameworkResource) dnsRecordToModel(
	_ context.Context,
	dnsRecord dnsRecordModel,
	model *dnsRecordFrameworkResourceModel,
	site string,
) {
	model.ID = types.StringValue(dnsRecord.ID)
	model.Site = types.StringValue(site)
	model.Name = types.StringValue(dnsRecord.Name)
	model.Value = types.StringValue(dnsRecord.Value)

	model.Enabled = types.BoolValue(dnsRecord.Enabled)

	if dnsRecord.Port != nil && *dnsRecord.Port != 0 {
		model.Port = types.Int64PointerValue(dnsRecord.Port)
	} else {
		model.Port = types.Int64Null()
	}

	if dnsRecord.Priority != 0 {
		model.Priority = types.Int64Value(dnsRecord.Priority)
	} else {
		model.Priority = types.Int64Null()
	}

	if dnsRecord.RecordType != "" {
		model.RecordType = types.StringValue(dnsRecord.RecordType)
	} else {
		model.RecordType = types.StringNull()
	}

	if dnsRecord.TTL != 0 {
		model.TTL = util.DurationValue(dnsRecord.TTL, time.Second)
	} else {
		model.TTL = timetypes.NewGoDurationNull()
	}

	if dnsRecord.Weight != 0 {
		model.Weight = types.Int64Value(dnsRecord.Weight)
	} else {
		model.Weight = types.Int64Null()
	}
}

// ListResourceConfigSchema implements [list.ListResource].
func (r *dnsRecordFrameworkResource) ListResourceConfigSchema(
	ctx context.Context,
	_ list.ListResourceSchemaRequest,
	resp *list.ListResourceSchemaResponse,
) {
	resp.Schema = listresource_dns_record.DnsRecordListResourceSchema(ctx)
}

// List implements [list.ListResource].
func (r *dnsRecordFrameworkResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config dnsRecordFrameworkListConfigModel

	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	site := config.Site.ValueString()
	if site == "" {
		site = r.defaultSite
	}

	// Process filter blocks.
	var filters []dnsRecordFrameworkListFilterModel
	if !config.Filter.IsNull() && !config.Filter.IsUnknown() {
		config.Filter.ElementsAs(ctx, &filters, false)
	}

	postFilters := make(map[string]string)
	for _, f := range filters {
		postFilters[f.Name.ValueString()] = f.Value.ValueString()
	}

	records, err := r.backend.List(ctx, site)
	if err != nil {
		var d diag.Diagnostics
		d.AddError("Error Listing DNS Records", "Could not list DNS records: "+err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for _, record := range records {
			// Apply name filter.
			if val, ok := postFilters["name"]; ok {
				if record.Name != val {
					continue
				}
			}

			// Apply record_type filter.
			if val, ok := postFilters["record_type"]; ok {
				if record.RecordType != val {
					continue
				}
			}

			// Apply enabled filter.
			if val, ok := postFilters["enabled"]; ok {
				enabled := fmt.Sprintf("%t", record.Enabled)
				if enabled != val {
					continue
				}
			}

			result := req.NewListResult(ctx)

			// Display name: prefer key, fall back to ID.
			if record.Name != "" {
				result.DisplayName = record.Name
			} else {
				result.DisplayName = record.ID
			}

			// Set identity.
			result.Diagnostics.Append(
				result.Identity.SetAttribute(
					ctx,
					path.Root("id"),
					types.StringValue(record.ID),
				)...,
			)

			// Convert to model.
			var model dnsRecordFrameworkResourceModel
			r.dnsRecordToModel(ctx, record, &model, site)
			model.Timeouts = timeoutsNullValue()
			result.Diagnostics.Append(result.Resource.Set(ctx, model)...)

			if !push(result) {
				return
			}
		}
	}
}
