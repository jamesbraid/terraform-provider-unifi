package unifi

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_wlan"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
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

// wlanFrameworkResource defines the resource implementation. The kit provides
// CRUD and both mappers; what's left here is the schema graft, the state upgrader, and the enhanced-IoT plan modifier.
type wlanFrameworkResource struct {
	resourcekit.Resource[wlanKitModel, unifi.WLAN]
}

func newWLANKitResource() *wlanFrameworkResource {
	r := &wlanFrameworkResource{}
	r.Spec = wlanKitSpec()
	r.SchemaSpec = wlanKitSchema()
	r.ListSurface = wlanKitList()
	return r
}

func NewWLANFrameworkResource() resource.Resource { return newWLANKitResource() }

func NewWLANListResource() list.ListResource { return newWLANKitResource() }

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
	// Grafted rather than generated: the code specification has no write_only member,
	// so a generated passphrase_wo would drop the property that's its entire reason for existing.
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
	client, ok := resourceClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}

	r.Spec.Backend = wlanKitBackend(client.ApiClient)
	// Prefetch is bound here rather than in the Spec: it needs the client, and
	// the Spec is built before Configure runs.
	r.Spec.Prefetch = wlanPrefetch(client.ApiClient)
	r.DefaultSite = client.Site
}

// ModifyPlan reconciles the fields the controller silently overrides when
// enhanced_iot is true (iapp_enabled, wpa3_support, wpa3_transition, pmf_mode,
// dtim_ng), so the post-apply consistency check doesn't fail on every plan (#283).
func (r *wlanFrameworkResource) ModifyPlan(
	ctx context.Context,
	req resource.ModifyPlanRequest,
	resp *resource.ModifyPlanResponse,
) {
	// No plan on destroy.
	if req.Plan.Raw.IsNull() {
		return
	}

	var plan wlanKitModel
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
func applyEnhancedIotOverrides(plan *wlanKitModel) bool {
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
