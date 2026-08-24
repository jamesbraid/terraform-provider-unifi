package unifi

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_power_supervisor"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_power_supervisor"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                 = &powerSupervisorResource{}
	_ resource.ResourceWithImportState  = &powerSupervisorResource{}
	_ resource.ResourceWithIdentity     = &powerSupervisorResource{}
	_ resource.ResourceWithUpgradeState = &powerSupervisorResource{}
)

// Ensure provider defined types fully satisfy list interfaces.
var (
	_ list.ListResource              = &powerSupervisorResource{}
	_ list.ListResourceWithConfigure = &powerSupervisorResource{}
)

func NewPowerSupervisorResource() resource.Resource {
	return &powerSupervisorResource{}
}

func NewPowerSupervisorListResource() list.ListResource {
	return &powerSupervisorResource{}
}

// powerSupervisorResource defines the resource implementation.
type powerSupervisorResource struct {
	client *Client
}

// powerSupervisorResourceModel describes the resource data model.
type powerSupervisorResourceModel struct {
	ID                  types.String         `tfsdk:"id"`
	Site                types.String         `tfsdk:"site"`
	DeviceMAC           hwtypes.MACAddress   `tfsdk:"device_mac"`
	Enabled             types.Bool           `tfsdk:"enabled"`
	HeartbeatInterval   timetypes.GoDuration `tfsdk:"heartbeat_interval"`
	SilenceThreshold    timetypes.GoDuration `tfsdk:"silence_threshold"`
	PowerOffDuration    timetypes.GoDuration `tfsdk:"power_off_duration"`
	ConsecutiveFailures types.Int64          `tfsdk:"consecutive_failures"`
	PowerSources        types.List           `tfsdk:"power_sources"`
	Timeouts            timeouts.Value       `tfsdk:"timeouts"`
}

// powerSupervisorListConfigModel describes the list configuration model.
type powerSupervisorListConfigModel struct {
	Site   types.String `tfsdk:"site"`
	Filter types.List   `tfsdk:"filter"`
}

// powerSupervisorListFilterModel represents a single name/value filter entry.
type powerSupervisorListFilterModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

// powerSourceAttrTypes is the object schema of a resolved upstream power source.
func powerSourceAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"client_psu_index":   types.Int64Type,
		"power_source_index": types.Int64Type,
		"power_source_mac":   types.StringType,
		"power_source_type":  types.StringType,
	}
}

func (r *powerSupervisorResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_power_supervisor"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *powerSupervisorResource) IdentitySchema(
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

func (r *powerSupervisorResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_power_supervisor.PowerSupervisorResourceSchema(ctx)
	// v1: the three settings durations changed from Int64 seconds to
	// GoDuration strings. See UpgradeState.
	resp.Schema.Version = 1
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

// UpgradeState migrates v0 state (heartbeat_interval/silence_threshold/
// power_off_duration stored as integer seconds) to v1 (GoDuration strings).
func (r *powerSupervisorResource) UpgradeState(
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
						util.SetDurationField(state, "heartbeat_interval", time.Second)
						util.SetDurationField(state, "silence_threshold", time.Second)
						util.SetDurationField(state, "power_off_duration", time.Second)
					},
				)
				if err != nil {
					resp.Diagnostics.AddError(
						"Failed to upgrade power supervisor state",
						err.Error(),
					)
					return
				}
				resp.DynamicValue = dv
			},
		},
	}
}

func (r *powerSupervisorResource) Configure(
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

func (r *powerSupervisorResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var data powerSupervisorResourceModel

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
		site = r.client.Site
	}

	created, err := r.client.CreatePowerSupervisor(ctx, site, r.modelToPowerSupervisor(&data))
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Power Supervisor", err.Error())
		return
	}

	resp.Diagnostics.Append(r.powerSupervisorToModel(created, &data, site)...)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), data.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *powerSupervisorResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var data powerSupervisorResourceModel

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
		site = r.client.Site
	}

	supervisor, err := r.client.GetPowerSupervisor(ctx, site, data.ID.ValueString())
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Power Supervisor",
			"Could not read power supervisor with ID "+data.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(r.powerSupervisorToModel(supervisor, &data, site)...)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), data.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *powerSupervisorResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var data powerSupervisorResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, timeoutDiags := data.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	site := data.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	supervisor := r.modelToPowerSupervisor(&data)
	supervisor.ID = data.ID.ValueString()

	updated, err := r.client.UpdatePowerSupervisor(ctx, site, supervisor)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Power Supervisor", err.Error())
		return
	}

	resp.Diagnostics.Append(r.powerSupervisorToModel(updated, &data, site)...)
	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), data.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *powerSupervisorResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var data powerSupervisorResourceModel

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
		site = r.client.Site
	}

	err := r.client.DeletePowerSupervisor(ctx, site, data.ID.ValueString())
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Power Supervisor", err.Error())
		return
	}
}

func (r *powerSupervisorResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	// Accepts "site:id", "site:mac", a bare id, or a bare MAC; a MAC contains
	// colons, so it must be detected before splitting on ":" (else "9c:05:..." parses as site "9c").
	macRE := regexp.MustCompile(`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`)
	site := r.client.Site
	identifier := req.ID
	if !macRE.MatchString(req.ID) {
		if parts := strings.SplitN(req.ID, ":", 2); len(parts) == 2 {
			site = parts[0]
			identifier = parts[1]
		}
	}

	// A MAC address identifies the supervised device; resolve it to the
	// controller id (the collection is keyed per device).
	if macRE.MatchString(identifier) {
		supervisor, err := r.client.GetPowerSupervisorByMAC(ctx, site, strings.ToLower(identifier))
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Importing Power Supervisor",
				"Could not find a power supervisor for device MAC "+identifier+": "+err.Error(),
			)
			return
		}
		identifier = supervisor.ID
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), site)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), identifier)...)
}

// modelToPowerSupervisor converts the Terraform model to the API struct. The
// controller resolves power_sources, so they are not sent.
func (r *powerSupervisorResource) modelToPowerSupervisor(
	model *powerSupervisorResourceModel,
) *unifi.PowerSupervisor {
	return &unifi.PowerSupervisor{
		ClientMAC: model.DeviceMAC.ValueString(),
		Enabled:   model.Enabled.ValueBool(),
		Settings: unifi.PowerSupervisorSettings{
			HeartbeatInterval: int(util.DurationUnits(model.HeartbeatInterval, time.Second)),
			SilenceThreshold:  int(util.DurationUnits(model.SilenceThreshold, time.Second)),
			PowerOffDuration:  int(util.DurationUnits(model.PowerOffDuration, time.Second)),
		},
		PowerSources: []unifi.PowerSupervisorSource{},
	}
}

// powerSupervisorToModel converts the API struct to the Terraform model.
func (r *powerSupervisorResource) powerSupervisorToModel(
	supervisor *unifi.PowerSupervisor,
	model *powerSupervisorResourceModel,
	site string,
) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(supervisor.ID)
	model.Site = types.StringValue(site)
	model.DeviceMAC = hwtypes.NewMACAddressValue(supervisor.ClientMAC)
	model.Enabled = types.BoolValue(supervisor.Enabled)
	model.HeartbeatInterval = util.DurationValue(
		int64(supervisor.Settings.HeartbeatInterval),
		time.Second,
	)
	model.SilenceThreshold = util.DurationValue(
		int64(supervisor.Settings.SilenceThreshold),
		time.Second,
	)
	model.PowerOffDuration = util.DurationValue(
		int64(supervisor.Settings.PowerOffDuration),
		time.Second,
	)
	model.ConsecutiveFailures = types.Int64Value(int64(supervisor.ConsecutiveFailures))

	elements := make([]attr.Value, 0, len(supervisor.PowerSources))
	for _, src := range supervisor.PowerSources {
		obj, objDiags := types.ObjectValue(powerSourceAttrTypes(), map[string]attr.Value{
			"client_psu_index":   types.Int64Value(int64(src.ClientPsuIndex)),
			"power_source_index": types.Int64Value(int64(src.PowerSourceIndex)),
			"power_source_mac":   types.StringValue(src.PowerSourceMAC),
			"power_source_type":  types.StringValue(src.PowerSourceType),
		})
		diags.Append(objDiags...)
		elements = append(elements, obj)
	}

	list, listDiags := types.ListValue(
		types.ObjectType{AttrTypes: powerSourceAttrTypes()},
		elements,
	)
	diags.Append(listDiags...)
	model.PowerSources = list

	return diags
}

// ListResourceConfigSchema implements [list.ListResource].
func (r *powerSupervisorResource) ListResourceConfigSchema(
	ctx context.Context,
	_ list.ListResourceSchemaRequest,
	resp *list.ListResourceSchemaResponse,
) {
	resp.Schema = listresource_power_supervisor.PowerSupervisorListResourceSchema(ctx)
}

// List implements [list.ListResource].
func (r *powerSupervisorResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config powerSupervisorListConfigModel

	diags := req.Config.Get(ctx, &config)
	if diags.HasError() {
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}

	site := config.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	var filters []powerSupervisorListFilterModel
	if !config.Filter.IsNull() && !config.Filter.IsUnknown() {
		config.Filter.ElementsAs(ctx, &filters, false)
	}

	postFilters := make(map[string]string)
	for _, f := range filters {
		postFilters[f.Name.ValueString()] = f.Value.ValueString()
	}

	supervisors, err := r.client.ListPowerSupervisors(ctx, site)
	if err != nil {
		var d diag.Diagnostics
		d.AddError(
			"Error Listing Power Supervisors",
			"Could not list power supervisors: "+err.Error(),
		)
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for i := range supervisors {
			supervisor := supervisors[i]

			if val, ok := postFilters["device_mac"]; ok {
				if supervisor.ClientMAC != val {
					continue
				}
			}

			result := req.NewListResult(ctx)

			if supervisor.ClientMAC != "" {
				result.DisplayName = supervisor.ClientMAC
			} else {
				result.DisplayName = supervisor.ID
			}

			result.Diagnostics.Append(
				result.Identity.SetAttribute(
					ctx,
					path.Root("id"),
					types.StringValue(supervisor.ID),
				)...,
			)

			var model powerSupervisorResourceModel
			result.Diagnostics.Append(r.powerSupervisorToModel(&supervisor, &model, site)...)
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
