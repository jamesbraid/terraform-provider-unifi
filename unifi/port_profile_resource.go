package unifi

import (
	"context"
	"fmt"
	"slices"
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
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_port_profile"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_port_profile"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                     = &portProfileResource{}
	_ resource.ResourceWithImportState      = &portProfileResource{}
	_ resource.ResourceWithIdentity         = &portProfileResource{}
	_ resource.ResourceWithUpgradeState     = &portProfileResource{}
	_ resource.ResourceWithConfigValidators = &portProfileResource{}
)

// Ensure provider defined types fully satisfy list interfaces.
var (
	_ list.ListResource              = &portProfileResource{}
	_ list.ListResourceWithConfigure = &portProfileResource{}
)

func NewPortProfileFrameworkResource() resource.Resource {
	return &portProfileResource{}
}

func NewPortProfileListResource() list.ListResource {
	return &portProfileResource{}
}

// portProfileResource defines the resource implementation.
type portProfileResource struct {
	client *Client
}

// portProfileListConfigModel describes the list configuration model.
type portProfileListConfigModel struct {
	Site   types.String `tfsdk:"site"`
	Filter types.List   `tfsdk:"filter"`
}

// portProfileListFilterModel represents a single name/value filter entry.
type portProfileListFilterModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

// portProfileResourceModel describes the resource data model.
type portProfileResourceModel struct {
	ID                         types.String         `tfsdk:"id"`
	Site                       types.String         `tfsdk:"site"`
	Autoneg                    types.Bool           `tfsdk:"autoneg"`
	Dot1XCtrl                  types.String         `tfsdk:"dot1x_ctrl"`
	Dot1XIdleTimeout           timetypes.GoDuration `tfsdk:"dot1x_idle_timeout"`
	EgressRateLimitKbps        types.Int64          `tfsdk:"egress_rate_limit_kbps"`
	EgressRateLimitKbpsEnabled types.Bool           `tfsdk:"egress_rate_limit_kbps_enabled"`
	Forward                    types.String         `tfsdk:"forward"`
	FullDuplex                 types.Bool           `tfsdk:"full_duplex"`
	Isolation                  types.Bool           `tfsdk:"isolation"`
	LLDPMedEnabled             types.Bool           `tfsdk:"lldpmed_enabled"`
	LLDPMedNotifyEnabled       types.Bool           `tfsdk:"lldpmed_notify_enabled"`
	NativeNetworkConfID        types.String         `tfsdk:"native_networkconf_id"`
	Name                       types.String         `tfsdk:"name"`
	OpMode                     types.String         `tfsdk:"op_mode"`
	PoeMode                    types.String         `tfsdk:"poe_mode"`
	PortSecurityEnabled        types.Bool           `tfsdk:"port_security_enabled"`
	PortSecurityMacAddress     types.Set            `tfsdk:"port_security_mac_address"`
	PriorityQueue1Level        types.Int64          `tfsdk:"priority_queue1_level"`
	PriorityQueue2Level        types.Int64          `tfsdk:"priority_queue2_level"`
	PriorityQueue3Level        types.Int64          `tfsdk:"priority_queue3_level"`
	PriorityQueue4Level        types.Int64          `tfsdk:"priority_queue4_level"`
	Speed                      types.Int64          `tfsdk:"speed"`
	StormctrlBcastEnabled      types.Bool           `tfsdk:"stormctrl_bcast_enabled"`
	StormctrlBcastLevel        types.Int64          `tfsdk:"stormctrl_bcast_level"`
	StormctrlBcastRate         types.Int64          `tfsdk:"stormctrl_bcast_rate"`
	StormctrlMcastEnabled      types.Bool           `tfsdk:"stormctrl_mcast_enabled"`
	StormctrlMcastLevel        types.Int64          `tfsdk:"stormctrl_mcast_level"`
	StormctrlMcastRate         types.Int64          `tfsdk:"stormctrl_mcast_rate"`
	StormctrlType              types.String         `tfsdk:"stormctrl_type"`
	StormctrlUcastEnabled      types.Bool           `tfsdk:"stormctrl_ucast_enabled"`
	StormctrlUcastLevel        types.Int64          `tfsdk:"stormctrl_ucast_level"`
	StormctrlUcastRate         types.Int64          `tfsdk:"stormctrl_ucast_rate"`
	STPPortMode                types.Bool           `tfsdk:"stp_port_mode"`
	TaggedNetworkConfIDs       types.Set            `tfsdk:"tagged_networkconf_ids"`
	VoiceNetworkConfID         types.String         `tfsdk:"voice_networkconf_id"`
	ExcludedNetworkConfIDs     types.Set            `tfsdk:"excluded_networkconf_ids"`
	MulticastRouterNetworkIDs  types.Set            `tfsdk:"multicast_router_networkconf_ids"`
	TaggedVLANMgmt             types.String         `tfsdk:"tagged_vlan_mgmt"`
	FecMode                    types.String         `tfsdk:"fec_mode"`
	SettingPreference          types.String         `tfsdk:"setting_preference"`
	PortKeepaliveEnabled       types.Bool           `tfsdk:"port_keepalive_enabled"`
	Timeouts                   timeouts.Value       `tfsdk:"timeouts"`
}

// portProfileTaggedNetworkUniverse returns the site networks which can be
// carried as tagged VLANs by a port profile. The native network is carried
// untagged and therefore never belongs to this set.
func portProfileTaggedNetworkUniverse(networks []unifi.Network, nativeNetworkID string) []string {
	ids := make([]string, 0, len(networks))
	for _, network := range networks {
		if network.ID == "" || network.ID == nativeNetworkID || network.VLAN == nil {
			continue
		}
		switch network.Purpose {
		case unifi.PurposeCorporate, unifi.PurposeGuest, unifi.PurposeVLANOnly:
			ids = append(ids, network.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

// portProfileExcludedNetworkIDs converts an exact tagged-network selection to
// the exclusion list accepted by the controller. IDs outside the eligible
// universe are returned separately so callers can fail before writing.
func portProfileExcludedNetworkIDs(universe, included []string) ([]string, []string) {
	eligible := make(map[string]struct{}, len(universe))
	for _, id := range universe {
		eligible[id] = struct{}{}
	}

	selected := make(map[string]struct{}, len(included))
	var invalid []string
	for _, id := range included {
		if _, ok := eligible[id]; !ok {
			invalid = append(invalid, id)
			continue
		}
		selected[id] = struct{}{}
	}

	excluded := make([]string, 0, len(universe))
	for _, id := range universe {
		if _, ok := selected[id]; !ok {
			excluded = append(excluded, id)
		}
	}
	slices.Sort(excluded)
	slices.Sort(invalid)
	return excluded, invalid
}

// portProfileActualTaggedNetworkIDs translates the controller's mode and
// exclusion list into the set users see in Terraform.
func portProfileActualTaggedNetworkIDs(mode string, universe, excluded []string) []string {
	switch mode {
	case "auto":
		return slices.Clone(universe)
	case "block_all":
		return []string{}
	case "custom":
		blocked := make(map[string]struct{}, len(excluded))
		for _, id := range excluded {
			blocked[id] = struct{}{}
		}
		included := make([]string, 0, len(universe))
		for _, id := range universe {
			if _, ok := blocked[id]; !ok {
				included = append(included, id)
			}
		}
		return included
	default:
		return nil
	}
}

func resolvePortProfileVLANMode(
	taggedConfigured bool,
	taggedCount int,
	excludedConfigured bool,
	configuredMode string,
) (string, error) {
	if taggedConfigured && excludedConfigured {
		return "", fmt.Errorf(
			"tagged_networkconf_ids and excluded_networkconf_ids cannot both be configured",
		)
	}

	if taggedConfigured {
		derived := "custom"
		if taggedCount == 0 {
			derived = "block_all"
		}
		if configuredMode != "" && configuredMode != derived {
			return "", fmt.Errorf(
				"tagged_vlan_mgmt must be %q when tagged_networkconf_ids contains %d network(s)",
				derived,
				taggedCount,
			)
		}
		return derived, nil
	}

	if excludedConfigured {
		if configuredMode != "" && configuredMode != "custom" {
			return "", fmt.Errorf(
				"tagged_vlan_mgmt must be %q when excluded_networkconf_ids is configured",
				"custom",
			)
		}
		return "custom", nil
	}

	return configuredMode, nil
}

func resolvePortProfileForward(mode, configuredForward string) (string, error) {
	derived := ""
	switch mode {
	case "auto":
		derived = "all"
	case "block_all":
		derived = "native"
	case "custom":
		derived = "customize"
	}
	if derived == "" {
		return configuredForward, nil
	}
	if configuredForward != "" && configuredForward != derived {
		return "", fmt.Errorf(
			"forward must be %q when tagged_vlan_mgmt is %q",
			derived,
			mode,
		)
	}
	return derived, nil
}

type portProfileVLANConfig struct {
	TaggedConfigured   bool
	TaggedIDs          []string
	ExcludedConfigured bool
	ExcludedIDs        []string
	Mode               string
	Forward            string
}

func portProfileVLANConfigFromModel(
	ctx context.Context,
	model *portProfileResourceModel,
) (portProfileVLANConfig, diag.Diagnostics) {
	var diags diag.Diagnostics
	config := portProfileVLANConfig{}

	if !model.TaggedNetworkConfIDs.IsNull() {
		config.TaggedConfigured = true
		if model.TaggedNetworkConfIDs.IsUnknown() {
			diags.AddError(
				"Unknown tagged network IDs",
				"tagged_networkconf_ids must be known before the port profile can be written.",
			)
		} else {
			diags.Append(
				model.TaggedNetworkConfIDs.ElementsAs(ctx, &config.TaggedIDs, false)...,
			)
		}
	}

	if !model.ExcludedNetworkConfIDs.IsNull() {
		config.ExcludedConfigured = true
		if model.ExcludedNetworkConfIDs.IsUnknown() {
			diags.AddError(
				"Unknown excluded network IDs",
				"excluded_networkconf_ids must be known before the port profile can be written.",
			)
		} else {
			diags.Append(
				model.ExcludedNetworkConfIDs.ElementsAs(ctx, &config.ExcludedIDs, false)...,
			)
		}
	}

	if !model.TaggedVLANMgmt.IsNull() {
		if model.TaggedVLANMgmt.IsUnknown() {
			diags.AddError(
				"Unknown tagged VLAN mode",
				"tagged_vlan_mgmt must be known before the port profile can be written.",
			)
		} else {
			config.Mode = model.TaggedVLANMgmt.ValueString()
		}
	}
	if !model.Forward.IsNull() {
		if model.Forward.IsUnknown() {
			diags.AddError(
				"Unknown forwarding mode",
				"forward must be known before the port profile can be written.",
			)
		} else {
			config.Forward = model.Forward.ValueString()
		}
	}

	if diags.HasError() {
		return config, diags
	}
	mode, err := resolvePortProfileVLANMode(
		config.TaggedConfigured,
		len(config.TaggedIDs),
		config.ExcludedConfigured,
		config.Mode,
	)
	if err != nil {
		diags.AddError("Invalid tagged VLAN configuration", err.Error())
		return config, diags
	}
	config.Mode = mode
	forward, err := resolvePortProfileForward(config.Mode, config.Forward)
	if err != nil {
		diags.AddError("Invalid tagged VLAN configuration", err.Error())
		return config, diags
	}
	config.Forward = forward
	return config, diags
}

func applyPortProfileVLANConfig(
	config portProfileVLANConfig,
	universe []string,
	api *unifi.PortProfile,
) error {
	var excluded []string
	forward, err := resolvePortProfileForward(config.Mode, config.Forward)
	if err != nil {
		return err
	}
	if config.TaggedConfigured && config.Mode == "custom" {
		var invalid []string
		excluded, invalid = portProfileExcludedNetworkIDs(universe, config.TaggedIDs)
		if len(invalid) > 0 {
			return fmt.Errorf(
				"tagged_networkconf_ids contains IDs that are not eligible tagged networks in this site: %v",
				invalid,
			)
		}
	} else if config.ExcludedConfigured {
		excluded = slices.Clone(config.ExcludedIDs)
		slices.Sort(excluded)
	}

	api.TaggedVLANMgmt = config.Mode
	api.ExcludedNetworkIDs = excluded
	if forward != "" {
		api.Forward = forward
	}
	return nil
}

func setPortProfileTaggedNetworkState(
	ctx context.Context,
	api *unifi.PortProfile,
	networks []unifi.Network,
	model *portProfileResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics
	universe := portProfileTaggedNetworkUniverse(networks, api.NATiveNetworkID)
	tagged := portProfileActualTaggedNetworkIDs(
		api.TaggedVLANMgmt,
		universe,
		api.ExcludedNetworkIDs,
	)
	if tagged == nil {
		model.TaggedNetworkConfIDs = types.SetNull(types.StringType)
		return diags
	}

	value, d := types.SetValueFrom(ctx, types.StringType, tagged)
	diags.Append(d...)
	model.TaggedNetworkConfIDs = value
	return diags
}

func (r *portProfileResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_port_profile"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *portProfileResource) IdentitySchema(
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

func (r *portProfileResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_port_profile.PortProfileResourceSchema(ctx)
	// v1: dot1x_idle_timeout changed from Int64 (seconds) to a GoDuration string.
	resp.Schema.Version = 1
	// The released schema describes this surface in plain text, which a
	// generated schema cannot express; see plainDescriptions.
	plainDescriptions(&resp.Schema)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
}

func (r *portProfileResource) ConfigValidators(
	_ context.Context,
) []resource.ConfigValidator {
	return []resource.ConfigValidator{&portProfileVLANConfigValidator{}}
}

type portProfileVLANConfigValidator struct{}

func (v *portProfileVLANConfigValidator) Description(_ context.Context) string {
	return "tagged VLAN include, exclude, and mode settings must describe one unambiguous policy"
}

func (v *portProfileVLANConfigValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v *portProfileVLANConfigValidator) ValidateResource(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var tagged types.Set
	var excluded types.Set
	var mode types.String
	var forward types.String
	resp.Diagnostics.Append(
		req.Config.GetAttribute(ctx, path.Root("tagged_networkconf_ids"), &tagged)...,
	)
	resp.Diagnostics.Append(
		req.Config.GetAttribute(ctx, path.Root("excluded_networkconf_ids"), &excluded)...,
	)
	resp.Diagnostics.Append(
		req.Config.GetAttribute(ctx, path.Root("tagged_vlan_mgmt"), &mode)...,
	)
	resp.Diagnostics.Append(
		req.Config.GetAttribute(ctx, path.Root("forward"), &forward)...,
	)
	if resp.Diagnostics.HasError() || tagged.IsUnknown() || excluded.IsUnknown() ||
		mode.IsUnknown() || forward.IsUnknown() {
		return
	}

	configuredMode := ""
	if !mode.IsNull() {
		configuredMode = mode.ValueString()
	}
	resolvedMode, err := resolvePortProfileVLANMode(
		!tagged.IsNull(),
		len(tagged.Elements()),
		!excluded.IsNull(),
		configuredMode,
	)
	if err != nil {
		resp.Diagnostics.AddError("Invalid tagged VLAN configuration", err.Error())
		return
	}
	configuredForward := ""
	if !forward.IsNull() {
		configuredForward = forward.ValueString()
	}
	if _, err := resolvePortProfileForward(resolvedMode, configuredForward); err != nil {
		resp.Diagnostics.AddError("Invalid tagged VLAN configuration", err.Error())
	}
}

var _ resource.ConfigValidator = &portProfileVLANConfigValidator{}

// UpgradeState migrates v0 state (dot1x_idle_timeout stored as integer seconds)
// to v1 (a GoDuration string).
func (r *portProfileResource) UpgradeState(
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
						util.SetDurationField(state, "dot1x_idle_timeout", time.Second)
					},
				)
				if err != nil {
					resp.Diagnostics.AddError("Failed to upgrade port profile state", err.Error())
					return
				}
				resp.DynamicValue = dv
			},
		},
	}
}

func (r *portProfileResource) Configure(
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

func (r *portProfileResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan portProfileResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var config portProfileResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
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

	networks, err := r.client.ListNetwork(ctx, site)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Networks for Port Profile",
			"Could not read the site network inventory before creating the port profile: "+err.Error(),
		)
		return
	}
	vlanConfig, vlanDiags := portProfileVLANConfigFromModel(ctx, &config)
	resp.Diagnostics.Append(vlanDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Convert model to API request
	portProfile, convDiags := r.modelToAPIPortProfile(ctx, &plan)
	resp.Diagnostics.Append(convDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := applyPortProfileVLANConfig(
		vlanConfig,
		portProfileTaggedNetworkUniverse(networks, portProfile.NATiveNetworkID),
		portProfile,
	); err != nil {
		resp.Diagnostics.AddError("Invalid tagged network selection", err.Error())
		return
	}

	apiPortProfile, err := r.client.CreatePortProfile(ctx, site, portProfile)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Port Profile",
			fmt.Sprintf("Could not create port profile: %s", err),
		)
		return
	}

	// Set state
	plan.ID = types.StringValue(apiPortProfile.ID)
	plan.Site = types.StringValue(site)
	resp.Diagnostics.Append(r.portProfileToModel(ctx, apiPortProfile, &plan, site)...)
	resp.Diagnostics.Append(
		setPortProfileTaggedNetworkState(ctx, apiPortProfile, networks, &plan)...,
	)

	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), plan.ID)...)
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *portProfileResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state portProfileResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
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

	id := state.ID.ValueString()
	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	portProfile, err := r.client.GetPortProfile(ctx, site, id)
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Port Profile",
			fmt.Sprintf("Could not read port profile %s: %s", id, err),
		)
		return
	}
	networks, err := r.client.ListNetwork(ctx, site)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Networks for Port Profile",
			"Could not read the site network inventory while reading the port profile: "+err.Error(),
		)
		return
	}

	// Update state from API response
	resp.Diagnostics.Append(r.portProfileToModel(ctx, portProfile, &state, site)...)
	resp.Diagnostics.Append(
		setPortProfileTaggedNetworkState(ctx, portProfile, networks, &state)...,
	)

	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *portProfileResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan portProfileResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var config portProfileResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state portProfileResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
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

	site := plan.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	id := state.ID.ValueString()

	// Read current port profile and merge with planned changes
	currentPortProfile, err := r.client.GetPortProfile(ctx, site, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Port Profile for Update",
			fmt.Sprintf("Could not read port profile %s for update: %s", id, err),
		)
		return
	}
	networks, err := r.client.ListNetwork(ctx, site)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Networks for Port Profile",
			"Could not read the site network inventory before updating the port profile: "+err.Error(),
		)
		return
	}
	vlanConfig, vlanDiags := portProfileVLANConfigFromModel(ctx, &config)
	resp.Diagnostics.Append(vlanDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Apply current API values to state
	r.setResourceData(ctx, currentPortProfile, &state, site)

	// Apply plan changes to the state (merge pattern)
	r.applyPlanToState(ctx, &plan, &state)

	// Convert updated state to API request
	portProfile, convDiags := r.modelToAPIPortProfile(ctx, &state)
	resp.Diagnostics.Append(convDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := applyPortProfileVLANConfig(
		vlanConfig,
		portProfileTaggedNetworkUniverse(networks, portProfile.NATiveNetworkID),
		portProfile,
	); err != nil {
		resp.Diagnostics.AddError("Invalid tagged network selection", err.Error())
		return
	}

	portProfile.ID = id
	portProfile.SiteID = site

	apiPortProfile, err := r.client.UpdatePortProfile(ctx, site, portProfile)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Port Profile",
			fmt.Sprintf("Could not update port profile %s: %s", id, err),
		)
		return
	}

	// Update state from API response
	resp.Diagnostics.Append(r.portProfileToModel(ctx, apiPortProfile, &state, site)...)
	resp.Diagnostics.Append(
		setPortProfileTaggedNetworkState(ctx, apiPortProfile, networks, &state)...,
	)

	state.Timeouts = plan.Timeouts

	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *portProfileResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state portProfileResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
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

	id := state.ID.ValueString()
	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	err := r.client.DeletePortProfile(ctx, site, id)
	if err != nil {
		if _, ok := err.(*unifi.NotFoundError); ok {
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting Port Profile",
			fmt.Sprintf("Could not delete port profile %s: %s", id, err),
		)
		return
	}
}

func (r *portProfileResource) ImportState(
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

// Helper methods

func (r *portProfileResource) modelToAPIPortProfile(
	ctx context.Context,
	model *portProfileResourceModel,
) (*unifi.PortProfile, diag.Diagnostics) {
	var diags diag.Diagnostics

	portProfile := &unifi.PortProfile{
		Name:   model.Name.ValueString(),
		OpMode: model.OpMode.ValueString(),
	}

	if !model.Autoneg.IsNull() && !model.Autoneg.IsUnknown() {
		portProfile.Autoneg = model.Autoneg.ValueBool()
	}

	if !model.FullDuplex.IsNull() && !model.FullDuplex.IsUnknown() {
		portProfile.FullDuplex = model.FullDuplex.ValueBool()
	}

	if !model.Isolation.IsNull() && !model.Isolation.IsUnknown() {
		portProfile.Isolation = model.Isolation.ValueBool()
	}

	if !model.Dot1XCtrl.IsNull() && !model.Dot1XCtrl.IsUnknown() {
		portProfile.Dot1XCtrl = model.Dot1XCtrl.ValueString()
	}

	portProfile.Dot1XIDleTimeout = util.DurationUnitsPtr(model.Dot1XIdleTimeout, time.Second)

	if !model.Forward.IsNull() && !model.Forward.IsUnknown() {
		portProfile.Forward = model.Forward.ValueString()
	}

	if !model.LLDPMedEnabled.IsNull() && !model.LLDPMedEnabled.IsUnknown() {
		portProfile.LldpmedEnabled = model.LLDPMedEnabled.ValueBool()
	}

	if !model.LLDPMedNotifyEnabled.IsNull() && !model.LLDPMedNotifyEnabled.IsUnknown() {
		portProfile.LldpmedNotifyEnabled = model.LLDPMedNotifyEnabled.ValueBool()
	}

	// Skip native network config for now as field name is unclear

	if !model.PoeMode.IsNull() && !model.PoeMode.IsUnknown() {
		portProfile.PoeMode = model.PoeMode.ValueString()
	}

	if !model.PortSecurityEnabled.IsNull() && !model.PortSecurityEnabled.IsUnknown() {
		portProfile.PortSecurityEnabled = model.PortSecurityEnabled.ValueBool()
	}

	// Convert port security MAC addresses
	if !model.PortSecurityMacAddress.IsNull() && !model.PortSecurityMacAddress.IsUnknown() {
		var macAddresses []string
		diags.Append(model.PortSecurityMacAddress.ElementsAs(ctx, &macAddresses, false)...)
		if !diags.HasError() {
			portProfile.PortSecurityMACAddress = macAddresses
		}
	}

	portProfile.Speed = model.Speed.ValueInt64Pointer()

	if !model.NativeNetworkConfID.IsNull() {
		portProfile.NATiveNetworkID = model.NativeNetworkConfID.ValueString()
	}
	if !model.VoiceNetworkConfID.IsNull() {
		portProfile.VoiceNetworkID = model.VoiceNetworkConfID.ValueString()
	}
	if !model.FecMode.IsNull() {
		portProfile.FecMode = model.FecMode.ValueString()
	}
	if !model.SettingPreference.IsNull() && !model.SettingPreference.IsUnknown() {
		portProfile.SettingPreference = model.SettingPreference.ValueString()
	}
	portProfile.PortKeepaliveEnabled = model.PortKeepaliveEnabled.ValueBool()
	portProfile.StpPortMode = model.STPPortMode.ValueBool()

	if !model.MulticastRouterNetworkIDs.IsNull() && !model.MulticastRouterNetworkIDs.IsUnknown() {
		var ids []string
		diags.Append(model.MulticastRouterNetworkIDs.ElementsAs(ctx, &ids, false)...)
		if !diags.HasError() {
			portProfile.MulticastRouterNetworkIDs = ids
		}
	}

	// Handle storm control and other complex fields as needed...

	return portProfile, diags
}

func (r *portProfileResource) setResourceData(
	ctx context.Context,
	portProfile *unifi.PortProfile,
	model *portProfileResourceModel,
	site string,
) {
	r.portProfileToModel(ctx, portProfile, model, site)
}

// portProfileToModel populates the resource model from the API struct, setting
// every schema field. It is the reusable API->model converter shared by Read
// and List. It only performs API->model field population; plan/state
// reconciliation (applyPlanToState) is intentionally left to the callers.
func (r *portProfileResource) portProfileToModel(
	ctx context.Context,
	api *unifi.PortProfile,
	model *portProfileResourceModel,
	site string,
) diag.Diagnostics {
	var diags diag.Diagnostics

	portProfile := api

	if portProfile.ID != "" {
		model.ID = types.StringValue(portProfile.ID)
	}

	model.Site = types.StringValue(site)

	if portProfile.Name == "" {
		model.Name = types.StringNull()
	} else {
		model.Name = types.StringValue(portProfile.Name)
	}

	model.Autoneg = types.BoolValue(portProfile.Autoneg)

	if portProfile.Dot1XCtrl == "" {
		model.Dot1XCtrl = types.StringValue("force_authorized")
	} else {
		model.Dot1XCtrl = types.StringValue(portProfile.Dot1XCtrl)
	}

	model.Dot1XIdleTimeout = util.DurationPtrValue(portProfile.Dot1XIDleTimeout, time.Second)

	if portProfile.Forward == "" {
		model.Forward = types.StringValue("native")
	} else {
		model.Forward = types.StringValue(portProfile.Forward)
	}

	model.FullDuplex = types.BoolValue(portProfile.FullDuplex)

	model.Isolation = types.BoolValue(portProfile.Isolation)

	model.LLDPMedEnabled = types.BoolValue(portProfile.LldpmedEnabled)

	// Only set lldpmed_notify_enabled if it was in the plan or if it's explicitly true
	if !model.LLDPMedNotifyEnabled.IsNull() || portProfile.LldpmedNotifyEnabled {
		model.LLDPMedNotifyEnabled = types.BoolValue(portProfile.LldpmedNotifyEnabled)
	} else {
		model.LLDPMedNotifyEnabled = types.BoolNull()
	}

	if portProfile.NATiveNetworkID != "" {
		model.NativeNetworkConfID = types.StringValue(portProfile.NATiveNetworkID)
	} else {
		model.NativeNetworkConfID = types.StringNull()
	}

	if portProfile.OpMode == "" {
		model.OpMode = types.StringValue("switch")
	} else {
		model.OpMode = types.StringValue(portProfile.OpMode)
	}

	if portProfile.PoeMode == "" {
		model.PoeMode = types.StringNull()
	} else {
		model.PoeMode = types.StringValue(portProfile.PoeMode)
	}

	model.PortSecurityEnabled = types.BoolValue(portProfile.PortSecurityEnabled)

	// Convert port security MAC addresses
	if len(portProfile.PortSecurityMACAddress) == 0 {
		model.PortSecurityMacAddress = types.SetNull(types.StringType)
	} else {
		macAddressList := make([]types.String, len(portProfile.PortSecurityMACAddress))
		for i, mac := range portProfile.PortSecurityMACAddress {
			macAddressList[i] = types.StringValue(mac)
		}
		macAddressSet, d := types.SetValueFrom(ctx, types.StringType, macAddressList)
		diags.Append(d...)
		model.PortSecurityMacAddress = macAddressSet
	}

	// Only set speed if it was in the plan or if it's non-zero
	model.Speed = types.Int64PointerValue(portProfile.Speed)

	// tagged_networkconf_ids has no corresponding go-unifi field; tagged VLANs are
	// managed via tagged_vlan_mgmt + excluded_networkconf_ids instead.
	model.TaggedNetworkConfIDs = types.SetNull(types.StringType)

	if portProfile.VoiceNetworkID != "" {
		model.VoiceNetworkConfID = types.StringValue(portProfile.VoiceNetworkID)
	} else {
		model.VoiceNetworkConfID = types.StringNull()
	}

	if portProfile.TaggedVLANMgmt != "" {
		model.TaggedVLANMgmt = types.StringValue(portProfile.TaggedVLANMgmt)
	} else {
		model.TaggedVLANMgmt = types.StringNull()
	}

	if portProfile.FecMode != "" {
		model.FecMode = types.StringValue(portProfile.FecMode)
	} else {
		model.FecMode = types.StringNull()
	}

	if portProfile.SettingPreference != "" {
		model.SettingPreference = types.StringValue(portProfile.SettingPreference)
	} else {
		model.SettingPreference = types.StringNull()
	}

	model.PortKeepaliveEnabled = types.BoolValue(portProfile.PortKeepaliveEnabled)

	if portProfile.TaggedVLANMgmt == "custom" {
		excluded := portProfile.ExcludedNetworkIDs
		if excluded == nil {
			excluded = []string{}
		}
		s, d := types.SetValueFrom(ctx, types.StringType, excluded)
		diags.Append(d...)
		model.ExcludedNetworkConfIDs = s
	} else {
		model.ExcludedNetworkConfIDs = types.SetNull(types.StringType)
	}

	if len(portProfile.MulticastRouterNetworkIDs) > 0 {
		s, d := types.SetValueFrom(ctx, types.StringType, portProfile.MulticastRouterNetworkIDs)
		diags.Append(d...)
		model.MulticastRouterNetworkIDs = s
	} else {
		model.MulticastRouterNetworkIDs = types.SetNull(types.StringType)
	}

	// Set remaining fields to defaults or null as appropriate
	model.EgressRateLimitKbps = types.Int64Null()
	model.EgressRateLimitKbpsEnabled = types.BoolValue(false)
	model.PriorityQueue1Level = types.Int64Null()
	model.PriorityQueue2Level = types.Int64Null()
	model.PriorityQueue3Level = types.Int64Null()
	model.PriorityQueue4Level = types.Int64Null()
	model.StormctrlBcastEnabled = types.BoolValue(false)
	model.StormctrlBcastLevel = types.Int64Null()
	model.StormctrlBcastRate = types.Int64Null()
	model.StormctrlMcastEnabled = types.BoolValue(false)
	model.StormctrlMcastLevel = types.Int64Null()
	model.StormctrlMcastRate = types.Int64Null()
	model.StormctrlType = types.StringNull()
	model.StormctrlUcastEnabled = types.BoolValue(false)
	model.StormctrlUcastLevel = types.Int64Null()
	model.StormctrlUcastRate = types.Int64Null()
	model.STPPortMode = types.BoolValue(portProfile.StpPortMode)

	return diags
}

func (r *portProfileResource) applyPlanToState(
	_ context.Context,
	plan *portProfileResourceModel,
	state *portProfileResourceModel,
) {
	// Apply all plan values that are not null/unknown to the state
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		state.Name = plan.Name
	}
	if !plan.Autoneg.IsNull() && !plan.Autoneg.IsUnknown() {
		state.Autoneg = plan.Autoneg
	}
	if !plan.Dot1XCtrl.IsNull() && !plan.Dot1XCtrl.IsUnknown() {
		state.Dot1XCtrl = plan.Dot1XCtrl
	}
	if !plan.Dot1XIdleTimeout.IsNull() && !plan.Dot1XIdleTimeout.IsUnknown() {
		state.Dot1XIdleTimeout = plan.Dot1XIdleTimeout
	}
	if !plan.EgressRateLimitKbps.IsNull() && !plan.EgressRateLimitKbps.IsUnknown() {
		state.EgressRateLimitKbps = plan.EgressRateLimitKbps
	}
	if !plan.EgressRateLimitKbpsEnabled.IsNull() && !plan.EgressRateLimitKbpsEnabled.IsUnknown() {
		state.EgressRateLimitKbpsEnabled = plan.EgressRateLimitKbpsEnabled
	}
	if !plan.Forward.IsNull() && !plan.Forward.IsUnknown() {
		state.Forward = plan.Forward
	}
	if !plan.FullDuplex.IsNull() && !plan.FullDuplex.IsUnknown() {
		state.FullDuplex = plan.FullDuplex
	}
	if !plan.Isolation.IsNull() && !plan.Isolation.IsUnknown() {
		state.Isolation = plan.Isolation
	}
	if !plan.LLDPMedEnabled.IsNull() && !plan.LLDPMedEnabled.IsUnknown() {
		state.LLDPMedEnabled = plan.LLDPMedEnabled
	}
	if !plan.LLDPMedNotifyEnabled.IsNull() && !plan.LLDPMedNotifyEnabled.IsUnknown() {
		state.LLDPMedNotifyEnabled = plan.LLDPMedNotifyEnabled
	}
	if !plan.NativeNetworkConfID.IsNull() && !plan.NativeNetworkConfID.IsUnknown() {
		state.NativeNetworkConfID = plan.NativeNetworkConfID
	}
	if !plan.OpMode.IsNull() && !plan.OpMode.IsUnknown() {
		state.OpMode = plan.OpMode
	}
	if !plan.PoeMode.IsNull() && !plan.PoeMode.IsUnknown() {
		state.PoeMode = plan.PoeMode
	}
	if !plan.PortSecurityEnabled.IsNull() && !plan.PortSecurityEnabled.IsUnknown() {
		state.PortSecurityEnabled = plan.PortSecurityEnabled
	}
	if !plan.PortSecurityMacAddress.IsNull() && !plan.PortSecurityMacAddress.IsUnknown() {
		state.PortSecurityMacAddress = plan.PortSecurityMacAddress
	}
	if !plan.Speed.IsNull() && !plan.Speed.IsUnknown() {
		state.Speed = plan.Speed
	}
	if !plan.TaggedNetworkConfIDs.IsNull() && !plan.TaggedNetworkConfIDs.IsUnknown() {
		state.TaggedNetworkConfIDs = plan.TaggedNetworkConfIDs
	}
	if !plan.VoiceNetworkConfID.IsNull() && !plan.VoiceNetworkConfID.IsUnknown() {
		state.VoiceNetworkConfID = plan.VoiceNetworkConfID
	}
	if !plan.ExcludedNetworkConfIDs.IsNull() && !plan.ExcludedNetworkConfIDs.IsUnknown() {
		state.ExcludedNetworkConfIDs = plan.ExcludedNetworkConfIDs
	}
	if !plan.MulticastRouterNetworkIDs.IsNull() && !plan.MulticastRouterNetworkIDs.IsUnknown() {
		state.MulticastRouterNetworkIDs = plan.MulticastRouterNetworkIDs
	}
	if !plan.TaggedVLANMgmt.IsNull() && !plan.TaggedVLANMgmt.IsUnknown() {
		state.TaggedVLANMgmt = plan.TaggedVLANMgmt
	}
	if !plan.FecMode.IsNull() && !plan.FecMode.IsUnknown() {
		state.FecMode = plan.FecMode
	}
	if !plan.SettingPreference.IsNull() && !plan.SettingPreference.IsUnknown() {
		state.SettingPreference = plan.SettingPreference
	}
	if !plan.PortKeepaliveEnabled.IsNull() && !plan.PortKeepaliveEnabled.IsUnknown() {
		state.PortKeepaliveEnabled = plan.PortKeepaliveEnabled
	}
	if !plan.STPPortMode.IsNull() && !plan.STPPortMode.IsUnknown() {
		state.STPPortMode = plan.STPPortMode
	}
	// Apply other fields as needed...
}

// ListResourceConfigSchema implements [list.ListResource].
func (r *portProfileResource) ListResourceConfigSchema(
	ctx context.Context,
	_ list.ListResourceSchemaRequest,
	resp *list.ListResourceSchemaResponse,
) {
	resp.Schema = listresource_port_profile.PortProfileListResourceSchema(ctx)
}

// List implements [list.ListResource].
func (r *portProfileResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config portProfileListConfigModel

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
	var filters []portProfileListFilterModel
	if !config.Filter.IsNull() && !config.Filter.IsUnknown() {
		config.Filter.ElementsAs(ctx, &filters, false)
	}

	postFilters := make(map[string]string)
	for _, f := range filters {
		postFilters[f.Name.ValueString()] = f.Value.ValueString()
	}

	profiles, err := r.client.ListPortProfile(ctx, site)
	if err != nil {
		var d diag.Diagnostics
		d.AddError(
			"Error Listing Port Profiles",
			"Could not list port profiles: "+err.Error(),
		)
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}
	networks, err := r.client.ListNetwork(ctx, site)
	if err != nil {
		var d diag.Diagnostics
		d.AddError(
			"Error Reading Networks for Port Profiles",
			"Could not read the site network inventory: "+err.Error(),
		)
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for _, profile := range profiles {
			// Apply name filter.
			if val, ok := postFilters["name"]; ok {
				if profile.Name != val {
					continue
				}
			}

			result := req.NewListResult(ctx)

			// Display name: prefer name, fall back to ID.
			if profile.Name != "" {
				result.DisplayName = profile.Name
			} else {
				result.DisplayName = profile.ID
			}

			// Set identity.
			result.Diagnostics.Append(
				result.Identity.SetAttribute(
					ctx,
					path.Root("id"),
					types.StringValue(profile.ID),
				)...,
			)

			// Convert to model.
			var model portProfileResourceModel
			result.Diagnostics.Append(
				r.portProfileToModel(ctx, &profile, &model, site)...,
			)
			result.Diagnostics.Append(
				setPortProfileTaggedNetworkState(ctx, &profile, networks, &model)...,
			)
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
