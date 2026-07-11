# Settings PR 2: connectivity services Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `mdns`, `teleport`, `magic_site_to_site_vpn`, `traffic_flow`, and `ether_lighting` nested sections to the `unifi_setting` resource, on top of the section-handler registry and raw-merge read-modify-write engine introduced in PR 1.

**Architecture:** Each section is one self-contained file implementing PR 1's `settingSection` interface (`key / attrTypes / schemaAttribute / get / set / overlay / read`), appended to the package-level `settingSections` registry. Apply is unchanged: `applySections` lists raw settings once, each section's `overlay` writes **only user-set fields** into the raw `Data` map, then a full-object PUT — controller fields go-unifi doesn't model (live examples: `mdns.enabled_for`, `magic_site_to_site_vpn.public_key`, `ether_lighting.network_defaults`) round-trip untouched. Reads use the typed generated structs, with one exception: `magic_site_to_site_vpn`'s key material (`public_key`, `x_private_key`) is absent from `settings.MagicSiteToSiteVpn`, so that section reads its raw `Data` map via `ListSettings` instead.

**Tech Stack:** Go, terraform-plugin-framework, go-unifi v1.33.43 fork (`unifi/settings` package: `Mdns`, `Teleport`, `MagicSiteToSiteVpn`, `TrafficFlow`, `EtherLighting`, `RawSetting`).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-10-setting-sections-design.md`. This plan is PR 2 of 6.
- **PR 1 is a prerequisite and is assumed merged into the working branch.** This plan consumes PR 1's exact names: the `settingSection` interface, the `settingSections` registry slice in `unifi/setting_section.go`, `applySections`/`readSections`, and the already-wired `Schema`/`Create`/`Update`/`readSettings` iteration. Nothing in `unifi/setting_resource.go` changes except five new model fields.
- **Nothing is pushed or posted publicly. Local branch/commits only; James reviews before any push.**
- Attribute names align with filipowm where the section overlaps: `teleport` uses `enabled` + `subnet` (their name for `subnet_cidr`); `magic_site_to_site_vpn` uses `enabled` (our `public_key`/`private_key` are a superset); `ether_lighting` uses `network_overrides`/`speed_overrides` with nested `network_id`/`speed` + `color_hex` (raw JSON keys are `key`/`raw_color_hex`). `mdns` and `traffic_flow` have no filipowm equivalent and follow go-unifi JSON naming.
- **Sensitive fields:** `magic_site_to_site_vpn` key material is never required in config. `private_key` (raw `x_private_key`) is Optional + Computed + `Sensitive: true`; `public_key` is Computed-only. The overlay never writes `public_key` and writes `x_private_key` only when the user set it. Synthetic values only in tests and docs — never real key material.
- Unordered collections are `types.Set`. Optional+Computed collections get `setplanmodifier.UseStateForUnknown()`; the controller-derived `public_key`/`private_key` strings get `stringplanmodifier.UseStateForUnknown()` (same #338-class churn rationale as PR 1).
- Every section's overlay unit test includes a **preservation assertion**: a field the overlay does not manage (preferably one observed live that go-unifi does not model) survives in the raw data map, like PR 1's `Test_globalSwitchModelToData` does with `link_debounce`.
- Model field names on `settingResourceModel`: `Mdns`, `Teleport`, `MagicSiteToSiteVpn`, `TrafficFlow`, `EtherLighting`.
- Existing sections (the original 13 plus PR 1's three) and their tests are untouched.
- Commit style: conventional commits matching the repo log (`feat(setting): …`), body explains why, `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` trailer.
- All commands run from the repo root: `/Users/jamesb/emdash/worktrees/terraform-provider-unifi/emdash/missing-config-uyrwq`.
- Unit tests: `go test ./unifi/ -run '<pattern>' -count=1`. Acceptance: docker demo controller only (Task 6) — never a live UDM.

---

### Task 1: `mdns` section

**Files:**
- Create: `unifi/setting_section_mdns.go`
- Create: `unifi/setting_section_mdns_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settingSections`, `settings.Mdns` (`CustomServices []SettingMdnsCustomServices{Address, Name string}`, `Mode string // all|auto|custom`, `PredefinedServices []SettingMdnsPredefinedServices{Code string}`), `ui.GetSetting[T]`, `util.StringValueOrNull`.
- Produces: `settingMdnsModel`, `settingMdnsCustomServiceModel`, `mdnsAttrTypes`, `mdnsCustomServiceAttrTypes`, `mdnsSection`, `mdnsModelToData(ctx, m, data, diags)`, `mdnsSettingToModel(ctx, s, diags)`.
- Naming: go-unifi JSON (no filipowm equivalent). `predefined_services` is flattened to a **set of code strings** on the TF side; the raw JSON stays `[{"code": "..."}]` (overlay wraps, read unwraps). Live-only fields `enabled_for` / `enabled_for_network_ids` are NOT exposed (absent from `settings.Mdns`; see self-review) — the raw merge preserves them, and the preservation test proves it.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_mdns_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_mdnsModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	svc, d := types.ObjectValueFrom(ctx, mdnsCustomServiceAttrTypes,
		settingMdnsCustomServiceModel{
			Name:    types.StringValue("Home Assistant"),
			Address: types.StringValue("_home-assistant._tcp"),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	custom, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes},
		[]types.Object{svc})
	if d.HasError() {
		t.Fatal(d)
	}
	predefined, d := types.SetValueFrom(ctx, types.StringType,
		[]string{"apple_airPlay"})
	if d.HasError() {
		t.Fatal(d)
	}

	m := &settingMdnsModel{
		Mode:               types.StringValue("custom"),
		CustomServices:     custom,
		PredefinedServices: predefined,
	}

	// The live controller carries fields go-unifi does not model
	// (enabled_for, enabled_for_network_ids); the raw merge must preserve
	// them verbatim.
	data := map[string]any{
		"enabled_for":             "some",
		"enabled_for_network_ids": []any{"6068a1508bf47808f667f3e8"},
	}

	mdnsModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["mode"] != "custom" {
		t.Fatalf("mode = %v", data["mode"])
	}
	if data["enabled_for"] != "some" {
		t.Fatal("unmodeled enabled_for was clobbered")
	}
	ids, ok := data["enabled_for_network_ids"].([]any)
	if !ok || len(ids) != 1 {
		t.Fatalf("unmodeled enabled_for_network_ids was clobbered: %v",
			data["enabled_for_network_ids"])
	}
	svcs, ok := data["custom_services"].([]map[string]any)
	if !ok || len(svcs) != 1 || svcs[0]["address"] != "_home-assistant._tcp" ||
		svcs[0]["name"] != "Home Assistant" {
		t.Fatalf("custom_services = %v", data["custom_services"])
	}
	codes, ok := data["predefined_services"].([]map[string]any)
	if !ok || len(codes) != 1 || codes[0]["code"] != "apple_airPlay" {
		t.Fatalf("predefined_services = %v", data["predefined_services"])
	}
}

func Test_mdnsModelToData_nullsLeaveRemoteValues(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := &settingMdnsModel{
		Mode: types.StringNull(),
		CustomServices: types.SetNull(
			types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes}),
		PredefinedServices: types.SetNull(types.StringType),
	}
	data := map[string]any{"mode": "all"}

	mdnsModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["mode"] != "all" {
		t.Fatalf("null mode overwrote remote value: %v", data["mode"])
	}
	if _, present := data["custom_services"]; present {
		t.Fatal("null set should not write custom_services")
	}
	if _, present := data["predefined_services"]; present {
		t.Fatal("null set should not write predefined_services")
	}
}

func Test_mdnsSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := mdnsSettingToModel(ctx, &settings.Mdns{
		Mode: "custom",
		CustomServices: []settings.SettingMdnsCustomServices{
			{Name: "Home Assistant", Address: "_home-assistant._tcp"},
		},
		PredefinedServices: []settings.SettingMdnsPredefinedServices{
			{Code: "sonos"},
		},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if m.Mode.ValueString() != "custom" {
		t.Fatalf("mode = %v", m.Mode)
	}
	var svcs []settingMdnsCustomServiceModel
	diags.Append(m.CustomServices.ElementsAs(ctx, &svcs, false)...)
	if len(svcs) != 1 || svcs[0].Address.ValueString() != "_home-assistant._tcp" {
		t.Fatalf("custom_services = %v", svcs)
	}
	var codes []string
	diags.Append(m.PredefinedServices.ElementsAs(ctx, &codes, false)...)
	if len(codes) != 1 || codes[0] != "sonos" {
		t.Fatalf("predefined_services = %v", codes)
	}

	empty := mdnsSettingToModel(ctx, &settings.Mdns{}, &diags)
	if !empty.Mode.IsNull() {
		t.Fatalf("empty mode should map to null, got %v", empty.Mode)
	}
}

func Test_settingResource_Schema_mdns(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["mdns"]; !ok {
		t.Fatal("schema is missing the mdns section attribute")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_mdns|Test_settingResource_Schema_mdns' -count=1`
Expected: compile FAILURE — `undefined: settingMdnsModel`, `undefined: mdnsModelToData`, etc.

- [ ] **Step 3: Create `unifi/setting_section_mdns.go`**

```go
package unifi

import (
	"context"
	"errors"
	"regexp"

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

var mdnsCustomServiceAddressRegexp = regexp.MustCompile(
	`^_[a-zA-Z0-9._-]+\._(tcp|udp)(\.local)?$`)

// settingMdnsModel is the nested mdns block: the multicast-DNS repeater
// service selection. Naming follows the controller's JSON (no filipowm
// equivalent exists). predefined_services is a set of service codes on the
// Terraform side; the raw JSON shape [{"code": "..."}] is wrapped/unwrapped
// in the converters. The controller's network scoping fields (enabled_for,
// enabled_for_network_ids) are not modeled by go-unifi yet; the raw merge
// preserves them. TODO(go-unifi): expose them once the structs gain them.
type settingMdnsModel struct {
	Mode               types.String `tfsdk:"mode"`
	CustomServices     types.Set    `tfsdk:"custom_services"`
	PredefinedServices types.Set    `tfsdk:"predefined_services"`
}

type settingMdnsCustomServiceModel struct {
	Name    types.String `tfsdk:"name"`
	Address types.String `tfsdk:"address"`
}

var (
	mdnsCustomServiceAttrTypes = map[string]attr.Type{
		"name":    types.StringType,
		"address": types.StringType,
	}
	mdnsAttrTypes = map[string]attr.Type{
		"mode": types.StringType,
		"custom_services": types.SetType{
			ElemType: types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes},
		},
		"predefined_services": types.SetType{ElemType: types.StringType},
	}
)

type mdnsSection struct{}

func (mdnsSection) key() string { return "mdns" }

func (mdnsSection) attrTypes() map[string]attr.Type { return mdnsAttrTypes }

func (mdnsSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Multicast DNS (mDNS) repeater settings: which " +
			"services are forwarded between networks. Controller-managed " +
			"network scoping fields not exposed here are preserved across " +
			"updates.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"mode": schema.StringAttribute{
				MarkdownDescription: "Service selection mode: `all`, `auto`, or `custom`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("all", "auto", "custom"),
				},
			},
			"custom_services": schema.SetNestedAttribute{
				MarkdownDescription: "Custom mDNS service definitions (used with `mode = \"custom\"`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Display name of the service.",
							Required:            true,
						},
						"address": schema.StringAttribute{
							MarkdownDescription: "mDNS service address, e.g. `_home-assistant._tcp`.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(
									mdnsCustomServiceAddressRegexp,
									"must look like `_name._tcp`, `_name._udp`, or with a `.local` suffix",
								),
							},
						},
					},
				},
			},
			"predefined_services": schema.SetAttribute{
				MarkdownDescription: "Predefined service codes to forward (used with " +
					"`mode = \"custom\"`), e.g. `apple_airPlay`, `google_chromecast`, " +
					"`printers`, `sonos`, `homeKit`, `matter_network`.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (mdnsSection) get(m *settingResourceModel) types.Object { return m.Mdns }

func (mdnsSection) set(m *settingResourceModel, obj types.Object) { m.Mdns = obj }

func (mdnsSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingMdnsModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	mdnsModelToData(ctx, &m, data, &diags)
	return diags
}

// mdnsModelToData writes only the user-set fields into the raw section
// document; unset fields — including controller fields go-unifi does not
// model, like enabled_for — keep their remote values.
func mdnsModelToData(
	ctx context.Context,
	m *settingMdnsModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.Mode.IsNull() && !m.Mode.IsUnknown() {
		data["mode"] = m.Mode.ValueString()
	}
	if !m.CustomServices.IsNull() && !m.CustomServices.IsUnknown() {
		var svcs []settingMdnsCustomServiceModel
		diags.Append(m.CustomServices.ElementsAs(ctx, &svcs, false)...)
		out := make([]map[string]any, 0, len(svcs))
		for _, svc := range svcs {
			out = append(out, map[string]any{
				"name":    svc.Name.ValueString(),
				"address": svc.Address.ValueString(),
			})
		}
		data["custom_services"] = out
	}
	if !m.PredefinedServices.IsNull() && !m.PredefinedServices.IsUnknown() {
		var codes []string
		diags.Append(m.PredefinedServices.ElementsAs(ctx, &codes, false)...)
		out := make([]map[string]any, 0, len(codes))
		for _, code := range codes {
			out = append(out, map[string]any{"code": code})
		}
		data["predefined_services"] = out
	}
}

func (mdnsSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Mdns](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(mdnsAttrTypes), diags
		}
		diags.AddError("Error Reading mDNS Setting", err.Error())
		return types.ObjectNull(mdnsAttrTypes), diags
	}
	model := mdnsSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(mdnsAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, mdnsAttrTypes, model)
}

func mdnsSettingToModel(
	ctx context.Context,
	s *settings.Mdns,
	diags *diag.Diagnostics,
) settingMdnsModel {
	svcs := make([]settingMdnsCustomServiceModel, 0, len(s.CustomServices))
	for _, svc := range s.CustomServices {
		svcs = append(svcs, settingMdnsCustomServiceModel{
			Name:    util.StringValueOrNull(svc.Name),
			Address: util.StringValueOrNull(svc.Address),
		})
	}
	custom, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes}, svcs)
	diags.Append(d...)

	codes := make([]string, 0, len(s.PredefinedServices))
	for _, svc := range s.PredefinedServices {
		codes = append(codes, svc.Code)
	}
	predefined, d := types.SetValueFrom(ctx, types.StringType, codes)
	diags.Append(d...)

	return settingMdnsModel{
		Mode:               util.StringValueOrNull(s.Mode),
		CustomServices:     custom,
		PredefinedServices: predefined,
	}
}
```

- [ ] **Step 4: Register the section**

In `unifi/setting_section.go`, extend the registry (after PR 1's entries):

```go
var settingSections = []settingSection{
	localeSection{},
	globalNatSection{},
	globalSwitchSection{},
	mdnsSection{},
}
```

In `unifi/setting_resource.go`, add to `settingResourceModel` (after `GlobalSwitch`):

```go
	Mdns          types.Object   `tfsdk:"mdns"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_mdns|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_mdns.go unifi/setting_section_mdns_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add mdns section"
```

(Body should note: predefined_services flattened to codes; enabled_for/enabled_for_network_ids observed live but unmodeled by go-unifi — preserved by the raw merge, exposure gated on a go-unifi bump.)

---

### Task 2: `teleport` section

**Files:**
- Create: `unifi/setting_section_teleport.go`
- Create: `unifi/setting_section_teleport_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settings.Teleport` (`Enabled bool`, `SubnetCidr string // CIDR or empty`), `ui.GetSetting[T]`.
- Produces: `settingTeleportModel`, `teleportAttrTypes`, `teleportSection`, `teleportModelToData(m, data)`, `teleportSettingToModel(s)`.
- filipowm-aligned names: `enabled`, `subnet` (their rename of the raw `subnet_cidr`; raw JSON key stays `subnet_cidr`). Note: `subnet` reads back as `types.StringValue` **always** (never null) — an empty subnet is a meaningful controller value ("auto"), and mapping `""`→null would make an explicit `subnet = ""` in config inconsistent after apply.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_teleport_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_teleportModelToData(t *testing.T) {
	m := &settingTeleportModel{
		Enabled: types.BoolValue(true),
		Subnet:  types.StringValue("192.168.100.0/24"),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": "keep"}

	teleportModelToData(m, data)

	if data["enabled"] != true {
		t.Fatalf("enabled = %v", data["enabled"])
	}
	if data["subnet_cidr"] != "192.168.100.0/24" {
		t.Fatalf("subnet_cidr = %v", data["subnet_cidr"])
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_teleportModelToData_nullsLeaveRemoteValues(t *testing.T) {
	m := &settingTeleportModel{
		Enabled: types.BoolNull(),
		Subnet:  types.StringNull(),
	}
	data := map[string]any{"enabled": true, "subnet_cidr": "192.168.2.1/24"}

	teleportModelToData(m, data)

	if data["enabled"] != true {
		t.Fatalf("null enabled overwrote remote value: %v", data["enabled"])
	}
	if data["subnet_cidr"] != "192.168.2.1/24" {
		t.Fatalf("null subnet overwrote remote value: %v", data["subnet_cidr"])
	}
}

func Test_teleportSettingToModel(t *testing.T) {
	m := teleportSettingToModel(&settings.Teleport{
		Enabled:    true,
		SubnetCidr: "192.168.2.1/24",
	})
	if !m.Enabled.ValueBool() {
		t.Fatalf("enabled = %v", m.Enabled)
	}
	if m.Subnet.ValueString() != "192.168.2.1/24" {
		t.Fatalf("subnet = %v", m.Subnet)
	}

	// Empty subnet is meaningful ("auto") and must read back as "" — not
	// null — so an explicit subnet = "" in config stays consistent.
	empty := teleportSettingToModel(&settings.Teleport{})
	if empty.Subnet.IsNull() || empty.Subnet.ValueString() != "" {
		t.Fatalf("empty subnet should be \"\", got %v", empty.Subnet)
	}
	if empty.Enabled.ValueBool() {
		t.Fatalf("enabled should be false, got %v", empty.Enabled)
	}
}

func Test_settingResource_Schema_teleport(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["teleport"]; !ok {
		t.Fatal("schema is missing the teleport section attribute")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_teleport|Test_settingResource_Schema_teleport' -count=1`
Expected: compile FAILURE — `undefined: settingTeleportModel` etc.

- [ ] **Step 3: Create `unifi/setting_section_teleport.go`**

```go
package unifi

import (
	"context"
	"errors"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// teleportSubnetRegexp is the controller's own validation: an IPv4 CIDR
// with a /8–/32 prefix, or empty (auto).
var teleportSubnetRegexp = regexp.MustCompile(
	`^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}` +
		`([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])` +
		`/([8-9]|[1-2][0-9]|3[0-2])$|^$`)

// settingTeleportModel is the nested teleport block: UniFi's WireGuard-based
// one-click remote access. Attribute names align with filipowm's
// unifi_setting_teleport (`subnet` is their rename of the raw subnet_cidr).
type settingTeleportModel struct {
	Enabled types.Bool   `tfsdk:"enabled"`
	Subnet  types.String `tfsdk:"subnet"`
}

var teleportAttrTypes = map[string]attr.Type{
	"enabled": types.BoolType,
	"subnet":  types.StringType,
}

type teleportSection struct{}

func (teleportSection) key() string { return "teleport" }

func (teleportSection) attrTypes() map[string]attr.Type { return teleportAttrTypes }

func (teleportSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Teleport (one-click WireGuard remote access) " +
			"settings. Requires controller version 7.2 or later.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether Teleport is enabled.",
				Optional:            true,
				Computed:            true,
			},
			"subnet": schema.StringAttribute{
				MarkdownDescription: "Subnet CIDR used for Teleport clients " +
					"(e.g. `192.168.100.0/24`). Empty string means the " +
					"controller chooses automatically.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						teleportSubnetRegexp,
						"must be an IPv4 CIDR (/8–/32) or empty",
					),
				},
			},
		},
	}
}

func (teleportSection) get(m *settingResourceModel) types.Object { return m.Teleport }

func (teleportSection) set(m *settingResourceModel, obj types.Object) { m.Teleport = obj }

func (teleportSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingTeleportModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	teleportModelToData(&m, data)
	return diags
}

// teleportModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values.
func teleportModelToData(m *settingTeleportModel, data map[string]any) {
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		data["enabled"] = m.Enabled.ValueBool()
	}
	if !m.Subnet.IsNull() && !m.Subnet.IsUnknown() {
		data["subnet_cidr"] = m.Subnet.ValueString()
	}
}

func (teleportSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Teleport](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(teleportAttrTypes), diags
		}
		diags.AddError("Error Reading Teleport Setting", err.Error())
		return types.ObjectNull(teleportAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, teleportAttrTypes, teleportSettingToModel(setting))
}

func teleportSettingToModel(s *settings.Teleport) settingTeleportModel {
	return settingTeleportModel{
		Enabled: types.BoolValue(s.Enabled),
		// Deliberately StringValue, not StringValueOrNull: "" is the
		// controller's "auto" value and must round-trip as "".
		Subnet: types.StringValue(s.SubnetCidr),
	}
}
```

- [ ] **Step 4: Register the section**

`unifi/setting_section.go` registry — append `teleportSection{}` after `mdnsSection{}`.

`unifi/setting_resource.go` model (after `Mdns`):

```go
	Teleport      types.Object   `tfsdk:"teleport"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_teleport|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_teleport.go unifi/setting_section_teleport_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add teleport section"
```

---

### Task 3: `magic_site_to_site_vpn` section

**Files:**
- Create: `unifi/setting_section_magic_site_to_site_vpn.go`
- Create: `unifi/setting_section_magic_site_to_site_vpn_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `Client.ListSettings` (raw read — `settings.MagicSiteToSiteVpn` models only `Enabled`, but the live section carries `public_key` and `x_private_key`, which the schema exposes), `util.StringValueOrNull`.
- Produces: `settingMagicSiteToSiteVpnModel`, `magicSiteToSiteVpnAttrTypes`, `magicSiteToSiteVpnSection`, `magicSiteToSiteVpnModelToData(m, data)`, `magicSiteToSiteVpnDataToModel(data)`.
- filipowm-aligned name: `enabled`. Superset: `public_key` (Computed-only, controller-derived), `private_key` (Optional + Computed + **Sensitive**; raw key `x_private_key`, following this provider's convention of dropping the `x_` secret prefix, cf. `mgmt.ssh_password`). The overlay **never** writes `public_key`; it writes `x_private_key` only when the user set it. Read is raw (via `ListSettings`) so the computed key fields populate — a typed read cannot see them.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_magic_site_to_site_vpn_test.go` (synthetic key strings only — never real key material):

```go
package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func Test_magicSiteToSiteVpnModelToData(t *testing.T) {
	m := &settingMagicSiteToSiteVpnModel{
		Enabled:    types.BoolValue(true),
		PublicKey:  types.StringValue("attacker-controlled-should-be-ignored"),
		PrivateKey: types.StringValue("synthetic-private-key"),
	}
	// The controller-generated key pair lives in the raw document; the
	// overlay must preserve public_key verbatim (it is derived, never
	// written) and any other unmodeled fields.
	data := map[string]any{
		"public_key":      "synthetic-public-key",
		"unmodeled_field": "keep",
	}

	magicSiteToSiteVpnModelToData(m, data)

	if data["enabled"] != true {
		t.Fatalf("enabled = %v", data["enabled"])
	}
	if data["x_private_key"] != "synthetic-private-key" {
		t.Fatalf("x_private_key = %v", data["x_private_key"])
	}
	if data["public_key"] != "synthetic-public-key" {
		t.Fatal("computed public_key must never be overwritten by the model")
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_magicSiteToSiteVpnModelToData_nullsLeaveRemoteValues(t *testing.T) {
	m := &settingMagicSiteToSiteVpnModel{
		Enabled:    types.BoolNull(),
		PublicKey:  types.StringNull(),
		PrivateKey: types.StringNull(),
	}
	data := map[string]any{
		"enabled":       true,
		"public_key":    "synthetic-public-key",
		"x_private_key": "synthetic-private-key",
	}

	magicSiteToSiteVpnModelToData(m, data)

	if data["enabled"] != true {
		t.Fatalf("null enabled overwrote remote value: %v", data["enabled"])
	}
	if data["x_private_key"] != "synthetic-private-key" {
		t.Fatal("null private_key must not touch the controller-generated key")
	}
	if data["public_key"] != "synthetic-public-key" {
		t.Fatal("public_key was clobbered")
	}
}

func Test_magicSiteToSiteVpnDataToModel(t *testing.T) {
	m := magicSiteToSiteVpnDataToModel(map[string]any{
		"enabled":       true,
		"public_key":    "synthetic-public-key",
		"x_private_key": "synthetic-private-key",
	})
	if !m.Enabled.ValueBool() {
		t.Fatalf("enabled = %v", m.Enabled)
	}
	if m.PublicKey.ValueString() != "synthetic-public-key" {
		t.Fatalf("public_key = %v", m.PublicKey)
	}
	if m.PrivateKey.ValueString() != "synthetic-private-key" {
		t.Fatalf("private_key = %v", m.PrivateKey)
	}

	empty := magicSiteToSiteVpnDataToModel(map[string]any{})
	if empty.Enabled.ValueBool() {
		t.Fatalf("missing enabled should be false, got %v", empty.Enabled)
	}
	if !empty.PublicKey.IsNull() || !empty.PrivateKey.IsNull() {
		t.Fatalf("missing keys should map to null, got %v / %v",
			empty.PublicKey, empty.PrivateKey)
	}
}

func Test_settingResource_Schema_magicSiteToSiteVpn(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	sect, ok := resp.Schema.Attributes["magic_site_to_site_vpn"].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatal("schema is missing the magic_site_to_site_vpn section attribute")
	}
	pk, ok := sect.Attributes["private_key"].(schema.StringAttribute)
	if !ok {
		t.Fatal("magic_site_to_site_vpn is missing private_key")
	}
	if !pk.Sensitive {
		t.Fatal("private_key must be Sensitive")
	}
	if pk.Required {
		t.Fatal("private_key must never be required")
	}
	pub, ok := sect.Attributes["public_key"].(schema.StringAttribute)
	if !ok {
		t.Fatal("magic_site_to_site_vpn is missing public_key")
	}
	if pub.Optional || pub.Required || !pub.Computed {
		t.Fatal("public_key must be Computed-only")
	}
}
```

(The schema test type-asserts the framework's concrete `schema.SingleNestedAttribute` / `schema.StringAttribute` to check `Sensitive`/`Computed` structurally — hence the extra `resource/schema` import in this test file.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_magicSiteToSiteVpn|Test_settingResource_Schema_magicSiteToSiteVpn' -count=1`
Expected: compile FAILURE — `undefined: settingMagicSiteToSiteVpnModel` etc.

- [ ] **Step 3: Create `unifi/setting_section_magic_site_to_site_vpn.go`**

```go
package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingMagicSiteToSiteVpnModel is the nested magic_site_to_site_vpn block:
// UniFi's Site Magic WireGuard mesh. `enabled` aligns with filipowm's
// unifi_setting_magic_site_to_site_vpn; the key pair is our superset.
// go-unifi's typed struct models only `enabled`, so this section reads the
// raw settings document to surface the controller-generated key material.
type settingMagicSiteToSiteVpnModel struct {
	Enabled    types.Bool   `tfsdk:"enabled"`
	PublicKey  types.String `tfsdk:"public_key"`
	PrivateKey types.String `tfsdk:"private_key"`
}

var magicSiteToSiteVpnAttrTypes = map[string]attr.Type{
	"enabled":     types.BoolType,
	"public_key":  types.StringType,
	"private_key": types.StringType,
}

type magicSiteToSiteVpnSection struct{}

func (magicSiteToSiteVpnSection) key() string { return "magic_site_to_site_vpn" }

func (magicSiteToSiteVpnSection) attrTypes() map[string]attr.Type {
	return magicSiteToSiteVpnAttrTypes
}

func (magicSiteToSiteVpnSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site Magic site-to-site VPN (WireGuard mesh) " +
			"settings. The controller generates the key pair when the " +
			"feature is first enabled; leave `private_key` unset to keep " +
			"the controller-managed keys.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the Site Magic site-to-site VPN is enabled.",
				Optional:            true,
				Computed:            true,
			},
			"public_key": schema.StringAttribute{
				MarkdownDescription: "WireGuard public key, derived by the " +
					"controller from the private key. Read-only.",
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"private_key": schema.StringAttribute{
				MarkdownDescription: "WireGuard private key. Controller-" +
					"generated unless explicitly set; never required.",
				Optional:  true,
				Computed:  true,
				Sensitive: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (magicSiteToSiteVpnSection) get(m *settingResourceModel) types.Object {
	return m.MagicSiteToSiteVpn
}

func (magicSiteToSiteVpnSection) set(m *settingResourceModel, obj types.Object) {
	m.MagicSiteToSiteVpn = obj
}

func (magicSiteToSiteVpnSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingMagicSiteToSiteVpnModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	magicSiteToSiteVpnModelToData(&m, data)
	return diags
}

// magicSiteToSiteVpnModelToData writes only the user-set fields into the raw
// section document. public_key is controller-derived and is NEVER written;
// x_private_key is written only when set (either by the user, or read back
// into state on a previous apply — re-sending the value the GET just
// returned is a no-op for the controller).
func magicSiteToSiteVpnModelToData(
	m *settingMagicSiteToSiteVpnModel,
	data map[string]any,
) {
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		data["enabled"] = m.Enabled.ValueBool()
	}
	if !m.PrivateKey.IsNull() && !m.PrivateKey.IsUnknown() {
		data["x_private_key"] = m.PrivateKey.ValueString()
	}
}

// read uses the raw settings list rather than the typed struct: go-unifi's
// MagicSiteToSiteVpn models only `enabled`, but the schema's computed
// public_key/private_key must be populated from the live document.
func (magicSiteToSiteVpnSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	raws, err := client.ListSettings(ctx, site)
	if err != nil {
		diags.AddError("Error Reading Magic Site-to-Site VPN Setting", err.Error())
		return types.ObjectNull(magicSiteToSiteVpnAttrTypes), diags
	}
	for _, raw := range raws {
		if raw.GetKey() != "magic_site_to_site_vpn" {
			continue
		}
		return types.ObjectValueFrom(ctx, magicSiteToSiteVpnAttrTypes,
			magicSiteToSiteVpnDataToModel(raw.Data))
	}
	return types.ObjectNull(magicSiteToSiteVpnAttrTypes), diags
}

func magicSiteToSiteVpnDataToModel(
	data map[string]any,
) settingMagicSiteToSiteVpnModel {
	enabled, _ := data["enabled"].(bool)
	publicKey, _ := data["public_key"].(string)
	privateKey, _ := data["x_private_key"].(string)
	return settingMagicSiteToSiteVpnModel{
		Enabled:    types.BoolValue(enabled),
		PublicKey:  util.StringValueOrNull(publicKey),
		PrivateKey: util.StringValueOrNull(privateKey),
	}
}
```

- [ ] **Step 4: Register the section**

`unifi/setting_section.go` registry — append `magicSiteToSiteVpnSection{}` after `teleportSection{}`.

`unifi/setting_resource.go` model (after `Teleport`):

```go
	MagicSiteToSiteVpn types.Object `tfsdk:"magic_site_to_site_vpn"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_magicSiteToSiteVpn|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_magic_site_to_site_vpn.go unifi/setting_section_magic_site_to_site_vpn_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add magic_site_to_site_vpn section"
```

(Body should note: raw read because go-unifi models only `enabled`; key material is sensitive/computed, never required, never regenerated by the provider.)

---

### Task 4: `traffic_flow` section

**Files:**
- Create: `unifi/setting_section_traffic_flow.go`
- Create: `unifi/setting_section_traffic_flow_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settings.TrafficFlow` (`EnabledAllowedTraffic bool`, `GatewayDNSEnabled bool`, `UnifiDeviceManagementEnabled bool`, `UnifiServicesEnabled bool`), `ui.GetSetting[T]`.
- Produces: `settingTrafficFlowModel`, `trafficFlowAttrTypes`, `trafficFlowSection`, `trafficFlowModelToData(m, data)`, `trafficFlowSettingToModel(s)`.
- Naming: go-unifi JSON (no filipowm equivalent): `enabled_allowed_traffic`, `gateway_dns_enabled`, `unifi_device_management_enabled`, `unifi_services_enabled`.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_traffic_flow_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_trafficFlowModelToData(t *testing.T) {
	m := &settingTrafficFlowModel{
		EnabledAllowedTraffic:        types.BoolValue(true),
		GatewayDNSEnabled:            types.BoolValue(false),
		UnifiDeviceManagementEnabled: types.BoolNull(),
		UnifiServicesEnabled:         types.BoolValue(true),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{
		"unmodeled_field":                 "keep",
		"unifi_device_management_enabled": true,
	}

	trafficFlowModelToData(m, data)

	if data["enabled_allowed_traffic"] != true {
		t.Fatalf("enabled_allowed_traffic = %v", data["enabled_allowed_traffic"])
	}
	if data["gateway_dns_enabled"] != false {
		t.Fatalf("gateway_dns_enabled = %v", data["gateway_dns_enabled"])
	}
	if data["unifi_services_enabled"] != true {
		t.Fatalf("unifi_services_enabled = %v", data["unifi_services_enabled"])
	}
	if data["unifi_device_management_enabled"] != true {
		t.Fatal("null unifi_device_management_enabled overwrote remote value")
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_trafficFlowSettingToModel(t *testing.T) {
	m := trafficFlowSettingToModel(&settings.TrafficFlow{
		EnabledAllowedTraffic:        true,
		GatewayDNSEnabled:            true,
		UnifiDeviceManagementEnabled: false,
		UnifiServicesEnabled:         true,
	})
	if !m.EnabledAllowedTraffic.ValueBool() || !m.GatewayDNSEnabled.ValueBool() ||
		!m.UnifiServicesEnabled.ValueBool() {
		t.Fatalf("bools not mapped: %+v", m)
	}
	if m.UnifiDeviceManagementEnabled.ValueBool() {
		t.Fatalf("unifi_device_management_enabled = %v", m.UnifiDeviceManagementEnabled)
	}
}

func Test_settingResource_Schema_trafficFlow(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["traffic_flow"]; !ok {
		t.Fatal("schema is missing the traffic_flow section attribute")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_trafficFlow|Test_settingResource_Schema_trafficFlow' -count=1`
Expected: compile FAILURE — `undefined: settingTrafficFlowModel` etc.

- [ ] **Step 3: Create `unifi/setting_section_traffic_flow.go`**

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
)

// settingTrafficFlowModel is the nested traffic_flow block: what the
// controller's Traffic Flows insights record. Naming follows the
// controller's JSON (no filipowm equivalent exists).
type settingTrafficFlowModel struct {
	EnabledAllowedTraffic        types.Bool `tfsdk:"enabled_allowed_traffic"`
	GatewayDNSEnabled            types.Bool `tfsdk:"gateway_dns_enabled"`
	UnifiDeviceManagementEnabled types.Bool `tfsdk:"unifi_device_management_enabled"`
	UnifiServicesEnabled         types.Bool `tfsdk:"unifi_services_enabled"`
}

var trafficFlowAttrTypes = map[string]attr.Type{
	"enabled_allowed_traffic":         types.BoolType,
	"gateway_dns_enabled":             types.BoolType,
	"unifi_device_management_enabled": types.BoolType,
	"unifi_services_enabled":          types.BoolType,
}

type trafficFlowSection struct{}

func (trafficFlowSection) key() string { return "traffic_flow" }

func (trafficFlowSection) attrTypes() map[string]attr.Type { return trafficFlowAttrTypes }

func (trafficFlowSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Traffic Flows recording settings: which flow " +
			"classes the controller records for insights.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled_allowed_traffic": schema.BoolAttribute{
				MarkdownDescription: "Record allowed traffic flows (not just blocked ones).",
				Optional:            true,
				Computed:            true,
			},
			"gateway_dns_enabled": schema.BoolAttribute{
				MarkdownDescription: "Record gateway DNS queries.",
				Optional:            true,
				Computed:            true,
			},
			"unifi_device_management_enabled": schema.BoolAttribute{
				MarkdownDescription: "Record management traffic between UniFi devices.",
				Optional:            true,
				Computed:            true,
			},
			"unifi_services_enabled": schema.BoolAttribute{
				MarkdownDescription: "Record traffic to UniFi cloud services.",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func (trafficFlowSection) get(m *settingResourceModel) types.Object { return m.TrafficFlow }

func (trafficFlowSection) set(m *settingResourceModel, obj types.Object) { m.TrafficFlow = obj }

func (trafficFlowSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingTrafficFlowModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	trafficFlowModelToData(&m, data)
	return diags
}

// trafficFlowModelToData writes only the user-set fields into the raw
// section document; unset fields keep their remote values.
func trafficFlowModelToData(m *settingTrafficFlowModel, data map[string]any) {
	if !m.EnabledAllowedTraffic.IsNull() && !m.EnabledAllowedTraffic.IsUnknown() {
		data["enabled_allowed_traffic"] = m.EnabledAllowedTraffic.ValueBool()
	}
	if !m.GatewayDNSEnabled.IsNull() && !m.GatewayDNSEnabled.IsUnknown() {
		data["gateway_dns_enabled"] = m.GatewayDNSEnabled.ValueBool()
	}
	if !m.UnifiDeviceManagementEnabled.IsNull() && !m.UnifiDeviceManagementEnabled.IsUnknown() {
		data["unifi_device_management_enabled"] = m.UnifiDeviceManagementEnabled.ValueBool()
	}
	if !m.UnifiServicesEnabled.IsNull() && !m.UnifiServicesEnabled.IsUnknown() {
		data["unifi_services_enabled"] = m.UnifiServicesEnabled.ValueBool()
	}
}

func (trafficFlowSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.TrafficFlow](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(trafficFlowAttrTypes), diags
		}
		diags.AddError("Error Reading Traffic Flow Setting", err.Error())
		return types.ObjectNull(trafficFlowAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, trafficFlowAttrTypes, trafficFlowSettingToModel(setting))
}

func trafficFlowSettingToModel(s *settings.TrafficFlow) settingTrafficFlowModel {
	return settingTrafficFlowModel{
		EnabledAllowedTraffic:        types.BoolValue(s.EnabledAllowedTraffic),
		GatewayDNSEnabled:            types.BoolValue(s.GatewayDNSEnabled),
		UnifiDeviceManagementEnabled: types.BoolValue(s.UnifiDeviceManagementEnabled),
		UnifiServicesEnabled:         types.BoolValue(s.UnifiServicesEnabled),
	}
}
```

- [ ] **Step 4: Register the section**

`unifi/setting_section.go` registry — append `trafficFlowSection{}` after `magicSiteToSiteVpnSection{}`.

`unifi/setting_resource.go` model (after `MagicSiteToSiteVpn`):

```go
	TrafficFlow   types.Object   `tfsdk:"traffic_flow"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_trafficFlow|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_traffic_flow.go unifi/setting_section_traffic_flow_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add traffic_flow section"
```

---

### Task 5: `ether_lighting` section

**Files:**
- Create: `unifi/setting_section_ether_lighting.go`
- Create: `unifi/setting_section_ether_lighting_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settings.EtherLighting` (`NetworkOverrides []SettingEtherLightingNetworkOverrides{Key, RawColorHex string}`, `SpeedOverrides []SettingEtherLightingSpeedOverrides{Key, RawColorHex string}`), `ui.GetSetting[T]`.
- Produces: `settingEtherLightingModel`, `settingEtherLightingNetworkOverrideModel`, `settingEtherLightingSpeedOverrideModel`, `etherLightingAttrTypes`, `etherLightingNetworkOverrideAttrTypes`, `etherLightingSpeedOverrideAttrTypes`, `etherLightingSection`, `etherLightingModelToData(ctx, m, data, diags)`, `etherLightingSettingToModel(ctx, s, diags)`.
- filipowm-aligned names (identical): `network_overrides` (`network_id`, `color_hex`), `speed_overrides` (`speed`, `color_hex`). Raw JSON keys stay `key`/`raw_color_hex`. The overlay carries filipowm's duplicate-key guard: a set of objects only dedupes whole objects, so two entries sharing a `network_id`/`speed` with different colors must be rejected with a diagnostic. The controller-managed `network_defaults`/`speed_defaults` arrays (observed live, unmodeled by go-unifi) are preserved by the raw merge.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_ether_lighting_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func etherLightingSpeedOverrideSet(
	t *testing.T, ctx context.Context,
	overrides []settingEtherLightingSpeedOverrideModel,
) types.Set {
	t.Helper()
	set, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: etherLightingSpeedOverrideAttrTypes}, overrides)
	if d.HasError() {
		t.Fatal(d)
	}
	return set
}

func Test_etherLightingModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	network, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes},
		[]settingEtherLightingNetworkOverrideModel{{
			NetworkID: types.StringValue("5dbaa47ea7986c04d72d4f5e"),
			ColorHex:  types.StringValue("0544ff"),
		}})
	if d.HasError() {
		t.Fatal(d)
	}
	speed := etherLightingSpeedOverrideSet(t, ctx,
		[]settingEtherLightingSpeedOverrideModel{{
			Speed:    types.StringValue("GbE"),
			ColorHex: types.StringValue("ff6c14"),
		}})

	m := &settingEtherLightingModel{
		NetworkOverrides: network,
		SpeedOverrides:   speed,
	}

	// The live controller carries default palettes go-unifi does not model
	// (network_defaults, speed_defaults); the raw merge must preserve them.
	data := map[string]any{
		"network_defaults": []any{map[string]any{
			"key": "5dbaa47ea7986c04d72d4f5e", "raw_color_hex": "0544ff",
		}},
		"speed_defaults": []any{map[string]any{
			"key": "10M", "raw_color_hex": "FFC105",
		}},
	}

	etherLightingModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if _, present := data["network_defaults"]; !present {
		t.Fatal("unmodeled network_defaults was clobbered")
	}
	if _, present := data["speed_defaults"]; !present {
		t.Fatal("unmodeled speed_defaults was clobbered")
	}
	nets, ok := data["network_overrides"].([]map[string]any)
	if !ok || len(nets) != 1 || nets[0]["key"] != "5dbaa47ea7986c04d72d4f5e" ||
		nets[0]["raw_color_hex"] != "0544ff" {
		t.Fatalf("network_overrides = %v", data["network_overrides"])
	}
	speeds, ok := data["speed_overrides"].([]map[string]any)
	if !ok || len(speeds) != 1 || speeds[0]["key"] != "GbE" ||
		speeds[0]["raw_color_hex"] != "ff6c14" {
		t.Fatalf("speed_overrides = %v", data["speed_overrides"])
	}
}

func Test_etherLightingModelToData_nullsLeaveRemoteValues(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := &settingEtherLightingModel{
		NetworkOverrides: types.SetNull(
			types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes}),
		SpeedOverrides: types.SetNull(
			types.ObjectType{AttrTypes: etherLightingSpeedOverrideAttrTypes}),
	}
	data := map[string]any{
		"speed_overrides": []any{map[string]any{
			"key": "GbE", "raw_color_hex": "aabbcc",
		}},
	}

	etherLightingModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	speeds, ok := data["speed_overrides"].([]any)
	if !ok || len(speeds) != 1 {
		t.Fatalf("null set overwrote remote speed_overrides: %v",
			data["speed_overrides"])
	}
	if _, present := data["network_overrides"]; present {
		t.Fatal("null set should not write network_overrides")
	}
}

func Test_etherLightingModelToData_duplicateKeysRejected(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	speed := etherLightingSpeedOverrideSet(t, ctx,
		[]settingEtherLightingSpeedOverrideModel{
			{Speed: types.StringValue("GbE"), ColorHex: types.StringValue("ff6c14")},
			{Speed: types.StringValue("GbE"), ColorHex: types.StringValue("0544ff")},
		})
	m := &settingEtherLightingModel{
		NetworkOverrides: types.SetNull(
			types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes}),
		SpeedOverrides: speed,
	}
	data := map[string]any{}

	etherLightingModelToData(ctx, m, data, &diags)

	if !diags.HasError() {
		t.Fatal("duplicate speed keys with different colors must be rejected")
	}
}

func Test_etherLightingSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	m := etherLightingSettingToModel(ctx, &settings.EtherLighting{
		NetworkOverrides: []settings.SettingEtherLightingNetworkOverrides{
			{Key: "5dbaa47ea7986c04d72d4f5e", RawColorHex: "0544ff"},
		},
		SpeedOverrides: []settings.SettingEtherLightingSpeedOverrides{
			{Key: "GbE", RawColorHex: "ff6c14"},
		},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	var nets []settingEtherLightingNetworkOverrideModel
	diags.Append(m.NetworkOverrides.ElementsAs(ctx, &nets, false)...)
	if len(nets) != 1 || nets[0].NetworkID.ValueString() != "5dbaa47ea7986c04d72d4f5e" ||
		nets[0].ColorHex.ValueString() != "0544ff" {
		t.Fatalf("network_overrides = %v", nets)
	}
	var speeds []settingEtherLightingSpeedOverrideModel
	diags.Append(m.SpeedOverrides.ElementsAs(ctx, &speeds, false)...)
	if len(speeds) != 1 || speeds[0].Speed.ValueString() != "GbE" ||
		speeds[0].ColorHex.ValueString() != "ff6c14" {
		t.Fatalf("speed_overrides = %v", speeds)
	}
}

func Test_settingResource_Schema_etherLighting(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["ether_lighting"]; !ok {
		t.Fatal("schema is missing the ether_lighting section attribute")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_etherLighting|Test_settingResource_Schema_etherLighting' -count=1`
Expected: compile FAILURE — `undefined: settingEtherLightingModel` etc.

- [ ] **Step 3: Create `unifi/setting_section_ether_lighting.go`**

```go
package unifi

import (
	"context"
	"errors"
	"fmt"
	"regexp"

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
)

var etherLightingColorHexRegexp = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)

// settingEtherLightingModel is the nested ether_lighting block: the
// site-level Etherlighting palette used by switches with per-port LEDs
// (USW Pro Max line). Attribute names align with filipowm's
// unifi_setting_ether_lighting: network_id/speed + color_hex map to the raw
// key/raw_color_hex. The controller's built-in default palettes
// (network_defaults/speed_defaults) are controller-managed and preserved by
// the raw merge, not exposed.
type settingEtherLightingModel struct {
	NetworkOverrides types.Set `tfsdk:"network_overrides"`
	SpeedOverrides   types.Set `tfsdk:"speed_overrides"`
}

type settingEtherLightingNetworkOverrideModel struct {
	NetworkID types.String `tfsdk:"network_id"`
	ColorHex  types.String `tfsdk:"color_hex"`
}

type settingEtherLightingSpeedOverrideModel struct {
	Speed    types.String `tfsdk:"speed"`
	ColorHex types.String `tfsdk:"color_hex"`
}

var (
	etherLightingNetworkOverrideAttrTypes = map[string]attr.Type{
		"network_id": types.StringType,
		"color_hex":  types.StringType,
	}
	etherLightingSpeedOverrideAttrTypes = map[string]attr.Type{
		"speed":     types.StringType,
		"color_hex": types.StringType,
	}
	etherLightingAttrTypes = map[string]attr.Type{
		"network_overrides": types.SetType{
			ElemType: types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes},
		},
		"speed_overrides": types.SetType{
			ElemType: types.ObjectType{AttrTypes: etherLightingSpeedOverrideAttrTypes},
		},
	}
)

type etherLightingSection struct{}

func (etherLightingSection) key() string { return "ether_lighting" }

func (etherLightingSection) attrTypes() map[string]attr.Type { return etherLightingAttrTypes }

func (etherLightingSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Etherlighting palette for switches with " +
			"per-port LEDs (USW Pro Max line). `network_overrides` colors " +
			"ports by network/VLAN, `speed_overrides` by link speed. " +
			"NOTE: the controller silently drops an override whose color " +
			"equals the built-in default for that key — declare only " +
			"colors that differ from the defaults or the entry will not " +
			"round-trip.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"network_overrides": schema.SetNestedAttribute{
				MarkdownDescription: "Per-network LED colors, used when a device's Etherlighting `mode` is `network`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"network_id": schema.StringAttribute{
							MarkdownDescription: "ID of the network/VLAN this color applies to.",
							Required:            true,
						},
						"color_hex": schema.StringAttribute{
							MarkdownDescription: "LED color as a 6-digit RGB hex string without `#` (e.g. `ff6c14`).",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(
									etherLightingColorHexRegexp,
									"must be a 6-digit RGB hex string without '#'",
								),
							},
						},
					},
				},
			},
			"speed_overrides": schema.SetNestedAttribute{
				MarkdownDescription: "Per-link-speed LED colors, used when a device's Etherlighting `mode` is `speed`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"speed": schema.StringAttribute{
							MarkdownDescription: "Link-speed class this color applies to.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.OneOf(
									"FE", "GbE", "2.5GbE", "5GbE",
									"10GbE", "25GbE", "40GbE", "100GbE",
								),
							},
						},
						"color_hex": schema.StringAttribute{
							MarkdownDescription: "LED color as a 6-digit RGB hex string without `#` (e.g. `ffc107`).",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(
									etherLightingColorHexRegexp,
									"must be a 6-digit RGB hex string without '#'",
								),
							},
						},
					},
				},
			},
		},
	}
}

func (etherLightingSection) get(m *settingResourceModel) types.Object { return m.EtherLighting }

func (etherLightingSection) set(m *settingResourceModel, obj types.Object) { m.EtherLighting = obj }

func (etherLightingSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingEtherLightingModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	etherLightingModelToData(ctx, &m, data, &diags)
	return diags
}

// etherLightingModelToData writes only the user-set fields into the raw
// section document; unset fields — including the controller's built-in
// default palettes (network_defaults, speed_defaults), which go-unifi does
// not model — keep their remote values. A set of objects only dedupes whole
// objects, so two entries sharing a key but differing in color both survive
// the plan; reject that here rather than sending conflicting colors.
func etherLightingModelToData(
	ctx context.Context,
	m *settingEtherLightingModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.NetworkOverrides.IsNull() && !m.NetworkOverrides.IsUnknown() {
		var overrides []settingEtherLightingNetworkOverrideModel
		diags.Append(m.NetworkOverrides.ElementsAs(ctx, &overrides, false)...)
		if diags.HasError() {
			return
		}
		seen := make(map[string]struct{}, len(overrides))
		out := make([]map[string]any, 0, len(overrides))
		for _, o := range overrides {
			key := o.NetworkID.ValueString()
			if _, dup := seen[key]; dup {
				diags.AddError(
					"Duplicate network_overrides entry",
					fmt.Sprintf("network_id %q appears more than once in "+
						"ether_lighting.network_overrides; each network may "+
						"set only one color.", key),
				)
				return
			}
			seen[key] = struct{}{}
			out = append(out, map[string]any{
				"key":           key,
				"raw_color_hex": o.ColorHex.ValueString(),
			})
		}
		data["network_overrides"] = out
	}
	if !m.SpeedOverrides.IsNull() && !m.SpeedOverrides.IsUnknown() {
		var overrides []settingEtherLightingSpeedOverrideModel
		diags.Append(m.SpeedOverrides.ElementsAs(ctx, &overrides, false)...)
		if diags.HasError() {
			return
		}
		seen := make(map[string]struct{}, len(overrides))
		out := make([]map[string]any, 0, len(overrides))
		for _, o := range overrides {
			key := o.Speed.ValueString()
			if _, dup := seen[key]; dup {
				diags.AddError(
					"Duplicate speed_overrides entry",
					fmt.Sprintf("speed %q appears more than once in "+
						"ether_lighting.speed_overrides; each speed may set "+
						"only one color.", key),
				)
				return
			}
			seen[key] = struct{}{}
			out = append(out, map[string]any{
				"key":           key,
				"raw_color_hex": o.ColorHex.ValueString(),
			})
		}
		data["speed_overrides"] = out
	}
}

func (etherLightingSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.EtherLighting](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(etherLightingAttrTypes), diags
		}
		diags.AddError("Error Reading Ether Lighting Setting", err.Error())
		return types.ObjectNull(etherLightingAttrTypes), diags
	}
	model := etherLightingSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(etherLightingAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, etherLightingAttrTypes, model)
}

func etherLightingSettingToModel(
	ctx context.Context,
	s *settings.EtherLighting,
	diags *diag.Diagnostics,
) settingEtherLightingModel {
	nets := make([]settingEtherLightingNetworkOverrideModel, 0, len(s.NetworkOverrides))
	for _, o := range s.NetworkOverrides {
		nets = append(nets, settingEtherLightingNetworkOverrideModel{
			NetworkID: types.StringValue(o.Key),
			ColorHex:  types.StringValue(o.RawColorHex),
		})
	}
	networkSet, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: etherLightingNetworkOverrideAttrTypes}, nets)
	diags.Append(d...)

	speeds := make([]settingEtherLightingSpeedOverrideModel, 0, len(s.SpeedOverrides))
	for _, o := range s.SpeedOverrides {
		speeds = append(speeds, settingEtherLightingSpeedOverrideModel{
			Speed:    types.StringValue(o.Key),
			ColorHex: types.StringValue(o.RawColorHex),
		})
	}
	speedSet, d := types.SetValueFrom(ctx,
		types.ObjectType{AttrTypes: etherLightingSpeedOverrideAttrTypes}, speeds)
	diags.Append(d...)

	return settingEtherLightingModel{
		NetworkOverrides: networkSet,
		SpeedOverrides:   speedSet,
	}
}
```

- [ ] **Step 4: Register the section**

`unifi/setting_section.go` — the registry in its final PR 2 state:

```go
var settingSections = []settingSection{
	localeSection{},
	globalNatSection{},
	globalSwitchSection{},
	mdnsSection{},
	teleportSection{},
	magicSiteToSiteVpnSection{},
	trafficFlowSection{},
	etherLightingSection{},
}
```

`unifi/setting_resource.go` model (after `TrafficFlow`) — the five PR 2 fields in their final state:

```go
	Mdns               types.Object `tfsdk:"mdns"`
	Teleport           types.Object `tfsdk:"teleport"`
	MagicSiteToSiteVpn types.Object `tfsdk:"magic_site_to_site_vpn"`
	TrafficFlow        types.Object `tfsdk:"traffic_flow"`
	EtherLighting      types.Object `tfsdk:"ether_lighting"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_etherLighting|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_ether_lighting.go unifi/setting_section_ether_lighting_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add ether_lighting section"
```

(Body should note: names align with filipowm's unifi_setting_ether_lighting; duplicate-key guard; controller default palettes preserved by the raw merge; the "override equal to default is silently dropped" controller behavior.)

---

### Task 6: Acceptance tests against the docker demo controller

**Files:**
- Modify: `unifi/setting_section_mdns_test.go`, `unifi/setting_section_teleport_test.go`, `unifi/setting_section_magic_site_to_site_vpn_test.go`, `unifi/setting_section_traffic_flow_test.go`, `unifi/setting_section_ether_lighting_test.go` (append acceptance tests)

**Interfaces:**
- Consumes: `preCheck(t)` (requires `UNIFI_USERNAME`, `UNIFI_PASSWORD`, `UNIFI_API` — see `unifi/provider_test.go:151`), `testAccProtoV6ProviderFactories`, `resource.Test` from terraform-plugin-testing.

- [ ] **Step 1: Append acceptance tests**

Each file gains `"github.com/hashicorp/terraform-plugin-testing/helper/resource"` in its imports (and `"fmt"` where noted).

To `unifi/setting_section_mdns_test.go`:

```go
func TestAccSettingResource_mdns(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_mdns("custom"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "mdns.mode", "custom",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "mdns.predefined_services.#", "2",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "mdns.custom_services.#", "1",
					),
				),
			},
			{
				Config: testAccSettingConfig_mdns("all"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "mdns.mode", "all",
				),
			},
		},
	})
}

func testAccSettingConfig_mdns(mode string) string {
	return `
resource "unifi_setting" "test" {
  mdns = {
    mode                = "` + mode + `"
    predefined_services = ["apple_airPlay", "google_chromecast"]
    custom_services = [{
      name    = "Home Assistant"
      address = "_home-assistant._tcp"
    }]
  }
}
`
}
```

To `unifi/setting_section_teleport_test.go` (add `"fmt"` to imports):

```go
func TestAccSettingResource_teleport(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_teleport(true, "192.168.100.0/24"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "teleport.enabled", "true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "teleport.subnet", "192.168.100.0/24",
					),
				),
			},
			{
				Config: testAccSettingConfig_teleport(false, "192.168.100.0/24"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "teleport.enabled", "false",
				),
			},
		},
	})
}

func testAccSettingConfig_teleport(enabled bool, subnet string) string {
	return fmt.Sprintf(`
resource "unifi_setting" "test" {
  teleport = {
    enabled = %t
    subnet  = %q
  }
}
`, enabled, subnet)
}
```

To `unifi/setting_section_magic_site_to_site_vpn_test.go` (add `"fmt"` to imports):

```go
func TestAccSettingResource_magicSiteToSiteVpn(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_magicSiteToSiteVpn(true),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "magic_site_to_site_vpn.enabled", "true",
				),
			},
			{
				Config: testAccSettingConfig_magicSiteToSiteVpn(false),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "magic_site_to_site_vpn.enabled", "false",
				),
			},
		},
	})
}

func testAccSettingConfig_magicSiteToSiteVpn(enabled bool) string {
	return fmt.Sprintf(`
resource "unifi_setting" "test" {
  magic_site_to_site_vpn = {
    enabled = %t
  }
}
`, enabled)
}
```

(No check on `public_key`: the demo controller has no real console identity, so key generation is not guaranteed. Never assert on or log key values.)

To `unifi/setting_section_traffic_flow_test.go` (add `"fmt"` to imports):

```go
func TestAccSettingResource_trafficFlow(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_trafficFlow(true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "traffic_flow.gateway_dns_enabled", "true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "traffic_flow.unifi_services_enabled", "true",
					),
				),
			},
			{
				Config: testAccSettingConfig_trafficFlow(false),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "traffic_flow.gateway_dns_enabled", "false",
				),
			},
		},
	})
}

func testAccSettingConfig_trafficFlow(gatewayDNS bool) string {
	return fmt.Sprintf(`
resource "unifi_setting" "test" {
  traffic_flow = {
    gateway_dns_enabled    = %t
    unifi_services_enabled = true
  }
}
`, gatewayDNS)
}
```

To `unifi/setting_section_ether_lighting_test.go` (add `"fmt"` to imports):

```go
func TestAccSettingResource_etherLighting(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Colors deliberately differ from the controller's built-in
				// defaults: the controller silently drops an override equal
				// to the default, which would fail the round-trip check.
				Config: testAccSettingConfig_etherLighting("ff6c14"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "ether_lighting.speed_overrides.#", "1",
					),
					resource.TestCheckTypeSetElemNestedAttrs(
						"unifi_setting.test", "ether_lighting.speed_overrides.*",
						map[string]string{"speed": "GbE", "color_hex": "ff6c14"},
					),
				),
			},
			{
				Config: testAccSettingConfig_etherLighting("0544aa"),
				Check: resource.TestCheckTypeSetElemNestedAttrs(
					"unifi_setting.test", "ether_lighting.speed_overrides.*",
					map[string]string{"speed": "GbE", "color_hex": "0544aa"},
				),
			},
		},
	})
}

func testAccSettingConfig_etherLighting(color string) string {
	return fmt.Sprintf(`
resource "unifi_setting" "test" {
  ether_lighting = {
    speed_overrides = [{
      speed     = "GbE"
      color_hex = %q
    }]
  }
}
`, color)
}
```

- [ ] **Step 2: Start the demo controller and run**

Check `unifi/provider_test.go` `preCheck` and `docker-compose.yaml` for the exact env values — read them, do not guess:

```bash
docker compose up -d
# wait for healthy:
docker compose ps
```

Run: `TF_ACC=1 go test ./unifi/ -run 'TestAccSettingResource_(mdns|teleport|magicSiteToSiteVpn|trafficFlow|etherLighting)' -v -count=1 -timeout 15m` (with the `UNIFI_API`/`UNIFI_USERNAME`/`UNIFI_PASSWORD` values the compose setup expects).
Expected: PASS.

**Contingency:** if the demo controller rejects or lacks a section (e.g. `api.err.InvalidPayload`, or a 400 on the PUT because the simulated controller predates the feature — `teleport` needs ≥7.2, `traffic_flow` and `magic_site_to_site_vpn` are newer still, `ether_lighting` needs Pro Max-era firmware support), add a skip-guard at the top of that acceptance test — follow the existing `t.Skip` pattern in the suite (cf. `unifi/ap_group_resource_test.go:117`), with message `"demo controller does not support <key> setting"` — and record which sections were skipped in the final report. Do NOT weaken the unit tests. The live UDM is never used.

- [ ] **Step 3: Commit**

```bash
git add unifi/setting_section_mdns_test.go unifi/setting_section_teleport_test.go unifi/setting_section_magic_site_to_site_vpn_test.go unifi/setting_section_traffic_flow_test.go unifi/setting_section_ether_lighting_test.go
git commit -m "test(setting): acceptance coverage for connectivity service sections"
```

---

### Task 7: Docs, changelog, final verification

**Files:**
- Modify: `examples/resources/unifi_setting/resource.tf` (add a connectivity-services example)
- Modify: `CHANGELOG.md` (Unreleased → Features)
- Generated: `docs/resources/setting.md` (via `go generate ./...`)

- [ ] **Step 1: Extend the example**

Append to `examples/resources/unifi_setting/resource.tf` (read it first; it already holds multiple independent `unifi_setting` resources with comment ledes — match that style):

```terraform
# Connectivity services: mDNS, Teleport, Site Magic VPN, traffic flows,
# and Etherlighting
resource "unifi_setting" "connectivity" {
  site = "default"

  mdns = {
    mode                = "custom"
    predefined_services = ["apple_airPlay", "google_chromecast", "printers"]
    custom_services = [{
      name    = "Home Assistant"
      address = "_home-assistant._tcp"
    }]
  }

  teleport = {
    enabled = true
    subnet  = "192.168.100.0/24"
  }

  # The controller generates and manages the WireGuard key pair;
  # private_key/public_key never need to be set.
  magic_site_to_site_vpn = {
    enabled = true
  }

  traffic_flow = {
    enabled_allowed_traffic         = true
    gateway_dns_enabled             = true
    unifi_device_management_enabled = false
    unifi_services_enabled          = true
  }

  # Only declare colors that differ from the controller's built-in
  # defaults — identical overrides are silently dropped by the controller.
  ether_lighting = {
    speed_overrides = [{
      speed     = "GbE"
      color_hex = "ff6c14"
    }]
    # network_overrides = [{
    #   network_id = unifi_network.iot.id
    #   color_hex  = "0544ff"
    # }]
  }
}
```

- [ ] **Step 2: Regenerate docs**

Run: `go generate ./...`
Expected: `docs/resources/setting.md` gains `mdns`, `teleport`, `magic_site_to_site_vpn`, `traffic_flow`, `ether_lighting` attribute documentation, with `private_key` rendered as sensitive. Inspect the diff: `git diff --stat docs/` and spot-check `git diff docs/resources/setting.md | grep -i sensitive`.

- [ ] **Step 3: Changelog**

Add under `## [Unreleased]` → `### ✨ Features` in `CHANGELOG.md` (match the existing bolded-lede prose style):

```markdown
- **`unifi_setting`: new `mdns`, `teleport`, `magic_site_to_site_vpn`, `traffic_flow`, and `ether_lighting` sections.** The connectivity-service settings are now codifiable: the mDNS repeater (mode plus predefined/custom service selection), Teleport remote access (`enabled` + `subnet`, matching the filipowm provider's attribute names), Site Magic site-to-site VPN (`enabled`; the controller-generated WireGuard key pair is exposed as computed `public_key` and sensitive `private_key`, never required and never regenerated by the provider), Traffic Flows recording toggles, and the Etherlighting palette (`network_overrides`/`speed_overrides` with filipowm-compatible names). All five use the raw read-modify-write merge, so controller-managed fields the SDK does not model (e.g. mDNS network scoping, Etherlighting default palettes) are preserved verbatim rather than silently dropped.
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
git commit -m "docs(setting): document connectivity service sections"
```

- [ ] **Step 6: STOP — do not push**

The branch stays local. Report completion with: sections added, test results (including any demo-controller skips from Task 6), and the diff stat. James reviews before anything is posted publicly.

---

## Self-review notes

- Spec coverage (PR 2 scope only): mdns ✓ (Task 1), teleport ✓ (Task 2), magic_site_to_site_vpn ✓ (Task 3), traffic_flow ✓ (Task 4), ether_lighting ✓ (Task 5), docker-only acceptance with skip contingency ✓ (Task 6), docs + changelog + no public push ✓ (Task 7). Consumes PR 1's `settingSection` / `settingSections` / `applySections` / `readSections` verbatim; no engine changes needed.
- filipowm name alignment verified against their sources: `teleport.enabled`/`teleport.subnet` (their rename of `subnet_cidr`), `magic_site_to_site_vpn.enabled`, `ether_lighting.network_overrides[].network_id|color_hex` and `speed_overrides[].speed|color_hex` (their renames of raw `key`/`raw_color_hex`), including their duplicate-key guard and their "override equal to default is silently dropped" doc note. `mdns`/`traffic_flow` have no filipowm equivalent and use go-unifi JSON names.
- Deviations from the PR 1 template, intentional:
  1. **`magic_site_to_site_vpn.read` is raw (`ListSettings` filter), not typed `GetSetting[T]`** — `settings.MagicSiteToSiteVpn` models only `Enabled`, but the schema's computed `public_key`/`private_key` (observed live) must populate from the raw document. Overlay semantics are unchanged. `TODO(go-unifi)`: switch to a typed read once the struct gains the key fields.
  2. **`teleport.subnet` reads back as `StringValue`, never null** — `""` is the controller's "auto" value; `StringValueOrNull` would make an explicit `subnet = ""` inconsistent after apply.
  3. **`mdns.predefined_services` is flattened** from raw `[{"code": "..."}]` to a set of code strings for config ergonomics; converters wrap/unwrap. Attribute name is unchanged from go-unifi.
- Sensitive-field constraint honored: `private_key` Optional + Computed + Sensitive with `UseStateForUnknown`, never required; overlay never writes `public_key` and writes `x_private_key` only when set (the value present in state from a prior read is re-sent verbatim — a controller no-op — never regenerated or fabricated). The `x_` prefix is dropped on the TF side per this provider's existing convention (`mgmt.ssh_password`). The schema test asserts sensitivity structurally. No real key material appears in any test, example, or doc.
- Preservation assertions present in every section's overlay test, using real live-observed unmodeled fields where they exist: `mdns.enabled_for`/`enabled_for_network_ids`, `magic_site_to_site_vpn.public_key`, `ether_lighting.network_defaults`/`speed_defaults`; generic `unmodeled_field` for teleport/traffic_flow (their live payloads carry nothing beyond base + modeled fields).
- Known risk, deliberate: the `speed_overrides.speed` `OneOf(FE, GbE, …)` enum comes from go-unifi's ace.jar comment and matches filipowm, but the live controller's `speed_defaults` uses a different vocabulary (`10M`, …). If the Task 6 acceptance PUT with `GbE` is rejected or fails to round-trip, drop the `OneOf` (keep the color regex), document observed values instead, and note it in the final report.
- Not exposed, deliberately: `mdns.enabled_for` / `mdns.enabled_for_network_ids` (live network scoping, absent from `settings.Mdns` and readable only raw) — preserved by the raw merge, flagged as a `TODO(go-unifi)` follow-up rather than giving mdns a second raw-read special case in this PR.
- Type consistency: every section implements the full `settingSection` method set; registry grows by exactly `mdnsSection{}`, `teleportSection{}`, `magicSiteToSiteVpnSection{}`, `trafficFlowSection{}`, `etherLightingSection{}` in Tasks 1–5; model fields `Mdns`/`Teleport`/`MagicSiteToSiteVpn`/`TrafficFlow`/`EtherLighting` match each section's `get`/`set` and the required naming constraint.
