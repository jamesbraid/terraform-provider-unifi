package unifi

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_wlan"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wlan"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var (
	_ resource.Resource                 = &wlanFrameworkResource{}
	_ resource.ResourceWithImportState  = &wlanFrameworkResource{}
	_ resource.ResourceWithIdentity     = &wlanFrameworkResource{}
	_ resource.ResourceWithUpgradeState = &wlanFrameworkResource{}
	_ resource.ResourceWithModifyPlan   = &wlanFrameworkResource{}
)

// Ensure provider defined types fully satisfy list interfaces.
var (
	_ list.ListResource              = &wlanFrameworkResource{}
	_ list.ListResourceWithConfigure = &wlanFrameworkResource{}
)

func NewWLANFrameworkResource() resource.Resource {
	return &wlanFrameworkResource{}
}

func NewWLANListResource() list.ListResource {
	return &wlanFrameworkResource{}
}

// wlanFrameworkResource defines the resource implementation.
type wlanFrameworkResource struct {
	client *Client
}

// wlanScheduleModel represents a schedule block for WLAN.
type wlanScheduleModel struct {
	DayOfWeek   types.String         `tfsdk:"day_of_week"`
	StartHour   types.Int64          `tfsdk:"start_hour"`
	StartMinute types.Int64          `tfsdk:"start_minute"`
	Duration    timetypes.GoDuration `tfsdk:"duration"`
	Name        types.String         `tfsdk:"name"`
}

// wlanMacFilterModel represents the MAC filter configuration for WLAN.
type wlanMacFilterModel struct {
	Enabled types.Bool   `tfsdk:"enabled"`
	List    types.Set    `tfsdk:"list"`
	Policy  types.String `tfsdk:"policy"`
}

// wlanPrivatePresharedKeyModel represents a single private pre-shared key (PPSK)
// entry: a per-key password optionally bound to its own VLAN/network.
type wlanPrivatePresharedKeyModel struct {
	NetworkID types.String `tfsdk:"network_id"`
	Password  types.String `tfsdk:"password"`
}

func (m wlanPrivatePresharedKeyModel) AttributeTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"network_id": types.StringType,
		"password":   types.StringType,
	}
}

// wlanFrameworkResourceModel describes the resource data model.
type wlanFrameworkResourceModel struct {
	ID                          types.String `tfsdk:"id"`
	Site                        types.String `tfsdk:"site"`
	Name                        types.String `tfsdk:"name"`
	NetworkID                   types.String `tfsdk:"network_id"`
	UserGroupID                 types.String `tfsdk:"user_group_id"`
	Security                    types.String `tfsdk:"security"`
	WPA3Support                 types.Bool   `tfsdk:"wpa3_support"`
	WPA3Transition              types.Bool   `tfsdk:"wpa3_transition"`
	PMFMode                     types.String `tfsdk:"pmf_mode"`
	Passphrase                  types.String `tfsdk:"passphrase"`
	PassphraseWO                types.String `tfsdk:"passphrase_wo"`
	HideSSID                    types.Bool   `tfsdk:"hide_ssid"`
	IsGuest                     types.Bool   `tfsdk:"is_guest"`
	Enabled                     types.Bool   `tfsdk:"enabled"`
	ApGroupIDs                  types.Set    `tfsdk:"ap_group_ids"`
	ApGroupMode                 types.String `tfsdk:"ap_group_mode"`
	VLANEnabled                 types.Bool   `tfsdk:"vlan_enabled"`
	VLAN                        types.Int64  `tfsdk:"vlan"`
	WLANBand                    types.String `tfsdk:"wlan_band"`
	WLANBands                   types.Set    `tfsdk:"wlan_bands"`
	MulticastEnhance            types.Bool   `tfsdk:"multicast_enhance"`
	MacFilter                   types.Object `tfsdk:"mac_filter"`
	PrivatePresharedKeysEnabled types.Bool   `tfsdk:"private_preshared_keys_enabled"`
	PrivatePresharedKeys        types.List   `tfsdk:"private_preshared_keys"`
	RadiusProfileID             types.String `tfsdk:"radius_profile_id"`
	NasIDentifierType           types.String `tfsdk:"nas_identifier_type"`
	Schedule                    types.List   `tfsdk:"schedule"`
	No2GhzOui                   types.Bool   `tfsdk:"no2ghz_oui"`
	L2Isolation                 types.Bool   `tfsdk:"l2_isolation"`
	ProxyArp                    types.Bool   `tfsdk:"proxy_arp"`
	BssTransition               types.Bool   `tfsdk:"bss_transition"`
	Uapsd                       types.Bool   `tfsdk:"uapsd"`
	FastRoamingEnabled          types.Bool   `tfsdk:"fast_roaming_enabled"`
	MinimumDataRate2GKbps       types.Int64  `tfsdk:"minimum_data_rate_2g_kbps"`
	MinimumDataRate5GKbps       types.Int64  `tfsdk:"minimum_data_rate_5g_kbps"`
	MinrateSettingPreference    types.String `tfsdk:"minrate_setting_preference"`
	RoamingAssistantNaEnabled   types.Bool   `tfsdk:"roaming_assistant_na_enabled"`
	RoamingAssistantNaRssi      types.Int64  `tfsdk:"roaming_assistant_na_rssi"`
	RoamingAssistant6EEnabled   types.Bool   `tfsdk:"roaming_assistant_6e_enabled"`
	RoamingAssistant6ERssi      types.Int64  `tfsdk:"roaming_assistant_6e_rssi"`

	// Security / encryption
	WPAMode types.String `tfsdk:"wpa_mode"`
	WPAEnc  types.String `tfsdk:"wpa_enc"`

	// DTIM
	DTIMMode types.String `tfsdk:"dtim_mode"`
	DTIMNg   types.Int64  `tfsdk:"dtim_ng"`
	DTIMNa   types.Int64  `tfsdk:"dtim_na"`
	DTIM6E   types.Int64  `tfsdk:"dtim_6e"`

	// Misc toggles
	GroupRekey           types.Int64 `tfsdk:"group_rekey"`
	IappEnabled          types.Bool  `tfsdk:"iapp_enabled"`
	WPA3FastRoaming      types.Bool  `tfsdk:"wpa3_fast_roaming"`
	WPA3Enhanced192      types.Bool  `tfsdk:"wpa3_enhanced_192"`
	RADIUSMacAuthEnabled types.Bool  `tfsdk:"radius_mac_auth_enabled"`
	EnhancedIot          types.Bool  `tfsdk:"enhanced_iot"`
	Hotspot2ConfEnabled  types.Bool  `tfsdk:"hotspot2conf_enabled"`
	MloEnabled           types.Bool  `tfsdk:"mlo_enabled"`
	BroadcastFilterList  types.Set   `tfsdk:"bc_filter_list"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

// wlanListConfigModel describes the list configuration model.
type wlanListConfigModel struct {
	Site   types.String `tfsdk:"site"`
	Filter types.List   `tfsdk:"filter"`
}

// wlanListFilterModel represents a single name/value filter entry.
type wlanListFilterModel struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

func (r *wlanFrameworkResource) Metadata(
	ctx context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_wlan"
}

// IdentitySchema implements [resource.ResourceWithIdentity].
func (r *wlanFrameworkResource) IdentitySchema(
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

func (r *wlanFrameworkResource) Schema(
	ctx context.Context,
	req resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = resource_wlan.WlanResourceSchema(ctx)
	// v1: schedule[].duration changed from Int64 minutes to a GoDuration string.
	resp.Schema.Version = 1
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)
	// Grafted rather than generated: the provider code specification has no
	// write_only member, so a generated passphrase_wo would silently lose the
	// property that is its entire reason for existing.
	resp.Schema.Attributes["passphrase_wo"] = schema.StringAttribute{
		MarkdownDescription: "Write-only equivalent of `passphrase` (Terraform 1.11+). " +
			"Used at apply time but never written to state, so it can be sourced from " +
			"an ephemeral resource (e.g. a Vault secret). Mutually exclusive with " +
			"`passphrase`.",
		Optional:  true,
		Sensitive: true,
		WriteOnly: true,
		Validators: []validator.String{
			stringvalidator.ConflictsWith(path.MatchRoot("passphrase")),
		},
	}
}

// UpgradeState migrates v0 state (schedule[].duration stored as integer minutes)
// to v1 (GoDuration strings).
func (r *wlanFrameworkResource) UpgradeState(
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
						if scheds, ok := state["schedule"].([]any); ok {
							for _, s := range scheds {
								if sm, ok := s.(map[string]any); ok {
									util.SetDurationField(sm, "duration", time.Minute)
								}
							}
						}
					},
				)
				if err != nil {
					resp.Diagnostics.AddError("Failed to upgrade WLAN state", err.Error())
					return
				}
				resp.DynamicValue = dv
			},
		},
	}
}

func (r *wlanFrameworkResource) Configure(
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

// ModifyPlan reconciles the fields the controller forces when enhanced IoT is
// enabled. With `enhanced_iot = true` the controller silently overrides
// iapp_enabled, wpa3_support, wpa3_transition, pmf_mode and dtim_ng with fixed
// values, so a plan that kept the configured/default values would fail the
// post-apply consistency check (and re-propose them on every plan). Pin those
// fields to what the controller will return. When enhanced_iot is false (the
// common case) this is a no-op, so non-IoT WLANs are unaffected. See #283.
func (r *wlanFrameworkResource) ModifyPlan(
	ctx context.Context,
	req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse,
) {
	// No plan on destroy.
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan wlanFrameworkResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if applyEnhancedIotOverrides(&plan) {
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
	}
}

// applyEnhancedIotOverrides pins the fields the controller forces when
// enhanced IoT is enabled. Returns true if it changed the model. A no-op when
// enhanced_iot is not true.
func applyEnhancedIotOverrides(plan *wlanFrameworkResourceModel) bool {
	if !plan.EnhancedIot.ValueBool() {
		return false
	}
	plan.IappEnabled = types.BoolValue(true)
	plan.WPA3Support = types.BoolValue(false)
	plan.WPA3Transition = types.BoolValue(false)
	plan.PMFMode = types.StringValue("disabled")
	plan.DTIMNg = types.Int64Value(1)
	return true
}

// setDefaultWLANGroupID populates wlan.WLANGroupID when it is empty. go-unifi
// serializes WLANGroupID without `omitempty`, so leaving it blank sends
// `"wlangroup_id":""` in every POST/PUT, which UniFi Network 10.x rejects with
// api.err.InvalidPayload. Default to the site's default WLAN group (mirrors the
// AP-group handling in Create).
func (r *wlanFrameworkResource) setDefaultWLANGroupID(
	ctx context.Context,
	site string,
	wlan *unifi.WLAN,
) error {
	if wlan.WLANGroupID != "" {
		return nil
	}
	groups, err := r.client.ListWLANGroup(ctx, site)
	if err != nil {
		return err
	}
	// The default WLAN group reports attr_hidden_id "Default" (note the casing
	// differs from AP groups, which use "default"); match case-insensitively.
	for _, group := range groups {
		if strings.EqualFold(group.HiddenID, "default") {
			wlan.WLANGroupID = group.ID
			return nil
		}
	}
	if len(groups) > 0 {
		wlan.WLANGroupID = groups[0].ID
	}
	return nil
}

func (r *wlanFrameworkResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan wlanFrameworkResourceModel

	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
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

	// Convert the plan to UniFi WLAN struct
	wlan, diags := r.planToWLAN(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Write-only passphrase: read from config, use at apply time, never persist.
	passphraseWO := r.readPassphraseWO(ctx, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !passphraseWO.IsNull() && !passphraseWO.IsUnknown() {
		wlan.Passphrase = passphraseWO.ValueString()
	}

	// UDM SE API requires ap_group_ids to be set even when ap_group_mode is "all".
	// Look up and set the default AP group ID if ap_group_mode is "all" and no ap_group_ids specified.
	if wlan.ApGroupMode == "all" && len(wlan.ApGroupIDs) == 0 {
		apGroups, err := r.client.ListAPGroup(ctx, site)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Listing AP Groups",
				"Could not list AP groups: "+err.Error(),
			)
			return
		}
		// Find the default AP group (attr_hidden_id == "default")
		for _, group := range apGroups {
			if group.HiddenID == "default" {
				wlan.ApGroupIDs = []string{group.ID}
				break
			}
		}
		// If no default found, use the first group
		if len(wlan.ApGroupIDs) == 0 && len(apGroups) > 0 {
			wlan.ApGroupIDs = []string{apGroups[0].ID}
		}
	}

	if err := r.setDefaultWLANGroupID(ctx, site, wlan); err != nil {
		resp.Diagnostics.AddError(
			"Error Listing WLAN Groups",
			"Could not list WLAN groups: "+err.Error(),
		)
		return
	}

	// Create the WLAN
	createdWLAN, err := r.client.CreateWLAN(ctx, site, wlan)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating WLAN",
			"Could not create WLAN: "+err.Error(),
		)
		return
	}

	// Convert response back to model
	diags = r.wlanToModel(ctx, createdWLAN, &plan, site)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// When the write-only passphrase is used, never persist the secret to state:
	// keep `passphrase` null (matching the config) instead of the API echo.
	if !passphraseWO.IsNull() {
		plan.Passphrase = types.StringNull()
	}

	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), plan.ID)...)
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *wlanFrameworkResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state wlanFrameworkResourceModel

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

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	var wlan *unifi.WLAN
	var err error

	if !state.ID.IsNull() && !state.ID.IsUnknown() {
		wlan, err = r.client.GetWLAN(ctx, site, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Reading WLAN",
				"Could not read WLAN with ID "+state.ID.ValueString()+": "+err.Error(),
			)
			return
		}
	} else {
		wlan, err = r.client.GetWLANByName(ctx, site, state.Name.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Reading WLAN",
				"Could not read WLAN with Name "+state.Name.ValueString()+": "+err.Error(),
			)
			return
		}
	}

	// Convert API response to model
	diags = r.wlanToModel(ctx, wlan, &state, site)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *wlanFrameworkResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var state wlanFrameworkResourceModel
	var plan wlanFrameworkResourceModel

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
	state.Timeouts = plan.Timeouts

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	// Step 3: Convert the updated state to API format
	wlan, diags := r.planToWLAN(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Write-only passphrase: read from config, use at apply time, never persist.
	passphraseWO := r.readPassphraseWO(ctx, req.Config, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if !passphraseWO.IsNull() && !passphraseWO.IsUnknown() {
		wlan.Passphrase = passphraseWO.ValueString()
	}

	// Step 4: Send to API
	wlan.ID = state.ID.ValueString()
	if err := r.setDefaultWLANGroupID(ctx, site, wlan); err != nil {
		resp.Diagnostics.AddError(
			"Error Listing WLAN Groups",
			"Could not list WLAN groups: "+err.Error(),
		)
		return
	}
	updatedWLAN, err := r.client.UpdateWLAN(ctx, site, wlan)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating WLAN",
			"Could not update WLAN with ID "+state.ID.ValueString()+": "+err.Error(),
		)
		return
	}

	// Step 5: Update state with API response
	diags = r.wlanToModel(ctx, updatedWLAN, &state, site)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// When the write-only passphrase is used, never persist the secret to state.
	if !passphraseWO.IsNull() {
		state.Passphrase = types.StringNull()
	}

	resp.Diagnostics.Append(resp.Identity.SetAttribute(ctx, path.Root("id"), state.ID)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// readPassphraseWO reads the write-only passphrase_wo attribute from config.
// Write-only values are only available via the request config (never plan/state).
func (r *wlanFrameworkResource) readPassphraseWO(
	ctx context.Context,
	config tfsdk.Config,
	diags *diag.Diagnostics,
) types.String {
	var passphraseWO types.String
	diags.Append(config.GetAttribute(ctx, path.Root("passphrase_wo"), &passphraseWO)...)
	return passphraseWO
}

// applyPlanToState merges plan values into state, preserving state values where plan is null/unknown.
func (r *wlanFrameworkResource) applyPlanToState(
	_ context.Context,
	plan *wlanFrameworkResourceModel,
	state *wlanFrameworkResourceModel,
) {
	// Apply plan values to state, but only if plan value is not null/unknown
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		state.Name = plan.Name
	}
	if !plan.NetworkID.IsNull() && !plan.NetworkID.IsUnknown() {
		state.NetworkID = plan.NetworkID
	}
	if !plan.UserGroupID.IsNull() && !plan.UserGroupID.IsUnknown() {
		state.UserGroupID = plan.UserGroupID
	}
	if !plan.Security.IsNull() && !plan.Security.IsUnknown() {
		state.Security = plan.Security
	}
	if !plan.WPA3Support.IsNull() && !plan.WPA3Support.IsUnknown() {
		state.WPA3Support = plan.WPA3Support
	}
	if !plan.WPA3Transition.IsNull() && !plan.WPA3Transition.IsUnknown() {
		state.WPA3Transition = plan.WPA3Transition
	}
	if !plan.PMFMode.IsNull() && !plan.PMFMode.IsUnknown() {
		state.PMFMode = plan.PMFMode
	}
	if !plan.Passphrase.IsNull() && !plan.Passphrase.IsUnknown() {
		state.Passphrase = plan.Passphrase
	}
	if !plan.HideSSID.IsNull() && !plan.HideSSID.IsUnknown() {
		state.HideSSID = plan.HideSSID
	}
	if !plan.IsGuest.IsNull() && !plan.IsGuest.IsUnknown() {
		state.IsGuest = plan.IsGuest
	}
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		state.Enabled = plan.Enabled
	}
	if !plan.ApGroupIDs.IsNull() && !plan.ApGroupIDs.IsUnknown() {
		state.ApGroupIDs = plan.ApGroupIDs
	}
	if !plan.ApGroupMode.IsNull() && !plan.ApGroupMode.IsUnknown() {
		state.ApGroupMode = plan.ApGroupMode
	}
	if !plan.VLANEnabled.IsNull() && !plan.VLANEnabled.IsUnknown() {
		state.VLANEnabled = plan.VLANEnabled
	}
	if !plan.VLAN.IsNull() && !plan.VLAN.IsUnknown() {
		state.VLAN = plan.VLAN
	}
	if !plan.WLANBand.IsNull() && !plan.WLANBand.IsUnknown() {
		state.WLANBand = plan.WLANBand
	}
	if !plan.WLANBands.IsNull() && !plan.WLANBands.IsUnknown() {
		state.WLANBands = plan.WLANBands
	}
	if !plan.MulticastEnhance.IsNull() && !plan.MulticastEnhance.IsUnknown() {
		state.MulticastEnhance = plan.MulticastEnhance
	}
	if !plan.MacFilter.IsNull() && !plan.MacFilter.IsUnknown() {
		state.MacFilter = plan.MacFilter
	}
	if !plan.PrivatePresharedKeysEnabled.IsNull() &&
		!plan.PrivatePresharedKeysEnabled.IsUnknown() {
		state.PrivatePresharedKeysEnabled = plan.PrivatePresharedKeysEnabled
	}
	if !plan.PrivatePresharedKeys.IsNull() && !plan.PrivatePresharedKeys.IsUnknown() {
		state.PrivatePresharedKeys = plan.PrivatePresharedKeys
	}
	if !plan.RadiusProfileID.IsNull() && !plan.RadiusProfileID.IsUnknown() {
		state.RadiusProfileID = plan.RadiusProfileID
	}
	if !plan.NasIDentifierType.IsNull() && !plan.NasIDentifierType.IsUnknown() {
		state.NasIDentifierType = plan.NasIDentifierType
	}
	if !plan.Schedule.IsNull() && !plan.Schedule.IsUnknown() {
		state.Schedule = plan.Schedule
	}
	if !plan.No2GhzOui.IsNull() && !plan.No2GhzOui.IsUnknown() {
		state.No2GhzOui = plan.No2GhzOui
	}
	if !plan.L2Isolation.IsNull() && !plan.L2Isolation.IsUnknown() {
		state.L2Isolation = plan.L2Isolation
	}
	if !plan.ProxyArp.IsNull() && !plan.ProxyArp.IsUnknown() {
		state.ProxyArp = plan.ProxyArp
	}
	if !plan.BssTransition.IsNull() && !plan.BssTransition.IsUnknown() {
		state.BssTransition = plan.BssTransition
	}
	if !plan.Uapsd.IsNull() && !plan.Uapsd.IsUnknown() {
		state.Uapsd = plan.Uapsd
	}
	if !plan.FastRoamingEnabled.IsNull() && !plan.FastRoamingEnabled.IsUnknown() {
		state.FastRoamingEnabled = plan.FastRoamingEnabled
	}
	if !plan.MinimumDataRate2GKbps.IsNull() && !plan.MinimumDataRate2GKbps.IsUnknown() {
		state.MinimumDataRate2GKbps = plan.MinimumDataRate2GKbps
	}
	if !plan.MinimumDataRate5GKbps.IsNull() && !plan.MinimumDataRate5GKbps.IsUnknown() {
		state.MinimumDataRate5GKbps = plan.MinimumDataRate5GKbps
	}
	if !plan.MinrateSettingPreference.IsNull() && !plan.MinrateSettingPreference.IsUnknown() {
		state.MinrateSettingPreference = plan.MinrateSettingPreference
	}
	if !plan.RoamingAssistantNaEnabled.IsNull() && !plan.RoamingAssistantNaEnabled.IsUnknown() {
		state.RoamingAssistantNaEnabled = plan.RoamingAssistantNaEnabled
	}
	if !plan.RoamingAssistantNaRssi.IsNull() && !plan.RoamingAssistantNaRssi.IsUnknown() {
		state.RoamingAssistantNaRssi = plan.RoamingAssistantNaRssi
	}
	if !plan.RoamingAssistant6EEnabled.IsNull() && !plan.RoamingAssistant6EEnabled.IsUnknown() {
		state.RoamingAssistant6EEnabled = plan.RoamingAssistant6EEnabled
	}
	if !plan.RoamingAssistant6ERssi.IsNull() && !plan.RoamingAssistant6ERssi.IsUnknown() {
		state.RoamingAssistant6ERssi = plan.RoamingAssistant6ERssi
	}
	if !plan.WPAMode.IsNull() && !plan.WPAMode.IsUnknown() {
		state.WPAMode = plan.WPAMode
	}
	if !plan.WPAEnc.IsNull() && !plan.WPAEnc.IsUnknown() {
		state.WPAEnc = plan.WPAEnc
	}
	if !plan.DTIMMode.IsNull() && !plan.DTIMMode.IsUnknown() {
		state.DTIMMode = plan.DTIMMode
	}
	if !plan.DTIMNg.IsNull() && !plan.DTIMNg.IsUnknown() {
		state.DTIMNg = plan.DTIMNg
	}
	if !plan.DTIMNa.IsNull() && !plan.DTIMNa.IsUnknown() {
		state.DTIMNa = plan.DTIMNa
	}
	if !plan.DTIM6E.IsNull() && !plan.DTIM6E.IsUnknown() {
		state.DTIM6E = plan.DTIM6E
	}
	if !plan.GroupRekey.IsNull() && !plan.GroupRekey.IsUnknown() {
		state.GroupRekey = plan.GroupRekey
	}
	if !plan.IappEnabled.IsNull() && !plan.IappEnabled.IsUnknown() {
		state.IappEnabled = plan.IappEnabled
	}
	if !plan.WPA3FastRoaming.IsNull() && !plan.WPA3FastRoaming.IsUnknown() {
		state.WPA3FastRoaming = plan.WPA3FastRoaming
	}
	if !plan.WPA3Enhanced192.IsNull() && !plan.WPA3Enhanced192.IsUnknown() {
		state.WPA3Enhanced192 = plan.WPA3Enhanced192
	}
	if !plan.RADIUSMacAuthEnabled.IsNull() && !plan.RADIUSMacAuthEnabled.IsUnknown() {
		state.RADIUSMacAuthEnabled = plan.RADIUSMacAuthEnabled
	}
	if !plan.EnhancedIot.IsNull() && !plan.EnhancedIot.IsUnknown() {
		state.EnhancedIot = plan.EnhancedIot
	}
	if !plan.Hotspot2ConfEnabled.IsNull() && !plan.Hotspot2ConfEnabled.IsUnknown() {
		state.Hotspot2ConfEnabled = plan.Hotspot2ConfEnabled
	}
	if !plan.MloEnabled.IsNull() && !plan.MloEnabled.IsUnknown() {
		state.MloEnabled = plan.MloEnabled
	}
	if !plan.BroadcastFilterList.IsNull() && !plan.BroadcastFilterList.IsUnknown() {
		state.BroadcastFilterList = plan.BroadcastFilterList
	}
}

func (r *wlanFrameworkResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state wlanFrameworkResourceModel

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

	site := state.Site.ValueString()
	if site == "" {
		site = r.client.Site
	}

	id := state.ID.ValueString()

	err := r.client.DeleteWLAN(ctx, site, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Deleting WLAN",
			"Could not delete WLAN with ID "+id+": "+err.Error(),
		)
		return
	}
}

func (r *wlanFrameworkResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	idParts := strings.Split(req.ID, ":")
	if len(idParts) == 2 {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), idParts[0])...)
		req.ID = idParts[1]
	}

	rootAttributeName := "name"
	if strings.HasPrefix(req.ID, "name=") {
		req.ID = strings.TrimPrefix(req.ID, "name=")
	} else if regexp.MustCompile(`^[0-9a-f]{24}$`).MatchString(req.ID) {
		rootAttributeName = "id"
	}

	resource.ImportStatePassthroughID(ctx, path.Root(rootAttributeName), req, resp)
}

// Helper functions for conversion and merging

func (r *wlanFrameworkResource) planToWLAN(
	ctx context.Context,
	plan wlanFrameworkResourceModel,
) (*unifi.WLAN, diag.Diagnostics) {
	var diags diag.Diagnostics

	wlan := &unifi.WLAN{
		ID:                       plan.ID.ValueString(),
		Name:                     plan.Name.ValueString(),
		NetworkID:                plan.NetworkID.ValueString(),
		UserGroupID:              plan.UserGroupID.ValueString(),
		Security:                 plan.Security.ValueString(),
		WPA3Support:              plan.WPA3Support.ValueBool(),
		WPA3Transition:           plan.WPA3Transition.ValueBool(),
		PMFMode:                  plan.PMFMode.ValueString(),
		Passphrase:               plan.Passphrase.ValueString(),
		HideSSID:                 plan.HideSSID.ValueBool(),
		IsGuest:                  plan.IsGuest.ValueBool(),
		Enabled:                  plan.Enabled.ValueBool(),
		ApGroupMode:              plan.ApGroupMode.ValueString(),
		VLANEnabled:              plan.VLANEnabled.ValueBool(),
		VLAN:                     plan.VLAN.ValueInt64Pointer(),
		MulticastEnhanceEnabled:  plan.MulticastEnhance.ValueBool(),
		RADIUSProfileID:          plan.RadiusProfileID.ValueString(),
		NasIDentifierType:        plan.NasIDentifierType.ValueString(),
		No2GhzOui:                plan.No2GhzOui.ValueBool(),
		L2Isolation:              plan.L2Isolation.ValueBool(),
		ProxyArp:                 plan.ProxyArp.ValueBool(),
		BssTransition:            plan.BssTransition.ValueBool(),
		UapsdEnabled:             plan.Uapsd.ValueBool(),
		FastRoamingEnabled:       plan.FastRoamingEnabled.ValueBool(),
		MinrateSettingPreference: plan.MinrateSettingPreference.ValueString(),
		MinrateNgEnabled:         plan.MinimumDataRate2GKbps.ValueInt64() > 0,
		MinrateNgDataRateKbps:    plan.MinimumDataRate2GKbps.ValueInt64Pointer(),
		MinrateNaEnabled:         plan.MinimumDataRate5GKbps.ValueInt64() > 0,
		MinrateNaDataRateKbps:    plan.MinimumDataRate5GKbps.ValueInt64Pointer(),

		RoamingAssistantNaEnabled: plan.RoamingAssistantNaEnabled.ValueBool(),
		RoamingAssistantNaRssi:    plan.RoamingAssistantNaRssi.ValueInt64Pointer(),
		RoamingAssistant6EEnabled: plan.RoamingAssistant6EEnabled.ValueBool(),
		RoamingAssistant6ERssi:    plan.RoamingAssistant6ERssi.ValueInt64Pointer(),

		GroupRekey:         plan.GroupRekey.ValueInt64Pointer(),
		DTIMMode:           plan.DTIMMode.ValueString(),
		WPAEnc:             plan.WPAEnc.ValueString(),
		WPAMode:            plan.WPAMode.ValueString(),
		NameCombineEnabled: true,

		IappEnabled:          plan.IappEnabled.ValueBool(),
		WPA3FastRoaming:      plan.WPA3FastRoaming.ValueBool(),
		WPA3Enhanced192:      plan.WPA3Enhanced192.ValueBool(),
		RADIUSMACAuthEnabled: plan.RADIUSMacAuthEnabled.ValueBool(),
		EnhancedIot:          plan.EnhancedIot.ValueBool(),
		Hotspot2ConfEnabled:  plan.Hotspot2ConfEnabled.ValueBool(),
		MloEnabled:           plan.MloEnabled.ValueBool(),
	}

	// DTIM per-band values (only sent when explicitly configured)
	if !plan.DTIMNg.IsNull() && !plan.DTIMNg.IsUnknown() {
		wlan.DTIMNg = plan.DTIMNg.ValueInt64Pointer()
	}
	if !plan.DTIMNa.IsNull() && !plan.DTIMNa.IsUnknown() {
		wlan.DTIMNa = plan.DTIMNa.ValueInt64Pointer()
	}
	if !plan.DTIM6E.IsNull() && !plan.DTIM6E.IsUnknown() {
		wlan.DTIM6E = plan.DTIM6E.ValueInt64Pointer()
	}

	// Broadcast filter list
	if !plan.BroadcastFilterList.IsNull() && !plan.BroadcastFilterList.IsUnknown() {
		var bcList []types.String
		diags.Append(plan.BroadcastFilterList.ElementsAs(ctx, &bcList, false)...)
		if diags.HasError() {
			return nil, diags
		}
		for _, mac := range bcList {
			wlan.BroadcastFilterList = append(wlan.BroadcastFilterList, mac.ValueString())
		}
	}

	// Handle MAC filter
	if !plan.MacFilter.IsNull() && !plan.MacFilter.IsUnknown() {
		var macFilter wlanMacFilterModel
		diags.Append(plan.MacFilter.As(ctx, &macFilter, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return nil, diags
		}

		wlan.MACFilterEnabled = macFilter.Enabled.ValueBool()
		wlan.MACFilterPolicy = macFilter.Policy.ValueString()

		if !macFilter.List.IsNull() && !macFilter.List.IsUnknown() {
			var macList []types.String
			diags.Append(macFilter.List.ElementsAs(ctx, &macList, false)...)
			if diags.HasError() {
				return nil, diags
			}

			for _, mac := range macList {
				wlan.MACFilterList = append(wlan.MACFilterList, mac.ValueString())
			}
		}
	}

	// Handle private pre-shared keys (PPSK)
	wlan.PrivatePresharedKeysEnabled = plan.PrivatePresharedKeysEnabled.ValueBool()
	if !plan.PrivatePresharedKeys.IsNull() && !plan.PrivatePresharedKeys.IsUnknown() {
		var ppskList []wlanPrivatePresharedKeyModel
		diags.Append(plan.PrivatePresharedKeys.ElementsAs(ctx, &ppskList, false)...)
		if diags.HasError() {
			return nil, diags
		}

		for _, ppsk := range ppskList {
			wlan.PrivatePresharedKeys = append(
				wlan.PrivatePresharedKeys,
				unifi.WLANPrivatePresharedKeys{
					NetworkID: ppsk.NetworkID.ValueString(),
					Password:  ppsk.Password.ValueString(),
				},
			)
		}
	}

	// Handle AP group IDs
	if !plan.ApGroupIDs.IsNull() && !plan.ApGroupIDs.IsUnknown() {
		var apGroupList []types.String
		diags.Append(plan.ApGroupIDs.ElementsAs(ctx, &apGroupList, false)...)
		if diags.HasError() {
			return nil, diags
		}

		for _, apGroupID := range apGroupList {
			wlan.ApGroupIDs = append(wlan.ApGroupIDs, apGroupID.ValueString())
		}
	}

	// Handle WLAN bands
	if !plan.WLANBands.IsNull() && !plan.WLANBands.IsUnknown() {
		var contains2g, contains5g bool

		var wlanBandsList []types.String
		diags.Append(plan.WLANBands.ElementsAs(ctx, &wlanBandsList, false)...)
		if diags.HasError() {
			return nil, diags
		}

		for _, band := range wlanBandsList {
			switch band.ValueString() {
			case "2g":
				contains2g = true
			case "5g":
				contains5g = true
			}
			wlan.WLANBands = append(wlan.WLANBands, band.ValueString())
		}

		if contains2g && contains5g {
			wlan.WLANBand = "both"
		} else if contains2g {
			wlan.WLANBand = "2g"
		} else if contains5g {
			wlan.WLANBand = "5g"
		}
	}

	// Handle schedule
	if !plan.Schedule.IsNull() && !plan.Schedule.IsUnknown() {
		var schedules []wlanScheduleModel
		diags.Append(plan.Schedule.ElementsAs(ctx, &schedules, false)...)
		if diags.HasError() {
			return nil, diags
		}

		for _, sched := range schedules {
			wlan.ScheduleWithDuration = append(
				wlan.ScheduleWithDuration,
				unifi.WLANScheduleWithDuration{
					StartDaysOfWeek: []string{sched.DayOfWeek.ValueString()},
					StartHour:       sched.StartHour.ValueInt64Pointer(),
					StartMinute:     sched.StartMinute.ValueInt64Pointer(),
					DurationMinutes: util.DurationUnitsPtr(sched.Duration, time.Minute),
					Name:            sched.Name.ValueString(),
				},
			)
		}
		wlan.ScheduleEnabled = len(wlan.ScheduleWithDuration) > 0
	}

	// The go-unifi schedule_with_duration field has no omitempty, so a nil slice
	// marshals as `null`, which the controller rejects with api.err.InvalidPayload.
	// Always send an empty list instead of null when there are no schedules.
	if wlan.ScheduleWithDuration == nil {
		wlan.ScheduleWithDuration = []unifi.WLANScheduleWithDuration{}
	}

	return wlan, diags
}

func (r *wlanFrameworkResource) wlanToModel(
	_ context.Context,
	wlan *unifi.WLAN,
	model *wlanFrameworkResourceModel,
	site string,
) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(wlan.ID)
	model.Site = types.StringValue(site)
	model.Name = types.StringValue(wlan.Name)

	if wlan.NetworkID != "" {
		model.NetworkID = types.StringValue(wlan.NetworkID)
	} else {
		model.NetworkID = types.StringNull()
	}

	model.UserGroupID = types.StringValue(wlan.UserGroupID)
	model.Security = types.StringValue(wlan.Security)
	model.WPA3Support = types.BoolValue(wlan.WPA3Support)
	model.WPA3Transition = types.BoolValue(wlan.WPA3Transition)

	if wlan.PMFMode != "" {
		model.PMFMode = types.StringValue(wlan.PMFMode)
	} else {
		model.PMFMode = types.StringValue("disabled")
	}

	// Only set passphrase if it's not empty (don't overwrite sensitive data unnecessarily)
	if wlan.Passphrase != "" {
		model.Passphrase = types.StringValue(wlan.Passphrase)
	}

	model.HideSSID = types.BoolValue(wlan.HideSSID)
	model.IsGuest = types.BoolValue(wlan.IsGuest)
	model.Enabled = types.BoolValue(wlan.Enabled)

	if wlan.ApGroupMode != "" {
		model.ApGroupMode = types.StringValue(wlan.ApGroupMode)
	} else {
		model.ApGroupMode = types.StringValue("all")
	}

	model.VLANEnabled = types.BoolValue(wlan.VLANEnabled)
	model.VLAN = types.Int64PointerValue(wlan.VLAN)

	if wlan.WLANBand != "" {
		model.WLANBand = types.StringValue(wlan.WLANBand)
	} else {
		model.WLANBand = types.StringValue("both")
	}

	model.MulticastEnhance = types.BoolValue(wlan.MulticastEnhanceEnabled)

	// Handle MAC filter
	macFilterEnabled := types.BoolValue(wlan.MACFilterEnabled)
	macFilterPolicy := types.StringValue("deny")
	if wlan.MACFilterPolicy != "" {
		macFilterPolicy = types.StringValue(wlan.MACFilterPolicy)
	}

	var macFilterList types.Set
	if len(wlan.MACFilterList) > 0 {
		macValues := make([]attr.Value, len(wlan.MACFilterList))
		for i, mac := range wlan.MACFilterList {
			macValues[i] = types.StringValue(mac)
		}
		var d diag.Diagnostics
		macFilterList, d = types.SetValue(types.StringType, macValues)
		diags.Append(d...)
	} else {
		macFilterList = types.SetNull(types.StringType)
	}

	macFilterObj, d := types.ObjectValue(
		map[string]attr.Type{
			"enabled": types.BoolType,
			"list":    types.SetType{ElemType: types.StringType},
			"policy":  types.StringType,
		},
		map[string]attr.Value{
			"enabled": macFilterEnabled,
			"list":    macFilterList,
			"policy":  macFilterPolicy,
		},
	)
	diags.Append(d...)
	model.MacFilter = macFilterObj

	// Handle private pre-shared keys (PPSK). The per-key password is sensitive
	// and not always echoed back by the controller; the plan value is preserved
	// in applyPlanToState for create/update, so this read path mainly serves
	// refresh and import.
	model.PrivatePresharedKeysEnabled = types.BoolValue(wlan.PrivatePresharedKeysEnabled)

	ppskType := types.ObjectType{AttrTypes: wlanPrivatePresharedKeyModel{}.AttributeTypes()}
	if len(wlan.PrivatePresharedKeys) > 0 {
		ppskValues := make([]attr.Value, len(wlan.PrivatePresharedKeys))
		for i, ppsk := range wlan.PrivatePresharedKeys {
			obj, d := types.ObjectValue(
				wlanPrivatePresharedKeyModel{}.AttributeTypes(),
				map[string]attr.Value{
					"network_id": types.StringValue(ppsk.NetworkID),
					"password":   types.StringValue(ppsk.Password),
				},
			)
			diags.Append(d...)
			ppskValues[i] = obj
		}
		ppskList, d := types.ListValue(ppskType, ppskValues)
		diags.Append(d...)
		model.PrivatePresharedKeys = ppskList
	} else {
		model.PrivatePresharedKeys = types.ListNull(ppskType)
	}

	if wlan.RADIUSProfileID != "" {
		model.RadiusProfileID = types.StringValue(wlan.RADIUSProfileID)
	} else {
		model.RadiusProfileID = types.StringNull()
	}

	if wlan.NasIDentifierType != "" {
		model.NasIDentifierType = types.StringValue(wlan.NasIDentifierType)
	} else {
		model.NasIDentifierType = types.StringValue("bssid")
	}

	model.No2GhzOui = types.BoolValue(wlan.No2GhzOui)
	model.L2Isolation = types.BoolValue(wlan.L2Isolation)
	model.ProxyArp = types.BoolValue(wlan.ProxyArp)
	model.BssTransition = types.BoolValue(wlan.BssTransition)
	model.Uapsd = types.BoolValue(wlan.UapsdEnabled)
	model.FastRoamingEnabled = types.BoolValue(wlan.FastRoamingEnabled)

	if wlan.MinrateSettingPreference != "" {
		model.MinrateSettingPreference = types.StringValue(wlan.MinrateSettingPreference)
	} else {
		model.MinrateSettingPreference = types.StringValue("auto")
	}

	model.RoamingAssistantNaEnabled = types.BoolValue(wlan.RoamingAssistantNaEnabled)
	model.RoamingAssistantNaRssi = types.Int64PointerValue(wlan.RoamingAssistantNaRssi)
	model.RoamingAssistant6EEnabled = types.BoolValue(wlan.RoamingAssistant6EEnabled)
	model.RoamingAssistant6ERssi = types.Int64PointerValue(wlan.RoamingAssistant6ERssi)

	// The API omits these fields from GET responses when unset; map the missing
	// value to 0 (the schema default) instead of null to avoid perpetual
	// null->0 plan drift after import.
	if wlan.MinrateNgDataRateKbps != nil {
		model.MinimumDataRate2GKbps = types.Int64Value(*wlan.MinrateNgDataRateKbps)
	} else {
		model.MinimumDataRate2GKbps = types.Int64Value(0)
	}
	if wlan.MinrateNaDataRateKbps != nil {
		model.MinimumDataRate5GKbps = types.Int64Value(*wlan.MinrateNaDataRateKbps)
	} else {
		model.MinimumDataRate5GKbps = types.Int64Value(0)
	}

	if wlan.WPAMode != "" {
		model.WPAMode = types.StringValue(wlan.WPAMode)
	} else {
		model.WPAMode = types.StringValue("wpa2")
	}
	if wlan.WPAEnc != "" {
		model.WPAEnc = types.StringValue(wlan.WPAEnc)
	} else {
		model.WPAEnc = types.StringValue("ccmp")
	}
	if wlan.DTIMMode != "" {
		model.DTIMMode = types.StringValue(wlan.DTIMMode)
	} else {
		model.DTIMMode = types.StringValue("default")
	}
	model.GroupRekey = types.Int64PointerValue(wlan.GroupRekey)
	model.DTIMNg = types.Int64PointerValue(wlan.DTIMNg)
	model.DTIMNa = types.Int64PointerValue(wlan.DTIMNa)
	model.DTIM6E = types.Int64PointerValue(wlan.DTIM6E)
	model.IappEnabled = types.BoolValue(wlan.IappEnabled)
	model.WPA3FastRoaming = types.BoolValue(wlan.WPA3FastRoaming)
	model.WPA3Enhanced192 = types.BoolValue(wlan.WPA3Enhanced192)
	model.RADIUSMacAuthEnabled = types.BoolValue(wlan.RADIUSMACAuthEnabled)
	model.EnhancedIot = types.BoolValue(wlan.EnhancedIot)
	model.Hotspot2ConfEnabled = types.BoolValue(wlan.Hotspot2ConfEnabled)
	model.MloEnabled = types.BoolValue(wlan.MloEnabled)

	if len(wlan.BroadcastFilterList) > 0 {
		bcValues := make([]attr.Value, len(wlan.BroadcastFilterList))
		for i, mac := range wlan.BroadcastFilterList {
			bcValues[i] = types.StringValue(mac)
		}
		bcSet, d := types.SetValue(types.StringType, bcValues)
		diags.Append(d...)
		model.BroadcastFilterList = bcSet
	} else {
		model.BroadcastFilterList = types.SetNull(types.StringType)
	}

	// Handle AP group IDs
	if len(wlan.ApGroupIDs) > 0 {
		apGroupValues := make([]attr.Value, len(wlan.ApGroupIDs))
		for i, id := range wlan.ApGroupIDs {
			apGroupValues[i] = types.StringValue(id)
		}
		apGroupSet, d := types.SetValue(types.StringType, apGroupValues)
		diags.Append(d...)
		model.ApGroupIDs = apGroupSet
	} else {
		model.ApGroupIDs = types.SetNull(types.StringType)
	}

	// Handle WLAN bands
	if len(wlan.WLANBands) > 0 {
		bandValues := make([]attr.Value, len(wlan.WLANBands))
		for i, band := range wlan.WLANBands {
			bandValues[i] = types.StringValue(band)
		}
		bandSet, d := types.SetValue(types.StringType, bandValues)
		diags.Append(d...)
		model.WLANBands = bandSet
	} else {
		model.WLANBands = types.SetNull(types.StringType)
	}

	// Handle schedule - convert WLANScheduleWithDuration back to individual schedule entries
	if len(wlan.ScheduleWithDuration) > 0 {
		var scheduleValues []attr.Value
		for _, sched := range wlan.ScheduleWithDuration {
			// Each schedule can have multiple days of week, so we need to expand them
			for _, dow := range sched.StartDaysOfWeek {
				scheduleObj, d := types.ObjectValue(
					map[string]attr.Type{
						"day_of_week":  types.StringType,
						"start_hour":   types.Int64Type,
						"start_minute": types.Int64Type,
						"duration":     timetypes.GoDurationType{},
						"name":         types.StringType,
					},
					map[string]attr.Value{
						"day_of_week":  types.StringValue(dow),
						"start_hour":   types.Int64PointerValue(sched.StartHour),
						"start_minute": types.Int64PointerValue(sched.StartMinute),
						"duration":     util.DurationPtrValue(sched.DurationMinutes, time.Minute),
						"name":         types.StringValue(sched.Name),
					},
				)
				diags.Append(d...)
				scheduleValues = append(scheduleValues, scheduleObj)
			}
		}
		scheduleList, d := types.ListValue(
			types.ObjectType{
				AttrTypes: map[string]attr.Type{
					"day_of_week":  types.StringType,
					"start_hour":   types.Int64Type,
					"start_minute": types.Int64Type,
					"duration":     timetypes.GoDurationType{},
					"name":         types.StringType,
				},
			},
			scheduleValues,
		)
		diags.Append(d...)
		model.Schedule = scheduleList
	} else {
		model.Schedule = types.ListNull(types.ObjectType{
			AttrTypes: map[string]attr.Type{
				"day_of_week":  types.StringType,
				"start_hour":   types.Int64Type,
				"start_minute": types.Int64Type,
				"duration":     timetypes.GoDurationType{},
				"name":         types.StringType,
			},
		})
	}

	return diags
}

// ListResourceConfigSchema implements [list.ListResource].
func (r *wlanFrameworkResource) ListResourceConfigSchema(
	ctx context.Context,
	_ list.ListResourceSchemaRequest,
	resp *list.ListResourceSchemaResponse,
) {
	resp.Schema = listresource_wlan.WlanListResourceSchema(ctx)
}

// List implements [list.ListResource].
func (r *wlanFrameworkResource) List(
	ctx context.Context,
	req list.ListRequest,
	stream *list.ListResultsStream,
) {
	var config wlanListConfigModel

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
	var filters []wlanListFilterModel
	if !config.Filter.IsNull() && !config.Filter.IsUnknown() {
		config.Filter.ElementsAs(ctx, &filters, false)
	}

	postFilters := make(map[string]string)
	for _, f := range filters {
		postFilters[f.Name.ValueString()] = f.Value.ValueString()
	}

	wlans, err := r.client.ListWLAN(ctx, site)
	if err != nil {
		var d diag.Diagnostics
		d.AddError("Error Listing WLANs", "Could not list WLANs: "+err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(d)
		return
	}

	stream.Results = func(push func(list.ListResult) bool) {
		for _, wlan := range wlans {
			// Apply name filter.
			if val, ok := postFilters["name"]; ok {
				if wlan.Name != val {
					continue
				}
			}

			// Apply enabled filter.
			if val, ok := postFilters["enabled"]; ok {
				enabled := fmt.Sprintf("%t", wlan.Enabled)
				if enabled != val {
					continue
				}
			}

			result := req.NewListResult(ctx)
			result.DisplayName = wlan.Name

			// Set identity.
			result.Diagnostics.Append(
				result.Identity.SetAttribute(
					ctx,
					path.Root("id"),
					types.StringValue(wlan.ID),
				)...,
			)

			// Convert to model.
			var model wlanFrameworkResourceModel
			result.Diagnostics.Append(r.wlanToModel(ctx, &wlan, &model, site)...)
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
