package unifi

// port_profile's surface-specific half.
//
// EVERYTHING BELOW IS ABOUT PORT PROFILES AND NOTHING ELSE. The controller
// stores tagged VLAN membership as a MODE plus an EXCLUSION list; the
// practitioner writes an INCLUSION list. Translating between the two needs the
// site's whole network inventory, so it cannot live in a field mapping, and it
// is not shaped like any other surface's logic, so there is nothing here for
// the kit to hold.

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_port_profile "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_port_profile"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

type portProfileKitResource struct {
	resourcekit.Resource[portProfileKitModel, ui.PortProfile]
}

var (
	_ resource.Resource                     = &portProfileKitResource{}
	_ resource.ResourceWithImportState      = &portProfileKitResource{}
	_ resource.ResourceWithConfigValidators = &portProfileKitResource{}
	_ resource.ResourceWithIdentity         = &portProfileKitResource{}
	_ resource.ResourceWithUpgradeState     = &portProfileKitResource{}
	_ list.ListResource                     = &portProfileKitResource{}
	_ list.ListResourceWithConfigure        = &portProfileKitResource{}
)

func newPortProfileKitResource() *portProfileKitResource {
	r := &portProfileKitResource{}
	r.Spec = portProfileKitSpec()
	r.SchemaSpec = portProfileKitSchema()
	r.ListSurface = portProfileKitList()
	return r
}

func NewPortProfileFrameworkResource() resource.Resource { return newPortProfileKitResource() }

func NewPortProfileListResource() list.ListResource { return newPortProfileKitResource() }

// Schema STAYS IN THIS PACKAGE, HAND-WRITTEN, because internal/schemabehaviour
// derives what the provider applies by parsing unifi/*.go for a receiver whose
// Metadata names the surface and whose Schema it can follow, and promotion from
// an embedded type is invisible to a parser.
func (r *portProfileKitResource) Schema(
	ctx context.Context,
	_ resource.SchemaRequest,
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

// Metadata is here for the same reason: the deriver reads the surface name off
// this method, in this package.
func (r *portProfileKitResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_port_profile"
}

func (r *portProfileKitResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.Spec.Backend = portProfileKitBackend(client.ApiClient)
	// PREFETCH IS BOUND HERE, NOT IN THE SPEC, because it needs the client and
	// the spec is built before one exists. Backend is bound the same way for
	// the same reason, so this is the established seam rather than a new one.
	r.Spec.Prefetch = portProfilePrefetchNetworks(client.ApiClient)
	r.DefaultSite = client.Site
}

// THE THREE HOOKS, and this is the surface they were designed for.
//
// Prefetch reads the site's networks once per operation. BeforeSend turns the
// practitioner's inclusion list into the controller's mode plus exclusion list.
// AfterReceive turns them back. The kit runs all three on create, read, update
// AND list, which is what makes a listed port profile agree with a read one.

func portProfilePrefetchNetworks(
	client *ui.ApiClient,
) func(context.Context, string) (any, diag.Diagnostics) {
	return func(ctx context.Context, site string) (any, diag.Diagnostics) {
		var diags diag.Diagnostics
		networks, err := client.ListNetwork(ctx, site)
		if err != nil {
			diags.AddError("Error Reading Networks for Port Profile",
				"Could not read the site network inventory: "+err.Error())
			return nil, diags
		}
		return networks, diags
	}
}

func portProfileBeforeSend(
	ctx context.Context,
	config, plan *portProfileKitModel,
	sdk *ui.PortProfile,
	prefetched any,
) diag.Diagnostics {
	// THE CONFIG, NOT THE PLAN. resolvePortProfileVLANMode refuses a
	// combination the practitioner WROTE; a plan carries values Terraform
	// filled in from prior state, and judging those would reject a
	// configuration nobody wrote.
	vlanConfig, diags := portProfileVLANConfigFromModel(ctx, config)
	if diags.HasError() {
		return diags
	}
	networks, _ := prefetched.([]ui.Network)
	if err := applyPortProfileVLANConfig(
		vlanConfig,
		portProfileTaggedNetworkUniverse(networks, sdk.NATiveNetworkID),
		sdk,
	); err != nil {
		diags.AddError("Invalid tagged network selection", err.Error())
	}
	return diags
}

func portProfileAfterReceive(
	ctx context.Context,
	sdk *ui.PortProfile,
	model *portProfileKitModel,
	prior portProfileKitModel,
	prefetched any,
) diag.Diagnostics {
	networks, _ := prefetched.([]ui.Network)
	diags := setPortProfileTaggedNetworkState(ctx, sdk, networks, model)

	// lldpmed_notify_enabled is Optional-only in the schema -- no Computed, no
	// Default -- which commits the provider to returning exactly what the
	// config said. BoolField.ToModel already wrote sdk.LldpmedNotifyEnabled
	// into model unconditionally, above in Spec.ToModel; that is right for
	// every OTHER bool on this surface, where a false the controller reports
	// is a real value, but here it turns an omitted config into an explicit
	// false and Terraform refuses the apply ("was null, but now cty.False").
	// Restore null unless the prior model already carried a value or the
	// controller reports true -- the same rule the hand-written v0.102.0
	// read used, moved onto the hook that can see the prior model.
	if prior.LLDPMedNotifyEnabled.IsNull() && !sdk.LldpmedNotifyEnabled {
		model.LLDPMedNotifyEnabled = types.BoolNull()
	}

	// excluded_networkconf_ids is read back ONLY under the custom mode, which
	// no field kind expresses, so it is not in Fields and is set here.
	if sdk.TaggedVLANMgmt == "custom" {
		excluded := sdk.ExcludedNetworkIDs
		if excluded == nil {
			excluded = []string{}
		}
		value, d := types.SetValueFrom(ctx, types.StringType, excluded)
		diags.Append(d...)
		model.ExcludedNetworkConfIDs = value
	} else {
		model.ExcludedNetworkConfIDs = types.SetNull(types.StringType)
	}
	return diags
}

// portProfileTaggedNetworkUniverse returns the site networks which can be
// carried as tagged VLANs by a port profile. The native network is carried
// untagged and therefore never belongs to this set.
func portProfileTaggedNetworkUniverse(networks []ui.Network, nativeNetworkID string) []string {
	ids := make([]string, 0, len(networks))
	for _, network := range networks {
		if network.ID == "" || network.ID == nativeNetworkID || network.VLAN == nil {
			continue
		}
		switch network.Purpose {
		case ui.PurposeCorporate, ui.PurposeGuest, ui.PurposeVLANOnly:
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
	model *portProfileKitModel,
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
	api *ui.PortProfile,
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
	api *ui.PortProfile,
	networks []ui.Network,
	model *portProfileKitModel,
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

func (r *portProfileKitResource) ConfigValidators(
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
func (r *portProfileKitResource) UpgradeState(
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
