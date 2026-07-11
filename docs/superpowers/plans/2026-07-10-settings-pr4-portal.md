# Settings PR 4: portal & long tail Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `dashboard`, `radio_ai`, and a feature-complete filipowm-aligned `guest_access` section to the `unifi_setting` resource, plus a **gated** `provider_capabilities` section, all on top of the section-handler registry and raw-merge engine from PR 1.

**Architecture:** Each section is one file implementing the `settingSection` interface from PR 1 Task 1, registered in `settingSections`. Writes stay raw (overlay only user-set fields into the section's `Data` map, full-object PUT). Reads are typed (`ui.GetSetting[T]`) for `dashboard`, `radio_ai`, and `provider_capabilities` — but **raw for `guest_access`**: the generated `settings.GuestAccess` declares `Expire string` while live controllers send `"expire": 480` as a JSON number, so the typed read fails with `json: cannot unmarshal number into Go struct field .Alias.expire of type string` (verified empirically against go-unifi v1.33.43-0.20260706191309). `guest_access` therefore reads its section out of `ListSettings` raw data via small typed accessors (`rawString`/`rawBool`/`rawInt`/…) added in Task 3, with a `TODO(go-unifi):` tag so it can move to a typed read once upstream models `expire` as a number.

**Tech Stack:** Go, terraform-plugin-framework (+ framework-validators `stringvalidator`, `int64validator`), go-unifi v1.33.43 fork (`unifi/settings`: `Dashboard`, `RadioAi`, `RawSetting`; `GuestAccess` for write-side reference only; `ProviderCapabilities` after go-unifi PR 0).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-10-setting-sections-design.md`. This plan is PR 4 of 6.
- **Nothing is pushed or posted publicly. Local branch/commits only; James reviews before any push.**
- PRs 1–3 are implemented. This plan consumes their exact names: `settingSection` (methods `key/attrTypes/schemaAttribute/get/set/overlay/read`), `var settingSections []settingSection`, `applySections`, `readSections` (all in `unifi/setting_section.go`), plus `util.StringValueOrNull`. Sections here only add files and registry/model entries — the engine is not modified.
- Model field names on `settingResourceModel` (exact): `GuestAccess`, `RadioAi`, `Dashboard`, `ProviderCapabilities` (tfsdk tags `guest_access`, `radio_ai`, `dashboard`, `provider_capabilities`).
- **filipowm alignment (guest_access):** attribute names match filipowm's `unifi_setting_guest_access` exactly wherever fields overlap (top-level names, nested block names `facebook`, `facebook_wifi`, `google`, `radius`, `redirect`, `wechat`, `authorize`, `ippay`, `merchant_warrior`, `paypal`, `quickpay`, `stripe`, `portal_customization`, and their children). We expose a small superset (`portal_customization.bg_image_enabled`, `portal_customization.logo_enabled`). Requiredness is relaxed to Optional+Computed everywhere (our raw-merge engine rebuilds the whole section object on read), and `*_enabled` flags are explicit attributes rather than derived from block presence — names still align, see self-review notes.
- **Sensitive fields (guest_access):** every credential is `Sensitive: true` — `password`, `facebook.app_secret`, `facebook_wifi.gateway_secret`, `google.client_secret`, `authorize.login_id`, `authorize.transaction_key`, `ippay.terminal_id`, `merchant_warrior.api_key`, `merchant_warrior.api_passphrase`, `merchant_warrior.merchant_uuid`, `paypal.username`, `paypal.password`, `paypal.signature`, `quickpay.agreement_id`, `quickpay.api_key`, `quickpay.merchant_id`, `stripe.api_key`, `wechat.app_secret`, `wechat.secret_key`. This is filipowm's list (verified directly against its source: `facebook.app_secret`, `facebook_wifi.gateway_secret`, `merchant_warrior.*`, `paypal.username/password/signature`, `quickpay.*`, `stripe.api_key`, `wechat.app_secret/secret_key`, top-level `password` all marked `Sensitive` there) plus three credentials filipowm leaves unmarked — an oversight, not a design choice: `google.client_secret` is present in filipowm but commented out (`// Sensitive: true`, alongside `google.client_id`, which we deliberately do NOT mark Sensitive — an OAuth client ID is not confidential by Google's own classification, unlike the secret), and `authorize.login_id`/`authorize.transaction_key` never had a `Sensitive` line at all in filipowm. The spec's "all portal/payment credentials Sensitive" rule wins for these three.
- **The captured live payload (`udm-settings.json` in the scratchpad) contains real secrets. Field names and shapes only — never copy values from it into code, tests, docs, or commits.**
- Collections: `types.Set` for unordered collections (radio_ai channel/device lists); `types.List` where order is meaningful (`restricted_dns_servers` — resolver priority, `portal_customization.languages` — display order, `dashboard.widgets` — layout order). The two List cases in guest_access also match filipowm's types.
- `radio_ai` is co-managed by the controller via `setting_preference`: **every** attribute is Optional+Computed with `UseStateForUnknown` (including bools/strings), and the schema description carries an explicit churn warning steering users to manage only `enabled`/`setting_preference`.
- Existing sections and their tests are untouched.
- Commit style: conventional commits matching the repo log (`feat(setting): …`), body explains why, `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` trailer.
- All commands run from the repo root: `/Users/jamesb/emdash/worktrees/terraform-provider-unifi/emdash/missing-config-uyrwq`.
- Unit tests: `go test ./unifi/ -run '<pattern>' -count=1`. Acceptance: docker demo controller only — `TestMain` in `unifi/provider_test.go` boots `docker-compose.yaml` itself via testcontainers when `TF_ACC=1` is set (no manual `docker compose up`); never a live UDM. `UNIFI_SKIP_CONTAINER` is only for humans pointing at real hardware — tests added here must **skip** guarded steps when it is unset only if the demo controller genuinely lacks the feature.
- Task 7 (`provider_capabilities`) is **GATED** on go-unifi PR 0 and has an explicit precondition check; if the precondition fails it is skipped entirely and reported, never half-done.

---

### Task 1: `dashboard` section

Trivial cosmetic fields: the site dashboard layout preference and widget list.

**Files:**
- Create: `unifi/setting_section_dashboard.go`
- Create: `unifi/setting_section_dashboard_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settingSections`, `settings.Dashboard` (`LayoutPreference string // auto|manual`, `Widgets []SettingDashboardWidgets{Enabled bool; Name string}`), `ui.GetSetting[T]`, `util.StringValueOrNull`.
- Produces: `settingDashboardModel`, `settingDashboardWidgetModel`, `dashboardAttrTypes`, `dashboardWidgetAttrTypes`, `dashboardSection`, `dashboardModelToData(ctx, m, data, diags)`, `dashboardSettingToModel(ctx, s, diags)`.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_dashboard_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_dashboardModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	widget, d := types.ObjectValueFrom(ctx, dashboardWidgetAttrTypes,
		settingDashboardWidgetModel{
			Name:    types.StringValue("wan_activity"),
			Enabled: types.BoolValue(true),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	widgets, d := types.ListValueFrom(ctx,
		types.ObjectType{AttrTypes: dashboardWidgetAttrTypes},
		[]types.Object{widget})
	if d.HasError() {
		t.Fatal(d)
	}

	m := &settingDashboardModel{
		LayoutPreference: types.StringValue("manual"),
		Widgets:          widgets,
	}
	// Raw fields go-unifi does not model must round-trip untouched.
	data := map[string]any{"unmodeled_field": "keep", "layout_preference": "auto"}

	dashboardModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["layout_preference"] != "manual" {
		t.Fatalf("layout_preference = %v", data["layout_preference"])
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
	entries, ok := data["widgets"].([]map[string]any)
	if !ok || len(entries) != 1 || entries[0]["name"] != "wan_activity" ||
		entries[0]["enabled"] != true {
		t.Fatalf("widgets = %v", data["widgets"])
	}
}

func Test_dashboardModelToData_nullsLeaveRemoteValues(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := &settingDashboardModel{
		LayoutPreference: types.StringNull(),
		Widgets:          types.ListNull(types.ObjectType{AttrTypes: dashboardWidgetAttrTypes}),
	}
	data := map[string]any{"layout_preference": "auto"}

	dashboardModelToData(ctx, m, data, &diags)

	if data["layout_preference"] != "auto" {
		t.Fatalf("null layout_preference overwrote remote value: %v", data["layout_preference"])
	}
	if _, present := data["widgets"]; present {
		t.Fatal("null widgets should not be written")
	}
}

func Test_dashboardSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := dashboardSettingToModel(ctx, &settings.Dashboard{
		LayoutPreference: "auto",
		Widgets: []settings.SettingDashboardWidgets{
			{Name: "wan_activity", Enabled: true},
		},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if m.LayoutPreference.ValueString() != "auto" {
		t.Fatalf("layout_preference = %v", m.LayoutPreference)
	}
	var widgets []settingDashboardWidgetModel
	diags.Append(m.Widgets.ElementsAs(ctx, &widgets, false)...)
	if len(widgets) != 1 || widgets[0].Name.ValueString() != "wan_activity" ||
		!widgets[0].Enabled.ValueBool() {
		t.Fatalf("widgets = %v", widgets)
	}

	empty := dashboardSettingToModel(ctx, &settings.Dashboard{}, &diags)
	if !empty.LayoutPreference.IsNull() {
		t.Fatalf("empty layout_preference should be null, got %v", empty.LayoutPreference)
	}
	if !empty.Widgets.IsNull() {
		t.Fatalf("empty widgets should be null, got %v", empty.Widgets)
	}
}

func Test_settingResource_Schema_dashboard(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["dashboard"]; !ok {
		t.Fatal("schema is missing the dashboard section attribute")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_dashboard|Test_settingResource_Schema_dashboard' -count=1`
Expected: compile FAILURE — `undefined: settingDashboardModel` etc.

- [ ] **Step 3: Create `unifi/setting_section_dashboard.go`**

```go
package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingDashboardModel is the nested dashboard block: cosmetic layout
// preferences for the controller UI dashboard.
type settingDashboardModel struct {
	LayoutPreference types.String `tfsdk:"layout_preference"`
	Widgets          types.List   `tfsdk:"widgets"`
}

type settingDashboardWidgetModel struct {
	Name    types.String `tfsdk:"name"`
	Enabled types.Bool   `tfsdk:"enabled"`
}

var (
	dashboardWidgetAttrTypes = map[string]attr.Type{
		"name":    types.StringType,
		"enabled": types.BoolType,
	}
	dashboardAttrTypes = map[string]attr.Type{
		"layout_preference": types.StringType,
		"widgets": types.ListType{
			ElemType: types.ObjectType{AttrTypes: dashboardWidgetAttrTypes},
		},
	}
)

type dashboardSection struct{}

func (dashboardSection) key() string { return "dashboard" }

func (dashboardSection) attrTypes() map[string]attr.Type { return dashboardAttrTypes }

func (dashboardSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Controller dashboard layout settings (cosmetic).",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"layout_preference": schema.StringAttribute{
				MarkdownDescription: "Dashboard layout preference: `auto` or `manual`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "manual"),
				},
			},
			"widgets": schema.ListNestedAttribute{
				MarkdownDescription: "Ordered dashboard widget list (only meaningful with `layout_preference = \"manual\"`). Widget names vary by controller version and are not validated.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Widget identifier (e.g. `wan_activity`).",
							Required:            true,
						},
						"enabled": schema.BoolAttribute{
							MarkdownDescription: "Whether the widget is shown.",
							Required:            true,
						},
					},
				},
			},
		},
	}
}

func (dashboardSection) get(m *settingResourceModel) types.Object { return m.Dashboard }

func (dashboardSection) set(m *settingResourceModel, obj types.Object) { m.Dashboard = obj }

func (dashboardSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingDashboardModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	dashboardModelToData(ctx, &m, data, &diags)
	return diags
}

// dashboardModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values.
func dashboardModelToData(
	ctx context.Context,
	m *settingDashboardModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.LayoutPreference.IsNull() && !m.LayoutPreference.IsUnknown() {
		data["layout_preference"] = m.LayoutPreference.ValueString()
	}
	if !m.Widgets.IsNull() && !m.Widgets.IsUnknown() {
		var widgets []settingDashboardWidgetModel
		diags.Append(m.Widgets.ElementsAs(ctx, &widgets, false)...)
		out := make([]map[string]any, 0, len(widgets))
		for _, w := range widgets {
			out = append(out, map[string]any{
				"name":    w.Name.ValueString(),
				"enabled": w.Enabled.ValueBool(),
			})
		}
		data["widgets"] = out
	}
}

func (dashboardSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Dashboard](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(dashboardAttrTypes), diags
		}
		diags.AddError("Error Reading Dashboard Setting", err.Error())
		return types.ObjectNull(dashboardAttrTypes), diags
	}
	model := dashboardSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(dashboardAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, dashboardAttrTypes, model)
}

func dashboardSettingToModel(
	ctx context.Context,
	s *settings.Dashboard,
	diags *diag.Diagnostics,
) settingDashboardModel {
	widgetType := types.ObjectType{AttrTypes: dashboardWidgetAttrTypes}
	widgets := types.ListNull(widgetType)
	if len(s.Widgets) > 0 {
		models := make([]settingDashboardWidgetModel, 0, len(s.Widgets))
		for _, w := range s.Widgets {
			models = append(models, settingDashboardWidgetModel{
				Name:    util.StringValueOrNull(w.Name),
				Enabled: types.BoolValue(w.Enabled),
			})
		}
		l, d := types.ListValueFrom(ctx, widgetType, models)
		diags.Append(d...)
		widgets = l
	}
	return settingDashboardModel{
		LayoutPreference: util.StringValueOrNull(s.LayoutPreference),
		Widgets:          widgets,
	}
}
```

- [ ] **Step 4: Register the section**

In `unifi/setting_section.go`, append to the registry slice (after the PR 3 entries):

```go
	dashboardSection{},
```

In `unifi/setting_resource.go`, add to `settingResourceModel` (after the last PR 3 section field):

```go
	Dashboard     types.Object   `tfsdk:"dashboard"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_dashboard|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_dashboard.go unifi/setting_section_dashboard_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add dashboard section"
```

---

### Task 2: `radio_ai` section

Co-managed by the controller: with `setting_preference = "auto"` the controller rewrites channel plans on its own schedule. Every attribute is Optional+Computed with `UseStateForUnknown`, and the docs warn about churn.

**Files:**
- Create: `unifi/setting_section_radio_ai.go`
- Create: `unifi/setting_section_radio_ai_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settings.RadioAi` (`AutoAdjustChannelsToCountry bool`, `AutoChannelPresetsType string`, `Channels6E/ChannelsNa/ChannelsNg/HtModesNa/HtModesNg []int64`, `ChannelsBlacklist []SettingRadioAiChannelsBlacklist{Channel *int64; ChannelWidth *int64; Radio string}`, `CronExpr string`, `Enabled bool`, `ExcludeDevices/HighPriorityDevices/Optimize/Radios []string`, `RadiosConfiguration []SettingRadioAiRadiosConfiguration{ChannelWidth *int64; Dfs bool; Radio string}`, `SettingPreference string`; `Default`/`UseXy` are controller-internal and deliberately not exposed).
- Produces: `settingRadioAiModel`, `settingRadioAiChannelsBlacklistModel`, `settingRadioAiRadiosConfigurationModel`, `radioAiAttrTypes`, `radioAiChannelsBlacklistAttrTypes`, `radioAiRadiosConfigurationAttrTypes`, `radioAiSection`, `radioAiModelToData(ctx, m, data, diags)`, `radioAiSettingToModel(ctx, s, diags)`.
- Note: the live controller stores an `auto_enabled` field the generated struct does not model — the raw-preservation test uses it. Channel lists arrive from the controller as JSON strings (`"36"`) but go-unifi normalizes them to int64 via `types.Number` on read; the overlay writes int64 and the controller accepts either representation.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_radio_ai_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

func Test_radioAiModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	channelsNg, d := types.SetValueFrom(ctx, types.Int64Type, []int64{1, 6, 11})
	if d.HasError() {
		t.Fatal(d)
	}
	radios, d := types.SetValueFrom(ctx, types.StringType, []string{"ng", "na"})
	if d.HasError() {
		t.Fatal(d)
	}
	blEntry, d := types.ObjectValueFrom(ctx, radioAiChannelsBlacklistAttrTypes,
		settingRadioAiChannelsBlacklistModel{
			Channel:      types.Int64Value(2),
			ChannelWidth: types.Int64Value(20),
			Radio:        types.StringValue("ng"),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	blacklist, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: radioAiChannelsBlacklistAttrTypes},
		[]types.Object{blEntry})
	if d.HasError() {
		t.Fatal(d)
	}

	m := &settingRadioAiModel{
		Enabled:                     types.BoolValue(true),
		SettingPreference:           types.StringValue("manual"),
		AutoAdjustChannelsToCountry: types.BoolNull(),
		AutoChannelPresetsType:      types.StringNull(),
		Channels6E:                  types.SetNull(types.Int64Type),
		ChannelsNa:                  types.SetNull(types.Int64Type),
		ChannelsNg:                  channelsNg,
		ChannelsBlacklist:           blacklist,
		CronExpr:                    types.StringValue("0 3 * * *"),
		ExcludeDevices:              types.SetNull(types.StringType),
		HighPriorityDevices:         types.SetNull(types.StringType),
		HtModesNa:                   types.SetNull(types.Int64Type),
		HtModesNg:                   types.SetNull(types.Int64Type),
		Optimize:                    types.SetNull(types.StringType),
		Radios:                      radios,
		RadiosConfiguration: types.SetNull(
			types.ObjectType{AttrTypes: radioAiRadiosConfigurationAttrTypes}),
	}

	// Live controllers carry auto_enabled, which go-unifi does not model:
	// the raw merge must preserve it verbatim.
	data := map[string]any{
		"auto_enabled":                    false,
		"auto_adjust_channels_to_country": true,
	}

	radioAiModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["auto_enabled"] != false {
		t.Fatal("unmodeled auto_enabled was clobbered")
	}
	if data["auto_adjust_channels_to_country"] != true {
		t.Fatal("null auto_adjust_channels_to_country overwrote remote value")
	}
	if data["enabled"] != true || data["setting_preference"] != "manual" {
		t.Fatalf("enabled/setting_preference wrong: %v", data)
	}
	if data["cron_expr"] != "0 3 * * *" {
		t.Fatalf("cron_expr = %v", data["cron_expr"])
	}
	ng, ok := data["channels_ng"].([]int64)
	if !ok || len(ng) != 3 {
		t.Fatalf("channels_ng = %v", data["channels_ng"])
	}
	if _, present := data["channels_na"]; present {
		t.Fatal("null channels_na should not be written")
	}
	bl, ok := data["channels_blacklist"].([]map[string]any)
	if !ok || len(bl) != 1 || bl[0]["channel"] != int64(2) ||
		bl[0]["channel_width"] != int64(20) || bl[0]["radio"] != "ng" {
		t.Fatalf("channels_blacklist = %v", data["channels_blacklist"])
	}
	rd, ok := data["radios"].([]string)
	if !ok || len(rd) != 2 {
		t.Fatalf("radios = %v", data["radios"])
	}
}

func Test_radioAiSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := radioAiSettingToModel(ctx, &settings.RadioAi{
		Enabled:                     true,
		SettingPreference:           "auto",
		AutoAdjustChannelsToCountry: true,
		AutoChannelPresetsType:      "maximum_speed",
		ChannelsNg:                  []int64{1, 6, 11},
		ChannelsBlacklist: []settings.SettingRadioAiChannelsBlacklist{
			{Channel: util.Ptr(int64(2)), ChannelWidth: util.Ptr(int64(20)), Radio: "ng"},
		},
		CronExpr: "0 3 * * *",
		Radios:   []string{"ng", "na"},
		RadiosConfiguration: []settings.SettingRadioAiRadiosConfiguration{
			{ChannelWidth: util.Ptr(int64(80)), Dfs: true, Radio: "na"},
		},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if !m.Enabled.ValueBool() || m.SettingPreference.ValueString() != "auto" {
		t.Fatalf("enabled/setting_preference = %v/%v", m.Enabled, m.SettingPreference)
	}
	if m.AutoChannelPresetsType.ValueString() != "maximum_speed" {
		t.Fatalf("auto_channel_presets_type = %v", m.AutoChannelPresetsType)
	}
	var ng []int64
	diags.Append(m.ChannelsNg.ElementsAs(ctx, &ng, false)...)
	if len(ng) != 3 {
		t.Fatalf("channels_ng = %v", ng)
	}
	var bl []settingRadioAiChannelsBlacklistModel
	diags.Append(m.ChannelsBlacklist.ElementsAs(ctx, &bl, false)...)
	if len(bl) != 1 || bl[0].Channel.ValueInt64() != 2 || bl[0].Radio.ValueString() != "ng" {
		t.Fatalf("channels_blacklist = %v", bl)
	}
	var rc []settingRadioAiRadiosConfigurationModel
	diags.Append(m.RadiosConfiguration.ElementsAs(ctx, &rc, false)...)
	if len(rc) != 1 || rc[0].ChannelWidth.ValueInt64() != 80 || !rc[0].Dfs.ValueBool() {
		t.Fatalf("radios_configuration = %v", rc)
	}
}

func Test_settingResource_Schema_radioAi(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["radio_ai"]; !ok {
		t.Fatal("schema is missing the radio_ai section attribute")
	}
}
```

(`fwresource` is already imported by `unifi/setting_section_dashboard_test.go`; this file needs its own import — add `fwresource "github.com/hashicorp/terraform-plugin-framework/resource"` to the import block above.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_radioAi|Test_settingResource_Schema_radioAi' -count=1`
Expected: compile FAILURE — `undefined: settingRadioAiModel` etc.

- [ ] **Step 3: Create `unifi/setting_section_radio_ai.go`**

```go
package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingRadioAiModel is the nested radio_ai block: the controller's
// automatic channel/power optimization. Co-managed by the controller — see
// the schema description's churn warning.
type settingRadioAiModel struct {
	Enabled                     types.Bool   `tfsdk:"enabled"`
	SettingPreference           types.String `tfsdk:"setting_preference"`
	AutoAdjustChannelsToCountry types.Bool   `tfsdk:"auto_adjust_channels_to_country"`
	AutoChannelPresetsType      types.String `tfsdk:"auto_channel_presets_type"`
	Channels6E                  types.Set    `tfsdk:"channels_6e"`
	ChannelsNa                  types.Set    `tfsdk:"channels_na"`
	ChannelsNg                  types.Set    `tfsdk:"channels_ng"`
	ChannelsBlacklist           types.Set    `tfsdk:"channels_blacklist"`
	CronExpr                    types.String `tfsdk:"cron_expr"`
	ExcludeDevices              types.Set    `tfsdk:"exclude_devices"`
	HighPriorityDevices         types.Set    `tfsdk:"high_priority_devices"`
	HtModesNa                   types.Set    `tfsdk:"ht_modes_na"`
	HtModesNg                   types.Set    `tfsdk:"ht_modes_ng"`
	Optimize                    types.Set    `tfsdk:"optimize"`
	Radios                      types.Set    `tfsdk:"radios"`
	RadiosConfiguration         types.Set    `tfsdk:"radios_configuration"`
}

type settingRadioAiChannelsBlacklistModel struct {
	Channel      types.Int64  `tfsdk:"channel"`
	ChannelWidth types.Int64  `tfsdk:"channel_width"`
	Radio        types.String `tfsdk:"radio"`
}

type settingRadioAiRadiosConfigurationModel struct {
	ChannelWidth types.Int64  `tfsdk:"channel_width"`
	Dfs          types.Bool   `tfsdk:"dfs"`
	Radio        types.String `tfsdk:"radio"`
}

var (
	radioAiChannelsBlacklistAttrTypes = map[string]attr.Type{
		"channel":       types.Int64Type,
		"channel_width": types.Int64Type,
		"radio":         types.StringType,
	}
	radioAiRadiosConfigurationAttrTypes = map[string]attr.Type{
		"channel_width": types.Int64Type,
		"dfs":           types.BoolType,
		"radio":         types.StringType,
	}
	radioAiAttrTypes = map[string]attr.Type{
		"enabled":                         types.BoolType,
		"setting_preference":              types.StringType,
		"auto_adjust_channels_to_country": types.BoolType,
		"auto_channel_presets_type":       types.StringType,
		"channels_6e":                     types.SetType{ElemType: types.Int64Type},
		"channels_na":                     types.SetType{ElemType: types.Int64Type},
		"channels_ng":                     types.SetType{ElemType: types.Int64Type},
		"channels_blacklist": types.SetType{
			ElemType: types.ObjectType{AttrTypes: radioAiChannelsBlacklistAttrTypes},
		},
		"cron_expr":             types.StringType,
		"exclude_devices":       types.SetType{ElemType: types.StringType},
		"high_priority_devices": types.SetType{ElemType: types.StringType},
		"ht_modes_na":           types.SetType{ElemType: types.Int64Type},
		"ht_modes_ng":           types.SetType{ElemType: types.Int64Type},
		"optimize":              types.SetType{ElemType: types.StringType},
		"radios":                types.SetType{ElemType: types.StringType},
		"radios_configuration": types.SetType{
			ElemType: types.ObjectType{AttrTypes: radioAiRadiosConfigurationAttrTypes},
		},
	}
)

type radioAiSection struct{}

func (radioAiSection) key() string { return "radio_ai" }

func (radioAiSection) attrTypes() map[string]attr.Type { return radioAiAttrTypes }

func (radioAiSection) schemaAttribute() schema.SingleNestedAttribute {
	usfuBool := []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}
	usfuString := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	usfuSet := []planmodifier.Set{setplanmodifier.UseStateForUnknown()}

	return schema.SingleNestedAttribute{
		MarkdownDescription: "Radio AI (automatic channel/power optimization). " +
			"**Co-managed by the controller:** while `setting_preference` is `auto` the controller " +
			"rewrites channel plans, radio configuration, and schedules on its own, so any attribute " +
			"you set here may drift and churn plans. Most users should manage only `enabled` and " +
			"`setting_preference`; set `setting_preference = \"manual\"` before pinning channels or " +
			"radio parameters. Unset attributes always follow the controller.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable Radio AI optimization.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuBool,
			},
			"setting_preference": schema.StringAttribute{
				MarkdownDescription: "`auto` (controller manages the plan) or `manual`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuString,
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "manual"),
				},
			},
			"auto_adjust_channels_to_country": schema.BoolAttribute{
				MarkdownDescription: "Restrict automatic channel selection to the site country's allowed channels.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuBool,
			},
			"auto_channel_presets_type": schema.StringAttribute{
				MarkdownDescription: "Channel preset: `maximum_speed`, `conservative`, or `custom`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuString,
				Validators: []validator.String{
					stringvalidator.OneOf("maximum_speed", "conservative", "custom"),
				},
			},
			"channels_6e": schema.SetAttribute{
				MarkdownDescription: "Candidate 6 GHz channels.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.Int64Type,
				PlanModifiers:       usfuSet,
			},
			"channels_na": schema.SetAttribute{
				MarkdownDescription: "Candidate 5 GHz channels.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.Int64Type,
				PlanModifiers:       usfuSet,
			},
			"channels_ng": schema.SetAttribute{
				MarkdownDescription: "Candidate 2.4 GHz channels.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.Int64Type,
				PlanModifiers:       usfuSet,
			},
			"channels_blacklist": schema.SetNestedAttribute{
				MarkdownDescription: "Channels excluded from optimization.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuSet,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"channel": schema.Int64Attribute{
							MarkdownDescription: "Channel number.",
							Required:            true,
						},
						"channel_width": schema.Int64Attribute{
							MarkdownDescription: "Channel width in MHz (20/40/80/160/240/320).",
							Required:            true,
						},
						"radio": schema.StringAttribute{
							MarkdownDescription: "Radio band: `ng`, `na`, or `6e`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("ng", "na", "6e"),
							},
						},
					},
				},
			},
			"cron_expr": schema.StringAttribute{
				MarkdownDescription: "Cron schedule for optimization runs (e.g. `0 3 * * *`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuString,
			},
			"exclude_devices": schema.SetAttribute{
				MarkdownDescription: "AP MAC addresses excluded from optimization.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers:       usfuSet,
			},
			"high_priority_devices": schema.SetAttribute{
				MarkdownDescription: "AP MAC addresses prioritized during optimization.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers:       usfuSet,
			},
			"ht_modes_na": schema.SetAttribute{
				MarkdownDescription: "Allowed 5 GHz channel widths in MHz.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.Int64Type,
				PlanModifiers:       usfuSet,
			},
			"ht_modes_ng": schema.SetAttribute{
				MarkdownDescription: "Allowed 2.4 GHz channel widths in MHz.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.Int64Type,
				PlanModifiers:       usfuSet,
			},
			"optimize": schema.SetAttribute{
				MarkdownDescription: "What to optimize: `channel` and/or `power`.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers:       usfuSet,
			},
			"radios": schema.SetAttribute{
				MarkdownDescription: "Radio bands under optimization: `ng`, `na`, `6e`.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers:       usfuSet,
			},
			"radios_configuration": schema.SetNestedAttribute{
				MarkdownDescription: "Per-band optimization parameters.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       usfuSet,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"channel_width": schema.Int64Attribute{
							MarkdownDescription: "Target channel width in MHz.",
							Optional:            true,
							Computed:            true,
						},
						"dfs": schema.BoolAttribute{
							MarkdownDescription: "Allow DFS channels.",
							Required:            true,
						},
						"radio": schema.StringAttribute{
							MarkdownDescription: "Radio band: `ng`, `na`, or `6e`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf("ng", "na", "6e"),
							},
						},
					},
				},
			},
		},
	}
}

func (radioAiSection) get(m *settingResourceModel) types.Object { return m.RadioAi }

func (radioAiSection) set(m *settingResourceModel, obj types.Object) { m.RadioAi = obj }

func (radioAiSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingRadioAiModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	radioAiModelToData(ctx, &m, data, &diags)
	return diags
}

// radioAiModelToData writes only the user-set fields into the raw section
// document; unset fields — including controller fields go-unifi does not
// model, like auto_enabled — keep their remote values.
func radioAiModelToData(
	ctx context.Context,
	m *settingRadioAiModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		data["enabled"] = m.Enabled.ValueBool()
	}
	if !m.SettingPreference.IsNull() && !m.SettingPreference.IsUnknown() {
		data["setting_preference"] = m.SettingPreference.ValueString()
	}
	if !m.AutoAdjustChannelsToCountry.IsNull() && !m.AutoAdjustChannelsToCountry.IsUnknown() {
		data["auto_adjust_channels_to_country"] = m.AutoAdjustChannelsToCountry.ValueBool()
	}
	if !m.AutoChannelPresetsType.IsNull() && !m.AutoChannelPresetsType.IsUnknown() {
		data["auto_channel_presets_type"] = m.AutoChannelPresetsType.ValueString()
	}
	if !m.CronExpr.IsNull() && !m.CronExpr.IsUnknown() {
		data["cron_expr"] = m.CronExpr.ValueString()
	}

	writeInt64Set := func(key string, v types.Set) {
		if v.IsNull() || v.IsUnknown() {
			return
		}
		var vals []int64
		diags.Append(v.ElementsAs(ctx, &vals, false)...)
		data[key] = vals
	}
	writeStringSet := func(key string, v types.Set) {
		if v.IsNull() || v.IsUnknown() {
			return
		}
		var vals []string
		diags.Append(v.ElementsAs(ctx, &vals, false)...)
		data[key] = vals
	}
	writeInt64Set("channels_6e", m.Channels6E)
	writeInt64Set("channels_na", m.ChannelsNa)
	writeInt64Set("channels_ng", m.ChannelsNg)
	writeInt64Set("ht_modes_na", m.HtModesNa)
	writeInt64Set("ht_modes_ng", m.HtModesNg)
	writeStringSet("exclude_devices", m.ExcludeDevices)
	writeStringSet("high_priority_devices", m.HighPriorityDevices)
	writeStringSet("optimize", m.Optimize)
	writeStringSet("radios", m.Radios)

	if !m.ChannelsBlacklist.IsNull() && !m.ChannelsBlacklist.IsUnknown() {
		var entries []settingRadioAiChannelsBlacklistModel
		diags.Append(m.ChannelsBlacklist.ElementsAs(ctx, &entries, false)...)
		out := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			out = append(out, map[string]any{
				"channel":       e.Channel.ValueInt64(),
				"channel_width": e.ChannelWidth.ValueInt64(),
				"radio":         e.Radio.ValueString(),
			})
		}
		data["channels_blacklist"] = out
	}
	if !m.RadiosConfiguration.IsNull() && !m.RadiosConfiguration.IsUnknown() {
		var entries []settingRadioAiRadiosConfigurationModel
		diags.Append(m.RadiosConfiguration.ElementsAs(ctx, &entries, false)...)
		out := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			entry := map[string]any{
				"dfs":   e.Dfs.ValueBool(),
				"radio": e.Radio.ValueString(),
			}
			if !e.ChannelWidth.IsNull() && !e.ChannelWidth.IsUnknown() {
				entry["channel_width"] = e.ChannelWidth.ValueInt64()
			}
			out = append(out, entry)
		}
		data["radios_configuration"] = out
	}
}

func (radioAiSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.RadioAi](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(radioAiAttrTypes), diags
		}
		diags.AddError("Error Reading Radio AI Setting", err.Error())
		return types.ObjectNull(radioAiAttrTypes), diags
	}
	model := radioAiSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(radioAiAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, radioAiAttrTypes, model)
}

func radioAiSettingToModel(
	ctx context.Context,
	s *settings.RadioAi,
	diags *diag.Diagnostics,
) settingRadioAiModel {
	int64Set := func(vals []int64) types.Set {
		set, d := types.SetValueFrom(ctx, types.Int64Type, vals)
		diags.Append(d...)
		return set
	}
	stringSet := func(vals []string) types.Set {
		set, d := types.SetValueFrom(ctx, types.StringType, vals)
		diags.Append(d...)
		return set
	}

	blacklist := make([]settingRadioAiChannelsBlacklistModel, 0, len(s.ChannelsBlacklist))
	for _, e := range s.ChannelsBlacklist {
		blacklist = append(blacklist, settingRadioAiChannelsBlacklistModel{
			Channel:      types.Int64PointerValue(e.Channel),
			ChannelWidth: types.Int64PointerValue(e.ChannelWidth),
			Radio:        util.StringValueOrNull(e.Radio),
		})
	}
	blacklistSet, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: radioAiChannelsBlacklistAttrTypes}, blacklist)
	diags.Append(d...)

	configs := make([]settingRadioAiRadiosConfigurationModel, 0, len(s.RadiosConfiguration))
	for _, e := range s.RadiosConfiguration {
		configs = append(configs, settingRadioAiRadiosConfigurationModel{
			ChannelWidth: types.Int64PointerValue(e.ChannelWidth),
			Dfs:          types.BoolValue(e.Dfs),
			Radio:        util.StringValueOrNull(e.Radio),
		})
	}
	configSet, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: radioAiRadiosConfigurationAttrTypes}, configs)
	diags.Append(d...)

	return settingRadioAiModel{
		Enabled:                     types.BoolValue(s.Enabled),
		SettingPreference:           util.StringValueOrNull(s.SettingPreference),
		AutoAdjustChannelsToCountry: types.BoolValue(s.AutoAdjustChannelsToCountry),
		AutoChannelPresetsType:      util.StringValueOrNull(s.AutoChannelPresetsType),
		Channels6E:                  int64Set(s.Channels6E),
		ChannelsNa:                  int64Set(s.ChannelsNa),
		ChannelsNg:                  int64Set(s.ChannelsNg),
		ChannelsBlacklist:           blacklistSet,
		CronExpr:                    util.StringValueOrNull(s.CronExpr),
		ExcludeDevices:              stringSet(s.ExcludeDevices),
		HighPriorityDevices:         stringSet(s.HighPriorityDevices),
		HtModesNa:                   int64Set(s.HtModesNa),
		HtModesNg:                   int64Set(s.HtModesNg),
		Optimize:                    stringSet(s.Optimize),
		Radios:                      stringSet(s.Radios),
		RadiosConfiguration:         configSet,
	}
}
```

- [ ] **Step 4: Register the section**

`unifi/setting_section.go` registry (after `dashboardSection{}`):

```go
	radioAiSection{},
```

`unifi/setting_resource.go` model (after `Dashboard`):

```go
	RadioAi       types.Object   `tfsdk:"radio_ai"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_radioAi|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_radio_ai.go unifi/setting_section_radio_ai_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add radio_ai section with controller co-management warning"
```

(Body should note: every attribute Optional+Computed+UseStateForUnknown because the controller rewrites this section under `setting_preference = "auto"`; users should normally manage only `enabled`/`setting_preference`.)

---

### Task 3: `guest_access` section — raw helpers, portal basics & customization

`guest_access` is the big one (~110 attributes when complete, filipowm-aligned). It stays **one** nested attribute on `unifi_setting`, built across Tasks 3–5: this task lands the section skeleton, the raw-value helpers, the flat portal fields, and the `portal_customization` + `redirect` blocks; Task 4 adds authentication providers; Task 5 adds payment gateways.

**Why a raw read:** `settings.GuestAccess` declares `Expire string`, but live controllers send `"expire": 480` as a JSON number, so `ui.GetSetting[*settings.GuestAccess]` fails with `json: cannot unmarshal number into Go struct field .Alias.expire of type string` (verified against v1.33.43-0.20260706191309). The section's `read` therefore pulls its raw document from `ListSettings` and converts with typed accessors. Tagged `TODO(go-unifi):` for the eventual upstream fix.

**Files:**
- Create: `unifi/setting_section_raw.go`
- Create: `unifi/setting_section_raw_test.go`
- Create: `unifi/setting_section_guest_access.go`
- Create: `unifi/setting_section_guest_access_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `Client` (`ListSettings`), `settings.RawSetting` (`Data map[string]any`, decoded by `encoding/json` — numbers arrive as `float64`).
- Produces (Tasks 4–5 and Task 7 rely on these exact names):
  - Raw helpers: `setRawString(data, key, v types.String)`, `setRawBool`, `setRawInt(data, key, v types.Int64)`; `rawString(data, key) types.String` (empty string → null, mirroring `util.StringValueOrNull`), `rawBool(data, key) types.Bool` (absent → null), `rawInt(data, key) types.Int64` (handles `float64`/`int`/`int64`/numeric `string`; absent/other → null), `rawStringList(data, key) types.List` (absent/non-list → null), `anyRawKey(data, keys...) bool`.
  - Section: `settingGuestAccessModel`, `settingGuestAccessPortalCustomizationModel`, `settingGuestAccessRedirectModel`, `guestAccessAttrTypes`, `guestAccessPortalCustomizationAttrTypes`, `guestAccessRedirectAttrTypes`, `guestAccessSection`, `guestAccessModelToData(ctx, m, data, diags)`, `guestAccessDataToModel(ctx, data, diags)`, `hexColorValidator`.
- JSON key oddities (from the generated struct, preserved verbatim on the wire): `allowed_subnet_` and `restricted_subnet_` carry trailing underscores; secrets use `x_` prefixes (`x_password`, …). Live controllers also store unmodeled `restricted_subnet_1..3` keys — the raw merge must preserve them (tested).

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_raw_test.go`:

```go
package unifi

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func Test_rawValueHelpers(t *testing.T) {
	data := map[string]any{
		"str":       "value",
		"empty_str": "",
		"flag":      true,
		"num":       float64(480), // encoding/json decodes numbers to float64
		"num_str":   "42",
		"langs":     []any{"en", "de"},
	}

	if got := rawString(data, "str"); got.ValueString() != "value" {
		t.Fatalf("rawString(str) = %v", got)
	}
	if !rawString(data, "empty_str").IsNull() {
		t.Fatal("rawString should map empty string to null")
	}
	if !rawString(data, "missing").IsNull() {
		t.Fatal("rawString should map missing key to null")
	}
	if got := rawBool(data, "flag"); !got.ValueBool() {
		t.Fatalf("rawBool(flag) = %v", got)
	}
	if !rawBool(data, "missing").IsNull() {
		t.Fatal("rawBool should map missing key to null")
	}
	if got := rawInt(data, "num"); got.ValueInt64() != 480 {
		t.Fatalf("rawInt(num) = %v", got)
	}
	if got := rawInt(data, "num_str"); got.ValueInt64() != 42 {
		t.Fatalf("rawInt(num_str) = %v", got)
	}
	if !rawInt(data, "missing").IsNull() {
		t.Fatal("rawInt should map missing key to null")
	}
	langs := rawStringList(data, "langs")
	if langs.IsNull() || len(langs.Elements()) != 2 {
		t.Fatalf("rawStringList(langs) = %v", langs)
	}
	if !rawStringList(data, "missing").IsNull() {
		t.Fatal("rawStringList should map missing key to null")
	}
	if !anyRawKey(data, "nope", "flag") || anyRawKey(data, "nope") {
		t.Fatal("anyRawKey wrong")
	}
}

func Test_setRawHelpers(t *testing.T) {
	data := map[string]any{"keep": "keep"}

	setRawString(data, "s", types.StringValue("v"))
	setRawString(data, "s_null", types.StringNull())
	setRawBool(data, "b", types.BoolValue(true))
	setRawBool(data, "b_null", types.BoolNull())
	setRawInt(data, "i", types.Int64Value(7))
	setRawInt(data, "i_null", types.Int64Null())

	if data["s"] != "v" || data["b"] != true || data["i"] != int64(7) {
		t.Fatalf("set values wrong: %v", data)
	}
	for _, k := range []string{"s_null", "b_null", "i_null"} {
		if _, present := data[k]; present {
			t.Fatalf("null value wrote key %q", k)
		}
	}
	if data["keep"] != "keep" {
		t.Fatal("unrelated key clobbered")
	}
}
```

Create `unifi/setting_section_guest_access_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

func Test_guestAccessModelToData_core(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	redirect, d := types.ObjectValueFrom(ctx, guestAccessRedirectAttrTypes,
		settingGuestAccessRedirectModel{
			UseHttps: types.BoolValue(true),
			ToHttps:  types.BoolValue(false),
			URL:      types.StringValue("https://example.com/welcome"),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	langs, d := types.ListValueFrom(ctx, types.StringType, []string{"en"})
	if d.HasError() {
		t.Fatal(d)
	}
	pc, d := types.ObjectValueFrom(ctx, guestAccessPortalCustomizationAttrTypes,
		settingGuestAccessPortalCustomizationModel{
			Customized:             types.BoolValue(true),
			AuthenticationText:     types.StringNull(),
			BgColor:                types.StringValue("#005ED9"),
			BgImageEnabled:         types.BoolNull(),
			BgImageFileID:          types.StringNull(),
			BgImageTile:            types.BoolNull(),
			BgType:                 types.StringValue("color"),
			BoxColor:               types.StringNull(),
			BoxLinkColor:           types.StringNull(),
			BoxOpacity:             types.Int64Value(90),
			BoxRadius:              types.Int64Null(),
			BoxTextColor:           types.StringNull(),
			ButtonColor:            types.StringNull(),
			ButtonText:             types.StringNull(),
			ButtonTextColor:        types.StringNull(),
			Languages:              langs,
			LinkColor:              types.StringNull(),
			LogoEnabled:            types.BoolNull(),
			LogoFileID:             types.StringNull(),
			LogoPosition:           types.StringNull(),
			LogoSize:               types.Int64Null(),
			SuccessText:            types.StringNull(),
			TextColor:              types.StringNull(),
			Title:                  types.StringValue("Guest WiFi"),
			Tos:                    types.StringNull(),
			TosEnabled:             types.BoolNull(),
			UnsplashAuthorName:     types.StringNull(),
			UnsplashAuthorUsername: types.StringNull(),
			WelcomeText:            types.StringNull(),
			WelcomeTextEnabled:     types.BoolNull(),
			WelcomeTextPosition:    types.StringNull(),
		})
	if d.HasError() {
		t.Fatal(d)
	}

	obj, d := types.ObjectValueFrom(ctx, guestAccessAttrTypes, settingGuestAccessModel{
		Auth:                types.StringValue("hotspot"),
		Expire:              types.Int64Value(480),
		ExpireNumber:        types.Int64Value(8),
		ExpireUnit:          types.Int64Value(60),
		PortalEnabled:       types.BoolValue(true),
		RedirectEnabled:     types.BoolValue(true),
		Redirect:            redirect,
		PortalCustomization: pc,
		// Everything else null: it must not be written.
		AllowedSubnet:     types.StringNull(),
		RestrictedSubnet:  types.StringNull(),
		AuthUrl:           types.StringNull(),
		CustomIP:          types.StringNull(),
		EcEnabled:         types.BoolNull(),
		PortalHostname:    types.StringNull(),
		PortalUseHostname: types.BoolNull(),
		TemplateEngine:    types.StringNull(),
		VoucherCustomized: types.BoolNull(),
		VoucherEnabled:    types.BoolNull(),
	})
	if d.HasError() {
		t.Fatal(d)
	}
	var m settingGuestAccessModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		t.Fatal(diags)
	}

	// Live controllers store restricted_subnet_1..3 which go-unifi does not
	// model: the raw merge must preserve them verbatim.
	data := map[string]any{
		"restricted_subnet_1": "192.168.0.0/16",
		"auth":                "none",
		"template_engine":     "angular",
	}

	guestAccessModelToData(ctx, &m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["restricted_subnet_1"] != "192.168.0.0/16" {
		t.Fatal("unmodeled restricted_subnet_1 was clobbered")
	}
	if data["template_engine"] != "angular" {
		t.Fatal("null template_engine overwrote remote value")
	}
	if data["auth"] != "hotspot" {
		t.Fatalf("auth = %v", data["auth"])
	}
	if data["expire"] != int64(480) || data["expire_number"] != int64(8) ||
		data["expire_unit"] != int64(60) {
		t.Fatalf("expire fields wrong: %v", data)
	}
	if data["portal_enabled"] != true || data["redirect_enabled"] != true {
		t.Fatalf("portal/redirect enabled wrong: %v", data)
	}
	if data["redirect_url"] != "https://example.com/welcome" ||
		data["redirect_https"] != true || data["redirect_to_https"] != false {
		t.Fatalf("redirect fields wrong: %v", data)
	}
	if data["portal_customized"] != true ||
		data["portal_customized_bg_color"] != "#005ED9" ||
		data["portal_customized_title"] != "Guest WiFi" ||
		data["portal_customized_box_opacity"] != int64(90) {
		t.Fatalf("portal_customized fields wrong: %v", data)
	}
	pcLangs, ok := data["portal_customized_languages"].([]string)
	if !ok || len(pcLangs) != 1 || pcLangs[0] != "en" {
		t.Fatalf("portal_customized_languages = %v", data["portal_customized_languages"])
	}
	if _, present := data["portal_hostname"]; present {
		t.Fatal("null portal_hostname should not be written")
	}
	if _, present := data["portal_customized_tos"]; present {
		t.Fatal("null portal_customization.tos should not be written")
	}
}

func Test_guestAccessDataToModel_liveShape(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	// Shape (not values) of a live UDM guest_access document. Numbers are
	// float64 exactly as encoding/json decodes them — including "expire",
	// which the generated go-unifi struct wrongly types as string
	// (TODO(go-unifi)): this test pins the raw-read workaround.
	data := map[string]any{
		"_id":                        "aaaaaaaaaaaaaaaaaaaaaaaa",
		"key":                        "guest_access",
		"auth":                       "none",
		"ec_enabled":                 true,
		"expire":                     float64(480),
		"expire_number":              float64(8),
		"expire_unit":                float64(60),
		"portal_enabled":             false,
		"portal_use_hostname":        false,
		"portal_customized":          false,
		"portal_customized_bg_color": "#005ED9",
		"portal_customized_bg_type":  "color",
		"portal_customized_box_opacity": float64(100),
		"portal_customized_languages":   []any{"en"},
		"portal_customized_title":       "UniFi Guest WiFi",
		"redirect_enabled":              false,
		"redirect_https":                true,
		"redirect_to_https":             false,
		"redirect_url":                  "",
		"restricted_subnet_1":           "192.168.0.0/16",
		"template_engine":               "angular",
	}

	m := guestAccessDataToModel(ctx, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if m.Auth.ValueString() != "none" {
		t.Fatalf("auth = %v", m.Auth)
	}
	if m.Expire.ValueInt64() != 480 || m.ExpireNumber.ValueInt64() != 8 ||
		m.ExpireUnit.ValueInt64() != 60 {
		t.Fatalf("expire fields = %v/%v/%v", m.Expire, m.ExpireNumber, m.ExpireUnit)
	}
	if !m.EcEnabled.ValueBool() || m.PortalEnabled.ValueBool() {
		t.Fatalf("ec/portal enabled = %v/%v", m.EcEnabled, m.PortalEnabled)
	}
	if m.TemplateEngine.ValueString() != "angular" {
		t.Fatalf("template_engine = %v", m.TemplateEngine)
	}
	if !m.RestrictedSubnet.IsNull() {
		t.Fatalf("restricted_subnet should be null (only indexed variants present), got %v", m.RestrictedSubnet)
	}

	if m.PortalCustomization.IsNull() {
		t.Fatal("portal_customization should be present")
	}
	var pc settingGuestAccessPortalCustomizationModel
	diags.Append(m.PortalCustomization.As(ctx, &pc, basetypes.ObjectAsOptions{})...)
	if pc.BgColor.ValueString() != "#005ED9" || pc.BoxOpacity.ValueInt64() != 100 ||
		pc.Title.ValueString() != "UniFi Guest WiFi" {
		t.Fatalf("portal_customization = %+v", pc)
	}
	if pc.Languages.IsNull() || len(pc.Languages.Elements()) != 1 {
		t.Fatalf("languages = %v", pc.Languages)
	}

	if m.Redirect.IsNull() {
		t.Fatal("redirect should be present (redirect_https key exists)")
	}
	var r settingGuestAccessRedirectModel
	diags.Append(m.Redirect.As(ctx, &r, basetypes.ObjectAsOptions{})...)
	if !r.UseHttps.ValueBool() || r.ToHttps.ValueBool() || !r.URL.IsNull() {
		t.Fatalf("redirect = %+v", r)
	}
}

func Test_guestAccessDataToModel_absentBlocks(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := guestAccessDataToModel(ctx, map[string]any{"auth": "none"}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if !m.PortalCustomization.IsNull() {
		t.Fatal("portal_customization should be null when no portal_customized key exists")
	}
	if !m.Redirect.IsNull() {
		t.Fatal("redirect should be null when no redirect keys exist")
	}
	if !m.Expire.IsNull() {
		t.Fatal("expire should be null when absent")
	}
}

func Test_settingResource_Schema_guestAccess(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["guest_access"]; !ok {
		t.Fatal("schema is missing the guest_access section attribute")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_rawValueHelpers|Test_setRawHelpers|Test_guestAccess|Test_settingResource_Schema_guestAccess' -count=1`
Expected: compile FAILURE — `undefined: rawString`, `undefined: settingGuestAccessModel`, etc.

- [ ] **Step 3: Create `unifi/setting_section_raw.go`**

```go
package unifi

import (
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Helpers for sections that convert to/from the raw rest/setting JSON
// document (settings.RawSetting.Data, a map[string]any decoded by
// encoding/json — numbers arrive as float64).

// setRawString writes the value only if it is user-set; null/unknown leaves
// the remote value untouched.
func setRawString(data map[string]any, key string, v types.String) {
	if !v.IsNull() && !v.IsUnknown() {
		data[key] = v.ValueString()
	}
}

func setRawBool(data map[string]any, key string, v types.Bool) {
	if !v.IsNull() && !v.IsUnknown() {
		data[key] = v.ValueBool()
	}
}

func setRawInt(data map[string]any, key string, v types.Int64) {
	if !v.IsNull() && !v.IsUnknown() {
		data[key] = v.ValueInt64()
	}
}

// rawString reads a string field; absent or empty maps to null, mirroring
// util.StringValueOrNull on the typed read path.
func rawString(data map[string]any, key string) types.String {
	if v, ok := data[key].(string); ok && v != "" {
		return types.StringValue(v)
	}
	return types.StringNull()
}

// rawBool reads a bool field; absent maps to null.
func rawBool(data map[string]any, key string) types.Bool {
	if v, ok := data[key].(bool); ok {
		return types.BoolValue(v)
	}
	return types.BoolNull()
}

// rawInt reads a numeric field, tolerating the representations UniFi
// controllers use interchangeably (JSON number → float64, or numeric
// string); absent or non-numeric maps to null.
func rawInt(data map[string]any, key string) types.Int64 {
	switch v := data[key].(type) {
	case float64:
		return types.Int64Value(int64(v))
	case int:
		return types.Int64Value(int64(v))
	case int64:
		return types.Int64Value(v)
	case string:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return types.Int64Value(n)
		}
	}
	return types.Int64Null()
}

// rawStringList reads a []any-of-strings field; absent maps to null.
func rawStringList(data map[string]any, key string) types.List {
	raw, ok := data[key].([]any)
	if !ok {
		return types.ListNull(types.StringType)
	}
	elems := make([]attr.Value, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			elems = append(elems, types.StringValue(s))
		}
	}
	// All elements are types.String; construction cannot fail.
	return types.ListValueMust(types.StringType, elems)
}

// anyRawKey reports whether any of the keys exists in the raw document —
// used to decide whether a nested block materializes or stays null.
func anyRawKey(data map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := data[k]; ok {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Create `unifi/setting_section_guest_access.go`**

```go
package unifi

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// settingGuestAccessModel is the nested guest_access block: the guest
// hotspot/captive portal. Attribute names align with filipowm's
// unifi_setting_guest_access wherever fields overlap. Unlike filipowm, the
// *_enabled flags are explicit attributes (configuring a provider block does
// NOT implicitly enable it) — the raw-merge engine only ever writes fields
// the user set.
type settingGuestAccessModel struct {
	AllowedSubnet       types.String `tfsdk:"allowed_subnet"`
	RestrictedSubnet    types.String `tfsdk:"restricted_subnet"`
	Auth                types.String `tfsdk:"auth"`
	AuthUrl             types.String `tfsdk:"auth_url"`
	CustomIP            types.String `tfsdk:"custom_ip"`
	EcEnabled           types.Bool   `tfsdk:"ec_enabled"`
	Expire              types.Int64  `tfsdk:"expire"`
	ExpireNumber        types.Int64  `tfsdk:"expire_number"`
	ExpireUnit          types.Int64  `tfsdk:"expire_unit"`
	PortalCustomization types.Object `tfsdk:"portal_customization"`
	PortalEnabled       types.Bool   `tfsdk:"portal_enabled"`
	PortalHostname      types.String `tfsdk:"portal_hostname"`
	PortalUseHostname   types.Bool   `tfsdk:"portal_use_hostname"`
	Redirect            types.Object `tfsdk:"redirect"`
	RedirectEnabled     types.Bool   `tfsdk:"redirect_enabled"`
	TemplateEngine      types.String `tfsdk:"template_engine"`
	VoucherCustomized   types.Bool   `tfsdk:"voucher_customized"`
	VoucherEnabled      types.Bool   `tfsdk:"voucher_enabled"`
}

type settingGuestAccessRedirectModel struct {
	ToHttps  types.Bool   `tfsdk:"to_https"`
	URL      types.String `tfsdk:"url"`
	UseHttps types.Bool   `tfsdk:"use_https"`
}

type settingGuestAccessPortalCustomizationModel struct {
	Customized             types.Bool   `tfsdk:"customized"`
	AuthenticationText     types.String `tfsdk:"authentication_text"`
	BgColor                types.String `tfsdk:"bg_color"`
	BgImageEnabled         types.Bool   `tfsdk:"bg_image_enabled"`
	BgImageFileID          types.String `tfsdk:"bg_image_file_id"`
	BgImageTile            types.Bool   `tfsdk:"bg_image_tile"`
	BgType                 types.String `tfsdk:"bg_type"`
	BoxColor               types.String `tfsdk:"box_color"`
	BoxLinkColor           types.String `tfsdk:"box_link_color"`
	BoxOpacity             types.Int64  `tfsdk:"box_opacity"`
	BoxRadius              types.Int64  `tfsdk:"box_radius"`
	BoxTextColor           types.String `tfsdk:"box_text_color"`
	ButtonColor            types.String `tfsdk:"button_color"`
	ButtonText             types.String `tfsdk:"button_text"`
	ButtonTextColor        types.String `tfsdk:"button_text_color"`
	Languages              types.List   `tfsdk:"languages"`
	LinkColor              types.String `tfsdk:"link_color"`
	LogoEnabled            types.Bool   `tfsdk:"logo_enabled"`
	LogoFileID             types.String `tfsdk:"logo_file_id"`
	LogoPosition           types.String `tfsdk:"logo_position"`
	LogoSize               types.Int64  `tfsdk:"logo_size"`
	SuccessText            types.String `tfsdk:"success_text"`
	TextColor              types.String `tfsdk:"text_color"`
	Title                  types.String `tfsdk:"title"`
	Tos                    types.String `tfsdk:"tos"`
	TosEnabled             types.Bool   `tfsdk:"tos_enabled"`
	UnsplashAuthorName     types.String `tfsdk:"unsplash_author_name"`
	UnsplashAuthorUsername types.String `tfsdk:"unsplash_author_username"`
	WelcomeText            types.String `tfsdk:"welcome_text"`
	WelcomeTextEnabled     types.Bool   `tfsdk:"welcome_text_enabled"`
	WelcomeTextPosition    types.String `tfsdk:"welcome_text_position"`
}

var (
	guestAccessRedirectAttrTypes = map[string]attr.Type{
		"to_https":  types.BoolType,
		"url":       types.StringType,
		"use_https": types.BoolType,
	}
	guestAccessPortalCustomizationAttrTypes = map[string]attr.Type{
		"customized":               types.BoolType,
		"authentication_text":      types.StringType,
		"bg_color":                 types.StringType,
		"bg_image_enabled":         types.BoolType,
		"bg_image_file_id":         types.StringType,
		"bg_image_tile":            types.BoolType,
		"bg_type":                  types.StringType,
		"box_color":                types.StringType,
		"box_link_color":           types.StringType,
		"box_opacity":              types.Int64Type,
		"box_radius":               types.Int64Type,
		"box_text_color":           types.StringType,
		"button_color":             types.StringType,
		"button_text":              types.StringType,
		"button_text_color":        types.StringType,
		"languages":                types.ListType{ElemType: types.StringType},
		"link_color":               types.StringType,
		"logo_enabled":             types.BoolType,
		"logo_file_id":             types.StringType,
		"logo_position":            types.StringType,
		"logo_size":                types.Int64Type,
		"success_text":             types.StringType,
		"text_color":               types.StringType,
		"title":                    types.StringType,
		"tos":                      types.StringType,
		"tos_enabled":              types.BoolType,
		"unsplash_author_name":     types.StringType,
		"unsplash_author_username": types.StringType,
		"welcome_text":             types.StringType,
		"welcome_text_enabled":     types.BoolType,
		"welcome_text_position":    types.StringType,
	}
	guestAccessAttrTypes = map[string]attr.Type{
		"allowed_subnet":    types.StringType,
		"restricted_subnet": types.StringType,
		"auth":              types.StringType,
		"auth_url":          types.StringType,
		"custom_ip":         types.StringType,
		"ec_enabled":        types.BoolType,
		"expire":            types.Int64Type,
		"expire_number":     types.Int64Type,
		"expire_unit":       types.Int64Type,
		"portal_customization": types.ObjectType{
			AttrTypes: guestAccessPortalCustomizationAttrTypes,
		},
		"portal_enabled":      types.BoolType,
		"portal_hostname":     types.StringType,
		"portal_use_hostname": types.BoolType,
		"redirect":            types.ObjectType{AttrTypes: guestAccessRedirectAttrTypes},
		"redirect_enabled":    types.BoolType,
		"template_engine":     types.StringType,
		"voucher_customized":  types.BoolType,
		"voucher_enabled":     types.BoolType,
	}
)

var hexColorValidator = stringvalidator.RegexMatches(
	regexp.MustCompile(`^#[0-9a-fA-F]{6}$|^#[0-9a-fA-F]{3}$|^$`),
	"must be a hex color like #FFF or #FFFFFF",
)

type guestAccessSection struct{}

func (guestAccessSection) key() string { return "guest_access" }

func (guestAccessSection) attrTypes() map[string]attr.Type { return guestAccessAttrTypes }

func (guestAccessSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Guest hotspot / captive portal settings. Attribute names align with the " +
			"filipowm provider's `unifi_setting_guest_access` for config portability. Note: unlike " +
			"filipowm, provider blocks (`facebook`, `google`, payment gateways, …) do not implicitly " +
			"flip their `*_enabled` flag — set the flag explicitly alongside the block. Controller " +
			"fields not modeled here (e.g. `restricted_subnet_1..3`) are preserved across updates.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"allowed_subnet": schema.StringAttribute{
				MarkdownDescription: "Subnet allowed for guest access.",
				Optional:            true,
				Computed:            true,
			},
			"restricted_subnet": schema.StringAttribute{
				MarkdownDescription: "Subnet restricted from guest access.",
				Optional:            true,
				Computed:            true,
			},
			"auth": schema.StringAttribute{
				MarkdownDescription: "Authentication method: `none`, `hotspot`, `facebook_wifi`, or `custom`. " +
					"For password/voucher/payment authentication set `auth = \"hotspot\"` plus the matching `*_enabled` flag.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.OneOf("none", "hotspot", "facebook_wifi", "custom"),
				},
			},
			"auth_url": schema.StringAttribute{
				MarkdownDescription: "External portal authentication URL (for `auth = \"custom\"`).",
				Optional:            true,
				Computed:            true,
			},
			"custom_ip": schema.StringAttribute{
				MarkdownDescription: "External portal server IPv4 address (for `auth = \"custom\"`).",
				Optional:            true,
				Computed:            true,
			},
			"ec_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable enterprise controller functionality.",
				Optional:            true,
				Computed:            true,
			},
			"expire": schema.Int64Attribute{
				MarkdownDescription: "Guest authorization lifetime in minutes (kept in sync with `expire_number` × `expire_unit` by the controller).",
				Optional:            true,
				Computed:            true,
			},
			"expire_number": schema.Int64Attribute{
				MarkdownDescription: "Number component of the authorization lifetime.",
				Optional:            true,
				Computed:            true,
			},
			"expire_unit": schema.Int64Attribute{
				MarkdownDescription: "Unit component of the authorization lifetime: `1` (minute), `60` (hour), `1440` (day), `10080` (week).",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.OneOf(1, 60, 1440, 10080),
				},
			},
			"portal_customization": guestAccessPortalCustomizationSchema(),
			"portal_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable the guest portal.",
				Optional:            true,
				Computed:            true,
			},
			"portal_hostname": schema.StringAttribute{
				MarkdownDescription: "Custom hostname for the captive portal.",
				Optional:            true,
				Computed:            true,
			},
			"portal_use_hostname": schema.BoolAttribute{
				MarkdownDescription: "Use `portal_hostname` for portal URLs.",
				Optional:            true,
				Computed:            true,
			},
			"redirect": schema.SingleNestedAttribute{
				MarkdownDescription: "Redirect-after-authentication settings (enable with `redirect_enabled`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"to_https": schema.BoolAttribute{
						MarkdownDescription: "Redirect HTTP requests to HTTPS.",
						Optional:            true,
						Computed:            true,
					},
					"url": schema.StringAttribute{
						MarkdownDescription: "URL to redirect to after authentication.",
						Optional:            true,
						Computed:            true,
					},
					"use_https": schema.BoolAttribute{
						MarkdownDescription: "Use HTTPS for the redirect.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"redirect_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable redirect after authentication.",
				Optional:            true,
				Computed:            true,
			},
			"template_engine": schema.StringAttribute{
				MarkdownDescription: "Portal template engine: `jsp` or `angular`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("jsp", "angular"),
				},
			},
			"voucher_customized": schema.BoolAttribute{
				MarkdownDescription: "Whether vouchers are customized.",
				Optional:            true,
				Computed:            true,
			},
			"voucher_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable voucher authentication (requires `auth = \"hotspot\"`).",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func guestAccessPortalCustomizationSchema() schema.SingleNestedAttribute {
	hexColor := []validator.String{hexColorValidator}
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Portal look & feel. `bg_image_enabled`/`logo_enabled` are a superset over filipowm.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"customized": schema.BoolAttribute{
				MarkdownDescription: "Whether the portal is customized.",
				Optional:            true,
				Computed:            true,
			},
			"authentication_text": schema.StringAttribute{
				MarkdownDescription: "Custom authentication text.",
				Optional:            true,
				Computed:            true,
			},
			"bg_color": schema.StringAttribute{
				MarkdownDescription: "Background color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"bg_image_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable the background image.",
				Optional:            true,
				Computed:            true,
			},
			"bg_image_file_id": schema.StringAttribute{
				MarkdownDescription: "Portal file ID of the background image.",
				Optional:            true,
				Computed:            true,
			},
			"bg_image_tile": schema.BoolAttribute{
				MarkdownDescription: "Tile the background image.",
				Optional:            true,
				Computed:            true,
			},
			"bg_type": schema.StringAttribute{
				MarkdownDescription: "Background type: `color`, `image`, or `gallery`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("color", "image", "gallery"),
				},
			},
			"box_color": schema.StringAttribute{
				MarkdownDescription: "Login box color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"box_link_color": schema.StringAttribute{
				MarkdownDescription: "Login box link color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"box_opacity": schema.Int64Attribute{
				MarkdownDescription: "Login box opacity (0-100).",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.Between(0, 100),
				},
			},
			"box_radius": schema.Int64Attribute{
				MarkdownDescription: "Login box border radius in pixels.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
			"box_text_color": schema.StringAttribute{
				MarkdownDescription: "Login box text color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"button_color": schema.StringAttribute{
				MarkdownDescription: "Button color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"button_text": schema.StringAttribute{
				MarkdownDescription: "Login button text.",
				Optional:            true,
				Computed:            true,
			},
			"button_text_color": schema.StringAttribute{
				MarkdownDescription: "Button text color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"languages": schema.ListAttribute{
				MarkdownDescription: "Enabled portal languages, in display order.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"link_color": schema.StringAttribute{
				MarkdownDescription: "Link color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"logo_enabled": schema.BoolAttribute{
				MarkdownDescription: "Show the logo.",
				Optional:            true,
				Computed:            true,
			},
			"logo_file_id": schema.StringAttribute{
				MarkdownDescription: "Portal file ID of the logo image.",
				Optional:            true,
				Computed:            true,
			},
			"logo_position": schema.StringAttribute{
				MarkdownDescription: "Logo position: `left`, `center`, or `right`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("left", "center", "right"),
				},
			},
			"logo_size": schema.Int64Attribute{
				MarkdownDescription: "Logo size in pixels.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
			"success_text": schema.StringAttribute{
				MarkdownDescription: "Text shown after successful authentication.",
				Optional:            true,
				Computed:            true,
			},
			"text_color": schema.StringAttribute{
				MarkdownDescription: "Main text color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "Portal page title.",
				Optional:            true,
				Computed:            true,
			},
			"tos": schema.StringAttribute{
				MarkdownDescription: "Terms of service text.",
				Optional:            true,
				Computed:            true,
			},
			"tos_enabled": schema.BoolAttribute{
				MarkdownDescription: "Require terms of service acceptance.",
				Optional:            true,
				Computed:            true,
			},
			"unsplash_author_name": schema.StringAttribute{
				MarkdownDescription: "Unsplash author name for gallery backgrounds.",
				Optional:            true,
				Computed:            true,
			},
			"unsplash_author_username": schema.StringAttribute{
				MarkdownDescription: "Unsplash author username for gallery backgrounds.",
				Optional:            true,
				Computed:            true,
			},
			"welcome_text": schema.StringAttribute{
				MarkdownDescription: "Welcome text.",
				Optional:            true,
				Computed:            true,
			},
			"welcome_text_enabled": schema.BoolAttribute{
				MarkdownDescription: "Show the welcome text.",
				Optional:            true,
				Computed:            true,
			},
			"welcome_text_position": schema.StringAttribute{
				MarkdownDescription: "Welcome text position: `under_logo` or `above_boxes`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("under_logo", "above_boxes"),
				},
			},
		},
	}
}

func (guestAccessSection) get(m *settingResourceModel) types.Object { return m.GuestAccess }

func (guestAccessSection) set(m *settingResourceModel, obj types.Object) { m.GuestAccess = obj }

func (guestAccessSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingGuestAccessModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	guestAccessModelToData(ctx, &m, data, &diags)
	return diags
}

// guestAccessModelToData writes only the user-set fields into the raw
// section document; unset fields — including controller fields go-unifi
// does not model, like restricted_subnet_1..3 — keep their remote values.
// Secret fields use the controller's x_ key prefix.
func guestAccessModelToData(
	ctx context.Context,
	m *settingGuestAccessModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	setRawString(data, "allowed_subnet_", m.AllowedSubnet)
	setRawString(data, "restricted_subnet_", m.RestrictedSubnet)
	setRawString(data, "auth", m.Auth)
	setRawString(data, "auth_url", m.AuthUrl)
	setRawString(data, "custom_ip", m.CustomIP)
	setRawBool(data, "ec_enabled", m.EcEnabled)
	setRawInt(data, "expire", m.Expire)
	setRawInt(data, "expire_number", m.ExpireNumber)
	setRawInt(data, "expire_unit", m.ExpireUnit)
	setRawBool(data, "portal_enabled", m.PortalEnabled)
	setRawString(data, "portal_hostname", m.PortalHostname)
	setRawBool(data, "portal_use_hostname", m.PortalUseHostname)
	setRawBool(data, "redirect_enabled", m.RedirectEnabled)
	setRawString(data, "template_engine", m.TemplateEngine)
	setRawBool(data, "voucher_customized", m.VoucherCustomized)
	setRawBool(data, "voucher_enabled", m.VoucherEnabled)

	if !m.Redirect.IsNull() && !m.Redirect.IsUnknown() {
		var r settingGuestAccessRedirectModel
		diags.Append(m.Redirect.As(ctx, &r, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawBool(data, "redirect_https", r.UseHttps)
		setRawBool(data, "redirect_to_https", r.ToHttps)
		setRawString(data, "redirect_url", r.URL)
	}

	if !m.PortalCustomization.IsNull() && !m.PortalCustomization.IsUnknown() {
		var pc settingGuestAccessPortalCustomizationModel
		diags.Append(m.PortalCustomization.As(ctx, &pc, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawBool(data, "portal_customized", pc.Customized)
		setRawString(data, "portal_customized_authentication_text", pc.AuthenticationText)
		setRawString(data, "portal_customized_bg_color", pc.BgColor)
		setRawBool(data, "portal_customized_bg_image_enabled", pc.BgImageEnabled)
		setRawString(data, "portal_customized_bg_image_filename", pc.BgImageFileID)
		setRawBool(data, "portal_customized_bg_image_tile", pc.BgImageTile)
		setRawString(data, "portal_customized_bg_type", pc.BgType)
		setRawString(data, "portal_customized_box_color", pc.BoxColor)
		setRawString(data, "portal_customized_box_link_color", pc.BoxLinkColor)
		setRawInt(data, "portal_customized_box_opacity", pc.BoxOpacity)
		setRawInt(data, "portal_customized_box_radius", pc.BoxRadius)
		setRawString(data, "portal_customized_box_text_color", pc.BoxTextColor)
		setRawString(data, "portal_customized_button_color", pc.ButtonColor)
		setRawString(data, "portal_customized_button_text", pc.ButtonText)
		setRawString(data, "portal_customized_button_text_color", pc.ButtonTextColor)
		if !pc.Languages.IsNull() && !pc.Languages.IsUnknown() {
			var langs []string
			diags.Append(pc.Languages.ElementsAs(ctx, &langs, false)...)
			data["portal_customized_languages"] = langs
		}
		setRawString(data, "portal_customized_link_color", pc.LinkColor)
		setRawBool(data, "portal_customized_logo_enabled", pc.LogoEnabled)
		setRawString(data, "portal_customized_logo_filename", pc.LogoFileID)
		setRawString(data, "portal_customized_logo_position", pc.LogoPosition)
		setRawInt(data, "portal_customized_logo_size", pc.LogoSize)
		setRawString(data, "portal_customized_success_text", pc.SuccessText)
		setRawString(data, "portal_customized_text_color", pc.TextColor)
		setRawString(data, "portal_customized_title", pc.Title)
		setRawString(data, "portal_customized_tos", pc.Tos)
		setRawBool(data, "portal_customized_tos_enabled", pc.TosEnabled)
		setRawString(data, "portal_customized_unsplash_author_name", pc.UnsplashAuthorName)
		setRawString(data, "portal_customized_unsplash_author_username", pc.UnsplashAuthorUsername)
		setRawString(data, "portal_customized_welcome_text", pc.WelcomeText)
		setRawBool(data, "portal_customized_welcome_text_enabled", pc.WelcomeTextEnabled)
		setRawString(data, "portal_customized_welcome_text_position", pc.WelcomeTextPosition)
	}
}

// read pulls the guest_access section out of the raw settings list.
//
// TODO(go-unifi): use ui.GetSetting[*settings.GuestAccess] once upstream
// models `expire` as a number — the generated struct declares it string,
// so the typed unmarshal fails on live controllers that send `"expire": 480`.
func (guestAccessSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	raws, err := client.ListSettings(ctx, site)
	if err != nil {
		diags.AddError("Error Reading Guest Access Setting", err.Error())
		return types.ObjectNull(guestAccessAttrTypes), diags
	}
	for _, raw := range raws {
		if raw.GetKey() != "guest_access" {
			continue
		}
		model := guestAccessDataToModel(ctx, raw.Data, &diags)
		if diags.HasError() {
			return types.ObjectNull(guestAccessAttrTypes), diags
		}
		return types.ObjectValueFrom(ctx, guestAccessAttrTypes, model)
	}
	return types.ObjectNull(guestAccessAttrTypes), diags
}

// guestAccessDataToModel converts the raw section document to the model.
// Nested blocks materialize only when at least one of their keys exists.
func guestAccessDataToModel(
	ctx context.Context,
	data map[string]any,
	diags *diag.Diagnostics,
) settingGuestAccessModel {
	m := settingGuestAccessModel{
		AllowedSubnet:     rawString(data, "allowed_subnet_"),
		RestrictedSubnet:  rawString(data, "restricted_subnet_"),
		Auth:              rawString(data, "auth"),
		AuthUrl:           rawString(data, "auth_url"),
		CustomIP:          rawString(data, "custom_ip"),
		EcEnabled:         rawBool(data, "ec_enabled"),
		Expire:            rawInt(data, "expire"),
		ExpireNumber:      rawInt(data, "expire_number"),
		ExpireUnit:        rawInt(data, "expire_unit"),
		PortalEnabled:     rawBool(data, "portal_enabled"),
		PortalHostname:    rawString(data, "portal_hostname"),
		PortalUseHostname: rawBool(data, "portal_use_hostname"),
		RedirectEnabled:   rawBool(data, "redirect_enabled"),
		TemplateEngine:    rawString(data, "template_engine"),
		VoucherCustomized: rawBool(data, "voucher_customized"),
		VoucherEnabled:    rawBool(data, "voucher_enabled"),
	}

	m.Redirect = types.ObjectNull(guestAccessRedirectAttrTypes)
	if anyRawKey(data, "redirect_https", "redirect_to_https", "redirect_url") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessRedirectAttrTypes,
			settingGuestAccessRedirectModel{
				ToHttps:  rawBool(data, "redirect_to_https"),
				URL:      rawString(data, "redirect_url"),
				UseHttps: rawBool(data, "redirect_https"),
			})
		diags.Append(d...)
		m.Redirect = obj
	}

	m.PortalCustomization = types.ObjectNull(guestAccessPortalCustomizationAttrTypes)
	if anyRawKey(data, "portal_customized") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessPortalCustomizationAttrTypes,
			settingGuestAccessPortalCustomizationModel{
				Customized:             rawBool(data, "portal_customized"),
				AuthenticationText:     rawString(data, "portal_customized_authentication_text"),
				BgColor:                rawString(data, "portal_customized_bg_color"),
				BgImageEnabled:         rawBool(data, "portal_customized_bg_image_enabled"),
				BgImageFileID:          rawString(data, "portal_customized_bg_image_filename"),
				BgImageTile:            rawBool(data, "portal_customized_bg_image_tile"),
				BgType:                 rawString(data, "portal_customized_bg_type"),
				BoxColor:               rawString(data, "portal_customized_box_color"),
				BoxLinkColor:           rawString(data, "portal_customized_box_link_color"),
				BoxOpacity:             rawInt(data, "portal_customized_box_opacity"),
				BoxRadius:              rawInt(data, "portal_customized_box_radius"),
				BoxTextColor:           rawString(data, "portal_customized_box_text_color"),
				ButtonColor:            rawString(data, "portal_customized_button_color"),
				ButtonText:             rawString(data, "portal_customized_button_text"),
				ButtonTextColor:        rawString(data, "portal_customized_button_text_color"),
				Languages:              rawStringList(data, "portal_customized_languages"),
				LinkColor:              rawString(data, "portal_customized_link_color"),
				LogoEnabled:            rawBool(data, "portal_customized_logo_enabled"),
				LogoFileID:             rawString(data, "portal_customized_logo_filename"),
				LogoPosition:           rawString(data, "portal_customized_logo_position"),
				LogoSize:               rawInt(data, "portal_customized_logo_size"),
				SuccessText:            rawString(data, "portal_customized_success_text"),
				TextColor:              rawString(data, "portal_customized_text_color"),
				Title:                  rawString(data, "portal_customized_title"),
				Tos:                    rawString(data, "portal_customized_tos"),
				TosEnabled:             rawBool(data, "portal_customized_tos_enabled"),
				UnsplashAuthorName:     rawString(data, "portal_customized_unsplash_author_name"),
				UnsplashAuthorUsername: rawString(data, "portal_customized_unsplash_author_username"),
				WelcomeText:            rawString(data, "portal_customized_welcome_text"),
				WelcomeTextEnabled:     rawBool(data, "portal_customized_welcome_text_enabled"),
				WelcomeTextPosition:    rawString(data, "portal_customized_welcome_text_position"),
			})
		diags.Append(d...)
		m.PortalCustomization = obj
	}

	return m
}
```

- [ ] **Step 5: Register the section**

`unifi/setting_section.go` registry (after `radioAiSection{}`):

```go
	guestAccessSection{},
```

`unifi/setting_resource.go` model (after `RadioAi`):

```go
	GuestAccess   types.Object   `tfsdk:"guest_access"`
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_rawValueHelpers|Test_setRawHelpers|Test_guestAccess|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 7: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_raw.go unifi/setting_section_raw_test.go unifi/setting_section_guest_access.go unifi/setting_section_guest_access_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add guest_access section (portal basics + customization)"
```

(Body must explain the raw-read workaround for the go-unifi `Expire string` bug, with the exact unmarshal error, and note filipowm name alignment.)

---

### Task 4: `guest_access` — authentication providers

Adds password, Facebook, Facebook WiFi, Google, RADIUS, WeChat, and restricted-DNS attributes to the section created in Task 3. All names match filipowm; all credentials Sensitive.

**Files:**
- Modify: `unifi/setting_section_guest_access.go`
- Modify: `unifi/setting_section_guest_access_test.go`

**Interfaces:**
- Consumes: raw helpers and the Task 3 section skeleton.
- Produces: `settingGuestAccessFacebookModel`, `settingGuestAccessFacebookWifiModel`, `settingGuestAccessGoogleModel`, `settingGuestAccessRadiusModel`, `settingGuestAccessWechatModel` and their attrTypes maps (`guestAccessFacebookAttrTypes`, `guestAccessFacebookWifiAttrTypes`, `guestAccessGoogleAttrTypes`, `guestAccessRadiusAttrTypes`, `guestAccessWechatAttrTypes`).
- JSON key map: `password` ↔ `x_password`, `facebook.app_id` ↔ `facebook_app_id`, `facebook.app_secret` ↔ `x_facebook_app_secret`, `facebook.scope_email` ↔ `facebook_scope_email`, `facebook_wifi.block_https` ↔ `facebook_wifi_block_https`, `facebook_wifi.gateway_id/name` ↔ `facebook_wifi_gw_id/name`, `facebook_wifi.gateway_secret` ↔ `x_facebook_wifi_gw_secret`, `google.client_id` ↔ `google_client_id`, `google.client_secret` ↔ `x_google_client_secret`, `google.domain` ↔ `google_domain`, `google.scope_email` ↔ `google_scope_email`, `radius.auth_type` ↔ `radius_auth_type`, `radius.disconnect_enabled/port` ↔ `radius_disconnect_enabled/port`, `radius.profile_id` ↔ `radiusprofile_id`, `wechat.app_id` ↔ `wechat_app_id`, `wechat.app_secret` ↔ `x_wechat_app_secret`, `wechat.secret_key` ↔ `x_wechat_secret_key`, `wechat.shop_id` ↔ `wechat_shop_id`, `restricted_dns_servers` ↔ `restricted_dns_servers`.

- [ ] **Step 1: Write the failing tests**

Append to `unifi/setting_section_guest_access_test.go`:

```go
func Test_guestAccessModelToData_authProviders(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	google, d := types.ObjectValueFrom(ctx, guestAccessGoogleAttrTypes,
		settingGuestAccessGoogleModel{
			ClientID:     types.StringValue("client-id"),
			ClientSecret: types.StringValue("client-secret"),
			Domain:       types.StringValue("example.com"),
			ScopeEmail:   types.BoolValue(true),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	radius, d := types.ObjectValueFrom(ctx, guestAccessRadiusAttrTypes,
		settingGuestAccessRadiusModel{
			AuthType:          types.StringValue("chap"),
			DisconnectEnabled: types.BoolValue(true),
			DisconnectPort:    types.Int64Value(3799),
			ProfileID:         types.StringValue("rp1"),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	dns, d := types.ListValueFrom(ctx, types.StringType, []string{"1.1.1.1", "8.8.8.8"})
	if d.HasError() {
		t.Fatal(d)
	}

	m := &settingGuestAccessModel{
		Password:             types.StringValue("guest-pass"),
		PasswordEnabled:      types.BoolValue(true),
		Google:               google,
		GoogleEnabled:        types.BoolValue(true),
		Radius:               radius,
		RestrictedDNSServers: dns,
		RestrictedDNSEnabled: types.BoolValue(true),
		// All other blocks null.
		Facebook:     types.ObjectNull(guestAccessFacebookAttrTypes),
		FacebookWifi: types.ObjectNull(guestAccessFacebookWifiAttrTypes),
		Wechat:       types.ObjectNull(guestAccessWechatAttrTypes),
	}

	data := map[string]any{"unmodeled_field": "keep"}
	guestAccessModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["x_password"] != "guest-pass" || data["password_enabled"] != true {
		t.Fatalf("password fields wrong: %v", data)
	}
	if data["google_client_id"] != "client-id" ||
		data["x_google_client_secret"] != "client-secret" ||
		data["google_domain"] != "example.com" ||
		data["google_scope_email"] != true || data["google_enabled"] != true {
		t.Fatalf("google fields wrong: %v", data)
	}
	if data["radius_auth_type"] != "chap" || data["radius_disconnect_enabled"] != true ||
		data["radius_disconnect_port"] != int64(3799) || data["radiusprofile_id"] != "rp1" {
		t.Fatalf("radius fields wrong: %v", data)
	}
	servers, ok := data["restricted_dns_servers"].([]string)
	if !ok || len(servers) != 2 || data["restricted_dns_enabled"] != true {
		t.Fatalf("restricted dns fields wrong: %v", data)
	}
	if _, present := data["facebook_app_id"]; present {
		t.Fatal("null facebook block should not write keys")
	}
	if _, present := data["wechat_app_id"]; present {
		t.Fatal("null wechat block should not write keys")
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_guestAccessDataToModel_authPresence(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	data := map[string]any{
		"auth":                   "hotspot",
		"password_enabled":       true,
		"x_password":             "guest-pass",
		"google_client_id":       "client-id",
		"x_google_client_secret": "client-secret",
		"radiusprofile_id":       "rp1",
		"radius_disconnect_port": float64(3799),
	}

	m := guestAccessDataToModel(ctx, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if m.Password.ValueString() != "guest-pass" || !m.PasswordEnabled.ValueBool() {
		t.Fatalf("password = %v enabled = %v", m.Password, m.PasswordEnabled)
	}
	if m.Google.IsNull() {
		t.Fatal("google block should materialize")
	}
	var g settingGuestAccessGoogleModel
	diags.Append(m.Google.As(ctx, &g, basetypes.ObjectAsOptions{})...)
	if g.ClientID.ValueString() != "client-id" ||
		g.ClientSecret.ValueString() != "client-secret" {
		t.Fatalf("google = %+v", g)
	}
	if m.Radius.IsNull() {
		t.Fatal("radius block should materialize")
	}
	var r settingGuestAccessRadiusModel
	diags.Append(m.Radius.As(ctx, &r, basetypes.ObjectAsOptions{})...)
	if r.ProfileID.ValueString() != "rp1" || r.DisconnectPort.ValueInt64() != 3799 {
		t.Fatalf("radius = %+v", r)
	}
	if !m.Facebook.IsNull() || !m.FacebookWifi.IsNull() || !m.Wechat.IsNull() {
		t.Fatal("absent provider blocks should stay null")
	}
	if !m.RestrictedDNSServers.IsNull() {
		t.Fatal("absent restricted_dns_servers should stay null")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_guestAccess' -count=1`
Expected: compile FAILURE — `unknown field Password`, `undefined: guestAccessGoogleAttrTypes`, etc.

- [ ] **Step 3: Extend `unifi/setting_section_guest_access.go`**

3a. Add fields to `settingGuestAccessModel` (keep alphabetical-ish grouping; insert after `EcEnabled`/before `Expire` is fine as long as all tags are unique — shown here as one block to append before the closing brace):

```go
	Facebook             types.Object `tfsdk:"facebook"`
	FacebookEnabled      types.Bool   `tfsdk:"facebook_enabled"`
	FacebookWifi         types.Object `tfsdk:"facebook_wifi"`
	Google               types.Object `tfsdk:"google"`
	GoogleEnabled        types.Bool   `tfsdk:"google_enabled"`
	Password             types.String `tfsdk:"password"`
	PasswordEnabled      types.Bool   `tfsdk:"password_enabled"`
	Radius               types.Object `tfsdk:"radius"`
	RadiusEnabled        types.Bool   `tfsdk:"radius_enabled"`
	RestrictedDNSEnabled types.Bool   `tfsdk:"restricted_dns_enabled"`
	RestrictedDNSServers types.List   `tfsdk:"restricted_dns_servers"`
	Wechat               types.Object `tfsdk:"wechat"`
	WechatEnabled        types.Bool   `tfsdk:"wechat_enabled"`
```

3b. Add nested models (after `settingGuestAccessPortalCustomizationModel`):

```go
type settingGuestAccessFacebookModel struct {
	AppID      types.String `tfsdk:"app_id"`
	AppSecret  types.String `tfsdk:"app_secret"`
	ScopeEmail types.Bool   `tfsdk:"scope_email"`
}

type settingGuestAccessFacebookWifiModel struct {
	BlockHttps    types.Bool   `tfsdk:"block_https"`
	GatewayID     types.String `tfsdk:"gateway_id"`
	GatewayName   types.String `tfsdk:"gateway_name"`
	GatewaySecret types.String `tfsdk:"gateway_secret"`
}

type settingGuestAccessGoogleModel struct {
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	Domain       types.String `tfsdk:"domain"`
	ScopeEmail   types.Bool   `tfsdk:"scope_email"`
}

type settingGuestAccessRadiusModel struct {
	AuthType          types.String `tfsdk:"auth_type"`
	DisconnectEnabled types.Bool   `tfsdk:"disconnect_enabled"`
	DisconnectPort    types.Int64  `tfsdk:"disconnect_port"`
	ProfileID         types.String `tfsdk:"profile_id"`
}

type settingGuestAccessWechatModel struct {
	AppID     types.String `tfsdk:"app_id"`
	AppSecret types.String `tfsdk:"app_secret"`
	SecretKey types.String `tfsdk:"secret_key"`
	ShopID    types.String `tfsdk:"shop_id"`
}
```

3c. Add attrTypes (inside the existing `var (...)` block):

```go
	guestAccessFacebookAttrTypes = map[string]attr.Type{
		"app_id":      types.StringType,
		"app_secret":  types.StringType,
		"scope_email": types.BoolType,
	}
	guestAccessFacebookWifiAttrTypes = map[string]attr.Type{
		"block_https":    types.BoolType,
		"gateway_id":     types.StringType,
		"gateway_name":   types.StringType,
		"gateway_secret": types.StringType,
	}
	guestAccessGoogleAttrTypes = map[string]attr.Type{
		"client_id":     types.StringType,
		"client_secret": types.StringType,
		"domain":        types.StringType,
		"scope_email":   types.BoolType,
	}
	guestAccessRadiusAttrTypes = map[string]attr.Type{
		"auth_type":          types.StringType,
		"disconnect_enabled": types.BoolType,
		"disconnect_port":    types.Int64Type,
		"profile_id":         types.StringType,
	}
	guestAccessWechatAttrTypes = map[string]attr.Type{
		"app_id":     types.StringType,
		"app_secret": types.StringType,
		"secret_key": types.StringType,
		"shop_id":    types.StringType,
	}
```

and the matching entries in `guestAccessAttrTypes`:

```go
		"facebook":               types.ObjectType{AttrTypes: guestAccessFacebookAttrTypes},
		"facebook_enabled":       types.BoolType,
		"facebook_wifi":          types.ObjectType{AttrTypes: guestAccessFacebookWifiAttrTypes},
		"google":                 types.ObjectType{AttrTypes: guestAccessGoogleAttrTypes},
		"google_enabled":         types.BoolType,
		"password":               types.StringType,
		"password_enabled":       types.BoolType,
		"radius":                 types.ObjectType{AttrTypes: guestAccessRadiusAttrTypes},
		"radius_enabled":         types.BoolType,
		"restricted_dns_enabled": types.BoolType,
		"restricted_dns_servers": types.ListType{ElemType: types.StringType},
		"wechat":                 types.ObjectType{AttrTypes: guestAccessWechatAttrTypes},
		"wechat_enabled":         types.BoolType,
```

3d. Add schema attributes (into the `Attributes` map of `schemaAttribute()`; every credential is `Sensitive: true`):

```go
			"facebook": schema.SingleNestedAttribute{
				MarkdownDescription: "Facebook authentication settings (enable with `facebook_enabled`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"app_id": schema.StringAttribute{
						MarkdownDescription: "Facebook application ID.",
						Optional:            true,
						Computed:            true,
					},
					"app_secret": schema.StringAttribute{
						MarkdownDescription: "Facebook application secret.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"scope_email": schema.BoolAttribute{
						MarkdownDescription: "Request the email scope.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"facebook_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable Facebook authentication.",
				Optional:            true,
				Computed:            true,
			},
			"facebook_wifi": schema.SingleNestedAttribute{
				MarkdownDescription: "Facebook WiFi settings (used with `auth = \"facebook_wifi\"`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"block_https": schema.BoolAttribute{
						MarkdownDescription: "Block HTTPS traffic before authentication.",
						Optional:            true,
						Computed:            true,
					},
					"gateway_id": schema.StringAttribute{
						MarkdownDescription: "Facebook WiFi gateway ID.",
						Optional:            true,
						Computed:            true,
					},
					"gateway_name": schema.StringAttribute{
						MarkdownDescription: "Facebook WiFi gateway name.",
						Optional:            true,
						Computed:            true,
					},
					"gateway_secret": schema.StringAttribute{
						MarkdownDescription: "Facebook WiFi gateway secret.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
				},
			},
			"google": schema.SingleNestedAttribute{
				MarkdownDescription: "Google authentication settings (enable with `google_enabled`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"client_id": schema.StringAttribute{
						MarkdownDescription: "Google OAuth client ID.",
						Optional:            true,
						Computed:            true,
					},
					"client_secret": schema.StringAttribute{
						MarkdownDescription: "Google OAuth client secret.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"domain": schema.StringAttribute{
						MarkdownDescription: "Restrict Google authentication to a domain.",
						Optional:            true,
						Computed:            true,
					},
					"scope_email": schema.BoolAttribute{
						MarkdownDescription: "Request the email scope.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"google_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable Google authentication.",
				Optional:            true,
				Computed:            true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "Guest portal password (used with `password_enabled`).",
				Optional:            true,
				Computed:            true,
				Sensitive:           true,
			},
			"password_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable simple password authentication.",
				Optional:            true,
				Computed:            true,
			},
			"radius": schema.SingleNestedAttribute{
				MarkdownDescription: "RADIUS authentication settings (enable with `radius_enabled`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"auth_type": schema.StringAttribute{
						MarkdownDescription: "RADIUS auth type: `chap` or `mschapv2`.",
						Optional:            true,
						Computed:            true,
						Validators: []validator.String{
							stringvalidator.OneOf("chap", "mschapv2"),
						},
					},
					"disconnect_enabled": schema.BoolAttribute{
						MarkdownDescription: "Enable RADIUS disconnect messages.",
						Optional:            true,
						Computed:            true,
					},
					"disconnect_port": schema.Int64Attribute{
						MarkdownDescription: "RADIUS disconnect port.",
						Optional:            true,
						Computed:            true,
						Validators: []validator.Int64{
							int64validator.Between(1, 65535),
						},
					},
					"profile_id": schema.StringAttribute{
						MarkdownDescription: "RADIUS profile ID.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"radius_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable RADIUS authentication.",
				Optional:            true,
				Computed:            true,
			},
			"restricted_dns_enabled": schema.BoolAttribute{
				MarkdownDescription: "Restrict guest DNS to `restricted_dns_servers`.",
				Optional:            true,
				Computed:            true,
			},
			"restricted_dns_servers": schema.ListAttribute{
				MarkdownDescription: "Allowed DNS servers for guests, in priority order.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"wechat": schema.SingleNestedAttribute{
				MarkdownDescription: "WeChat authentication settings (enable with `wechat_enabled`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"app_id": schema.StringAttribute{
						MarkdownDescription: "WeChat app ID.",
						Optional:            true,
						Computed:            true,
					},
					"app_secret": schema.StringAttribute{
						MarkdownDescription: "WeChat app secret.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"secret_key": schema.StringAttribute{
						MarkdownDescription: "WeChat secret key.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"shop_id": schema.StringAttribute{
						MarkdownDescription: "WeChat shop ID.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"wechat_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable WeChat authentication.",
				Optional:            true,
				Computed:            true,
			},
```

3e. Append to `guestAccessModelToData` (before the function's closing brace):

```go
	setRawString(data, "x_password", m.Password)
	setRawBool(data, "password_enabled", m.PasswordEnabled)
	setRawBool(data, "facebook_enabled", m.FacebookEnabled)
	setRawBool(data, "google_enabled", m.GoogleEnabled)
	setRawBool(data, "radius_enabled", m.RadiusEnabled)
	setRawBool(data, "wechat_enabled", m.WechatEnabled)
	setRawBool(data, "restricted_dns_enabled", m.RestrictedDNSEnabled)
	if !m.RestrictedDNSServers.IsNull() && !m.RestrictedDNSServers.IsUnknown() {
		var servers []string
		diags.Append(m.RestrictedDNSServers.ElementsAs(ctx, &servers, false)...)
		data["restricted_dns_servers"] = servers
	}

	if !m.Facebook.IsNull() && !m.Facebook.IsUnknown() {
		var fb settingGuestAccessFacebookModel
		diags.Append(m.Facebook.As(ctx, &fb, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "facebook_app_id", fb.AppID)
		setRawString(data, "x_facebook_app_secret", fb.AppSecret)
		setRawBool(data, "facebook_scope_email", fb.ScopeEmail)
	}
	if !m.FacebookWifi.IsNull() && !m.FacebookWifi.IsUnknown() {
		var fw settingGuestAccessFacebookWifiModel
		diags.Append(m.FacebookWifi.As(ctx, &fw, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawBool(data, "facebook_wifi_block_https", fw.BlockHttps)
		setRawString(data, "facebook_wifi_gw_id", fw.GatewayID)
		setRawString(data, "facebook_wifi_gw_name", fw.GatewayName)
		setRawString(data, "x_facebook_wifi_gw_secret", fw.GatewaySecret)
	}
	if !m.Google.IsNull() && !m.Google.IsUnknown() {
		var g settingGuestAccessGoogleModel
		diags.Append(m.Google.As(ctx, &g, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "google_client_id", g.ClientID)
		setRawString(data, "x_google_client_secret", g.ClientSecret)
		setRawString(data, "google_domain", g.Domain)
		setRawBool(data, "google_scope_email", g.ScopeEmail)
	}
	if !m.Radius.IsNull() && !m.Radius.IsUnknown() {
		var r settingGuestAccessRadiusModel
		diags.Append(m.Radius.As(ctx, &r, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "radius_auth_type", r.AuthType)
		setRawBool(data, "radius_disconnect_enabled", r.DisconnectEnabled)
		setRawInt(data, "radius_disconnect_port", r.DisconnectPort)
		setRawString(data, "radiusprofile_id", r.ProfileID)
	}
	if !m.Wechat.IsNull() && !m.Wechat.IsUnknown() {
		var w settingGuestAccessWechatModel
		diags.Append(m.Wechat.As(ctx, &w, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "wechat_app_id", w.AppID)
		setRawString(data, "x_wechat_app_secret", w.AppSecret)
		setRawString(data, "x_wechat_secret_key", w.SecretKey)
		setRawString(data, "wechat_shop_id", w.ShopID)
	}
```

3f. Append to `guestAccessDataToModel` (before `return m`; also add the flat fields to the initial struct literal — `Password: rawString(data, "x_password")`, `PasswordEnabled: rawBool(data, "password_enabled")`, `FacebookEnabled: rawBool(data, "facebook_enabled")`, `GoogleEnabled: rawBool(data, "google_enabled")`, `RadiusEnabled: rawBool(data, "radius_enabled")`, `WechatEnabled: rawBool(data, "wechat_enabled")`, `RestrictedDNSEnabled: rawBool(data, "restricted_dns_enabled")`, `RestrictedDNSServers: rawStringList(data, "restricted_dns_servers")`):

```go
	m.Facebook = types.ObjectNull(guestAccessFacebookAttrTypes)
	if anyRawKey(data, "facebook_app_id", "x_facebook_app_secret", "facebook_scope_email") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessFacebookAttrTypes,
			settingGuestAccessFacebookModel{
				AppID:      rawString(data, "facebook_app_id"),
				AppSecret:  rawString(data, "x_facebook_app_secret"),
				ScopeEmail: rawBool(data, "facebook_scope_email"),
			})
		diags.Append(d...)
		m.Facebook = obj
	}

	m.FacebookWifi = types.ObjectNull(guestAccessFacebookWifiAttrTypes)
	if anyRawKey(data, "facebook_wifi_gw_id", "facebook_wifi_gw_name",
		"x_facebook_wifi_gw_secret", "facebook_wifi_block_https") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessFacebookWifiAttrTypes,
			settingGuestAccessFacebookWifiModel{
				BlockHttps:    rawBool(data, "facebook_wifi_block_https"),
				GatewayID:     rawString(data, "facebook_wifi_gw_id"),
				GatewayName:   rawString(data, "facebook_wifi_gw_name"),
				GatewaySecret: rawString(data, "x_facebook_wifi_gw_secret"),
			})
		diags.Append(d...)
		m.FacebookWifi = obj
	}

	m.Google = types.ObjectNull(guestAccessGoogleAttrTypes)
	if anyRawKey(data, "google_client_id", "x_google_client_secret",
		"google_domain", "google_scope_email") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessGoogleAttrTypes,
			settingGuestAccessGoogleModel{
				ClientID:     rawString(data, "google_client_id"),
				ClientSecret: rawString(data, "x_google_client_secret"),
				Domain:       rawString(data, "google_domain"),
				ScopeEmail:   rawBool(data, "google_scope_email"),
			})
		diags.Append(d...)
		m.Google = obj
	}

	m.Radius = types.ObjectNull(guestAccessRadiusAttrTypes)
	if anyRawKey(data, "radius_auth_type", "radiusprofile_id",
		"radius_disconnect_enabled", "radius_disconnect_port") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessRadiusAttrTypes,
			settingGuestAccessRadiusModel{
				AuthType:          rawString(data, "radius_auth_type"),
				DisconnectEnabled: rawBool(data, "radius_disconnect_enabled"),
				DisconnectPort:    rawInt(data, "radius_disconnect_port"),
				ProfileID:         rawString(data, "radiusprofile_id"),
			})
		diags.Append(d...)
		m.Radius = obj
	}

	m.Wechat = types.ObjectNull(guestAccessWechatAttrTypes)
	if anyRawKey(data, "wechat_app_id", "x_wechat_app_secret",
		"x_wechat_secret_key", "wechat_shop_id") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessWechatAttrTypes,
			settingGuestAccessWechatModel{
				AppID:     rawString(data, "wechat_app_id"),
				AppSecret: rawString(data, "x_wechat_app_secret"),
				SecretKey: rawString(data, "x_wechat_secret_key"),
				ShopID:    rawString(data, "wechat_shop_id"),
			})
		diags.Append(d...)
		m.Wechat = obj
	}
```

3g. **Update the two Task 3 tests** that construct a full `settingGuestAccessModel` via `types.ObjectValueFrom` (`Test_guestAccessModelToData_core`): add the new fields as nulls —

```go
		Facebook:             types.ObjectNull(guestAccessFacebookAttrTypes),
		FacebookEnabled:      types.BoolNull(),
		FacebookWifi:         types.ObjectNull(guestAccessFacebookWifiAttrTypes),
		Google:               types.ObjectNull(guestAccessGoogleAttrTypes),
		GoogleEnabled:        types.BoolNull(),
		Password:             types.StringNull(),
		PasswordEnabled:      types.BoolNull(),
		Radius:               types.ObjectNull(guestAccessRadiusAttrTypes),
		RadiusEnabled:        types.BoolNull(),
		RestrictedDNSEnabled: types.BoolNull(),
		RestrictedDNSServers: types.ListNull(types.StringType),
		Wechat:               types.ObjectNull(guestAccessWechatAttrTypes),
		WechatEnabled:        types.BoolNull(),
```

(The Task 4 test above constructs the model struct directly, not via ObjectValueFrom, so it does not need the Task 5 fields — but after Task 5 lands, its `guestAccessAttrTypes` map grows again; constructing models directly with `&settingGuestAccessModel{...}` keeps these tests immune to later field additions, which is why Task 4/5 tests do it that way.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_guestAccess|Test_rawValueHelpers|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 5: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_guest_access.go unifi/setting_section_guest_access_test.go
git commit -m "feat(setting): guest_access authentication providers"
```

---

### Task 5: `guest_access` — payment gateways

Adds `payment_enabled`, `payment_gateway`, and the six gateway blocks. Names and Sensitive choices per filipowm (plus the two `authorize` credentials filipowm forgot to mark).

**Files:**
- Modify: `unifi/setting_section_guest_access.go`
- Modify: `unifi/setting_section_guest_access_test.go`

**Interfaces:**
- Produces: `settingGuestAccessAuthorizeModel`, `settingGuestAccessIPpayModel`, `settingGuestAccessMerchantWarriorModel`, `settingGuestAccessPaypalModel`, `settingGuestAccessQuickpayModel`, `settingGuestAccessStripeModel` and attrTypes maps (`guestAccessAuthorizeAttrTypes`, `guestAccessIPpayAttrTypes`, `guestAccessMerchantWarriorAttrTypes`, `guestAccessPaypalAttrTypes`, `guestAccessQuickpayAttrTypes`, `guestAccessStripeAttrTypes`).
- JSON key map: `payment_gateway` ↔ `gateway`; `authorize.login_id` ↔ `x_authorize_loginid`, `authorize.transaction_key` ↔ `x_authorize_transactionkey`, `authorize.use_sandbox` ↔ `authorize_use_sandbox`; `ippay.terminal_id` ↔ `x_ippay_terminalid`, `ippay.use_sandbox` ↔ `ippay_use_sandbox`; `merchant_warrior.api_key/api_passphrase/merchant_uuid` ↔ `x_merchantwarrior_apikey/apipassphrase/merchantuuid`, `merchant_warrior.use_sandbox` ↔ `merchantwarrior_use_sandbox`; `paypal.username/password/signature` ↔ `x_paypal_username/password/signature`, `paypal.use_sandbox` ↔ `paypal_use_sandbox`; `quickpay.agreement_id/api_key/merchant_id` ↔ `x_quickpay_agreementid/apikey/merchantid`, `quickpay.use_sandbox` ↔ `quickpay_testmode`; `stripe.api_key` ↔ `x_stripe_api_key`.
- Unlike filipowm, the overlay writes every configured gateway block (no switch on `payment_gateway`) — the engine's only-write-what-is-set rule applies uniformly; the controller uses whichever `gateway` selects.

- [ ] **Step 1: Write the failing tests**

Append to `unifi/setting_section_guest_access_test.go`:

```go
func Test_guestAccessModelToData_paymentGateways(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	paypal, d := types.ObjectValueFrom(ctx, guestAccessPaypalAttrTypes,
		settingGuestAccessPaypalModel{
			Username:   types.StringValue("merchant@example.com"),
			Password:   types.StringValue("paypal-pass"),
			Signature:  types.StringValue("paypal-sig"),
			UseSandbox: types.BoolValue(true),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	quickpay, d := types.ObjectValueFrom(ctx, guestAccessQuickpayAttrTypes,
		settingGuestAccessQuickpayModel{
			AgreementID: types.StringValue("agreement"),
			APIKey:      types.StringValue("qp-key"),
			MerchantID:  types.StringValue("merchant"),
			UseSandbox:  types.BoolValue(true),
		})
	if d.HasError() {
		t.Fatal(d)
	}

	m := &settingGuestAccessModel{
		PaymentEnabled: types.BoolValue(true),
		PaymentGateway: types.StringValue("paypal"),
		Paypal:         paypal,
		Quickpay:       quickpay,
		Authorize:      types.ObjectNull(guestAccessAuthorizeAttrTypes),
		IPpay:          types.ObjectNull(guestAccessIPpayAttrTypes),
		MerchantWarrior: types.ObjectNull(
			guestAccessMerchantWarriorAttrTypes),
		Stripe: types.ObjectNull(guestAccessStripeAttrTypes),
	}

	data := map[string]any{"unmodeled_field": "keep"}
	guestAccessModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["payment_enabled"] != true || data["gateway"] != "paypal" {
		t.Fatalf("payment fields wrong: %v", data)
	}
	if data["x_paypal_username"] != "merchant@example.com" ||
		data["x_paypal_password"] != "paypal-pass" ||
		data["x_paypal_signature"] != "paypal-sig" ||
		data["paypal_use_sandbox"] != true {
		t.Fatalf("paypal fields wrong: %v", data)
	}
	if data["x_quickpay_agreementid"] != "agreement" ||
		data["x_quickpay_apikey"] != "qp-key" ||
		data["x_quickpay_merchantid"] != "merchant" ||
		data["quickpay_testmode"] != true {
		t.Fatalf("quickpay fields wrong: %v", data)
	}
	if _, present := data["x_stripe_api_key"]; present {
		t.Fatal("null stripe block should not write keys")
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_guestAccessDataToModel_paymentPresence(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	data := map[string]any{
		"payment_enabled":  true,
		"gateway":          "stripe",
		"x_stripe_api_key": "stripe-key",
	}

	m := guestAccessDataToModel(ctx, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if !m.PaymentEnabled.ValueBool() || m.PaymentGateway.ValueString() != "stripe" {
		t.Fatalf("payment = %v gateway = %v", m.PaymentEnabled, m.PaymentGateway)
	}
	if m.Stripe.IsNull() {
		t.Fatal("stripe block should materialize")
	}
	var s settingGuestAccessStripeModel
	diags.Append(m.Stripe.As(ctx, &s, basetypes.ObjectAsOptions{})...)
	if s.APIKey.ValueString() != "stripe-key" {
		t.Fatalf("stripe = %+v", s)
	}
	if !m.Paypal.IsNull() || !m.Quickpay.IsNull() || !m.Authorize.IsNull() ||
		!m.IPpay.IsNull() || !m.MerchantWarrior.IsNull() {
		t.Fatal("absent gateway blocks should stay null")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_guestAccess' -count=1`
Expected: compile FAILURE — `unknown field PaymentEnabled` etc.

- [ ] **Step 3: Extend `unifi/setting_section_guest_access.go`**

3a. Model fields (append to `settingGuestAccessModel`):

```go
	Authorize       types.Object `tfsdk:"authorize"`
	IPpay           types.Object `tfsdk:"ippay"`
	MerchantWarrior types.Object `tfsdk:"merchant_warrior"`
	PaymentEnabled  types.Bool   `tfsdk:"payment_enabled"`
	PaymentGateway  types.String `tfsdk:"payment_gateway"`
	Paypal          types.Object `tfsdk:"paypal"`
	Quickpay        types.Object `tfsdk:"quickpay"`
	Stripe          types.Object `tfsdk:"stripe"`
```

3b. Nested models:

```go
type settingGuestAccessAuthorizeModel struct {
	LoginID        types.String `tfsdk:"login_id"`
	TransactionKey types.String `tfsdk:"transaction_key"`
	UseSandbox     types.Bool   `tfsdk:"use_sandbox"`
}

type settingGuestAccessIPpayModel struct {
	TerminalID types.String `tfsdk:"terminal_id"`
	UseSandbox types.Bool   `tfsdk:"use_sandbox"`
}

type settingGuestAccessMerchantWarriorModel struct {
	APIKey        types.String `tfsdk:"api_key"`
	APIPassphrase types.String `tfsdk:"api_passphrase"`
	MerchantUUID  types.String `tfsdk:"merchant_uuid"`
	UseSandbox    types.Bool   `tfsdk:"use_sandbox"`
}

type settingGuestAccessPaypalModel struct {
	Password   types.String `tfsdk:"password"`
	Signature  types.String `tfsdk:"signature"`
	UseSandbox types.Bool   `tfsdk:"use_sandbox"`
	Username   types.String `tfsdk:"username"`
}

type settingGuestAccessQuickpayModel struct {
	AgreementID types.String `tfsdk:"agreement_id"`
	APIKey      types.String `tfsdk:"api_key"`
	MerchantID  types.String `tfsdk:"merchant_id"`
	UseSandbox  types.Bool   `tfsdk:"use_sandbox"`
}

type settingGuestAccessStripeModel struct {
	APIKey types.String `tfsdk:"api_key"`
}
```

3c. attrTypes (inside the `var (...)` block):

```go
	guestAccessAuthorizeAttrTypes = map[string]attr.Type{
		"login_id":        types.StringType,
		"transaction_key": types.StringType,
		"use_sandbox":     types.BoolType,
	}
	guestAccessIPpayAttrTypes = map[string]attr.Type{
		"terminal_id": types.StringType,
		"use_sandbox": types.BoolType,
	}
	guestAccessMerchantWarriorAttrTypes = map[string]attr.Type{
		"api_key":        types.StringType,
		"api_passphrase": types.StringType,
		"merchant_uuid":  types.StringType,
		"use_sandbox":    types.BoolType,
	}
	guestAccessPaypalAttrTypes = map[string]attr.Type{
		"password":    types.StringType,
		"signature":   types.StringType,
		"use_sandbox": types.BoolType,
		"username":    types.StringType,
	}
	guestAccessQuickpayAttrTypes = map[string]attr.Type{
		"agreement_id": types.StringType,
		"api_key":      types.StringType,
		"merchant_id":  types.StringType,
		"use_sandbox":  types.BoolType,
	}
	guestAccessStripeAttrTypes = map[string]attr.Type{
		"api_key": types.StringType,
	}
```

and in `guestAccessAttrTypes`:

```go
		"authorize":        types.ObjectType{AttrTypes: guestAccessAuthorizeAttrTypes},
		"ippay":            types.ObjectType{AttrTypes: guestAccessIPpayAttrTypes},
		"merchant_warrior": types.ObjectType{AttrTypes: guestAccessMerchantWarriorAttrTypes},
		"payment_enabled":  types.BoolType,
		"payment_gateway":  types.StringType,
		"paypal":           types.ObjectType{AttrTypes: guestAccessPaypalAttrTypes},
		"quickpay":         types.ObjectType{AttrTypes: guestAccessQuickpayAttrTypes},
		"stripe":           types.ObjectType{AttrTypes: guestAccessStripeAttrTypes},
```

3d. Schema attributes (into `schemaAttribute()`'s `Attributes` map; every credential `Sensitive: true`):

```go
			"authorize": schema.SingleNestedAttribute{
				MarkdownDescription: "Authorize.net payment settings.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"login_id": schema.StringAttribute{
						MarkdownDescription: "Authorize.net login ID.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"transaction_key": schema.StringAttribute{
						MarkdownDescription: "Authorize.net transaction key.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"use_sandbox": schema.BoolAttribute{
						MarkdownDescription: "Use sandbox mode.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"ippay": schema.SingleNestedAttribute{
				MarkdownDescription: "IPpay payment settings.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"terminal_id": schema.StringAttribute{
						MarkdownDescription: "IPpay terminal ID.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"use_sandbox": schema.BoolAttribute{
						MarkdownDescription: "Use sandbox mode.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"merchant_warrior": schema.SingleNestedAttribute{
				MarkdownDescription: "MerchantWarrior payment settings.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"api_key": schema.StringAttribute{
						MarkdownDescription: "MerchantWarrior API key.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"api_passphrase": schema.StringAttribute{
						MarkdownDescription: "MerchantWarrior API passphrase.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"merchant_uuid": schema.StringAttribute{
						MarkdownDescription: "MerchantWarrior merchant UUID.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"use_sandbox": schema.BoolAttribute{
						MarkdownDescription: "Use sandbox mode.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"payment_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable paid guest access (requires `auth = \"hotspot\"`).",
				Optional:            true,
				Computed:            true,
			},
			"payment_gateway": schema.StringAttribute{
				MarkdownDescription: "Payment gateway: `paypal`, `stripe`, `authorize`, `quickpay`, `merchantwarrior`, or `ippay`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("paypal", "stripe", "authorize", "quickpay", "merchantwarrior", "ippay"),
				},
			},
			"paypal": schema.SingleNestedAttribute{
				MarkdownDescription: "PayPal payment settings.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"password": schema.StringAttribute{
						MarkdownDescription: "PayPal API password.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"signature": schema.StringAttribute{
						MarkdownDescription: "PayPal API signature.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"use_sandbox": schema.BoolAttribute{
						MarkdownDescription: "Use sandbox mode.",
						Optional:            true,
						Computed:            true,
					},
					"username": schema.StringAttribute{
						MarkdownDescription: "PayPal API username.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
				},
			},
			"quickpay": schema.SingleNestedAttribute{
				MarkdownDescription: "QuickPay payment settings.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"agreement_id": schema.StringAttribute{
						MarkdownDescription: "QuickPay agreement ID.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"api_key": schema.StringAttribute{
						MarkdownDescription: "QuickPay API key.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"merchant_id": schema.StringAttribute{
						MarkdownDescription: "QuickPay merchant ID.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"use_sandbox": schema.BoolAttribute{
						MarkdownDescription: "Use test mode (`quickpay_testmode`).",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"stripe": schema.SingleNestedAttribute{
				MarkdownDescription: "Stripe payment settings.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"api_key": schema.StringAttribute{
						MarkdownDescription: "Stripe API key.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
				},
			},
```

3e. Append to `guestAccessModelToData`:

```go
	setRawBool(data, "payment_enabled", m.PaymentEnabled)
	setRawString(data, "gateway", m.PaymentGateway)

	if !m.Authorize.IsNull() && !m.Authorize.IsUnknown() {
		var a settingGuestAccessAuthorizeModel
		diags.Append(m.Authorize.As(ctx, &a, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "x_authorize_loginid", a.LoginID)
		setRawString(data, "x_authorize_transactionkey", a.TransactionKey)
		setRawBool(data, "authorize_use_sandbox", a.UseSandbox)
	}
	if !m.IPpay.IsNull() && !m.IPpay.IsUnknown() {
		var p settingGuestAccessIPpayModel
		diags.Append(m.IPpay.As(ctx, &p, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "x_ippay_terminalid", p.TerminalID)
		setRawBool(data, "ippay_use_sandbox", p.UseSandbox)
	}
	if !m.MerchantWarrior.IsNull() && !m.MerchantWarrior.IsUnknown() {
		var mw settingGuestAccessMerchantWarriorModel
		diags.Append(m.MerchantWarrior.As(ctx, &mw, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "x_merchantwarrior_apikey", mw.APIKey)
		setRawString(data, "x_merchantwarrior_apipassphrase", mw.APIPassphrase)
		setRawString(data, "x_merchantwarrior_merchantuuid", mw.MerchantUUID)
		setRawBool(data, "merchantwarrior_use_sandbox", mw.UseSandbox)
	}
	if !m.Paypal.IsNull() && !m.Paypal.IsUnknown() {
		var p settingGuestAccessPaypalModel
		diags.Append(m.Paypal.As(ctx, &p, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "x_paypal_username", p.Username)
		setRawString(data, "x_paypal_password", p.Password)
		setRawString(data, "x_paypal_signature", p.Signature)
		setRawBool(data, "paypal_use_sandbox", p.UseSandbox)
	}
	if !m.Quickpay.IsNull() && !m.Quickpay.IsUnknown() {
		var q settingGuestAccessQuickpayModel
		diags.Append(m.Quickpay.As(ctx, &q, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "x_quickpay_agreementid", q.AgreementID)
		setRawString(data, "x_quickpay_apikey", q.APIKey)
		setRawString(data, "x_quickpay_merchantid", q.MerchantID)
		setRawBool(data, "quickpay_testmode", q.UseSandbox)
	}
	if !m.Stripe.IsNull() && !m.Stripe.IsUnknown() {
		var s settingGuestAccessStripeModel
		diags.Append(m.Stripe.As(ctx, &s, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "x_stripe_api_key", s.APIKey)
	}
```

3f. Extend `guestAccessDataToModel` — add to the initial struct literal:

```go
		PaymentEnabled: rawBool(data, "payment_enabled"),
		PaymentGateway: rawString(data, "gateway"),
```

and append before `return m`:

```go
	m.Authorize = types.ObjectNull(guestAccessAuthorizeAttrTypes)
	if anyRawKey(data, "x_authorize_loginid", "x_authorize_transactionkey", "authorize_use_sandbox") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessAuthorizeAttrTypes,
			settingGuestAccessAuthorizeModel{
				LoginID:        rawString(data, "x_authorize_loginid"),
				TransactionKey: rawString(data, "x_authorize_transactionkey"),
				UseSandbox:     rawBool(data, "authorize_use_sandbox"),
			})
		diags.Append(d...)
		m.Authorize = obj
	}

	m.IPpay = types.ObjectNull(guestAccessIPpayAttrTypes)
	if anyRawKey(data, "x_ippay_terminalid", "ippay_use_sandbox") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessIPpayAttrTypes,
			settingGuestAccessIPpayModel{
				TerminalID: rawString(data, "x_ippay_terminalid"),
				UseSandbox: rawBool(data, "ippay_use_sandbox"),
			})
		diags.Append(d...)
		m.IPpay = obj
	}

	m.MerchantWarrior = types.ObjectNull(guestAccessMerchantWarriorAttrTypes)
	if anyRawKey(data, "x_merchantwarrior_apikey", "x_merchantwarrior_apipassphrase",
		"x_merchantwarrior_merchantuuid", "merchantwarrior_use_sandbox") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessMerchantWarriorAttrTypes,
			settingGuestAccessMerchantWarriorModel{
				APIKey:        rawString(data, "x_merchantwarrior_apikey"),
				APIPassphrase: rawString(data, "x_merchantwarrior_apipassphrase"),
				MerchantUUID:  rawString(data, "x_merchantwarrior_merchantuuid"),
				UseSandbox:    rawBool(data, "merchantwarrior_use_sandbox"),
			})
		diags.Append(d...)
		m.MerchantWarrior = obj
	}

	m.Paypal = types.ObjectNull(guestAccessPaypalAttrTypes)
	if anyRawKey(data, "x_paypal_username", "x_paypal_password",
		"x_paypal_signature", "paypal_use_sandbox") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessPaypalAttrTypes,
			settingGuestAccessPaypalModel{
				Password:   rawString(data, "x_paypal_password"),
				Signature:  rawString(data, "x_paypal_signature"),
				UseSandbox: rawBool(data, "paypal_use_sandbox"),
				Username:   rawString(data, "x_paypal_username"),
			})
		diags.Append(d...)
		m.Paypal = obj
	}

	m.Quickpay = types.ObjectNull(guestAccessQuickpayAttrTypes)
	if anyRawKey(data, "x_quickpay_agreementid", "x_quickpay_apikey",
		"x_quickpay_merchantid", "quickpay_testmode") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessQuickpayAttrTypes,
			settingGuestAccessQuickpayModel{
				AgreementID: rawString(data, "x_quickpay_agreementid"),
				APIKey:      rawString(data, "x_quickpay_apikey"),
				MerchantID:  rawString(data, "x_quickpay_merchantid"),
				UseSandbox:  rawBool(data, "quickpay_testmode"),
			})
		diags.Append(d...)
		m.Quickpay = obj
	}

	m.Stripe = types.ObjectNull(guestAccessStripeAttrTypes)
	if anyRawKey(data, "x_stripe_api_key") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessStripeAttrTypes,
			settingGuestAccessStripeModel{
				APIKey: rawString(data, "x_stripe_api_key"),
			})
		diags.Append(d...)
		m.Stripe = obj
	}
```

3g. Update `Test_guestAccessModelToData_core` (the ObjectValueFrom construction) with the new fields as nulls:

```go
		Authorize:       types.ObjectNull(guestAccessAuthorizeAttrTypes),
		IPpay:           types.ObjectNull(guestAccessIPpayAttrTypes),
		MerchantWarrior: types.ObjectNull(guestAccessMerchantWarriorAttrTypes),
		PaymentEnabled:  types.BoolNull(),
		PaymentGateway:  types.StringNull(),
		Paypal:          types.ObjectNull(guestAccessPaypalAttrTypes),
		Quickpay:        types.ObjectNull(guestAccessQuickpayAttrTypes),
		Stripe:          types.ObjectNull(guestAccessStripeAttrTypes),
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_guestAccess|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 5: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_guest_access.go unifi/setting_section_guest_access_test.go
git commit -m "feat(setting): guest_access payment gateways"
```

(Body should note: all gateway credentials Sensitive, including both `authorize` credentials that filipowm never marks and `google.client_secret`, which filipowm marks only in a commented-out line.)

---

> **PRECONDITION for Task 6:** `provider_capabilities` needs the typed struct `settings.ProviderCapabilities` that only exists after the go-unifi PR 0 lands (plan: `docs/superpowers/plans/2026-07-10-go-unifi-pr0.md`, Task 1). Before starting Task 6, verify:
>
> ```bash
> go doc github.com/ubiquiti-community/go-unifi/unifi/settings ProviderCapabilities
> ```
>
> - If it resolves: proceed. The expected shape (pinned by the go-unifi PR 0 plan, Task 1) is `type ProviderCapabilities struct { BaseSetting; Download int64 `+"`json:\"download,omitempty\"`"+`; Upload int64 `+"`json:\"upload,omitempty\"`"+` }` — plain scalars, default JSON (un)marshalling, no custom `UnmarshalJSON`. If the generator/hand-written struct that landed differs (different field names, pointer types, a custom `UnmarshalJSON`), adjust `providerCapabilitiesSettingToModel` and its test to match — nothing else in this task changes.
> - If it does not resolve: either bump `go.mod` to the go-unifi commit/release containing PR 0, or add a development-only directive `replace github.com/ubiquiti-community/go-unifi => /Users/jamesb/projects/go-unifi` (MUST be dropped before release — note it in the final report), then `go mod tidy`.
> - If go-unifi PR 0 has not landed at all: **skip Task 6 entirely** and continue at Task 7 with `dashboard`, `radio_ai`, and `guest_access` only. Do not stub, guess, or hand-write the struct in this repo — that lives in go-unifi, not here. Report the skip explicitly; it is not a failure.

### Task 6 (GATED): `provider_capabilities` section

The advertised ISP download/upload capacity (kbps) the controller uses for utilization displays and Smart Queues sizing. Two scalar fields — the smallest section in this plan.

**Files:**
- Create: `unifi/setting_section_provider_capabilities.go`
- Create: `unifi/setting_section_provider_capabilities_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settings.ProviderCapabilities` (`Download int64 // json:"download,omitempty"`, `Upload int64 // json:"upload,omitempty"`), `ui.GetSetting[T]`.
- Produces: `settingProviderCapabilitiesModel`, `providerCapabilitiesAttrTypes`, `providerCapabilitiesSection`, `providerCapabilitiesModelToData(m, data)`, `providerCapabilitiesSettingToModel(s)`.
- Live shape confirmed against the captured payload (`udm-settings.json`, `provider_capabilities` section): only `download`, `upload`, plus the standard `_id`/`key`/`site_id` envelope — no unmodeled fields observed, so no dedicated raw-preservation test is needed here (the raw-merge overlay still preserves anything unmodeled by construction, same as every other section; there is simply no known field to assert on).

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_provider_capabilities_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_providerCapabilitiesModelToData(t *testing.T) {
	var diags diag.Diagnostics
	m := &settingProviderCapabilitiesModel{
		Download: types.Int64Value(1000000),
		Upload:   types.Int64Null(),
	}
	// Raw fields go-unifi does not model (there are none known today, but the
	// overlay must still preserve whatever it is given) must round-trip.
	data := map[string]any{"unmodeled_field": "keep", "upload": float64(500000)}

	providerCapabilitiesModelToData(m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["download"] != int64(1000000) {
		t.Fatalf("download = %v", data["download"])
	}
	if data["upload"] != float64(500000) {
		t.Fatal("null upload overwrote remote value")
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_providerCapabilitiesSettingToModel(t *testing.T) {
	m := providerCapabilitiesSettingToModel(&settings.ProviderCapabilities{
		Download: 1000000,
		Upload:   1000000,
	})
	if m.Download.ValueInt64() != 1000000 || m.Upload.ValueInt64() != 1000000 {
		t.Fatalf("download/upload = %v/%v", m.Download, m.Upload)
	}

	empty := providerCapabilitiesSettingToModel(&settings.ProviderCapabilities{})
	if !empty.Download.IsNull() || !empty.Upload.IsNull() {
		t.Fatalf("zero-value download/upload should map to null, got %v/%v", empty.Download, empty.Upload)
	}
}

func Test_settingResource_Schema_providerCapabilities(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["provider_capabilities"]; !ok {
		t.Fatal("schema is missing the provider_capabilities section attribute")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_providerCapabilities|Test_settingResource_Schema_providerCapabilities' -count=1`
Expected: compile FAILURE — `undefined: settingProviderCapabilitiesModel` etc. (or, if the precondition check failed and Task 6 was skipped, this step is not run at all.)

- [ ] **Step 3: Create `unifi/setting_section_provider_capabilities.go`**

```go
package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// settingProviderCapabilitiesModel is the nested provider_capabilities
// block: the advertised ISP download/upload capacity (kbps) the controller
// uses for WAN utilization displays and Smart Queues sizing. Not exposed by
// any prior-art provider (filipowm has no equivalent); names follow the
// go-unifi/controller JSON keys directly.
type settingProviderCapabilitiesModel struct {
	Download types.Int64 `tfsdk:"download"`
	Upload   types.Int64 `tfsdk:"upload"`
}

var providerCapabilitiesAttrTypes = map[string]attr.Type{
	"download": types.Int64Type,
	"upload":   types.Int64Type,
}

type providerCapabilitiesSection struct{}

func (providerCapabilitiesSection) key() string { return "provider_capabilities" }

func (providerCapabilitiesSection) attrTypes() map[string]attr.Type {
	return providerCapabilitiesAttrTypes
}

func (providerCapabilitiesSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "ISP capability settings: the advertised download/upload capacity of the " +
			"internet connection, in kbps. Used by the controller for WAN utilization displays and " +
			"Smart Queues sizing; not otherwise enforced.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"download": schema.Int64Attribute{
				MarkdownDescription: "Advertised download capacity in kbps (e.g. `1000000` for 1 Gbps).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"upload": schema.Int64Attribute{
				MarkdownDescription: "Advertised upload capacity in kbps.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (providerCapabilitiesSection) get(m *settingResourceModel) types.Object {
	return m.ProviderCapabilities
}

func (providerCapabilitiesSection) set(m *settingResourceModel, obj types.Object) {
	m.ProviderCapabilities = obj
}

func (providerCapabilitiesSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingProviderCapabilitiesModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	providerCapabilitiesModelToData(&m, data, &diags)
	return diags
}

// providerCapabilitiesModelToData writes only the user-set fields into the
// raw section document; unset fields keep their remote values.
func providerCapabilitiesModelToData(
	m *settingProviderCapabilitiesModel,
	data map[string]any,
	_ *diag.Diagnostics,
) {
	if !m.Download.IsNull() && !m.Download.IsUnknown() {
		data["download"] = m.Download.ValueInt64()
	}
	if !m.Upload.IsNull() && !m.Upload.IsUnknown() {
		data["upload"] = m.Upload.ValueInt64()
	}
}

func (providerCapabilitiesSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.ProviderCapabilities](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(providerCapabilitiesAttrTypes), diags
		}
		diags.AddError("Error Reading Provider Capabilities Setting", err.Error())
		return types.ObjectNull(providerCapabilitiesAttrTypes), diags
	}
	model := providerCapabilitiesSettingToModel(setting)
	return types.ObjectValueFrom(ctx, providerCapabilitiesAttrTypes, model)
}

func providerCapabilitiesSettingToModel(s *settings.ProviderCapabilities) settingProviderCapabilitiesModel {
	m := settingProviderCapabilitiesModel{
		Download: types.Int64Null(),
		Upload:   types.Int64Null(),
	}
	if s.Download != 0 {
		m.Download = types.Int64Value(s.Download)
	}
	if s.Upload != 0 {
		m.Upload = types.Int64Value(s.Upload)
	}
	return m
}
```

Note: `providerCapabilitiesModelToData` takes `*diag.Diagnostics` (unused) purely so its call signature matches every other section's `<name>ModelToData(m, data, diags)` shape used elsewhere in this file and PRs 1–3 — consistency for readers scanning across sections beats trimming an unused parameter. `go vet`/linters do not flag unused function parameters (only unused locals/imports), so this is safe.

- [ ] **Step 4: Register the section**

`unifi/setting_section.go` registry (after `guestAccessSection{}`):

```go
	providerCapabilitiesSection{},
```

`unifi/setting_resource.go` model (after `GuestAccess`):

```go
	ProviderCapabilities types.Object `tfsdk:"provider_capabilities"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_providerCapabilities|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_provider_capabilities.go unifi/setting_section_provider_capabilities_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add provider_capabilities section"
```

(Body should note: gated on go-unifi PR 0's `settings.ProviderCapabilities`; if a development-only `replace` directive was needed to unblock this task, call it out explicitly as a pre-release removal item, matching PR 3's precedent.)

---

### Task 7: Acceptance tests against the docker demo controller

**Files:**
- Modify: `unifi/setting_section_dashboard_test.go`, `unifi/setting_section_radio_ai_test.go`, `unifi/setting_section_guest_access_test.go` (append acceptance tests)
- Modify (only if Task 6 was implemented): `unifi/setting_section_provider_capabilities_test.go`

**Interfaces:**
- Consumes: `preCheck(t)`, `testAccProtoV6ProviderFactories` (see `unifi/provider_test.go`), `resource.Test` from terraform-plugin-testing.
- Sensitive attributes are still checkable with `TestCheckResourceAttr` (sensitivity affects display, not state); all secret-shaped values in these tests are synthetic `tfacc-…` strings — never values from the captured live payload.

- [ ] **Step 1: Append acceptance tests**

To `unifi/setting_section_dashboard_test.go` (add import `"github.com/hashicorp/terraform-plugin-testing/helper/resource"`):

```go
func TestAccSettingResource_dashboard(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_dashboard("auto"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "dashboard.layout_preference", "auto",
				),
			},
			{
				Config: testAccSettingConfig_dashboard("manual"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "dashboard.layout_preference", "manual",
				),
			},
		},
	})
}

func testAccSettingConfig_dashboard(pref string) string {
	return `
resource "unifi_setting" "test" {
  dashboard = {
    layout_preference = "` + pref + `"
  }
}
`
}
```

To `unifi/setting_section_radio_ai_test.go` (this section is controller co-managed — the acceptance test pins only `enabled`/`setting_preference`, per the schema description's own guidance, rather than fighting the controller's channel plan):

```go
func TestAccSettingResource_radioAi(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_radioAi(true, "manual"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "radio_ai.enabled", "true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "radio_ai.setting_preference", "manual",
					),
				),
			},
			{
				Config: testAccSettingConfig_radioAi(true, "auto"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "radio_ai.setting_preference", "auto",
				),
			},
		},
	})
}

func testAccSettingConfig_radioAi(enabled bool, pref string) string {
	return fmt.Sprintf(`
resource "unifi_setting" "test" {
  radio_ai = {
    enabled            = %t
    setting_preference = %q
  }
}
`, enabled, pref)
}
```

(Add `"fmt"` to that file's imports.)

To `unifi/setting_section_guest_access_test.go`:

```go
func TestAccSettingResource_guestAccess(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_guestAccess("hotspot", "tfacc-guest-pass"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "guest_access.auth", "hotspot",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "guest_access.password", "tfacc-guest-pass",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "guest_access.password_enabled", "true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "guest_access.portal_customization.title", "Guest WiFi",
					),
				),
			},
			{
				Config: testAccSettingConfig_guestAccess("none", "tfacc-guest-pass2"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "guest_access.auth", "none",
				),
			},
		},
	})
}

func testAccSettingConfig_guestAccess(auth, password string) string {
	return fmt.Sprintf(`
resource "unifi_setting" "test" {
  guest_access = {
    auth              = %q
    password          = %q
    password_enabled  = true
    portal_enabled    = true

    portal_customization = {
      customized = true
      title      = "Guest WiFi"
      bg_color   = "#005ED9"
    }
  }
}
`, auth, password)
}
```

(`fmt` is already imported by this file from Task 1's helpers, if not, add it.)

To `unifi/setting_section_provider_capabilities_test.go` (only if Task 6 was implemented):

```go
func TestAccSettingResource_providerCapabilities(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_setting" "test" {
  provider_capabilities = {
    download = 1000000
    upload   = 500000
  }
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "provider_capabilities.download", "1000000",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "provider_capabilities.upload", "500000",
					),
				),
			},
		},
	})
}
```

- [ ] **Step 2: Run**

`TestMain` in `unifi/provider_test.go` boots the docker-compose demo controller itself via testcontainers when `TF_ACC=1` is set — no manual `docker compose up`. Check `preCheck` for the exact env vars it expects, then:

Run: `TF_ACC=1 go test ./unifi/ -run 'TestAccSettingResource_(dashboard|radioAi|guestAccess|providerCapabilities)' -v -count=1 -timeout 15m` (with the `UNIFI_API`/`UNIFI_USERNAME`/`UNIFI_PASSWORD`/`UNIFI_INSECURE` values `preCheck` expects if running with `UNIFI_SKIP_CONTAINER` against a pre-set controller — read them from `unifi/provider_test.go` / CI workflow, do not guess).
Expected: PASS, or documented skips per the contingency below.

**Contingency:** if the demo controller rejects a section (e.g. `api.err.InvalidPayload`/`api.err.InvalidObject`, or a 400 because the simulated controller lacks the feature — `guest_access` payment-gateway fields and `provider_capabilities` are the most likely to be refused by a lightweight demo image since they are typically UDM/gateway-class), add a skip-guard at the top of that acceptance test following the existing pattern in `unifi/setting_resource_test.go` (`TestAccSettingResource_dohCustomServers`):

```go
	// <key> requires a real gateway/UDM-class controller; the demo/simulation
	// controller rejects the section.
	if os.Getenv("UNIFI_SKIP_CONTAINER") == "" {
		t.Skip("<key> requires a real controller; set UNIFI_SKIP_CONTAINER to run")
	}
```

(add `"os"` to imports.) Record which sections/attributes were skipped in the final report. Do NOT weaken the unit tests' raw-merge assertions to work around an acceptance failure — if a live-shape assumption (e.g. a JSON key) turns out wrong against the demo controller, fix the section code and its unit test together, and only then decide whether a skip-guard is still needed for an environment-specific reason (missing feature, not a code bug).

- [ ] **Step 3: Commit**

```bash
git add unifi/setting_section_dashboard_test.go unifi/setting_section_radio_ai_test.go unifi/setting_section_guest_access_test.go unifi/setting_section_provider_capabilities_test.go
git commit -m "test(setting): acceptance coverage for dashboard, radio_ai, guest_access, provider_capabilities"
```

(Body lists which sections/attributes got demo-controller skip-guards and why; omit `provider_capabilities` from the commit and message if Task 6 was gated out.)

---

### Task 8: Docs, changelog, final verification

**Files:**
- Modify: `examples/resources/unifi_setting/resource.tf` (add the new sections to the example)
- Modify: `CHANGELOG.md` (Unreleased → Features)
- Generated: `docs/resources/setting.md` (via `go generate ./...`)

- [ ] **Step 1: Extend the example**

Append to `examples/resources/unifi_setting/resource.tf` (read it first; match its commenting style and how PRs 1–3 folded their sections in). Omit `provider_capabilities` if Task 6 was gated out:

```terraform
# Portal, radio optimization, and dashboard settings. Guest portal
# credentials are sensitive — source them from variables, never literals.
variable "guest_portal_password" {
  type      = string
  sensitive = true
}

resource "unifi_setting" "portal" {
  site = "default"

  guest_access = {
    auth             = "hotspot"
    password         = var.guest_portal_password
    password_enabled = true
    portal_enabled   = true

    portal_customization = {
      customized = true
      title      = "Guest WiFi"
      bg_color   = "#005ED9"
    }
  }

  # Co-managed by the controller: only enabled/setting_preference are set
  # here. Leave setting_preference = "auto" (the default) unless you need to
  # pin specific channels — see the attribute's churn warning.
  radio_ai = {
    enabled = true
  }

  dashboard = {
    layout_preference = "auto"
  }

  # Requires go-unifi PR 0 (settings.ProviderCapabilities):
  provider_capabilities = {
    download = 1000000
    upload   = 500000
  }
}
```

Note: if the example file's existing convention is one `unifi_setting` resource per site, fold these sections into the existing block instead — follow the file, not this snippet. Drop the `provider_capabilities` block (and its comment) if Task 6 was gated out.

- [ ] **Step 2: Regenerate docs**

Run: `go generate ./...`
Expected: `docs/resources/setting.md` gains `guest_access`, `radio_ai`, `dashboard` (and `provider_capabilities` if Task 6 ran) attribute documentation, with all guest_access credential fields rendered as sensitive. Inspect the diff: `git diff --stat docs/`.

- [ ] **Step 3: Changelog**

Add under `## [Unreleased]` → `### ✨ Features` in `CHANGELOG.md` (match the existing bolded-lede prose style). Trim the `provider_capabilities` clause if Task 6 was gated out:

```markdown
- **`unifi_setting`: new `guest_access`, `radio_ai`, `dashboard`, and `provider_capabilities` sections.** The guest hotspot/captive portal is now feature-complete and attribute-compatible with the filipowm provider's `unifi_setting_guest_access`: portal basics, look-and-feel customization, Facebook/Facebook WiFi/Google/RADIUS/WeChat authentication, and six payment gateways (PayPal, Stripe, Authorize.net, QuickPay, MerchantWarrior, IPpay). Every credential — portal password, OAuth/API secrets, gateway keys — is marked sensitive, including three filipowm leaves unmarked (`google.client_secret`, `authorize.login_id`, `authorize.transaction_key`). `guest_access` reads through a raw JSON path rather than the typed SDK struct: the generated `Expire` field is typed `string` but live controllers send a JSON number, so the typed read fails outright (`TODO(go-unifi)` tracks the upstream fix); the write path was always raw-merge like every other section. `radio_ai` (automatic channel/power optimization) is controller co-managed — every attribute is `Optional+Computed` with `UseStateForUnknown`, and the schema description warns that setting anything beyond `enabled`/`setting_preference` while the controller is in `auto` mode can churn on every apply. `dashboard` covers the cosmetic layout preference and widget list. `provider_capabilities` (advertised WAN download/upload, used for utilization displays and Smart Queues sizing) has no filipowm equivalent and follows the go-unifi JSON keys directly. All four sections use the raw read-modify-write merge, so controller fields the SDK does not model (e.g. guest_access's `restricted_subnet_1..3`, radio_ai's `auto_enabled`) are preserved verbatim instead of silently dropped.
```

- [ ] **Step 4: Full verification**

Run, in order:

```bash
go build ./...
go vet ./...
go test ./unifi/ -count=1 2>&1 | tail -5
```

Expected: all clean, tests PASS. If the repo has a lint config (`.golangci.yml`), also run `golangci-lint run ./unifi/...` if the tool is installed — do not install tools globally to satisfy this. If a `replace` directive was added for Task 6, confirm it is still present and flagged: `grep replace go.mod` — it must be called out in the final report as a pre-release removal item.

- [ ] **Step 5: Commit**

```bash
git add examples/resources/unifi_setting/resource.tf docs/ CHANGELOG.md
git commit -m "docs(setting): document guest_access, radio_ai, dashboard, provider_capabilities sections"
```

- [ ] **Step 6: STOP — do not push**

The branch stays local. Report completion with: sections added (and whether Task 6 executed or was gated out), test results (including any demo-controller skips), whether a go.mod `replace` is in place, and the diff stat. James reviews before anything is posted publicly. Manual `tofu plan` validation against the live UDM is a James-driven step after review — not part of this plan.

---

## Self-review notes

- **Spec coverage** (PR 4 scope only): `guest_access` ✓ (Tasks 3–5, feature-complete, filipowm-aligned, all portal/payment credentials Sensitive), `radio_ai` ✓ (Task 2, every attribute Optional+Computed+UseStateForUnknown, explicit churn warning, guidance to manage only `enabled`/`setting_preference`), `dashboard` ✓ (Task 1, trivial cosmetic fields), `provider_capabilities` ✓ (Task 6, GATED on go-unifi PR 0 with an explicit precondition check and skip path, consuming the exact `Download int64`/`Upload int64` shape pinned by the go-unifi PR 0 plan), acceptance tests ✓ (Task 7, docker-demo-only via `TestMain`'s testcontainers boot, skip contingency for gateway/UDM-only features), docs + changelog ✓ (Task 8), no public push ✓ (Task 8 Step 6). Model field names (`GuestAccess`, `RadioAi`, `Dashboard`, `ProviderCapabilities`) match the Global Constraints exactly and are used consistently by every section's `get`/`set`.
- **Interface contract:** every section implements exactly the PR 1 Task 1 `settingSection` method set (`key`/`attrTypes`/`schemaAttribute`/`get`/`set`/`overlay`/`read`) and registers in `settingSections`; the engine (`applySections`/`readSections`) is consumed, never modified. Registry grows `dashboardSection{}` → `+radioAiSection{}` → `+guestAccessSection{}` → `+providerCapabilitiesSection{}` (Task 6 only if ungated).
- **Deviation from the typed-read pattern, intentional and load-bearing:** `guest_access` is the only section in PRs 1–4 whose `read` does not use `ui.GetSetting[T]`. Verified directly against `go-unifi@v1.33.43-0.20260706191309`'s `guest_access.generated.go`: `Expire` is declared `string` with no `types.Number`-based custom unmarshalling (unlike `ExpireNumber`/`ExpireUnit`, which do get that treatment in the struct's `UnmarshalJSON`), while the live payload (`udm-settings.json`) has `"expire": 480` as a bare JSON number — confirming the typed unmarshal would fail exactly as Task 3 describes. The write path is unaffected: every section's overlay was already raw-JSON (`RawSetting.Data`), so `guest_access` only differs on the read side, and only until the upstream `TODO(go-unifi)` is fixed.
- **Raw-key gotchas encoded in tests:** guest_access's `allowed_subnet_`/`restricted_subnet_` carry a trailing underscore in the wire key (go-unifi: `json:"allowed_subnet_,omitempty"` / `json:"restricted_subnet_,omitempty"`) with no index — confirmed against the live payload that the controller actually populates indexed variants (`restricted_subnet_1/2/3`) instead, which neither go-unifi nor this section models; `Test_guestAccessDataToModel_liveShape` and the Task 3 raw-preservation test correctly assert the bare-keyed `restricted_subnet` model attribute stays null in that shape while the indexed keys round-trip untouched through the raw merge — this is intentional, not a gap, and is called out here so a future PR extending guest_access does not "fix" it into an accidental behavior change. `radio_ai`'s `auto_enabled` unmodeled field is confirmed present in the live payload (not just asserted in the plan's prose). `provider_capabilities`'s live payload shows no unmodeled fields at all — Task 6 notes this explicitly rather than fabricating a raw-preservation assertion for a key that doesn't exist.
- **Sensitive-field audit vs. filipowm, independently verified against its source** (not taken on faith from an earlier draft of this plan): filipowm marks `Sensitive` on `facebook.app_secret`, `facebook_wifi.gateway_secret`, `merchant_warrior.api_key/api_passphrase/merchant_uuid`, `paypal.username/password/signature`, `quickpay.agreement_id/api_key/merchant_id`, `stripe.api_key`, `wechat.app_secret/secret_key`, and top-level `password`. It does **not** mark `google.client_id`/`google.client_secret` (present but commented out) or `authorize.login_id`/`authorize.transaction_key` (no `Sensitive` line ever existed for these). This plan marks all of those Sensitive per the spec's blanket rule, with one deliberate exception: `google.client_id` is left non-Sensitive, because an OAuth client ID is not confidential (only the paired secret is) — this is a judgment call, not an oversight, and is now called out explicitly instead of silently deviating from "every credential."
- **Type consistency:** `types.Set` is used for radio_ai's unordered channel/device/radio lists (`channels_6e/na/ng`, `ht_modes_na/ng`, `exclude_devices`, `high_priority_devices`, `optimize`, `radios`, and the two nested-object collections `channels_blacklist`/`radios_configuration`); `types.List` is used where order is meaningful — `dashboard.widgets` (layout order), `guest_access.portal_customization.languages` (display order), and `guest_access.restricted_dns_servers` (resolver priority) — matching the Global Constraints and filipowm's own types for the two guest_access cases. `types.Int64` is used uniformly for all numeric fields, including `guest_access.expire`/`expire_number`/`expire_unit` (raw-read as JSON `float64` via `rawInt`, converted to `int64`) and `provider_capabilities.download`/`upload` (typed read, plain `int64` fields, no pointer/Number wrapper per the go-unifi PR 0 plan's "simple scalars" note for hand-written settings).
- **Placeholder scan:** no `TODO` markers in this plan represent unfinished plan content — the two `TODO(go-unifi):` occurrences (Task 3's doc comment, Task 3's function comment) are intentional forward-pointers into the *generated code* the plan produces, tracking a known upstream bug to fix later, not a gap in this plan. Grepped the full file for `FIXME`/`XXX`/`\.\.\.`(elided code)/`// implementation`-style stand-ins: none found outside prose that quotes go-unifi's own comments.
- **Fixes made to the partial draft during completion** (see task-runner summary for the full list): (1) Global Constraints' Sensitive-fields bullet and Task 5's commit-body note both misattributed filipowm's two commented-out `Sensitive` lines to the `authorize` block; independently re-verified against filipowm's source and corrected — the commented-out lines are `google.client_id`/`google.client_secret`, while `authorize.login_id`/`authorize.transaction_key` never had a `Sensitive` line at all (both are still unmarked in filipowm either way, so the *code* in Tasks 4–5 was already correct; only the prose explaining *why* was wrong). (2) Added the explicit note that `google.client_id` is deliberately left non-Sensitive (schema code already did this correctly; the prose previously implied otherwise).
- **Known open questions for the implementer, not blocking:** (a) Task 6's gate depends on go-unifi PR 0 having actually landed by the time this plan executes — if it has not, Task 6 is skipped per its precondition block and Tasks 7–8 proceed without `provider_capabilities`, exactly like PR 3's tranche B pattern. (b) The acceptance test contingency in Task 7 flags `guest_access` payment fields and `provider_capabilities` as likely demo-controller rejections, but this is a prediction, not a verified fact — the implementer must actually run the tests and record what happens, not assume the skip-guard is needed. (c) `radio_ai`'s acceptance test deliberately avoids asserting on channel/blacklist/radios-configuration fields because the controller may silently rewrite them under `setting_preference = "auto"`; if the demo controller defaults to `manual`, this is conservative rather than wrong, but the implementer should confirm the actual demo-controller default and adjust the test's second step if `auto`→`manual` transition behaves unexpectedly.
