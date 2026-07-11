# Settings PR 5: new resources Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add two standalone resources — `unifi_nat_rule` (v2 NAT API) and `unifi_content_filtering` (v2 content-filtering API) — and extend `unifi_firewall_policy` with `APP`/`APP_CATEGORY` matching (`app_ids`/`app_category_ids` on the source/destination endpoints).

**Architecture:** These are plain plugin-framework resources following the repo's v2-API shape (`unifi/firewall_policy_resource.go` for discriminator handling, `unifi/ap_group_resource.go` for a recent small resource): model struct + `AttributeTypes()`, pure model↔API converter functions unit-tested without a controller, CRUD via the go-unifi client, `ResourceWithIdentity` + `ImportState` via `util.ParseImportID`. **No `settingSection` registry involvement** — these are not settings. The firewall-policy change extends the existing `matching_target` discriminator in place, including its v0→v1 state upgrader.

**Tech Stack:** Go, terraform-plugin-framework (+nettypes/hwtypes for MACs), go-unifi fork (`Nat` CRUD already exported at the pinned version; content-filtering client and firewall-policy app fields come from go-unifi PR 0).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-10-setting-sections-design.md`. This plan is PR 5 of 6.
- **Nothing is pushed or posted publicly. Local branch/commits only; James reviews before any push.**
- **go-unifi precondition (Tasks 3–5 only):** the content-filtering client and the firewall-policy `AppIDs`/`AppCategoryIDs` fields do not exist in the pinned `go-unifi v1.33.43-0.20260706191309-bc63776a9ebf`. Tasks 3–5 **require a go.mod bump to a post-PR-0 version or a local replace => `/Users/jamesb/projects/go-unifi`** (`go mod edit -replace github.com/ubiquiti-community/go-unifi=/Users/jamesb/projects/go-unifi && go mod tidy`). A local replace is fine during development but must be flagged in the final report — it is dropped before release. Each gated task starts with a dependency-gate step: if the symbols are missing, STOP and report; do not stub.
- **NAT is NOT gated** (verified in the modcache): the pinned version already exports `ListNat`, `GetNat`, `CreateNat`, `UpdateNat`, `DeleteNat` in `unifi/nat.go` (hand-written wrappers over the unexported generated client). Tasks 1–2 run against the current go.mod. Flag this in the final report — the PR 0 plan's "export NAT CRUD" item is already satisfied upstream.
- **Assumed go-unifi PR 0 API (write code against these exact names; if PR 0 lands different names, adapt at the gate step and note the delta):**
  - `unifi.ContentFiltering` struct: `ID string "_id"`, `Name string`, `Enabled bool`, `Categories []string`, `ClientMACs []string "client_macs"`, `NetworkIDs []string "network_ids"`, `AllowList []string "allow_list"`, `BlockList []string "block_list"`, `SafeSearch []string "safe_search"`, `Schedule *unifi.ContentFilteringSchedule` with `Mode string`. List fields must NOT be `omitempty` (the live objects carry explicit empty arrays; dropping the keys on PUT is untested).
  - Methods: `ListContentFiltering(ctx, site) ([]ContentFiltering, error)`, `GetContentFiltering(ctx, site, id) (*ContentFiltering, error)`, `CreateContentFiltering(ctx, site, *ContentFiltering) (*ContentFiltering, error)`, `UpdateContentFiltering(ctx, site, *ContentFiltering) (*ContentFiltering, error)`, `DeleteContentFiltering(ctx, site, id) error` — mirroring the `Nat` wrappers.
  - `unifi.FirewallPolicySource` / `unifi.FirewallPolicyDestination` gain `AppIDs []int64 "app_ids,omitempty"` and `AppCategoryIDs []int64 "app_category_ids,omitempty"`. **They are integers on the wire** (live "block shield DNS" policy: `app_ids = [589885, 1310919, 1310917]`), not strings as in filipowm. If PR 0 lands them as `[]int`, insert a loop conversion in the converters instead of direct `ElementsAs`.
- Live-payload facts baked into this plan (field names/enum tokens only, no values copied): content-filtering objects have exactly `_id, name, enabled, categories, client_macs, network_ids, allow_list, block_list, safe_search, schedule{mode}`; observed `schedule.mode` is `ALWAYS`; observed `safe_search` tokens are `GOOGLE`, `YOUTUBE`, `BING`; the APP-matched firewall-policy destination carries **no** `matching_target_type` key on the wire.
- Unordered string collections in NEW schemas are `types.Set`. MAC collections use `hwtypes.MACAddressType` elements plus `cleanMAC` normalization on the write path (the `unifi_ap_group` convention). `Optional+Computed` collections get `UseStateForUnknown`.
- Acceptance tests run **only** against the docker demo controller (`docker-compose.yaml`, `preCheck` env vars `UNIFI_API`/`UNIFI_USERNAME`/`UNIFI_PASSWORD`). The old demo controller almost certainly lacks the v2 NAT and content-filtering endpoints: both acceptance tests use a **probe-once skip guard** (list the collection; any error → `t.Skipf`). The live UDM is never touched; the "block shield DNS import" validation is a manual post-review step, documented but not automated.
- Commit style: conventional commits matching the repo log (`feat(nat): …`), body explains why, `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` trailer.
- All commands run from the repo root: `/Users/jamesb/emdash/worktrees/terraform-provider-unifi/emdash/missing-config-uyrwq`.
- Unit tests: `go test ./unifi/ -run '<pattern>' -count=1`. Full suite must stay green after every task.

---

### Task 1: `unifi_nat_rule` resource

**Files:**
- Create: `unifi/nat_rule_resource.go`
- Create: `unifi/nat_rule_resource_test.go`
- Modify: `unifi/provider.go` (register in `Resources`)

**Interfaces:**
- Consumes: `unifi.Nat`, `unifi.NatSourceFilter`, `unifi.NatDestinationFilter`, exported client methods `ListNat/GetNat/CreateNat/UpdateNat/DeleteNat`, `util.StringValueOrNull`, `util.Ptr`, `util.ParseImportID`, `timeouts`, `*Client`.
- Produces: `NewNatRuleResource`, `natRuleResourceModel`, `natRuleFilterModel` (+`AttributeTypes()`), `modelToNat`, `natToModel`, `modelToNatSourceFilter`, `modelToNatDestinationFilter`, `natSourceFilterToModel`, `natDestinationFilterToModel`, `natPortValue`.
- Deliberately out of scope: `list.ListResource` support (can ride a follow-up; keeps this PR reviewable), the read-only `attr_hidden/attr_hidden_id/attr_no_delete/attr_no_edit` and `is_predefined` fields (excluded from the schema entirely; `is_predefined` marshals as `false` which is correct for user-created rules — managing controller-predefined rules is documented as unsupported).

- [ ] **Step 1: Write the failing tests**

Create `unifi/nat_rule_resource_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func Test_modelToNat_dnat(t *testing.T) {
	ctx := context.Background()

	groups, d := types.SetValueFrom(ctx, types.StringType, []string{"fg1"})
	if d.HasError() {
		t.Fatal(d)
	}
	srcFilter, d := types.ObjectValueFrom(ctx, natRuleFilterModel{}.AttributeTypes(),
		natRuleFilterModel{
			FilterType:       types.StringValue("FIREWALL_GROUPS"),
			Address:          types.StringValue(""),
			FirewallGroupIDs: groups,
			InvertAddress:    types.BoolValue(false),
			InvertPort:       types.BoolValue(false),
			NetworkID:        types.StringValue(""),
			Port:             types.Int64Null(),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	dstFilter, d := types.ObjectValueFrom(ctx, natRuleFilterModel{}.AttributeTypes(),
		natRuleFilterModel{
			FilterType:       types.StringValue("ADDRESS_AND_PORT"),
			Address:          types.StringValue("192.0.2.10"),
			FirewallGroupIDs: types.SetNull(types.StringType),
			InvertAddress:    types.BoolValue(false),
			InvertPort:       types.BoolValue(true),
			NetworkID:        types.StringValue(""),
			Port:             types.Int64Value(8443),
		})
	if d.HasError() {
		t.Fatal(d)
	}

	m := natRuleResourceModel{
		ID:                    types.StringValue("abc123"),
		Description:           types.StringValue("dnat to web"),
		Type:                  types.StringValue("DNAT"),
		Enabled:               types.BoolValue(true),
		Exclude:               types.BoolValue(false),
		IPAddress:             types.StringValue("10.0.0.5"),
		InInterface:           types.StringValue("eth4"),
		OutInterface:          types.StringNull(),
		Logging:               types.BoolValue(true),
		Port:                  types.Int64Value(443),
		PppoeUseBaseInterface: types.BoolValue(false),
		Protocol:              types.StringValue("tcp"),
		RuleIndex:             types.Int64Value(2000),
		SettingPreference:     types.StringValue("manual"),
		IPVersion:             types.StringValue("IPV4"),
		SourceFilter:          srcFilter,
		DestinationFilter:     dstFilter,
	}

	nat, diags := modelToNat(ctx, m)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if nat.ID != "abc123" || nat.Type != "DNAT" || nat.Description != "dnat to web" {
		t.Fatalf("scalar fields wrong: %+v", nat)
	}
	if nat.IPAddress != "10.0.0.5" || nat.InInterface != "eth4" || nat.OutInterface != "" {
		t.Fatalf("address/interface fields wrong: %+v", nat)
	}
	if !nat.Enabled || nat.Exclude || !nat.Logging {
		t.Fatalf("bool fields wrong: %+v", nat)
	}
	if nat.Protocol != "tcp" || nat.Version != "IPV4" || nat.SettingPreference != "manual" {
		t.Fatalf("enum fields wrong: %+v", nat)
	}
	if nat.Port == nil || *nat.Port != 443 {
		t.Fatalf("port = %v, want 443", nat.Port)
	}
	if nat.RuleIndex == nil || *nat.RuleIndex != 2000 {
		t.Fatalf("rule_index = %v, want 2000", nat.RuleIndex)
	}
	if nat.SourceFilter == nil || nat.SourceFilter.FilterType != "FIREWALL_GROUPS" ||
		len(nat.SourceFilter.FirewallGroupIDs) != 1 ||
		nat.SourceFilter.FirewallGroupIDs[0] != "fg1" {
		t.Fatalf("source_filter wrong: %+v", nat.SourceFilter)
	}
	if nat.DestinationFilter == nil || nat.DestinationFilter.FilterType != "ADDRESS_AND_PORT" ||
		nat.DestinationFilter.Address != "192.0.2.10" ||
		!nat.DestinationFilter.InvertPort ||
		nat.DestinationFilter.Port == nil || *nat.DestinationFilter.Port != 8443 {
		t.Fatalf("destination_filter wrong: %+v", nat.DestinationFilter)
	}
}

func Test_modelToNat_masqueradeMinimal(t *testing.T) {
	ctx := context.Background()
	m := natRuleResourceModel{
		Type:              types.StringValue("MASQUERADE"),
		OutInterface:      types.StringValue("eth8"),
		Enabled:           types.BoolValue(true),
		Exclude:           types.BoolValue(false),
		Logging:           types.BoolValue(false),
		Description:       types.StringValue(""),
		Port:              types.Int64Null(),
		RuleIndex:         types.Int64Null(),
		SourceFilter:      types.ObjectNull(natRuleFilterModel{}.AttributeTypes()),
		DestinationFilter: types.ObjectNull(natRuleFilterModel{}.AttributeTypes()),
	}

	nat, diags := modelToNat(ctx, m)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if nat.Type != "MASQUERADE" || nat.OutInterface != "eth8" {
		t.Fatalf("fields wrong: %+v", nat)
	}
	if nat.Port != nil {
		t.Fatalf("unset port must marshal as nil (omitted), got %v", *nat.Port)
	}
	if nat.RuleIndex != nil {
		t.Fatalf("unset rule_index must marshal as nil (omitted), got %v", *nat.RuleIndex)
	}
	if nat.SourceFilter != nil || nat.DestinationFilter != nil {
		t.Fatal("null filter objects must map to nil filters")
	}
}

func Test_natToModel_roundTrip(t *testing.T) {
	ctx := context.Background()
	port := int64(443)
	idx := int64(2010)
	nat := &unifi.Nat{
		ID:           "abc123",
		Type:         "SNAT",
		Description:  "snat out",
		Enabled:      true,
		Exclude:      false,
		Logging:      false,
		IPAddress:    "203.0.113.7",
		OutInterface: "eth8",
		Protocol:     "all",
		Port:         &port,
		RuleIndex:    &idx,
		Version:      "IPV4",
		SourceFilter: &unifi.NatSourceFilter{
			FilterType: "NETWORK_CONF", NetworkConfID: "net1",
		},
	}

	var m natRuleResourceModel
	diags := natToModel(ctx, nat, &m)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if m.ID.ValueString() != "abc123" || m.Type.ValueString() != "SNAT" {
		t.Fatalf("id/type wrong: %v %v", m.ID, m.Type)
	}
	if m.IPAddress.ValueString() != "203.0.113.7" || m.OutInterface.ValueString() != "eth8" {
		t.Fatalf("address fields wrong: %v %v", m.IPAddress, m.OutInterface)
	}
	if !m.InInterface.IsNull() {
		t.Fatalf("empty in_interface should be null, got %v", m.InInterface)
	}
	if m.Port.ValueInt64() != 443 || m.RuleIndex.ValueInt64() != 2010 {
		t.Fatalf("port/rule_index wrong: %v %v", m.Port, m.RuleIndex)
	}
	if m.SourceFilter.IsNull() {
		t.Fatal("source_filter should be set")
	}
	var fm natRuleFilterModel
	d := m.SourceFilter.As(ctx, &fm, objectAsOptions)
	if d.HasError() {
		t.Fatal(d)
	}
	if fm.FilterType.ValueString() != "NETWORK_CONF" || fm.NetworkID.ValueString() != "net1" {
		t.Fatalf("filter wrong: %+v", fm)
	}
	if !m.DestinationFilter.IsNull() {
		t.Fatal("nil destination filter should map to null object")
	}
}

func Test_natToModel_zeroPortIsNull(t *testing.T) {
	// go-unifi's UnmarshalJSON maps an empty-string port to *int64(0); the
	// model must treat 0 as "no port" so plans stay clean.
	ctx := context.Background()
	zero := int64(0)
	nat := &unifi.Nat{ID: "x", Type: "MASQUERADE", Port: &zero}

	var m natRuleResourceModel
	if diags := natToModel(ctx, nat, &m); diags.HasError() {
		t.Fatal(diags)
	}
	if !m.Port.IsNull() {
		t.Fatalf("zero port should be null, got %v", m.Port)
	}
}

func Test_natRuleResource_Schema(t *testing.T) {
	r := &natRuleResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	for _, name := range []string{
		"id", "site", "type", "description", "enabled", "exclude", "ip_address",
		"in_interface", "out_interface", "logging", "port",
		"pppoe_use_base_interface", "protocol", "rule_index",
		"setting_preference", "ip_version", "source_filter",
		"destination_filter", "timeouts",
	} {
		if _, ok := resp.Schema.Attributes[name]; !ok {
			t.Errorf("schema missing attribute %q", name)
		}
	}
	src, ok := resp.Schema.Attributes["source_filter"].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatal("source_filter is not a SingleNestedAttribute")
	}
	for _, name := range []string{
		"filter_type", "address", "firewall_group_ids",
		"invert_address", "invert_port", "network_id", "port",
	} {
		if _, ok := src.Attributes[name]; !ok {
			t.Errorf("source_filter missing attribute %q", name)
		}
	}
}

func Test_natRuleResource_Metadata(t *testing.T) {
	r := &natRuleResource{}
	var resp fwresource.MetadataResponse
	r.Metadata(context.Background(),
		fwresource.MetadataRequest{ProviderTypeName: "unifi"}, &resp)
	if resp.TypeName != "unifi_nat_rule" {
		t.Fatalf("TypeName = %q, want unifi_nat_rule", resp.TypeName)
	}
}

func TestNewNatRuleResource(t *testing.T) {
	got := NewNatRuleResource()
	if _, ok := got.(fwresource.ResourceWithImportState); !ok {
		t.Error("NewNatRuleResource() does not implement resource.ResourceWithImportState")
	}
}
```

Note: the test uses a package-level `objectAsOptions` helper. If the repo doesn't already have one, the resource file (Step 3) declares `var objectAsOptions = basetypes.ObjectAsOptions{}` — check first with `grep -rn 'objectAsOptions' unifi/*.go`; if a name collision exists, inline `basetypes.ObjectAsOptions{}` in the test instead (adding the `basetypes` import).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_modelToNat|Test_natToModel|Test_natRuleResource|TestNewNatRuleResource' -count=1`
Expected: compile FAILURE — `undefined: natRuleResourceModel`, `undefined: modelToNat`, etc.

- [ ] **Step 3: Create `unifi/nat_rule_resource.go`**

```go
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
				Attributes:          filterAttrs,
			},
			"destination_filter": schema.SingleNestedAttribute{
				MarkdownDescription: "Filter on the traffic destination.",
				Optional:            true,
				Attributes:          filterAttrs,
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
```

Note on `Test_natToModel_roundTrip`: `nat.SourceFilter` in the API response carries `Address: ""` → the model gets `StringValue("")`, matching the schema default. The test's filter assertions only check `filter_type`/`network_id`, so no conflict.

- [ ] **Step 4: Register the resource**

In `unifi/provider.go`, in the `Resources` slice, after `NewTrafficRouteResource,` add:

```go
		NewNatRuleResource,
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_modelToNat|Test_natToModel|Test_natRuleResource|TestNewNatRuleResource' -count=1 -v`
Expected: PASS. If `objectAsOptions` collides with an existing declaration, delete the new one and keep the existing.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go vet ./unifi/... && go test ./unifi/ -count=1 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/nat_rule_resource.go unifi/nat_rule_resource_test.go unifi/provider.go
git commit -m "feat(nat): add unifi_nat_rule resource"
```

(Body: v2 `nat` API, DNAT/SNAT/MASQUERADE with filter_type-discriminated source/destination filters; predefined/attr_ fields excluded by design; go-unifi already exports the NAT client at the pinned version.)

---

### Task 2: `unifi_nat_rule` acceptance tests (docker demo controller)

**Files:**
- Modify: `unifi/nat_rule_resource_test.go` (append)

**Interfaces:**
- Consumes: `preCheck(t)`, `testAccProtoV6ProviderFactories`, `unifi.New`, `unifi.Config`, `Client`, `resource.Test`.
- Produces: `testAccNatRulePreCheck` (probe-once skip guard), `testAccNatRuleCheckDestroy`.

- [ ] **Step 1: Append acceptance tests**

Add imports to `unifi/nat_rule_resource_test.go`: `"fmt"`, `"os"`, `"github.com/hashicorp/terraform-plugin-testing/helper/resource"`, `"github.com/hashicorp/terraform-plugin-testing/terraform"`. Then append:

```go
// testAccNatRulePreCheck skips when the controller does not expose the v2 NAT
// API (the docker demo controller predates it). The probe is a plain list:
// any error — 404, HTML error page, api.err.* — means "unsupported here".
func testAccNatRulePreCheck(t *testing.T) {
	preCheck(t)
	ctx := context.Background()
	apiClient, err := unifi.New(ctx, &unifi.Config{
		BaseURL:       os.Getenv("UNIFI_API"),
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("could not build probe client: %v", err)
	}
	c := &Client{ApiClient: apiClient, Site: "default"}
	if _, err := c.ListNat(ctx, c.Site); err != nil {
		t.Skipf("controller does not support the v2 NAT API: %v", err)
	}
}

// testAccNatRuleCheckDestroy verifies every unifi_nat_rule in state is gone.
func testAccNatRuleCheckDestroy(s *terraform.State) error {
	ctx := context.Background()
	apiURL := os.Getenv("UNIFI_API")
	if apiURL == "" {
		return nil
	}
	apiClient, err := unifi.New(ctx, &unifi.Config{
		BaseURL:       apiURL,
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
	if err != nil {
		return nil //nolint:nilerr // best-effort check; skip when no live client
	}
	c := &Client{ApiClient: apiClient, Site: "default"}
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "unifi_nat_rule" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = c.Site
		}
		_, err := c.GetNat(ctx, site, rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("unifi_nat_rule %s still exists", rs.Primary.ID)
		}
		if _, ok := err.(*unifi.NotFoundError); !ok {
			return err
		}
	}
	return nil
}

func TestAccNatRule_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccNatRulePreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccNatRuleCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccNatRuleConfig("tf-acc masquerade", false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_nat_rule.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_nat_rule.test", "type", "MASQUERADE",
					),
					resource.TestCheckResourceAttr(
						"unifi_nat_rule.test", "description", "tf-acc masquerade",
					),
					resource.TestCheckResourceAttr(
						"unifi_nat_rule.test", "enabled", "false",
					),
				),
			},
			// In-place update: description and enabled.
			{
				Config: testAccNatRuleConfig("tf-acc masquerade v2", true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_nat_rule.test", "description", "tf-acc masquerade v2",
					),
					resource.TestCheckResourceAttr(
						"unifi_nat_rule.test", "enabled", "true",
					),
				),
			},
			// Import round-trip.
			{
				ResourceName:      "unifi_nat_rule.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccNatRuleConfig(description string, enabled bool) string {
	return fmt.Sprintf(`
resource "unifi_nat_rule" "test" {
  type          = "MASQUERADE"
  description   = %q
  enabled       = %t
  out_interface = "eth8"
}
`, description, enabled)
}
```

- [ ] **Step 2: Start the demo controller and run**

```bash
docker compose up -d
docker compose ps   # wait for healthy (start_period 90s)
```

Run: `TF_ACC=1 UNIFI_API=https://localhost:8443 UNIFI_USERNAME=admin UNIFI_PASSWORD=admin UNIFI_INSECURE=true go test ./unifi/ -run 'TestAccNatRule' -v -count=1 -timeout 10m` — first confirm the exact env values other acceptance runs use (check `.github/workflows` / any `docs` on running acceptance tests; do not guess credentials).

Expected: **SKIP** with "controller does not support the v2 NAT API" on the demo controller, or PASS if it does support it.

**Contingency:** if the probe list succeeds but *create* fails (e.g. the controller validates `out_interface` against real WAN interfaces the demo box lacks — error like `api.err.Invalid…`), do NOT weaken unit tests. Tighten the guard instead: extend `testAccNatRulePreCheck` to also attempt-and-delete a probe rule, skipping with the create error, and record this in the final report.

- [ ] **Step 3: Commit**

```bash
git add unifi/nat_rule_resource_test.go
git commit -m "test(nat): acceptance coverage with v2-API probe skip"
```

---

### Task 3: `unifi_firewall_policy` APP / APP_CATEGORY matching

**GATED on go-unifi PR 0** (`AppIDs`/`AppCategoryIDs` on `FirewallPolicySource`/`FirewallPolicyDestination`).

**Files:**
- Modify: `unifi/firewall_policy_resource.go`
- Modify: `unifi/firewall_policy_resource_test.go`

**Interfaces:**
- Consumes: `unifi.FirewallPolicySource.AppIDs []int64`, `.AppCategoryIDs []int64` (same on Destination) from go-unifi PR 0.
- Produces: `app_ids` / `app_category_ids` `types.Set` of `types.Int64Type` on both endpoints; `matching_target` accepts `APP` and `APP_CATEGORY`; `firewallPolicyMatchingTargetType` returns `""` for app matches (observed wire: the live APP policy has **no** `matching_target_type` key; `omitempty` then drops it from the PUT).

- [ ] **Step 1: Dependency gate**

Run: `go doc github.com/ubiquiti-community/go-unifi/unifi.FirewallPolicySource | grep -i app`
Expected: shows `AppIDs` and `AppCategoryIDs`. If absent: apply the local replace (`go mod edit -replace github.com/ubiquiti-community/go-unifi=/Users/jamesb/projects/go-unifi && go mod tidy && go build ./...`) and re-check. If still absent, **STOP this task and Task 4/5; report the missing go-unifi symbols.** If PR 0 landed the fields as `[]int` (not `[]int64`), note it and use the loop-conversion variant flagged in Step 3.

- [ ] **Step 2: Write the failing tests**

In `unifi/firewall_policy_resource_test.go`:

(a) Extend the case table in `TestFirewallPolicyMatchingTargetType` — add after the `{"ANY", "ANY", "ANY"},` line:

```go
		// APP/APP_CATEGORY matches carry no matching_target_type on the wire
		// (observed live: the "block shield DNS" APP policy has none), so the
		// helper must never inject SPECIFIC and must clear stale values.
		{"APP", "", ""},
		{"APP", "SPECIFIC", ""},
		{"APP_CATEGORY", "", ""},
		{"APP_CATEGORY", "ANY", ""},
```

(b) Append new tests:

```go
// TestFirewallPolicyAppMatchingRoundTrip covers APP/APP_CATEGORY matching:
// integer DPI app ids survive model -> API -> model, matching_target is
// preserved, and no matching_target_type is emitted for app matches.
func TestFirewallPolicyAppMatchingRoundTrip(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	appIDs, d := types.SetValueFrom(ctx, types.Int64Type, []int64{589885, 1310919})
	if d.HasError() {
		t.Fatal(d)
	}

	m := firewallPolicyEndpointModel{
		ZoneID:             types.StringValue("z1"),
		MatchingTarget:     types.StringValue("APP"),
		AppIDs:             appIDs,
		AppCategoryIDs:     types.SetNull(types.Int64Type),
		NetworkIDs:         types.ListNull(types.StringType),
		ClientMACs:         types.ListNull(types.StringType),
		IPs:                types.ListNull(types.StringType),
		WebDomains:         types.ListNull(types.StringType),
		Port:               types.StringNull(),
		PortGroupID:        types.StringNull(),
		PortMatchingType:   types.StringValue("ANY"),
		MatchingTargetType: types.StringNull(),
	}

	dst := endpointModelToDestination(ctx, m, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if dst.MatchingTarget != "APP" {
		t.Fatalf("MatchingTarget = %q, want APP", dst.MatchingTarget)
	}
	if dst.MatchingTargetType != "" {
		t.Fatalf("APP match must not carry matching_target_type, got %q",
			dst.MatchingTargetType)
	}
	if len(dst.AppIDs) != 2 {
		t.Fatalf("AppIDs = %v, want 2 ids", dst.AppIDs)
	}

	back := apiDestinationToEndpointModel(ctx, dst, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	var ids []int64
	diags.Append(back.AppIDs.ElementsAs(ctx, &ids, false)...)
	if len(ids) != 2 {
		t.Fatalf("round-trip AppIDs = %v", ids)
	}

	// Category variant on the source side.
	catIDs, d := types.SetValueFrom(ctx, types.Int64Type, []int64{7})
	if d.HasError() {
		t.Fatal(d)
	}
	m.MatchingTarget = types.StringValue("APP_CATEGORY")
	m.AppIDs = types.SetNull(types.Int64Type)
	m.AppCategoryIDs = catIDs
	src := endpointModelToSource(ctx, m, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if src.MatchingTarget != "APP_CATEGORY" || len(src.AppCategoryIDs) != 1 {
		t.Fatalf("source category match wrong: %+v", src)
	}
	if src.MatchingTargetType != "" {
		t.Fatalf("APP_CATEGORY match must not carry matching_target_type, got %q",
			src.MatchingTargetType)
	}
}

// Test_firewallPolicyResource_Schema_appMatching asserts the endpoint blocks
// expose the app matching attributes.
func Test_firewallPolicyResource_Schema_appMatching(t *testing.T) {
	r := &firewallPolicyResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	for _, key := range []string{"source", "destination"} {
		nested, ok := resp.Schema.Attributes[key].(schema.SingleNestedAttribute)
		if !ok {
			t.Fatalf("%s is not a SingleNestedAttribute", key)
		}
		if _, ok := nested.Attributes["app_ids"]; !ok {
			t.Errorf("%s missing app_ids", key)
		}
		if _, ok := nested.Attributes["app_category_ids"]; !ok {
			t.Errorf("%s missing app_category_ids", key)
		}
	}
}
```

Check the test file's imports: it needs `fwresource "github.com/hashicorp/terraform-plugin-framework/resource"` and `"github.com/hashicorp/terraform-plugin-framework/resource/schema"` — add whichever is missing.

(c) Update `Test_firewallPolicyEndpointModel_AttributeTypes`: add to the `want` map:

```go
				"app_ids":              types.SetType{ElemType: types.Int64Type},
				"app_category_ids":     types.SetType{ElemType: types.Int64Type},
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'TestFirewallPolicyAppMatchingRoundTrip|TestFirewallPolicyMatchingTargetType|Test_firewallPolicyEndpointModel_AttributeTypes|Test_firewallPolicyResource_Schema_appMatching' -count=1`
Expected: compile FAILURE (`unknown field AppIDs`, etc.).

- [ ] **Step 4: Implement in `unifi/firewall_policy_resource.go`**

Seven precise edits:

1. Add the import `"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"` to the import block.

2. `firewallPolicyEndpointModel` — after the `WebDomains` field add:

```go
	AppIDs           types.Set    `tfsdk:"app_ids"`
	AppCategoryIDs   types.Set    `tfsdk:"app_category_ids"`
```

3. `AttributeTypes()` — after the `"web_domains"` entry add:

```go
		"app_ids":          types.SetType{ElemType: types.Int64Type},
		"app_category_ids": types.SetType{ElemType: types.Int64Type},
```

4. In `Schema`, `endpointAttrs`: replace the `matching_target` attribute with

```go
		"matching_target": schema.StringAttribute{
			MarkdownDescription: "What to match: `ANY`, `NETWORK`, `CLIENT`, `IP`, `DEVICE`, `MAC`, `WEB` (domains/FQDN), `APP` (DPI applications), or `APP_CATEGORY` (DPI application categories).",
			Required:            true,
			Validators: []validator.String{
				stringvalidator.OneOf(
					"ANY", "NETWORK", "CLIENT", "IP", "DEVICE", "MAC", "WEB",
					"APP", "APP_CATEGORY",
				),
			},
		},
```

and after the `"web_domains"` attribute add:

```go
		"app_ids": schema.SetAttribute{
			MarkdownDescription: "DPI application IDs to match (integers, e.g. from the UniFi application list). Used when `matching_target` is `APP`.",
			Optional:            true,
			Computed:            true,
			ElementType:         types.Int64Type,
			PlanModifiers: []planmodifier.Set{
				setplanmodifier.UseStateForUnknown(),
			},
		},
		"app_category_ids": schema.SetAttribute{
			MarkdownDescription: "DPI application category IDs to match. Used when `matching_target` is `APP_CATEGORY`.",
			Optional:            true,
			Computed:            true,
			ElementType:         types.Int64Type,
			PlanModifiers: []planmodifier.Set{
				setplanmodifier.UseStateForUnknown(),
			},
		},
```

5. `UpgradeState`: v0 state predates app matching, so the derived prior schema must not contain the new attributes. In the `for _, key := range []string{"source", "destination"}` loop, after `attrs["port"] = schema.Int64Attribute{Optional: true, Computed: true}` add:

```go
			// v0 predates APP/APP_CATEGORY matching; its stored state has no
			// app fields, so the prior schema must not declare them.
			delete(attrs, "app_ids")
			delete(attrs, "app_category_ids")
```

and in `upgradeFirewallPolicyEndpointV0`, add to the `upgraded := firewallPolicyEndpointModel{...}` literal:

```go
		AppIDs:         types.SetNull(types.Int64Type),
		AppCategoryIDs: types.SetNull(types.Int64Type),
```

6. `firewallPolicyMatchingTargetType` — replace the function body:

```go
func firewallPolicyMatchingTargetType(matchingTarget, currentType string) string {
	// APP and APP_CATEGORY matches carry no matching_target_type on the wire
	// (observed live: an APP-matched destination has no such key at all).
	// Returning "" lets omitempty drop it from the PUT and clears any stale
	// type left over from a previous non-APP matching_target.
	switch matchingTarget {
	case "APP", "APP_CATEGORY":
		return ""
	}
	if matchingTarget != "" && matchingTarget != "ANY" &&
		(currentType == "" || currentType == "ANY") {
		return "SPECIFIC"
	}
	return currentType
}
```

7. Converters. In BOTH `endpointModelToSource` and `endpointModelToDestination`, after the `WebDomains` block add:

```go
	if !m.AppIDs.IsNull() && !m.AppIDs.IsUnknown() {
		diags.Append(m.AppIDs.ElementsAs(ctx, &ep.AppIDs, false)...)
	}
	if !m.AppCategoryIDs.IsNull() && !m.AppCategoryIDs.IsUnknown() {
		diags.Append(m.AppCategoryIDs.ElementsAs(ctx, &ep.AppCategoryIDs, false)...)
	}
```

(If PR 0 landed `AppIDs []int`: instead decode into a local `[]int64` and copy with a loop — `ElementsAs` cannot target `[]int`.)

In BOTH `apiSourceToEndpointModel` and `apiDestinationToEndpointModel`, after the `webDomains` block add (using `src.`/`dst.` respectively):

```go
	appIDs, ad := types.SetValueFrom(ctx, types.Int64Type, src.AppIDs)
	diags.Append(ad...)
	m.AppIDs = appIDs

	appCategoryIDs, acd := types.SetValueFrom(ctx, types.Int64Type, src.AppCategoryIDs)
	diags.Append(acd...)
	m.AppCategoryIDs = appCategoryIDs
```

- [ ] **Step 5: Repair existing test literals**

Every `firewallPolicyEndpointModel{...}` composite literal in `unifi/firewall_policy_resource_test.go` that is later passed to `types.ObjectValueFrom` must now carry typed nulls, or the conversion errors on the nil element type. Sweep:

```bash
grep -n 'firewallPolicyEndpointModel{' unifi/firewall_policy_resource_test.go
```

and to each such literal (there are several: `TestFirewallPolicyMatchingTargetType`, `TestFirewallPolicyPreserveMatchingTargetType`, `Test_endpointModelToSource`, `Test_endpointModelToDestination`, state-upgrade tests, etc.) add:

```go
		AppIDs:         types.SetNull(types.Int64Type),
		AppCategoryIDs: types.SetNull(types.Int64Type),
```

Also grep the rest of the package for other constructors of this model (e.g. upgrade-state tests in separate files): `grep -rn 'firewallPolicyEndpointModel{' unifi/ --include='*.go'`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'TestFirewallPolicy|Test_firewallPolicy|Test_endpointModel|Test_apiSource|Test_apiDestination' -count=1`
Expected: PASS — including all pre-existing firewall-policy tests.

- [ ] **Step 7: Full unit suite + commit**

Run: `go build ./... && go vet ./unifi/... && go test ./unifi/ -count=1 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/firewall_policy_resource.go unifi/firewall_policy_resource_test.go
git commit -m "feat(firewall_policy): APP and APP_CATEGORY matching targets"
```

(Body: extends the matching_target discriminator; integer DPI ids per the live wire format; app matches deliberately send no matching_target_type, matching the controller's own serialization; requires go-unifi app fields — note replace/bump status. Mention that live APP policies, e.g. a DNS-blocking policy matched on APP, become importable — manual validation against the UDM happens post-review per the spec's release flow, never from tests.)

---

### Task 4: `unifi_content_filtering` resource

**GATED on go-unifi PR 0** (content-filtering client).

**Files:**
- Create: `unifi/content_filtering_resource.go`
- Create: `unifi/content_filtering_resource_test.go`
- Modify: `unifi/provider.go`

**Interfaces:**
- Consumes: `unifi.ContentFiltering`, `unifi.ContentFilteringSchedule`, client methods `ListContentFiltering/GetContentFiltering/CreateContentFiltering/UpdateContentFiltering/DeleteContentFiltering` (assumed PR 0 names — Global Constraints), `hwtypes.MACAddressType`, `cleanMAC`, `util.ParseImportID`.
- Produces: `NewContentFilteringResource`, `contentFilteringResourceModel`, `contentFilteringScheduleModel`, `contentFilteringScheduleAttrTypes`, `modelToContentFiltering`, `contentFilteringToModel`.
- Schema is exactly the observed live shape: `name, enabled, categories, client_macs, network_ids, allow_list, block_list, safe_search, schedule{mode}`. No speculative fields. `schedule.mode` defaults to `ALWAYS` (only observed value); other modes are accepted syntactically (no OneOf validator) but documented as unverified, leaving room for API-side schedule modes without a breaking change.

- [ ] **Step 1: Dependency gate**

Run: `go doc github.com/ubiquiti-community/go-unifi/unifi.ContentFiltering`
Expected: struct + methods exist. If absent, apply/verify the local replace as in Task 3 Step 1; if still absent, **STOP and report.** If PR 0 used different method or field names, adapt the code below at the call sites and record every delta in the final report.

- [ ] **Step 2: Write the failing tests**

Create `unifi/content_filtering_resource_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
)

func Test_modelToContentFiltering(t *testing.T) {
	ctx := context.Background()

	categories, d := types.SetValueFrom(ctx, types.StringType,
		[]string{"ADVERTISEMENT"})
	if d.HasError() {
		t.Fatal(d)
	}
	// Upper/dash MAC: the write path must normalize to lower/colon.
	macs, d := types.SetValueFrom(ctx, hwtypes.MACAddressType{},
		[]string{"AA-BB-CC-DD-EE-FF"})
	if d.HasError() {
		t.Fatal(d)
	}
	blockList, d := types.SetValueFrom(ctx, types.StringType,
		[]string{"example.com"})
	if d.HasError() {
		t.Fatal(d)
	}

	m := contentFilteringResourceModel{
		ID:         types.StringValue("cf1"),
		Name:       types.StringValue("kids"),
		Enabled:    types.BoolValue(true),
		Categories: categories,
		ClientMACs: macs,
		NetworkIDs: types.SetNull(types.StringType),
		AllowList:  types.SetNull(types.StringType),
		BlockList:  blockList,
		SafeSearch: types.SetNull(types.StringType),
		Schedule:   types.ObjectNull(contentFilteringScheduleAttrTypes),
	}

	cf, diags := modelToContentFiltering(ctx, m)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if cf.ID != "cf1" || cf.Name != "kids" || !cf.Enabled {
		t.Fatalf("scalars wrong: %+v", cf)
	}
	if len(cf.Categories) != 1 || cf.Categories[0] != "ADVERTISEMENT" {
		t.Fatalf("categories = %v", cf.Categories)
	}
	if len(cf.ClientMACs) != 1 || cf.ClientMACs[0] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("client_macs not normalized: %v", cf.ClientMACs)
	}
	if len(cf.BlockList) != 1 || cf.BlockList[0] != "example.com" {
		t.Fatalf("block_list = %v", cf.BlockList)
	}
	// Unset collections must serialize as explicit empty arrays, matching the
	// live objects (which always carry every key).
	if cf.NetworkIDs == nil || len(cf.NetworkIDs) != 0 {
		t.Fatalf("network_ids should be an empty slice, got %#v", cf.NetworkIDs)
	}
	if cf.AllowList == nil || cf.SafeSearch == nil {
		t.Fatalf("allow_list/safe_search should be empty slices: %#v %#v",
			cf.AllowList, cf.SafeSearch)
	}
	// A null schedule defaults to ALWAYS.
	if cf.Schedule == nil || cf.Schedule.Mode != "ALWAYS" {
		t.Fatalf("schedule = %+v, want mode ALWAYS", cf.Schedule)
	}
}

func Test_contentFilteringToModel(t *testing.T) {
	ctx := context.Background()

	cf := &unifi.ContentFiltering{
		ID:         "cf1",
		Name:       "kids",
		Enabled:    true,
		Categories: []string{"FAMILY", "ADVERTISEMENT"},
		ClientMACs: []string{"aa:bb:cc:dd:ee:ff"},
		SafeSearch: []string{"GOOGLE", "YOUTUBE", "BING"},
		Schedule:   &unifi.ContentFilteringSchedule{Mode: "ALWAYS"},
	}

	var m contentFilteringResourceModel
	diags := contentFilteringToModel(ctx, cf, &m, "default")
	if diags.HasError() {
		t.Fatal(diags)
	}

	if m.ID.ValueString() != "cf1" || m.Name.ValueString() != "kids" ||
		!m.Enabled.ValueBool() {
		t.Fatalf("scalars wrong: %+v", m)
	}
	if m.Site.ValueString() != "default" {
		t.Fatalf("site = %v", m.Site)
	}
	var cats []string
	diags.Append(m.Categories.ElementsAs(ctx, &cats, false)...)
	if len(cats) != 2 {
		t.Fatalf("categories = %v", cats)
	}
	var search []string
	diags.Append(m.SafeSearch.ElementsAs(ctx, &search, false)...)
	if len(search) != 3 {
		t.Fatalf("safe_search = %v", search)
	}
	// nil slices map to empty sets (not null) so `x = []` round-trips.
	if m.NetworkIDs.IsNull() || len(m.NetworkIDs.Elements()) != 0 {
		t.Fatalf("network_ids should be an empty set, got %v", m.NetworkIDs)
	}
	if m.AllowList.IsNull() || m.BlockList.IsNull() {
		t.Fatalf("allow/block lists should be empty sets: %v %v",
			m.AllowList, m.BlockList)
	}
	var sched contentFilteringScheduleModel
	d := m.Schedule.As(ctx, &sched, objectAsOptions)
	if d.HasError() {
		t.Fatal(d)
	}
	if sched.Mode.ValueString() != "ALWAYS" {
		t.Fatalf("schedule mode = %v", sched.Mode)
	}
}

func Test_contentFilteringResource_Schema(t *testing.T) {
	r := &contentFilteringResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	for _, name := range []string{
		"id", "site", "name", "enabled", "categories", "client_macs",
		"network_ids", "allow_list", "block_list", "safe_search",
		"schedule", "timeouts",
	} {
		if _, ok := resp.Schema.Attributes[name]; !ok {
			t.Errorf("schema missing attribute %q", name)
		}
	}
}

func Test_contentFilteringResource_Metadata(t *testing.T) {
	r := &contentFilteringResource{}
	var resp fwresource.MetadataResponse
	r.Metadata(context.Background(),
		fwresource.MetadataRequest{ProviderTypeName: "unifi"}, &resp)
	if resp.TypeName != "unifi_content_filtering" {
		t.Fatalf("TypeName = %q, want unifi_content_filtering", resp.TypeName)
	}
}

func TestNewContentFilteringResource(t *testing.T) {
	got := NewContentFilteringResource()
	if _, ok := got.(fwresource.ResourceWithImportState); !ok {
		t.Error("NewContentFilteringResource() does not implement resource.ResourceWithImportState")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_modelToContentFiltering|Test_contentFiltering|TestNewContentFilteringResource' -count=1`
Expected: compile FAILURE — `undefined: contentFilteringResourceModel` etc.

- [ ] **Step 4: Create `unifi/content_filtering_resource.go`**

```go
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
```

- [ ] **Step 5: Register the resource**

In `unifi/provider.go`, in the `Resources` slice, after the `NewNatRuleResource,` line added in Task 1 add:

```go
		NewContentFilteringResource,
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_modelToContentFiltering|Test_contentFiltering|TestNewContentFilteringResource' -count=1 -v`
Expected: PASS.

- [ ] **Step 7: Full unit suite + commit**

Run: `go build ./... && go vet ./unifi/... && go test ./unifi/ -count=1 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/content_filtering_resource.go unifi/content_filtering_resource_test.go unifi/provider.go
git commit -m "feat(content_filtering): add unifi_content_filtering resource"
```

(Body: v2 content-filtering API; schema is exactly the live object shape; schedule modeled as mode-only with ALWAYS default; MACs semantically equal via hwtypes; gated on go-unifi content-filtering client — note replace/bump status.)

---

### Task 5: `unifi_content_filtering` acceptance tests (docker demo controller)

**Files:**
- Modify: `unifi/content_filtering_resource_test.go` (append)

- [ ] **Step 1: Append acceptance tests**

Add imports: `"fmt"`, `"os"`, `"github.com/hashicorp/terraform-plugin-testing/helper/resource"`, `"github.com/hashicorp/terraform-plugin-testing/terraform"`. Append:

```go
// testAccContentFilteringPreCheck skips when the controller lacks the v2
// content-filtering API (the docker demo controller predates it).
func testAccContentFilteringPreCheck(t *testing.T) {
	preCheck(t)
	ctx := context.Background()
	apiClient, err := unifi.New(ctx, &unifi.Config{
		BaseURL:       os.Getenv("UNIFI_API"),
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("could not build probe client: %v", err)
	}
	c := &Client{ApiClient: apiClient, Site: "default"}
	if _, err := c.ListContentFiltering(ctx, c.Site); err != nil {
		t.Skipf("controller does not support the v2 content-filtering API: %v", err)
	}
}

func testAccContentFilteringCheckDestroy(s *terraform.State) error {
	ctx := context.Background()
	apiURL := os.Getenv("UNIFI_API")
	if apiURL == "" {
		return nil
	}
	apiClient, err := unifi.New(ctx, &unifi.Config{
		BaseURL:       apiURL,
		Username:      os.Getenv("UNIFI_USERNAME"),
		Password:      os.Getenv("UNIFI_PASSWORD"),
		AllowInsecure: true,
	})
	if err != nil {
		return nil //nolint:nilerr // best-effort check; skip when no live client
	}
	c := &Client{ApiClient: apiClient, Site: "default"}
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "unifi_content_filtering" {
			continue
		}
		site := rs.Primary.Attributes["site"]
		if site == "" {
			site = c.Site
		}
		_, err := c.GetContentFiltering(ctx, site, rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("unifi_content_filtering %s still exists", rs.Primary.ID)
		}
		if _, ok := err.(*unifi.NotFoundError); !ok {
			return err
		}
	}
	return nil
}

func TestAccContentFiltering_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccContentFilteringPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccContentFilteringCheckDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccContentFilteringConfig("tf-acc-cf", true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("unifi_content_filtering.test", "id"),
					resource.TestCheckResourceAttr(
						"unifi_content_filtering.test", "name", "tf-acc-cf",
					),
					resource.TestCheckResourceAttr(
						"unifi_content_filtering.test", "enabled", "true",
					),
					resource.TestCheckResourceAttr(
						"unifi_content_filtering.test", "block_list.#", "1",
					),
					resource.TestCheckResourceAttr(
						"unifi_content_filtering.test", "schedule.mode", "ALWAYS",
					),
				),
			},
			// In-place update: rename and disable.
			{
				Config: testAccContentFilteringConfig("tf-acc-cf-2", false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_content_filtering.test", "name", "tf-acc-cf-2",
					),
					resource.TestCheckResourceAttr(
						"unifi_content_filtering.test", "enabled", "false",
					),
				),
			},
			// Import round-trip.
			{
				ResourceName:      "unifi_content_filtering.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccContentFilteringConfig(name string, enabled bool) string {
	return fmt.Sprintf(`
resource "unifi_content_filtering" "test" {
  name       = %q
  enabled    = %t
  categories = ["ADVERTISEMENT"]
  block_list = ["example.com"]
}
`, name, enabled)
}
```

- [ ] **Step 2: Run against the demo controller**

Same invocation shape as Task 2 Step 2, `-run 'TestAccContentFiltering'`.
Expected: **SKIP** on the demo controller ("does not support the v2 content-filtering API"), or PASS.

**Contingency:** if list succeeds but create fails with a category-validation error (`ADVERTISEMENT` unknown on old firmware), drop `categories` from the config and rely on `block_list` only; record the change in the final report. Do not weaken unit tests.

- [ ] **Step 3: Commit**

```bash
git add unifi/content_filtering_resource_test.go
git commit -m "test(content_filtering): acceptance coverage with v2-API probe skip"
```

---

### Task 6: Docs, examples, changelog, final verification

**Files:**
- Create: `examples/resources/unifi_nat_rule/resource.tf`, `examples/resources/unifi_nat_rule/import.sh`
- Create: `examples/resources/unifi_content_filtering/resource.tf`, `examples/resources/unifi_content_filtering/import.sh`
- Modify: `examples/resources/unifi_firewall_policy/resource.tf` (append APP example)
- Modify: `CHANGELOG.md`
- Generated: `docs/resources/nat_rule.md`, `docs/resources/content_filtering.md`, `docs/resources/firewall_policy.md` (via `go generate ./...` → tfplugindocs)

- [ ] **Step 1: Write the examples**

`examples/resources/unifi_nat_rule/resource.tf`:

```terraform
# Port-map style destination NAT: forward WAN tcp/443 to an internal host.
resource "unifi_nat_rule" "https_dnat" {
  type         = "DNAT"
  description  = "HTTPS to reverse proxy"
  in_interface = "eth4"
  protocol     = "tcp"
  ip_address   = "10.0.10.5"
  port         = 443

  destination_filter = {
    filter_type = "ADDRESS_AND_PORT"
    port        = 443
  }
}

# Source NAT a lab network out of a specific WAN address.
resource "unifi_nat_rule" "lab_snat" {
  type          = "SNAT"
  description   = "Lab egress address"
  out_interface = "eth8"
  ip_address    = "203.0.113.7"

  source_filter = {
    filter_type = "NETWORK_CONF"
    network_id  = unifi_network.lab.id
  }
}
```

`examples/resources/unifi_nat_rule/import.sh`:

```sh
# NAT rules can be imported using the rule ID.
terraform import unifi_nat_rule.example 5f3e9b2c4ee8cb0f1f4a1234

# For a non-default site, prefix the ID with the site name and a colon.
terraform import unifi_nat_rule.example default:5f3e9b2c4ee8cb0f1f4a1234
```

`examples/resources/unifi_content_filtering/resource.tf`:

```terraform
# Block ad and adult content for the kids network, with safe search enforced.
resource "unifi_content_filtering" "kids" {
  name        = "Kids"
  categories  = ["ADVERTISEMENT"]
  network_ids = [unifi_network.kids.id]
  block_list  = ["example-tracker.com"]
  allow_list  = ["example-school.org"]
  safe_search = ["GOOGLE", "YOUTUBE", "BING"]
}
```

`examples/resources/unifi_content_filtering/import.sh`:

```sh
# Content filtering policies can be imported using the policy ID.
terraform import unifi_content_filtering.example 5f3e9b2c4ee8cb0f1f4a1234

# For a non-default site, prefix the ID with the site name and a colon.
terraform import unifi_content_filtering.example default:5f3e9b2c4ee8cb0f1f4a1234
```

Append to `examples/resources/unifi_firewall_policy/resource.tf` (read it first and match its zone-reference style — reuse the zone data sources/resources already in the file rather than inventing new ones):

```terraform
# Block specific DPI-classified applications from the LAN to the internet.
# app_ids are the integer DPI application IDs from the UniFi application list.
resource "unifi_firewall_policy" "block_apps" {
  name   = "Block streaming apps"
  action = "BLOCK"

  source {
    zone_id         = data.unifi_firewall_zone.internal.id
    matching_target = "ANY"
  }

  destination {
    zone_id         = data.unifi_firewall_zone.external.id
    matching_target = "APP"
    app_ids         = [589885]
  }
}
```

(Adjust `source`/`destination` block-vs-attribute syntax to match the existing examples in that file — they are `SingleNestedAttribute`s, so the existing file likely uses `source = { ... }` assignment form; copy whichever form is already there.)

- [ ] **Step 2: Regenerate docs**

Run: `go generate ./...`
Expected: `docs/resources/nat_rule.md` and `docs/resources/content_filtering.md` created; `docs/resources/firewall_policy.md` gains `app_ids`/`app_category_ids` and the extended `matching_target` enum. Inspect: `git diff --stat docs/ && git status --short docs/`.

- [ ] **Step 3: Changelog**

Add under `## [Unreleased]` → `### ✨ Features` in `CHANGELOG.md` (match the bolded-lede prose style):

```markdown
- **New resource `unifi_nat_rule`: manage NAT rules.** Full CRUD over the v2 NAT API for `DNAT` (port-map style forwards), `SNAT`, and `MASQUERADE` rules, including per-rule source/destination filters (`filter_type` of `ADDRESS_AND_PORT`, `FIREWALL_GROUPS`, or `NETWORK_CONF`), logging, NAT exclusions, and rule ordering. Controller-predefined rules are read-only firmware objects and are not manageable. Import takes the rule ID, or `site:id` for a non-default site.
- **New resource `unifi_content_filtering`: manage content-filtering policies.** Category-based blocking plus explicit allow/block domain lists, safe-search enforcement (`GOOGLE`, `YOUTUBE`, `BING`), scoped to specific clients and/or networks, over the v2 content-filtering API. `client_macs` uses the same semantic MAC equality as `unifi_client`/`unifi_ap_group`, so `AA-BB-…` and `aa:bb:…` never churn the plan. Import takes the policy ID, or `site:id` for a non-default site.
- **`unifi_firewall_policy`: DPI application matching.** `matching_target` now accepts `APP` and `APP_CATEGORY`, with new `app_ids` / `app_category_ids` (integer DPI IDs) on the `source` and `destination` blocks. Existing controller-authored APP policies become importable. App matches intentionally send no `matching_target_type`, mirroring how the controller serializes them.
```

- [ ] **Step 4: Full verification**

```bash
go build ./...
go vet ./...
go test ./unifi/ -count=1 2>&1 | tail -5
```

Expected: all clean, tests PASS. If `golangci-lint` is installed and the repo has a config, run `golangci-lint run ./unifi/...` too — do not install tools to satisfy this. Then check the go.mod state: `grep replace go.mod` — if a local replace is present, it stays for James's review but MUST be called out in the final report as a pre-release removal item.

- [ ] **Step 5: Commit**

```bash
git add examples/resources/unifi_nat_rule examples/resources/unifi_content_filtering examples/resources/unifi_firewall_policy/resource.tf docs/ CHANGELOG.md
git commit -m "docs: document unifi_nat_rule, unifi_content_filtering, APP matching"
```

- [ ] **Step 6: STOP — do not push**

The branch stays local. Report completion with: resources added, exact go-unifi symbols consumed (and any name deltas from the assumptions in Global Constraints), acceptance results including which tests skipped on the demo controller and why, go.mod replace status, and the diff stat. The manual live-UDM validation (import of the existing APP policy "block shield DNS" by its ID; `tofu plan` no-op check) is done by James post-review per the spec's release flow — never automated, never from this working tree.

---

## Self-review notes

- Spec coverage (PR 5 scope): `unifi_nat_rule` ✓ (Tasks 1–2), `unifi_content_filtering` ✓ (Tasks 4–5), firewall-policy APP matching ✓ (Task 3), registration ✓ (Tasks 1/4), docs + examples + changelog ✓ (Task 6), docker-only acceptance with probe-once skip guards ✓ (Tasks 2/5), no `settingSection` registry involvement ✓, no public push ✓ (Task 6 Step 6).
- **Verified against the modcache, not assumed:** the pinned go-unifi already exports the NAT CRUD (`unifi/nat.go`), so only Tasks 3–5 are gated on PR 0 — the plan orders NAT first so work can start before the go-unifi bump lands. `NatSourceFilter`/`NatDestinationFilter` are structurally identical but distinct types, hence the duplicated (firewall_policy-style) converters.
- **Wire-format facts encoded as tests:** `app_ids` are integers (live payload), so the schema uses `types.Int64Type` — deliberate deviation from filipowm's string lists; APP-matched endpoints carry no `matching_target_type`, so `firewallPolicyMatchingTargetType` returns `""` for APP/APP_CATEGORY and a unit test pins it; go-unifi maps an empty NAT port to `*int64(0)`, so `natPortValue` treats 0 as null.
- **State-upgrade hazard handled:** `firewallPolicyResource.UpgradeState` derives the v0 schema from the live schema; adding endpoint attributes would silently leak them into the prior schema, so the upgrader deletes `app_ids`/`app_category_ids` and the v0→v1 converter fills typed nulls. Existing endpoint-model test literals need typed nulls too (Task 3 Step 5 sweep).
- **Null-vs-empty discipline:** NAT filter nested attrs use `Default("")`/`Default(false)` with verbatim read-back (avoiding inconsistent-result errors inside an Optional nested object), while content-filtering collections read back nil→empty-set (the ap_group empty-membership convention) because the live objects always carry every list key; `setElementsOrEmpty` sends explicit empty arrays on the write path — which is also why PR 0 must not put `omitempty` on those fields (flagged in Global Constraints).
- Type consistency across tasks: every schema attribute ↔ `tfsdk` tag ↔ `AttributeTypes()` entry was cross-checked per model; `objectAsOptions` is declared once in Task 1 with a collision check; Task 4 reuses it.
- Intentional scope cuts, stated in-plan: no `list.ListResource` for the two new resources; NAT `attr_*`/`is_predefined` excluded from schema; no OneOf on content-filtering `safe_search`/`schedule.mode` beyond the ALWAYS default (only observed, undocumented enums).
