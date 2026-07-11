# Settings PR 1: switching & NAT globals Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `global_switch`, `global_nat`, and `locale` nested sections to the `unifi_setting` resource, introducing the section-handler registry and raw-merge read-modify-write engine that later PRs build on.

**Architecture:** Each new section is a self-contained file implementing a small `settingSection` interface (schema fragment, model accessors, raw overlay, typed read), registered in a package-level slice. Apply happens at the **raw JSON level**: `ListSettings` once per operation → merge only user-set fields into the section's `Data` map → full-object PUT via `RawSetting` — so controller fields go-unifi doesn't model (e.g. live `link_debounce` on `global_switch`) round-trip untouched, and a failed list aborts before any PUT. Reads use the typed generated structs.

**Tech Stack:** Go, terraform-plugin-framework, go-unifi v1.33.43 fork (`unifi/settings` package: `GlobalSwitch`, `GlobalNat`, `Locale`, `RawSetting`).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-10-setting-sections-design.md`. This plan is PR 1 of 6.
- **Nothing is pushed or posted publicly. Local branch/commits only; James reviews before any push.**
- New-section attribute names align with filipowm where the section overlaps: `global_switch` uses `acl_device_isolation`, `acl_l3_isolation` (nested `source_network` + `destination_networks`), `switch_exclusions` — all `types.Set`, matching filipowm. Sections filipowm lacks follow go-unifi JSON naming (snake_case, `networkconf` → `network`).
- Unordered string collections in NEW sections are `types.Set` (not List). Optional+Computed collections get `setplanmodifier.UseStateForUnknown()` (see CHANGELOG entry re #338-class churn).
- Existing 13 sections and their tests are untouched in this PR.
- Commit style: conventional commits matching the repo log (`feat(setting): …`), body explains why, `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` trailer.
- All commands run from the repo root: `/Users/jamesb/emdash/worktrees/terraform-provider-unifi/emdash/missing-config-uyrwq`.
- Unit tests: `go test ./unifi/ -run '<pattern>' -count=1`. Acceptance: docker demo controller only (Task 4) — never a live UDM.

---

### Task 1: Section registry engine + `locale` section

The engine and its first (smallest) section land together: the locale tests are what force the engine into existence.

**Files:**
- Create: `unifi/setting_section.go`
- Create: `unifi/setting_section_locale.go`
- Create: `unifi/setting_section_locale_test.go`
- Modify: `unifi/setting_resource.go` (model struct ~line 224; `Schema` end ~line 1264; `Create` after the igmp block ~line 1592; `Update` after its igmp block; `readSettings` end)

**Interfaces:**
- Consumes: `settingResourceModel`, `Client` (embeds `*ui.ApiClient`: `ListSettings`, `UpdateSetting`), `ui.GetSetting[T]`, `settings.RawSetting`, `util.StringValueOrNull`.
- Produces (later tasks rely on these exact names):
  - `type settingSection interface { key() string; attrTypes() map[string]attr.Type; schemaAttribute() schema.SingleNestedAttribute; get(m *settingResourceModel) types.Object; set(m *settingResourceModel, obj types.Object); overlay(ctx context.Context, obj types.Object, data map[string]any) diag.Diagnostics; read(ctx context.Context, client *Client, site string) (types.Object, diag.Diagnostics) }`
  - `var settingSections []settingSection`
  - `func (r *settingResource) applySections(ctx context.Context, site string, m *settingResourceModel) diag.Diagnostics`
  - `func (r *settingResource) readSections(ctx context.Context, site string, m *settingResourceModel, diags *diag.Diagnostics)`

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_locale_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_localeModelToData(t *testing.T) {
	tz := "America/Vancouver"
	m := &settingLocaleModel{Timezone: types.StringValue(tz)}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": true}

	localeModelToData(m, data)

	if got := data["timezone"]; got != tz {
		t.Fatalf("timezone = %v, want %q", got, tz)
	}
	if got := data["unmodeled_field"]; got != true {
		t.Fatalf("unmodeled_field was clobbered: %v", got)
	}
}

func Test_localeModelToData_nullLeavesRemoteValue(t *testing.T) {
	m := &settingLocaleModel{Timezone: types.StringNull()}
	data := map[string]any{"timezone": "Etc/UTC"}

	localeModelToData(m, data)

	if got := data["timezone"]; got != "Etc/UTC" {
		t.Fatalf("null timezone overwrote remote value: %v", got)
	}
}

func Test_localeSettingToModel(t *testing.T) {
	m := localeSettingToModel(&settings.Locale{Timezone: "America/Vancouver"})
	if m.Timezone.ValueString() != "America/Vancouver" {
		t.Fatalf("timezone = %v", m.Timezone)
	}
	empty := localeSettingToModel(&settings.Locale{})
	if !empty.Timezone.IsNull() {
		t.Fatalf("empty timezone should map to null, got %v", empty.Timezone)
	}
}

func Test_settingResource_Schema_locale(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["locale"]; !ok {
		t.Fatal("schema is missing the locale section attribute")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_locale|Test_settingResource_Schema_locale' -count=1`
Expected: compile FAILURE — `undefined: settingLocaleModel`, `undefined: localeModelToData`, etc.

- [ ] **Step 3: Create the engine — `unifi/setting_section.go`**

```go
package unifi

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// settingSection is one nested section of the unifi_setting resource backed
// by its own controller setting object (rest/setting/<key>). Sections
// register in settingSections; Schema, Create, Update, and readSettings
// iterate the registry, so adding a section is one new file plus one
// registry entry.
type settingSection interface {
	// key is both the nested attribute name and the controller setting key.
	key() string
	attrTypes() map[string]attr.Type
	schemaAttribute() schema.SingleNestedAttribute
	// get/set access this section's object on the resource model.
	get(m *settingResourceModel) types.Object
	set(m *settingResourceModel, obj types.Object)
	// overlay writes only the user-configured fields into the section's raw
	// JSON document. Fields already in data — including ones go-unifi does
	// not model — are preserved and sent back in the PUT.
	overlay(ctx context.Context, obj types.Object, data map[string]any) diag.Diagnostics
	// read fetches the section and converts it to a model object. A missing
	// section yields a null object without error.
	read(ctx context.Context, client *Client, site string) (types.Object, diag.Diagnostics)
}

// settingSections is the registry of sections using the raw-merge engine.
var settingSections = []settingSection{
	localeSection{},
}

// applySections performs the read-modify-write for every configured registry
// section. The raw settings list is fetched once; a fetch failure aborts
// before any PUT so a transient read error can never clobber unmanaged
// fields with a zero-value base.
func (r *settingResource) applySections(
	ctx context.Context,
	site string,
	m *settingResourceModel,
) diag.Diagnostics {
	var diags diag.Diagnostics

	var active []settingSection
	for _, s := range settingSections {
		if obj := s.get(m); !obj.IsNull() && !obj.IsUnknown() {
			active = append(active, s)
		}
	}
	if len(active) == 0 {
		return diags
	}

	raws, err := r.client.ListSettings(ctx, site)
	if err != nil {
		diags.AddError("Error Reading Settings", err.Error())
		return diags
	}
	byKey := make(map[string]settings.RawSetting, len(raws))
	for _, raw := range raws {
		byKey[raw.GetKey()] = raw
	}

	for _, s := range active {
		raw := byKey[s.key()]
		if raw.Data == nil {
			raw.Data = map[string]any{}
		}
		raw.SetKey(s.key())
		diags.Append(s.overlay(ctx, s.get(m), raw.Data)...)
		if diags.HasError() {
			return diags
		}
		if err := r.client.UpdateSetting(ctx, site, &raw); err != nil {
			diags.AddError(
				fmt.Sprintf("Error Updating %s Setting", s.key()),
				err.Error(),
			)
			return diags
		}
	}
	return diags
}

// readSections refreshes every registry section present in the model,
// mirroring the inline sections' behavior: sections absent from
// configuration stay null.
func (r *settingResource) readSections(
	ctx context.Context,
	site string,
	m *settingResourceModel,
	diags *diag.Diagnostics,
) {
	for _, s := range settingSections {
		obj := s.get(m)
		if obj.IsNull() || obj.IsUnknown() {
			s.set(m, types.ObjectNull(s.attrTypes()))
			continue
		}
		value, d := s.read(ctx, r.client, site)
		diags.Append(d...)
		if diags.HasError() {
			return
		}
		s.set(m, value)
	}
}
```

- [ ] **Step 4: Create `unifi/setting_section_locale.go`**

```go
package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingLocaleModel is the nested locale block: the site timezone. The
// attribute name aligns with filipowm's unifi_setting_locale for config
// portability.
type settingLocaleModel struct {
	Timezone types.String `tfsdk:"timezone"`
}

var localeAttrTypes = map[string]attr.Type{
	"timezone": types.StringType,
}

type localeSection struct{}

func (localeSection) key() string { return "locale" }

func (localeSection) attrTypes() map[string]attr.Type { return localeAttrTypes }

func (localeSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site locale settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"timezone": schema.StringAttribute{
				MarkdownDescription: "Site timezone as an IANA zone name (e.g. `America/Vancouver`).",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func (localeSection) get(m *settingResourceModel) types.Object { return m.Locale }

func (localeSection) set(m *settingResourceModel, obj types.Object) { m.Locale = obj }

func (localeSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingLocaleModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	localeModelToData(&m, data)
	return diags
}

// localeModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values.
func localeModelToData(m *settingLocaleModel, data map[string]any) {
	if !m.Timezone.IsNull() && !m.Timezone.IsUnknown() {
		data["timezone"] = m.Timezone.ValueString()
	}
}

func (localeSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Locale](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(localeAttrTypes), diags
		}
		diags.AddError("Error Reading Locale Setting", err.Error())
		return types.ObjectNull(localeAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, localeAttrTypes, localeSettingToModel(setting))
}

func localeSettingToModel(s *settings.Locale) settingLocaleModel {
	return settingLocaleModel{Timezone: util.StringValueOrNull(s.Timezone)}
}
```

- [ ] **Step 5: Wire the registry into `unifi/setting_resource.go`**

Four edits:

1. Add the model field to `settingResourceModel` (after `IgmpSnooping`):

```go
	Locale        types.Object   `tfsdk:"locale"`
```

2. At the end of `Schema` (after the `resp.Schema = schema.Schema{...}` assignment closes, before the function's closing brace):

```go
	for _, s := range settingSections {
		resp.Schema.Attributes[s.key()] = s.schemaAttribute()
	}
```

3. In `Create`, immediately after the igmp block (before the `// Read back the settings` comment, ~line 1594) — and the same two lines in `Update` after its igmp block (~line 1880):

```go
	resp.Diagnostics.Append(r.applySections(ctx, site, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
```

(In `Update` the model variable is named `plan`, not `data` — pass `&plan`.)

4. At the end of `readSettings` (after the igmp read block, ~line 2296):

```go
	r.readSections(ctx, site, data, diags)
```

(`readSettings` already receives `data *settingResourceModel, diags *diag.Diagnostics` — check the signature at its definition and match parameter names.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_locale|Test_settingResource_Schema' -count=1`
Expected: PASS (including the pre-existing `Test_settingResource_Schema`).

- [ ] **Step 7: Run the full unit suite**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -20`
Expected: PASS, no failures in existing tests. (Acceptance tests auto-skip without `TF_ACC`.)

- [ ] **Step 8: Commit**

```bash
git add unifi/setting_section.go unifi/setting_section_locale.go unifi/setting_section_locale_test.go unifi/setting_resource.go
git commit -m "feat(setting): add locale section via raw-merge section registry"
```

(Write a body explaining the registry + raw-merge rationale: preserves controller fields go-unifi doesn't model; list-failure aborts before any PUT.)

---

### Task 2: `global_nat` section

**Files:**
- Create: `unifi/setting_section_global_nat.go`
- Create: `unifi/setting_section_global_nat_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settingSections`, `settings.GlobalNat` (`Mode string // auto|custom|off`, `ExcludedNetworkIDs []string`).
- Produces: `settingGlobalNatModel`, `globalNatAttrTypes`, `globalNatSection`, `globalNatModelToData(ctx, m, data, diags)`, `globalNatSettingToModel(ctx, s, diags)`.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_global_nat_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_globalNatModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	excluded, d := types.SetValueFrom(ctx, types.StringType, []string{"abc123"})
	if d.HasError() {
		t.Fatal(d)
	}
	m := &settingGlobalNatModel{
		Mode:               types.StringValue("auto"),
		ExcludedNetworkIDs: excluded,
	}
	data := map[string]any{"unmodeled_field": "keep"}

	globalNatModelToData(ctx, m, data, &diags)

	if diags.HasError() {
		t.Fatal(diags)
	}
	if data["mode"] != "auto" {
		t.Fatalf("mode = %v", data["mode"])
	}
	ids, ok := data["excluded_network_ids"].([]string)
	if !ok || len(ids) != 1 || ids[0] != "abc123" {
		t.Fatalf("excluded_network_ids = %v", data["excluded_network_ids"])
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_globalNatModelToData_nullsLeaveRemoteValues(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := &settingGlobalNatModel{
		Mode:               types.StringNull(),
		ExcludedNetworkIDs: types.SetNull(types.StringType),
	}
	data := map[string]any{"mode": "custom"}

	globalNatModelToData(ctx, m, data, &diags)

	if data["mode"] != "custom" {
		t.Fatalf("null mode overwrote remote value: %v", data["mode"])
	}
	if _, present := data["excluded_network_ids"]; present {
		t.Fatal("null set should not write excluded_network_ids")
	}
}

func Test_globalNatSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := globalNatSettingToModel(ctx, &settings.GlobalNat{
		Mode:               "auto",
		ExcludedNetworkIDs: []string{"abc123"},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	if m.Mode.ValueString() != "auto" {
		t.Fatalf("mode = %v", m.Mode)
	}
	var ids []string
	diags.Append(m.ExcludedNetworkIDs.ElementsAs(ctx, &ids, false)...)
	if len(ids) != 1 || ids[0] != "abc123" {
		t.Fatalf("ids = %v", ids)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_globalNat' -count=1`
Expected: compile FAILURE — `undefined: settingGlobalNatModel` etc.

- [ ] **Step 3: Create `unifi/setting_section_global_nat.go`**

```go
package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingGlobalNatModel is the nested global_nat block: the site-wide NAT
// mode and per-network exclusions that pair with unifi_nat_rule.
type settingGlobalNatModel struct {
	Mode               types.String `tfsdk:"mode"`
	ExcludedNetworkIDs types.Set    `tfsdk:"excluded_network_ids"`
}

var globalNatAttrTypes = map[string]attr.Type{
	"mode":                 types.StringType,
	"excluded_network_ids": types.SetType{ElemType: types.StringType},
}

type globalNatSection struct{}

func (globalNatSection) key() string { return "global_nat" }

func (globalNatSection) attrTypes() map[string]attr.Type { return globalNatAttrTypes }

func (globalNatSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site-wide NAT settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"mode": schema.StringAttribute{
				MarkdownDescription: "NAT mode: `auto`, `custom`, or `off`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "custom", "off"),
				},
			},
			"excluded_network_ids": schema.SetAttribute{
				MarkdownDescription: "Network IDs excluded from automatic NAT.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (globalNatSection) get(m *settingResourceModel) types.Object { return m.GlobalNat }

func (globalNatSection) set(m *settingResourceModel, obj types.Object) { m.GlobalNat = obj }

func (globalNatSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingGlobalNatModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	globalNatModelToData(ctx, &m, data, &diags)
	return diags
}

// globalNatModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values.
func globalNatModelToData(
	ctx context.Context,
	m *settingGlobalNatModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.Mode.IsNull() && !m.Mode.IsUnknown() {
		data["mode"] = m.Mode.ValueString()
	}
	if !m.ExcludedNetworkIDs.IsNull() && !m.ExcludedNetworkIDs.IsUnknown() {
		var ids []string
		diags.Append(m.ExcludedNetworkIDs.ElementsAs(ctx, &ids, false)...)
		data["excluded_network_ids"] = ids
	}
}

func (globalNatSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.GlobalNat](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(globalNatAttrTypes), diags
		}
		diags.AddError("Error Reading Global NAT Setting", err.Error())
		return types.ObjectNull(globalNatAttrTypes), diags
	}
	model := globalNatSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(globalNatAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, globalNatAttrTypes, model)
}

func globalNatSettingToModel(
	ctx context.Context,
	s *settings.GlobalNat,
	diags *diag.Diagnostics,
) settingGlobalNatModel {
	ids, d := types.SetValueFrom(ctx, types.StringType, s.ExcludedNetworkIDs)
	diags.Append(d...)
	return settingGlobalNatModel{
		Mode:               util.StringValueOrNull(s.Mode),
		ExcludedNetworkIDs: ids,
	}
}
```

- [ ] **Step 4: Register the section**

In `unifi/setting_section.go`, extend the registry:

```go
var settingSections = []settingSection{
	localeSection{},
	globalNatSection{},
}
```

In `unifi/setting_resource.go`, add to `settingResourceModel` (after `Locale`):

```go
	GlobalNat     types.Object   `tfsdk:"global_nat"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_globalNat|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_global_nat.go unifi/setting_section_global_nat_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add global_nat section"
```

---

### Task 3: `global_switch` section

**Files:**
- Create: `unifi/setting_section_global_switch.go`
- Create: `unifi/setting_section_global_switch_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settings.GlobalSwitch` (fields: `AclDeviceIsolation []string`, `AclL3Isolation []SettingGlobalSwitchAclL3Isolation{DestinationNetworks []string; SourceNetwork string}`, `DHCPSnoop bool`, `Dot1XFallbackNetworkID string` (json `dot1x_fallback_networkconf_id`), `Dot1XPortctrlEnabled bool`, `FloodKnownProtocols bool`, `FlowctrlEnabled bool`, `ForwardUnknownMcastRouterPorts bool`, `JumboframeEnabled bool`, `RADIUSProfileID string` (json `radiusprofile_id`), `StpVersion string // stp|rstp|disabled`, `SwitchExclusions []string`).
- Produces: `settingGlobalSwitchModel`, `settingGlobalSwitchACLL3IsolationModel`, `globalSwitchAttrTypes`, `globalSwitchACLL3IsolationAttrTypes`, `globalSwitchSection`, `globalSwitchModelToData(ctx, m, data, diags)`, `globalSwitchSettingToModel(ctx, s, diags)`.
- filipowm-aligned names (identical): `acl_device_isolation`, `acl_l3_isolation` (`source_network`, `destination_networks`), `switch_exclusions`. Superset fields use go-unifi JSON names with `networkconf`→`network` and `radiusprofile_id`→`radius_profile_id` normalization on the TF side only (raw JSON keys stay `dot1x_fallback_networkconf_id` / `radiusprofile_id`).

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_global_switch_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_globalSwitchModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	dst, d := types.SetValueFrom(ctx, types.StringType, []string{"net2"})
	if d.HasError() {
		t.Fatal(d)
	}
	rule, d := types.ObjectValueFrom(ctx, globalSwitchACLL3IsolationAttrTypes,
		settingGlobalSwitchACLL3IsolationModel{
			SourceNetwork:       types.StringValue("net1"),
			DestinationNetworks: dst,
		})
	if d.HasError() {
		t.Fatal(d)
	}
	rules, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: globalSwitchACLL3IsolationAttrTypes},
		[]types.Object{rule})
	if d.HasError() {
		t.Fatal(d)
	}
	exclusions, d := types.SetValueFrom(ctx, types.StringType,
		[]string{"aa:bb:cc:dd:ee:ff"})
	if d.HasError() {
		t.Fatal(d)
	}

	m := &settingGlobalSwitchModel{
		ACLDeviceIsolation:             types.SetNull(types.StringType),
		ACLL3Isolation:                 rules,
		SwitchExclusions:               exclusions,
		DHCPSnoop:                      types.BoolValue(true),
		Dot1XFallbackNetworkID:         types.StringValue("fallback1"),
		Dot1XPortctrlEnabled:           types.BoolNull(),
		FloodKnownProtocols:            types.BoolNull(),
		FlowctrlEnabled:                types.BoolValue(false),
		ForwardUnknownMcastRouterPorts: types.BoolNull(),
		JumboframeEnabled:              types.BoolValue(true),
		RADIUSProfileID:                types.StringNull(),
		StpVersion:                     types.StringValue("rstp"),
	}

	// The live controller has fields go-unifi does not model (e.g.
	// link_debounce); the raw merge must preserve them verbatim.
	data := map[string]any{"link_debounce": true, "dot1x_portctrl_enabled": true}

	globalSwitchModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["link_debounce"] != true {
		t.Fatal("unmodeled link_debounce was clobbered")
	}
	if data["dot1x_portctrl_enabled"] != true {
		t.Fatal("null dot1x_portctrl_enabled overwrote remote value")
	}
	if data["dhcp_snoop"] != true || data["flowctrl_enabled"] != false ||
		data["jumboframe_enabled"] != true {
		t.Fatalf("bool fields wrong: %v", data)
	}
	if data["stp_version"] != "rstp" {
		t.Fatalf("stp_version = %v", data["stp_version"])
	}
	if data["dot1x_fallback_networkconf_id"] != "fallback1" {
		t.Fatalf("dot1x_fallback_networkconf_id = %v", data["dot1x_fallback_networkconf_id"])
	}
	if _, present := data["radiusprofile_id"]; present {
		t.Fatal("null radius_profile_id should not be written")
	}
	if _, present := data["acl_device_isolation"]; present {
		t.Fatal("null acl_device_isolation should not be written")
	}
	l3, ok := data["acl_l3_isolation"].([]map[string]any)
	if !ok || len(l3) != 1 || l3[0]["source_network"] != "net1" {
		t.Fatalf("acl_l3_isolation = %v", data["acl_l3_isolation"])
	}
	excl, ok := data["switch_exclusions"].([]string)
	if !ok || len(excl) != 1 || excl[0] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("switch_exclusions = %v", data["switch_exclusions"])
	}
}

func Test_globalSwitchSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := globalSwitchSettingToModel(ctx, &settings.GlobalSwitch{
		AclDeviceIsolation: []string{"dev1"},
		AclL3Isolation: []settings.SettingGlobalSwitchAclL3Isolation{
			{SourceNetwork: "net1", DestinationNetworks: []string{"net2"}},
		},
		DHCPSnoop:         true,
		JumboframeEnabled: true,
		StpVersion:        "rstp",
		RADIUSProfileID:   "rp1",
		SwitchExclusions:  []string{"aa:bb:cc:dd:ee:ff"},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if !m.DHCPSnoop.ValueBool() || !m.JumboframeEnabled.ValueBool() {
		t.Fatal("bools not mapped")
	}
	if m.StpVersion.ValueString() != "rstp" {
		t.Fatalf("stp_version = %v", m.StpVersion)
	}
	if m.RADIUSProfileID.ValueString() != "rp1" {
		t.Fatalf("radius_profile_id = %v", m.RADIUSProfileID)
	}
	if m.Dot1XFallbackNetworkID.IsUnknown() || !m.Dot1XFallbackNetworkID.IsNull() {
		t.Fatalf("empty dot1x fallback should be null, got %v", m.Dot1XFallbackNetworkID)
	}
	var rules []settingGlobalSwitchACLL3IsolationModel
	diags.Append(m.ACLL3Isolation.ElementsAs(ctx, &rules, false)...)
	if len(rules) != 1 || rules[0].SourceNetwork.ValueString() != "net1" {
		t.Fatalf("acl_l3_isolation = %v", rules)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_globalSwitch' -count=1`
Expected: compile FAILURE — `undefined: settingGlobalSwitchModel` etc.

- [ ] **Step 3: Create `unifi/setting_section_global_switch.go`**

```go
package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingGlobalSwitchModel is the nested global_switch block: site-wide
// switch behavior. acl_device_isolation, acl_l3_isolation, and
// switch_exclusions align with filipowm's unifi_setting_global_switch; the
// remaining fields are this provider's superset.
type settingGlobalSwitchModel struct {
	ACLDeviceIsolation             types.Set    `tfsdk:"acl_device_isolation"`
	ACLL3Isolation                 types.Set    `tfsdk:"acl_l3_isolation"`
	SwitchExclusions               types.Set    `tfsdk:"switch_exclusions"`
	DHCPSnoop                      types.Bool   `tfsdk:"dhcp_snoop"`
	Dot1XFallbackNetworkID         types.String `tfsdk:"dot1x_fallback_network_id"`
	Dot1XPortctrlEnabled           types.Bool   `tfsdk:"dot1x_portctrl_enabled"`
	FloodKnownProtocols            types.Bool   `tfsdk:"flood_known_protocols"`
	FlowctrlEnabled                types.Bool   `tfsdk:"flowctrl_enabled"`
	ForwardUnknownMcastRouterPorts types.Bool   `tfsdk:"forward_unknown_mcast_router_ports"`
	JumboframeEnabled              types.Bool   `tfsdk:"jumboframe_enabled"`
	RADIUSProfileID                types.String `tfsdk:"radius_profile_id"`
	StpVersion                     types.String `tfsdk:"stp_version"`
}

type settingGlobalSwitchACLL3IsolationModel struct {
	SourceNetwork       types.String `tfsdk:"source_network"`
	DestinationNetworks types.Set    `tfsdk:"destination_networks"`
}

var (
	globalSwitchACLL3IsolationAttrTypes = map[string]attr.Type{
		"source_network":       types.StringType,
		"destination_networks": types.SetType{ElemType: types.StringType},
	}
	globalSwitchAttrTypes = map[string]attr.Type{
		"acl_device_isolation": types.SetType{ElemType: types.StringType},
		"acl_l3_isolation": types.SetType{
			ElemType: types.ObjectType{AttrTypes: globalSwitchACLL3IsolationAttrTypes},
		},
		"switch_exclusions":                  types.SetType{ElemType: types.StringType},
		"dhcp_snoop":                         types.BoolType,
		"dot1x_fallback_network_id":          types.StringType,
		"dot1x_portctrl_enabled":             types.BoolType,
		"flood_known_protocols":              types.BoolType,
		"flowctrl_enabled":                   types.BoolType,
		"forward_unknown_mcast_router_ports": types.BoolType,
		"jumboframe_enabled":                 types.BoolType,
		"radius_profile_id":                  types.StringType,
		"stp_version":                        types.StringType,
	}
)

type globalSwitchSection struct{}

func (globalSwitchSection) key() string { return "global_switch" }

func (globalSwitchSection) attrTypes() map[string]attr.Type { return globalSwitchAttrTypes }

func (globalSwitchSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site-wide switch settings. Controller-managed fields not exposed here (e.g. link debounce) are preserved across updates.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"acl_device_isolation": schema.SetAttribute{
				MarkdownDescription: "Device identifiers isolated by the controller's Device Isolation control.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"acl_l3_isolation": schema.SetNestedAttribute{
				MarkdownDescription: "Layer-3 (network-to-network) isolation rules.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"source_network": schema.StringAttribute{
							MarkdownDescription: "UniFi network ID the rule applies to.",
							Required:            true,
						},
						"destination_networks": schema.SetAttribute{
							MarkdownDescription: "UniFi network IDs the source network is isolated from.",
							Required:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
			"switch_exclusions": schema.SetAttribute{
				MarkdownDescription: "Switch MAC addresses excluded from isolation enforcement.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"dhcp_snoop": schema.BoolAttribute{
				MarkdownDescription: "Enable DHCP snooping.",
				Optional:            true,
				Computed:            true,
			},
			"dot1x_fallback_network_id": schema.StringAttribute{
				MarkdownDescription: "Fallback network ID for 802.1X (empty for none).",
				Optional:            true,
				Computed:            true,
			},
			"dot1x_portctrl_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable 802.1X port control.",
				Optional:            true,
				Computed:            true,
			},
			"flood_known_protocols": schema.BoolAttribute{
				MarkdownDescription: "Flood known protocols.",
				Optional:            true,
				Computed:            true,
			},
			"flowctrl_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable flow control.",
				Optional:            true,
				Computed:            true,
			},
			"forward_unknown_mcast_router_ports": schema.BoolAttribute{
				MarkdownDescription: "Forward unknown multicast to router ports.",
				Optional:            true,
				Computed:            true,
			},
			"jumboframe_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable jumbo frames.",
				Optional:            true,
				Computed:            true,
			},
			"radius_profile_id": schema.StringAttribute{
				MarkdownDescription: "RADIUS profile ID used for 802.1X.",
				Optional:            true,
				Computed:            true,
			},
			"stp_version": schema.StringAttribute{
				MarkdownDescription: "Spanning tree mode: `stp`, `rstp`, or `disabled`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("stp", "rstp", "disabled"),
				},
			},
		},
	}
}

func (globalSwitchSection) get(m *settingResourceModel) types.Object { return m.GlobalSwitch }

func (globalSwitchSection) set(m *settingResourceModel, obj types.Object) { m.GlobalSwitch = obj }

func (globalSwitchSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingGlobalSwitchModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	globalSwitchModelToData(ctx, &m, data, &diags)
	return diags
}

// globalSwitchModelToData writes only the user-set fields into the raw
// section document; unset fields — including controller fields go-unifi
// does not model, like link_debounce — keep their remote values.
func globalSwitchModelToData(
	ctx context.Context,
	m *settingGlobalSwitchModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.ACLDeviceIsolation.IsNull() && !m.ACLDeviceIsolation.IsUnknown() {
		var ids []string
		diags.Append(m.ACLDeviceIsolation.ElementsAs(ctx, &ids, false)...)
		data["acl_device_isolation"] = ids
	}
	if !m.ACLL3Isolation.IsNull() && !m.ACLL3Isolation.IsUnknown() {
		var rules []settingGlobalSwitchACLL3IsolationModel
		diags.Append(m.ACLL3Isolation.ElementsAs(ctx, &rules, false)...)
		out := make([]map[string]any, 0, len(rules))
		for _, rule := range rules {
			var dst []string
			diags.Append(rule.DestinationNetworks.ElementsAs(ctx, &dst, false)...)
			out = append(out, map[string]any{
				"source_network":       rule.SourceNetwork.ValueString(),
				"destination_networks": dst,
			})
		}
		data["acl_l3_isolation"] = out
	}
	if !m.SwitchExclusions.IsNull() && !m.SwitchExclusions.IsUnknown() {
		var macs []string
		diags.Append(m.SwitchExclusions.ElementsAs(ctx, &macs, false)...)
		data["switch_exclusions"] = macs
	}
	if !m.DHCPSnoop.IsNull() && !m.DHCPSnoop.IsUnknown() {
		data["dhcp_snoop"] = m.DHCPSnoop.ValueBool()
	}
	if !m.Dot1XFallbackNetworkID.IsNull() && !m.Dot1XFallbackNetworkID.IsUnknown() {
		data["dot1x_fallback_networkconf_id"] = m.Dot1XFallbackNetworkID.ValueString()
	}
	if !m.Dot1XPortctrlEnabled.IsNull() && !m.Dot1XPortctrlEnabled.IsUnknown() {
		data["dot1x_portctrl_enabled"] = m.Dot1XPortctrlEnabled.ValueBool()
	}
	if !m.FloodKnownProtocols.IsNull() && !m.FloodKnownProtocols.IsUnknown() {
		data["flood_known_protocols"] = m.FloodKnownProtocols.ValueBool()
	}
	if !m.FlowctrlEnabled.IsNull() && !m.FlowctrlEnabled.IsUnknown() {
		data["flowctrl_enabled"] = m.FlowctrlEnabled.ValueBool()
	}
	if !m.ForwardUnknownMcastRouterPorts.IsNull() && !m.ForwardUnknownMcastRouterPorts.IsUnknown() {
		data["forward_unknown_mcast_router_ports"] = m.ForwardUnknownMcastRouterPorts.ValueBool()
	}
	if !m.JumboframeEnabled.IsNull() && !m.JumboframeEnabled.IsUnknown() {
		data["jumboframe_enabled"] = m.JumboframeEnabled.ValueBool()
	}
	if !m.RADIUSProfileID.IsNull() && !m.RADIUSProfileID.IsUnknown() {
		data["radiusprofile_id"] = m.RADIUSProfileID.ValueString()
	}
	if !m.StpVersion.IsNull() && !m.StpVersion.IsUnknown() {
		data["stp_version"] = m.StpVersion.ValueString()
	}
}

func (globalSwitchSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.GlobalSwitch](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(globalSwitchAttrTypes), diags
		}
		diags.AddError("Error Reading Global Switch Setting", err.Error())
		return types.ObjectNull(globalSwitchAttrTypes), diags
	}
	model := globalSwitchSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(globalSwitchAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, globalSwitchAttrTypes, model)
}

func globalSwitchSettingToModel(
	ctx context.Context,
	s *settings.GlobalSwitch,
	diags *diag.Diagnostics,
) settingGlobalSwitchModel {
	aclDevice, d := types.SetValueFrom(ctx, types.StringType, s.AclDeviceIsolation)
	diags.Append(d...)

	rules := make([]settingGlobalSwitchACLL3IsolationModel, 0, len(s.AclL3Isolation))
	for _, rule := range s.AclL3Isolation {
		dst, d := types.SetValueFrom(ctx, types.StringType, rule.DestinationNetworks)
		diags.Append(d...)
		rules = append(rules, settingGlobalSwitchACLL3IsolationModel{
			SourceNetwork:       types.StringValue(rule.SourceNetwork),
			DestinationNetworks: dst,
		})
	}
	aclL3, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: globalSwitchACLL3IsolationAttrTypes}, rules)
	diags.Append(d...)

	exclusions, d := types.SetValueFrom(ctx, types.StringType, s.SwitchExclusions)
	diags.Append(d...)

	return settingGlobalSwitchModel{
		ACLDeviceIsolation:             aclDevice,
		ACLL3Isolation:                 aclL3,
		SwitchExclusions:               exclusions,
		DHCPSnoop:                      types.BoolValue(s.DHCPSnoop),
		Dot1XFallbackNetworkID:         util.StringValueOrNull(s.Dot1XFallbackNetworkID),
		Dot1XPortctrlEnabled:           types.BoolValue(s.Dot1XPortctrlEnabled),
		FloodKnownProtocols:            types.BoolValue(s.FloodKnownProtocols),
		FlowctrlEnabled:                types.BoolValue(s.FlowctrlEnabled),
		ForwardUnknownMcastRouterPorts: types.BoolValue(s.ForwardUnknownMcastRouterPorts),
		JumboframeEnabled:              types.BoolValue(s.JumboframeEnabled),
		RADIUSProfileID:                util.StringValueOrNull(s.RADIUSProfileID),
		StpVersion:                     util.StringValueOrNull(s.StpVersion),
	}
}
```

- [ ] **Step 4: Register the section**

`unifi/setting_section.go` registry:

```go
var settingSections = []settingSection{
	localeSection{},
	globalNatSection{},
	globalSwitchSection{},
}
```

`unifi/setting_resource.go` model (after `GlobalNat`):

```go
	GlobalSwitch  types.Object   `tfsdk:"global_switch"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_globalSwitch|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_global_switch.go unifi/setting_section_global_switch_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add global_switch section"
```

(Body should note: raw-merge preserves live fields absent from go-unifi's struct, e.g. link_debounce; the three isolation attributes align with filipowm's unifi_setting_global_switch.)

---

### Task 4: Acceptance tests against the docker demo controller

**Files:**
- Modify: `unifi/setting_section_locale_test.go`, `unifi/setting_section_global_nat_test.go`, `unifi/setting_section_global_switch_test.go` (append acceptance tests)

**Interfaces:**
- Consumes: `preCheck(t)`, `testAccProtoV6ProviderFactories` (see `unifi/provider_test.go`), `resource.Test` from terraform-plugin-testing.

- [ ] **Step 1: Append acceptance tests**

To `unifi/setting_section_locale_test.go` (add imports `"github.com/hashicorp/terraform-plugin-testing/helper/resource"`):

```go
func TestAccSettingResource_locale(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_locale("America/Vancouver"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "locale.timezone", "America/Vancouver",
				),
			},
			{
				Config: testAccSettingConfig_locale("Etc/UTC"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "locale.timezone", "Etc/UTC",
				),
			},
		},
	})
}

func testAccSettingConfig_locale(tz string) string {
	return `
resource "unifi_setting" "test" {
  locale = {
    timezone = "` + tz + `"
  }
}
`
}
```

To `unifi/setting_section_global_nat_test.go`:

```go
func TestAccSettingResource_globalNat(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_globalNat("auto"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "global_nat.mode", "auto",
				),
			},
			{
				Config: testAccSettingConfig_globalNat("off"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "global_nat.mode", "off",
				),
			},
		},
	})
}

func testAccSettingConfig_globalNat(mode string) string {
	return `
resource "unifi_setting" "test" {
  global_nat = {
    mode = "` + mode + `"
  }
}
`
}
```

To `unifi/setting_section_global_switch_test.go`:

```go
func TestAccSettingResource_globalSwitch(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_globalSwitch(true, "rstp"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "global_switch.jumboframe_enabled", "true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "global_switch.stp_version", "rstp",
					),
				),
			},
			{
				Config: testAccSettingConfig_globalSwitch(false, "stp"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "global_switch.jumboframe_enabled", "false",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "global_switch.stp_version", "stp",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_globalSwitch(jumbo bool, stp string) string {
	return fmt.Sprintf(`
resource "unifi_setting" "test" {
  global_switch = {
    jumboframe_enabled = %t
    stp_version        = %q
  }
}
`, jumbo, stp)
}
```

(Add `"fmt"` to that file's imports.)

- [ ] **Step 2: Start the demo controller and run**

Check `unifi/provider_test.go` `preCheck` for the exact env vars, then:

```bash
docker compose up -d
# wait for healthy:
docker compose ps
```

Run: `TF_ACC=1 go test ./unifi/ -run 'TestAccSettingResource_(locale|globalNat|globalSwitch)' -v -count=1 -timeout 10m` (with the `UNIFI_API`/`UNIFI_USERNAME`/`UNIFI_PASSWORD`/`UNIFI_INSECURE` values `preCheck` expects — read them from `unifi/provider_test.go` / CI workflow, do not guess).
Expected: PASS.

**Contingency:** if the demo controller rejects a section (e.g. `api.err.InvalidPayload` because the simulated controller lacks it), add a skip-guard at the top of that acceptance test — follow the pattern of any existing `t.Skip` usage in the test suite, with message `"demo controller does not support <key> setting"` — and record which sections were skipped in the final report. Do NOT weaken the unit tests.

- [ ] **Step 3: Commit**

```bash
git add unifi/setting_section_locale_test.go unifi/setting_section_global_nat_test.go unifi/setting_section_global_switch_test.go
git commit -m "test(setting): acceptance coverage for locale, global_nat, global_switch"
```

---

### Task 5: Docs, changelog, final verification

**Files:**
- Modify: `examples/resources/unifi_setting/resource.tf` (add the three sections to the example)
- Modify: `CHANGELOG.md` (Unreleased → Features)
- Generated: `docs/resources/setting.md` (via `go generate ./...`)

- [ ] **Step 1: Extend the example**

Append to `examples/resources/unifi_setting/resource.tf` (read it first; match its commenting style):

```terraform
# Site-wide switching, NAT, and locale settings
resource "unifi_setting" "globals" {
  site = "default"

  global_switch = {
    stp_version        = "rstp"
    jumboframe_enabled = false
    dhcp_snoop         = true
  }

  global_nat = {
    mode = "auto"
  }

  locale = {
    timezone = "America/Vancouver"
  }
}
```

Note: if the existing example file already declares a `unifi_setting` resource for the default site, fold these sections into a distinctly-named example block the way the file already handles multiple examples — two `unifi_setting` resources for one site is fine as an illustration but keep the file's existing convention.

- [ ] **Step 2: Regenerate docs**

Run: `go generate ./...`
Expected: `docs/resources/setting.md` gains `global_switch`, `global_nat`, `locale` attribute documentation. Inspect the diff: `git diff --stat docs/`.

- [ ] **Step 3: Changelog**

Add under `## [Unreleased]` → `### ✨ Features` in `CHANGELOG.md` (match the existing bolded-lede prose style):

```markdown
- **`unifi_setting`: new `global_switch`, `global_nat`, and `locale` sections.** Site-wide switch behavior (STP mode, jumbo frames, flow control, DHCP snooping, 802.1X, device/L3 ACL isolation, switch exclusions), the global NAT mode with per-network exclusions, and the site timezone are now codifiable. Sections are applied with a raw read-modify-write merge: controller fields the SDK does not model (e.g. `link_debounce`) are preserved verbatim instead of being silently dropped, and a failed settings read aborts the apply before anything is written. `acl_device_isolation`, `acl_l3_isolation`, and `switch_exclusions` use the same attribute names as the filipowm provider for config portability.
```

- [ ] **Step 4: Full verification**

Run, in order:

```bash
go build ./...
go vet ./...
go test ./unifi/ -count=1 2>&1 | tail -5
```

Expected: all clean, tests PASS. If the repo has a lint config (`.golangci.yml`), also run `golangci-lint run ./unifi/...` if the tool is installed — do not install tools globally to satisfy this.

- [ ] **Step 5: Commit**

```bash
git add examples/resources/unifi_setting/resource.tf docs/ CHANGELOG.md
git commit -m "docs(setting): document global_switch, global_nat, locale sections"
```

- [ ] **Step 6: STOP — do not push**

The branch stays local. Report completion with: sections added, test results (including any demo-controller skips), and the diff stat. James reviews before anything is posted publicly.

---

## Self-review notes

- Spec coverage (PR 1 scope only): global_switch ✓ (Task 3), global_nat ✓ (Task 2), locale ✓ (Task 1), handler registry + abort-on-read-failure RMW ✓ (Task 1), filipowm name alignment ✓ (Task 3 + global constraint), docker-only acceptance ✓ (Task 4), docs + changelog ✓ (Task 5), no public push ✓ (Task 5 Step 6). `global_network` is correctly absent — gated on the go-unifi PR (separate plan).
- Deviation from spec, intentional: apply uses raw-JSON merge (`ListSettings` + `RawSetting`) instead of typed GET→PUT. Reason: typed round-trips silently drop controller fields missing from generated structs (observed live: `global_switch.link_debounce`). Reads remain typed. The spec's abort-on-GET-failure and overlay-only-set-fields semantics are unchanged.
- Type consistency: `settingSection` method set matches across Tasks 1–3; registry grows `localeSection{}` → `+globalNatSection{}` → `+globalSwitchSection{}`; model fields `Locale`/`GlobalNat`/`GlobalSwitch` referenced by each section's `get`/`set`.
