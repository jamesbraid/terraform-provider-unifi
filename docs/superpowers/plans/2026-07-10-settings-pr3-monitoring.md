# Settings PR 3: monitoring & security parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `snmp`, `netflow`, and `ssl_inspection` nested sections to the `unifi_setting` resource (tranche A, ungated), plus `global_network`, `usg_geo`, and `ipsec` (tranche B, gated on the go-unifi PR 0 bump), reusing the `settingSection` registry and raw-merge read-modify-write engine introduced in PR 1.

**Architecture:** Identical to PRs 1–2: each section is a self-contained `unifi/setting_section_<name>.go` implementing the `settingSection` interface from PR 1 Task 1, registered in `settingSections`. Apply is a raw-JSON overlay (only user-set fields written into the section's `Data` map, unmodeled controller fields — e.g. live `ssl_inspection.identity_certificate_*` — round-trip untouched); reads use the typed `unifi/settings` structs. New in this PR: sensitive-field handling for SNMP — the community string is `Sensitive` and read back from the controller; the v3 password maps to the controller's write-only `x_password` and is never read back (the section's `set` carries the configured value forward, same semantics as the existing `mgmt.ssh_password`).

**Tech Stack:** Go, terraform-plugin-framework, go-unifi v1.33.43 fork (`unifi/settings` package: `Snmp`, `Netflow`, `SslInspection`; after PR 0: `GlobalNetwork`, `UsgGeo`, `Ipsec`, `RawSetting`).

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-10-setting-sections-design.md`. This plan is PR 3 of 6.
- **Nothing is pushed or posted publicly. Local branch/commits only; James reviews before any push.**
- **PRs 1–2 are assumed merged locally**: `unifi/setting_section.go` exists and defines exactly the PR 1 Task 1 contract — `type settingSection interface { key() string; attrTypes() map[string]attr.Type; schemaAttribute() schema.SingleNestedAttribute; get(m *settingResourceModel) types.Object; set(m *settingResourceModel, obj types.Object); overlay(ctx context.Context, obj types.Object, data map[string]any) diag.Diagnostics; read(ctx context.Context, client *Client, site string) (types.Object, diag.Diagnostics) }`, `var settingSections []settingSection`, `applySections`, `readSections`. This PR only appends to the registry and to `settingResourceModel`; the engine is not modified. If any of these names differ on the branch, STOP and reconcile with the PR 1 plan before proceeding.
- **Tranche B (Tasks 4–6: `global_network`, `usg_geo`, `ipsec`) is GATED on the go-unifi PR 0 bump** — typed structs `settings.GlobalNetwork`, `settings.UsgGeo`, `settings.Ipsec` must exist. See the precondition block before Task 4. If the gate fails, skip Tasks 4–6 entirely and execute Tasks 7–8 for tranche A only (this is the spec's "gated sections ride a later PR" path).
- **Sensitive-value hygiene:** the captured live payload (`udm-settings.json` in the scratchpad) contains real secrets. Field NAMES and TYPES from it informed this plan; never copy VALUES from it into code, tests, examples, docs, or commit messages. All test fixtures use synthetic values (`tfacc-…`, RFC 5737 addresses).
- Sensitive attributes: `snmp.community` and `snmp.password` are `Sensitive: true`. `snmp.username` is not (SNMPv3 sends the securityName in cleartext; the controller does not `x_`-prefix it). `snmp.password` follows the repo's `mgmt.ssh_password` precedent: `Optional + Sensitive`, NOT `Computed`, never read back, configured value preserved.
- Attribute naming: `ssl_inspection.state` aligns with filipowm's `unifi_setting_ssl_inspection` (`off|simple|advanced`, same attribute name). filipowm has no `snmp`, `netflow`, `global_network`, `usg_geo`, or `ipsec` resources, so those follow go-unifi/controller JSON naming (snake_case; TF attribute `enabled_v3` maps to raw key `enabledV3`).
- Enum validators only where the generated structs document the enum: `netflow.sampling_mode` (`off|hash|random|deterministic`), `netflow.version` (`5|9|10`), `netflow.port` (1024–65535), `ssl_inspection.state` (`off|simple|advanced`), `usg_geo.ip_filtering.action` (`block|allow`) and `.traffic_direction` (`both|ingress|egress`) (both grounded in the `settings.Usg` geo enums). No validator on `global_network.default_security_posture` or `ipsec.ikev2_reauthentication_method` — only one live value observed each.
- Unordered string collections are `types.Set`; Optional+Computed collections get `setplanmodifier.UseStateForUnknown()`.
- Existing sections (13 inline + PR 1–2 registry sections) and their tests are untouched in this PR.
- Commit style: conventional commits matching the repo log (`feat(setting): …`), body explains why, `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` trailer.
- All commands run from the repo root: `/Users/jamesb/emdash/worktrees/terraform-provider-unifi/emdash/missing-config-uyrwq`.
- Unit tests: `go test ./unifi/ -run '<pattern>' -count=1`. Acceptance: docker demo controller only (Task 7) — never a live UDM.

---

## Tranche A — ungated sections (structs exist in go-unifi v1.33.43)

### Task 1: `snmp` section

**Files:**
- Create: `unifi/setting_section_snmp.go`
- Create: `unifi/setting_section_snmp_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settingSections`, `settings.Snmp` (`Community string // json community`, `Enabled bool`, `EnabledV3 bool`, `Password string // json x_password`, `Username string`), `ui.GetSetting[T]`, `util.StringValueOrNull`.
- Produces: `settingSnmpModel`, `snmpAttrTypes`, `snmpSection`, `snmpModelToData(m, data)`, `snmpSettingToModel(s)`, `preserveSnmpPassword(prior, fresh)`.
- Raw JSON keys (overlay targets): `community`, `enabled`, `enabledV3` (camelCase in the controller document — NOT `enabled_v3`), `username`, `x_password`.
- Sensitivity: `community` Optional+Computed+Sensitive (the controller returns it on GET — no `x_` prefix); `password` Optional+Sensitive, write-only (`x_password`), value preserved from configuration by `set()`.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_snmp_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_snmpModelToData(t *testing.T) {
	m := &settingSnmpModel{
		Community: types.StringValue("tfacc-ro"),
		Enabled:   types.BoolValue(true),
		EnabledV3: types.BoolValue(true),
		Username:  types.StringValue("tfacc-user"),
		Password:  types.StringValue("tfacc-password-1"),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": "keep"}

	snmpModelToData(m, data)

	if data["community"] != "tfacc-ro" {
		t.Fatalf("community = %v", data["community"])
	}
	if data["enabled"] != true {
		t.Fatalf("enabled = %v", data["enabled"])
	}
	// The controller document uses camelCase for this one key.
	if data["enabledV3"] != true {
		t.Fatalf("enabledV3 = %v", data["enabledV3"])
	}
	if _, present := data["enabled_v3"]; present {
		t.Fatal("wrote snake_case enabled_v3; controller key is enabledV3")
	}
	if data["username"] != "tfacc-user" {
		t.Fatalf("username = %v", data["username"])
	}
	if data["x_password"] != "tfacc-password-1" {
		t.Fatalf("x_password = %v", data["x_password"])
	}
	if _, present := data["password"]; present {
		t.Fatal("wrote bare password key; controller key is x_password")
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_snmpModelToData_nullsLeaveRemoteValues(t *testing.T) {
	m := &settingSnmpModel{
		Community: types.StringNull(),
		Enabled:   types.BoolNull(),
		EnabledV3: types.BoolNull(),
		Username:  types.StringNull(),
		Password:  types.StringNull(),
	}
	data := map[string]any{"community": "remote-value", "enabled": true}

	snmpModelToData(m, data)

	if data["community"] != "remote-value" {
		t.Fatalf("null community overwrote remote value: %v", data["community"])
	}
	if data["enabled"] != true {
		t.Fatalf("null enabled overwrote remote value: %v", data["enabled"])
	}
	for _, key := range []string{"enabledV3", "username", "x_password"} {
		if _, present := data[key]; present {
			t.Fatalf("null model wrote %s", key)
		}
	}
}

func Test_snmpSettingToModel(t *testing.T) {
	m := snmpSettingToModel(&settings.Snmp{
		Community: "tfacc-ro",
		Enabled:   true,
		EnabledV3: false,
		Username:  "tfacc-user",
		Password:  "controller-echoed-hash",
	})
	if m.Community.ValueString() != "tfacc-ro" {
		t.Fatalf("community = %v", m.Community)
	}
	if !m.Enabled.ValueBool() || m.EnabledV3.ValueBool() {
		t.Fatalf("bools wrong: enabled=%v enabled_v3=%v", m.Enabled, m.EnabledV3)
	}
	if m.Username.ValueString() != "tfacc-user" {
		t.Fatalf("username = %v", m.Username)
	}
	// x_password is write-only: whatever the controller returns is ignored.
	if !m.Password.IsNull() {
		t.Fatalf("password must never be read back, got %v", m.Password)
	}

	empty := snmpSettingToModel(&settings.Snmp{})
	if !empty.Community.IsNull() || !empty.Username.IsNull() {
		t.Fatalf("empty strings should map to null: %v / %v", empty.Community, empty.Username)
	}
}

func Test_preserveSnmpPassword(t *testing.T) {
	ctx := context.Background()

	mkObj := func(pw types.String) types.Object {
		obj, d := types.ObjectValueFrom(ctx, snmpAttrTypes, settingSnmpModel{
			Community: types.StringValue("tfacc-ro"),
			Enabled:   types.BoolValue(true),
			EnabledV3: types.BoolValue(false),
			Username:  types.StringNull(),
			Password:  pw,
		})
		if d.HasError() {
			t.Fatal(d)
		}
		return obj
	}

	prior := mkObj(types.StringValue("tfacc-password-1"))
	fresh := mkObj(types.StringNull())

	merged := preserveSnmpPassword(prior, fresh)
	pw := merged.Attributes()["password"].(types.String)
	if pw.ValueString() != "tfacc-password-1" {
		t.Fatalf("configured password not preserved: %v", pw)
	}

	// No prior object: the fresh read passes through untouched.
	if got := preserveSnmpPassword(types.ObjectNull(snmpAttrTypes), fresh); !got.Equal(fresh) {
		t.Fatalf("null prior should pass fresh through, got %v", got)
	}
	// Prior without a password: nothing to preserve.
	if got := preserveSnmpPassword(mkObj(types.StringNull()), fresh); !got.Equal(fresh) {
		t.Fatalf("passwordless prior should pass fresh through, got %v", got)
	}
}

func Test_settingResource_Schema_snmp(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	attrRaw, ok := resp.Schema.Attributes["snmp"]
	if !ok {
		t.Fatal("schema is missing the snmp section attribute")
	}
	nested, ok := attrRaw.(schema.SingleNestedAttribute)
	if !ok {
		t.Fatalf("snmp attribute is %T, want SingleNestedAttribute", attrRaw)
	}
	for _, name := range []string{"community", "password"} {
		a, ok := nested.Attributes[name].(schema.StringAttribute)
		if !ok || !a.Sensitive {
			t.Fatalf("snmp.%s must be a Sensitive string attribute", name)
		}
	}
	if pw := nested.Attributes["password"].(schema.StringAttribute); pw.Computed {
		t.Fatal("snmp.password must not be Computed (write-only x_ field)")
	}
	if user, ok := nested.Attributes["username"].(schema.StringAttribute); !ok || user.Sensitive {
		t.Fatal("snmp.username must exist and not be Sensitive")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_snmp|Test_preserveSnmpPassword|Test_settingResource_Schema_snmp' -count=1`
Expected: compile FAILURE — `undefined: settingSnmpModel`, `undefined: snmpModelToData`, etc.

- [ ] **Step 3: Create `unifi/setting_section_snmp.go`**

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

// settingSnmpModel is the nested snmp block: the site SNMP v1/v2c community
// and SNMPv3 agent credentials. community and password are Sensitive;
// password maps to the controller's write-only x_password and is never read
// back (the configured value is preserved, mirroring mgmt.ssh_password).
type settingSnmpModel struct {
	Community types.String `tfsdk:"community"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	EnabledV3 types.Bool   `tfsdk:"enabled_v3"`
	Username  types.String `tfsdk:"username"`
	Password  types.String `tfsdk:"password"`
}

var snmpAttrTypes = map[string]attr.Type{
	"community":  types.StringType,
	"enabled":    types.BoolType,
	"enabled_v3": types.BoolType,
	"username":   types.StringType,
	"password":   types.StringType,
}

type snmpSection struct{}

func (snmpSection) key() string { return "snmp" }

func (snmpSection) attrTypes() map[string]attr.Type { return snmpAttrTypes }

func (snmpSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site SNMP agent settings (v1/v2c community and SNMPv3 credentials).",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"community": schema.StringAttribute{
				MarkdownDescription: "SNMP v1/v2c community string.",
				Optional:            true,
				Computed:            true,
				Sensitive:           true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable the SNMP v1/v2c agent.",
				Optional:            true,
				Computed:            true,
			},
			"enabled_v3": schema.BoolAttribute{
				MarkdownDescription: "Enable the SNMPv3 agent.",
				Optional:            true,
				Computed:            true,
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "SNMPv3 username.",
				Optional:            true,
				Computed:            true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "SNMPv3 password (8–32 characters). Sensitive — the controller " +
					"treats this as write-only, so the value is kept from configuration and not read back.",
				Optional:  true,
				Sensitive: true,
			},
		},
	}
}

func (snmpSection) get(m *settingResourceModel) types.Object { return m.Snmp }

// set installs a freshly-read snmp object, carrying the configured password
// forward: x_password is write-only on the controller, so the read value can
// never be trusted to round-trip.
func (snmpSection) set(m *settingResourceModel, obj types.Object) {
	m.Snmp = preserveSnmpPassword(m.Snmp, obj)
}

// preserveSnmpPassword returns fresh with the password attribute carried over
// from prior (the plan or prior state). If prior has no password, fresh
// passes through unchanged.
func preserveSnmpPassword(prior, fresh types.Object) types.Object {
	if prior.IsNull() || prior.IsUnknown() || fresh.IsNull() || fresh.IsUnknown() {
		return fresh
	}
	pv, ok := prior.Attributes()["password"]
	if !ok || pv.IsNull() || pv.IsUnknown() {
		return fresh
	}
	attrs := make(map[string]attr.Value, len(fresh.Attributes()))
	for k, v := range fresh.Attributes() {
		attrs[k] = v
	}
	attrs["password"] = pv
	merged, d := types.ObjectValue(snmpAttrTypes, attrs)
	if d.HasError() {
		return fresh
	}
	return merged
}

func (snmpSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingSnmpModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	snmpModelToData(&m, data)
	return diags
}

// snmpModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values. Note the controller's key
// names: enabledV3 (camelCase) and x_password (write-only secret prefix).
func snmpModelToData(m *settingSnmpModel, data map[string]any) {
	if !m.Community.IsNull() && !m.Community.IsUnknown() {
		data["community"] = m.Community.ValueString()
	}
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		data["enabled"] = m.Enabled.ValueBool()
	}
	if !m.EnabledV3.IsNull() && !m.EnabledV3.IsUnknown() {
		data["enabledV3"] = m.EnabledV3.ValueBool()
	}
	if !m.Username.IsNull() && !m.Username.IsUnknown() {
		data["username"] = m.Username.ValueString()
	}
	if !m.Password.IsNull() && !m.Password.IsUnknown() {
		data["x_password"] = m.Password.ValueString()
	}
}

func (snmpSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Snmp](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(snmpAttrTypes), diags
		}
		diags.AddError("Error Reading SNMP Setting", err.Error())
		return types.ObjectNull(snmpAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, snmpAttrTypes, snmpSettingToModel(setting))
}

func snmpSettingToModel(s *settings.Snmp) settingSnmpModel {
	return settingSnmpModel{
		Community: util.StringValueOrNull(s.Community),
		Enabled:   types.BoolValue(s.Enabled),
		EnabledV3: types.BoolValue(s.EnabledV3),
		Username:  util.StringValueOrNull(s.Username),
		// x_password is write-only; never surface what the controller returns.
		Password: types.StringNull(),
	}
}
```

- [ ] **Step 4: Register the section**

In `unifi/setting_section.go`, append to the registry (after the last PR 2 entry — keep registry order matching model-field order):

```go
	snmpSection{},
```

In `unifi/setting_resource.go`, add to `settingResourceModel` (after the last PR 2 section field):

```go
	Snmp          types.Object   `tfsdk:"snmp"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_snmp|Test_preserveSnmpPassword|Test_settingResource_Schema' -count=1`
Expected: PASS (including pre-existing schema tests).

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_snmp.go unifi/setting_section_snmp_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add snmp section with sensitive credentials"
```

(Body should note: community and password are Sensitive; x_password is write-only so the configured value is preserved instead of read back, mirroring mgmt.ssh_password; enabledV3 stays camelCase on the wire.)

---

### Task 2: `netflow` section

**Files:**
- Create: `unifi/setting_section_netflow.go`
- Create: `unifi/setting_section_netflow_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settings.Netflow` (`AutoEngineIDEnabled bool`, `Enabled bool`, `EngineID *int64`, `ExportFrequency *int64`, `NetworkIDs []string`, `Port *int64 // 1024..65535`, `RefreshRate *int64`, `SamplingMode string // off|hash|random|deterministic`, `SamplingRate *int64`, `Server string`, `Version *int64 // 5|9|10`), `util.ConvertInt64FromAPIValue`, `util.StringValueOrNull`.
- Produces: `settingNetflowModel`, `netflowAttrTypes`, `netflowSection`, `netflowModelToData(ctx, m, data, diags)`, `netflowSettingToModel(ctx, s, diags)`.
- No sensitive fields (collector address/port are not secrets).

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_netflow_test.go`:

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

func Test_netflowModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	ids, d := types.SetValueFrom(ctx, types.StringType, []string{"net1"})
	if d.HasError() {
		t.Fatal(d)
	}
	m := &settingNetflowModel{
		AutoEngineIDEnabled: types.BoolValue(false),
		Enabled:             types.BoolValue(true),
		EngineID:            types.Int64Value(42),
		ExportFrequency:     types.Int64Null(),
		NetworkIDs:          ids,
		Port:                types.Int64Value(2055),
		RefreshRate:         types.Int64Null(),
		SamplingMode:        types.StringValue("off"),
		SamplingRate:        types.Int64Null(),
		Server:              types.StringValue("192.0.2.10"),
		Version:             types.Int64Value(10),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": "keep", "refresh_rate": float64(20)}

	netflowModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
	if data["refresh_rate"] != float64(20) {
		t.Fatal("null refresh_rate overwrote remote value")
	}
	if data["enabled"] != true || data["auto_engine_id_enabled"] != false {
		t.Fatalf("bools wrong: %v", data)
	}
	if data["engine_id"] != int64(42) || data["port"] != int64(2055) || data["version"] != int64(10) {
		t.Fatalf("ints wrong: %v", data)
	}
	if data["sampling_mode"] != "off" || data["server"] != "192.0.2.10" {
		t.Fatalf("strings wrong: %v", data)
	}
	got, ok := data["network_ids"].([]string)
	if !ok || len(got) != 1 || got[0] != "net1" {
		t.Fatalf("network_ids = %v", data["network_ids"])
	}
	for _, key := range []string{"export_frequency", "sampling_rate"} {
		if _, present := data[key]; present {
			t.Fatalf("null model wrote %s", key)
		}
	}
}

func Test_netflowSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := netflowSettingToModel(ctx, &settings.Netflow{
		AutoEngineIDEnabled: true,
		Enabled:             false,
		ExportFrequency:     util.Ptr(int64(5)),
		NetworkIDs:          []string{"net1"},
		Port:                util.Ptr(int64(2055)),
		RefreshRate:         util.Ptr(int64(20)),
		Version:             util.Ptr(int64(10)),
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if !m.AutoEngineIDEnabled.ValueBool() || m.Enabled.ValueBool() {
		t.Fatalf("bools wrong: %v / %v", m.AutoEngineIDEnabled, m.Enabled)
	}
	if m.Port.ValueInt64() != 2055 || m.Version.ValueInt64() != 10 {
		t.Fatalf("ints wrong: %v / %v", m.Port, m.Version)
	}
	// nil pointers and empty strings map to null.
	if !m.EngineID.IsNull() || !m.SamplingRate.IsNull() {
		t.Fatalf("nil int pointers should be null: %v / %v", m.EngineID, m.SamplingRate)
	}
	if !m.SamplingMode.IsNull() || !m.Server.IsNull() {
		t.Fatalf("empty strings should be null: %v / %v", m.SamplingMode, m.Server)
	}
	var ids []string
	diags.Append(m.NetworkIDs.ElementsAs(ctx, &ids, false)...)
	if len(ids) != 1 || ids[0] != "net1" {
		t.Fatalf("network_ids = %v", ids)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_netflow' -count=1`
Expected: compile FAILURE — `undefined: settingNetflowModel` etc.

- [ ] **Step 3: Create `unifi/setting_section_netflow.go`**

```go
package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
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

// settingNetflowModel is the nested netflow block: the NetFlow/IPFIX exporter
// configuration (collector, template, and sampling parameters).
type settingNetflowModel struct {
	AutoEngineIDEnabled types.Bool   `tfsdk:"auto_engine_id_enabled"`
	Enabled             types.Bool   `tfsdk:"enabled"`
	EngineID            types.Int64  `tfsdk:"engine_id"`
	ExportFrequency     types.Int64  `tfsdk:"export_frequency"`
	NetworkIDs          types.Set    `tfsdk:"network_ids"`
	Port                types.Int64  `tfsdk:"port"`
	RefreshRate         types.Int64  `tfsdk:"refresh_rate"`
	SamplingMode        types.String `tfsdk:"sampling_mode"`
	SamplingRate        types.Int64  `tfsdk:"sampling_rate"`
	Server              types.String `tfsdk:"server"`
	Version             types.Int64  `tfsdk:"version"`
}

var netflowAttrTypes = map[string]attr.Type{
	"auto_engine_id_enabled": types.BoolType,
	"enabled":                types.BoolType,
	"engine_id":              types.Int64Type,
	"export_frequency":       types.Int64Type,
	"network_ids":            types.SetType{ElemType: types.StringType},
	"port":                   types.Int64Type,
	"refresh_rate":           types.Int64Type,
	"sampling_mode":          types.StringType,
	"sampling_rate":          types.Int64Type,
	"server":                 types.StringType,
	"version":                types.Int64Type,
}

type netflowSection struct{}

func (netflowSection) key() string { return "netflow" }

func (netflowSection) attrTypes() map[string]attr.Type { return netflowAttrTypes }

func (netflowSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "NetFlow/IPFIX exporter settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"auto_engine_id_enabled": schema.BoolAttribute{
				MarkdownDescription: "Derive the exporter engine ID automatically.",
				Optional:            true,
				Computed:            true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable NetFlow export.",
				Optional:            true,
				Computed:            true,
			},
			"engine_id": schema.Int64Attribute{
				MarkdownDescription: "Exporter engine ID (used when `auto_engine_id_enabled` is `false`).",
				Optional:            true,
				Computed:            true,
			},
			"export_frequency": schema.Int64Attribute{
				MarkdownDescription: "Flow export frequency in seconds.",
				Optional:            true,
				Computed:            true,
			},
			"network_ids": schema.SetAttribute{
				MarkdownDescription: "UniFi network IDs whose traffic is exported.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "Collector UDP port (1024–65535).",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.Between(1024, 65535),
				},
			},
			"refresh_rate": schema.Int64Attribute{
				MarkdownDescription: "Template refresh rate in packets.",
				Optional:            true,
				Computed:            true,
			},
			"sampling_mode": schema.StringAttribute{
				MarkdownDescription: "Packet sampling mode: `off`, `hash`, `random`, or `deterministic`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("off", "hash", "random", "deterministic"),
				},
			},
			"sampling_rate": schema.Int64Attribute{
				MarkdownDescription: "Sampling rate (1-in-N packets) when sampling is enabled.",
				Optional:            true,
				Computed:            true,
			},
			"server": schema.StringAttribute{
				MarkdownDescription: "Collector host (IP address or FQDN).",
				Optional:            true,
				Computed:            true,
			},
			"version": schema.Int64Attribute{
				MarkdownDescription: "Export format version: `5`, `9`, or `10` (IPFIX).",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.OneOf(5, 9, 10),
				},
			},
		},
	}
}

func (netflowSection) get(m *settingResourceModel) types.Object { return m.Netflow }

func (netflowSection) set(m *settingResourceModel, obj types.Object) { m.Netflow = obj }

func (netflowSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingNetflowModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	netflowModelToData(ctx, &m, data, &diags)
	return diags
}

// netflowModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values.
func netflowModelToData(
	ctx context.Context,
	m *settingNetflowModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.AutoEngineIDEnabled.IsNull() && !m.AutoEngineIDEnabled.IsUnknown() {
		data["auto_engine_id_enabled"] = m.AutoEngineIDEnabled.ValueBool()
	}
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		data["enabled"] = m.Enabled.ValueBool()
	}
	if !m.EngineID.IsNull() && !m.EngineID.IsUnknown() {
		data["engine_id"] = m.EngineID.ValueInt64()
	}
	if !m.ExportFrequency.IsNull() && !m.ExportFrequency.IsUnknown() {
		data["export_frequency"] = m.ExportFrequency.ValueInt64()
	}
	if !m.NetworkIDs.IsNull() && !m.NetworkIDs.IsUnknown() {
		var ids []string
		diags.Append(m.NetworkIDs.ElementsAs(ctx, &ids, false)...)
		data["network_ids"] = ids
	}
	if !m.Port.IsNull() && !m.Port.IsUnknown() {
		data["port"] = m.Port.ValueInt64()
	}
	if !m.RefreshRate.IsNull() && !m.RefreshRate.IsUnknown() {
		data["refresh_rate"] = m.RefreshRate.ValueInt64()
	}
	if !m.SamplingMode.IsNull() && !m.SamplingMode.IsUnknown() {
		data["sampling_mode"] = m.SamplingMode.ValueString()
	}
	if !m.SamplingRate.IsNull() && !m.SamplingRate.IsUnknown() {
		data["sampling_rate"] = m.SamplingRate.ValueInt64()
	}
	if !m.Server.IsNull() && !m.Server.IsUnknown() {
		data["server"] = m.Server.ValueString()
	}
	if !m.Version.IsNull() && !m.Version.IsUnknown() {
		data["version"] = m.Version.ValueInt64()
	}
}

func (netflowSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Netflow](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(netflowAttrTypes), diags
		}
		diags.AddError("Error Reading NetFlow Setting", err.Error())
		return types.ObjectNull(netflowAttrTypes), diags
	}
	model := netflowSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(netflowAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, netflowAttrTypes, model)
}

func netflowSettingToModel(
	ctx context.Context,
	s *settings.Netflow,
	diags *diag.Diagnostics,
) settingNetflowModel {
	ids, d := types.SetValueFrom(ctx, types.StringType, s.NetworkIDs)
	diags.Append(d...)
	return settingNetflowModel{
		AutoEngineIDEnabled: types.BoolValue(s.AutoEngineIDEnabled),
		Enabled:             types.BoolValue(s.Enabled),
		EngineID:            util.ConvertInt64FromAPIValue(s.EngineID),
		ExportFrequency:     util.ConvertInt64FromAPIValue(s.ExportFrequency),
		NetworkIDs:          ids,
		Port:                util.ConvertInt64FromAPIValue(s.Port),
		RefreshRate:         util.ConvertInt64FromAPIValue(s.RefreshRate),
		SamplingMode:        util.StringValueOrNull(s.SamplingMode),
		SamplingRate:        util.ConvertInt64FromAPIValue(s.SamplingRate),
		Server:              util.StringValueOrNull(s.Server),
		Version:             util.ConvertInt64FromAPIValue(s.Version),
	}
}
```

- [ ] **Step 4: Register the section**

`unifi/setting_section.go` registry (after `snmpSection{}`):

```go
	netflowSection{},
```

`unifi/setting_resource.go` model (after `Snmp`):

```go
	Netflow       types.Object   `tfsdk:"netflow"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_netflow|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_netflow.go unifi/setting_section_netflow_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add netflow section"
```

---

### Task 3: `ssl_inspection` section

**Files:**
- Create: `unifi/setting_section_ssl_inspection.go`
- Create: `unifi/setting_section_ssl_inspection_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settings.SslInspection` (`State string // off|simple|advanced`).
- Produces: `settingSslInspectionModel`, `sslInspectionAttrTypes`, `sslInspectionSection`, `sslInspectionModelToData(m, data)`, `sslInspectionSettingToModel(s)`.
- **Alignment:** the attribute is named `state` with values `off|simple|advanced`, exactly matching filipowm's `unifi_setting_ssl_inspection.state` (their resource is `Required`; ours is Optional+Computed because it is a nested section of a shared singleton).
- The live controller document carries fields go-unifi does not model (`identity_certificate_all_users`, `identity_certificate_groups`, `identity_certificate_users`) — the raw merge must preserve them; that is the raw-preservation assertion below.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_ssl_inspection_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_sslInspectionModelToData(t *testing.T) {
	m := &settingSslInspectionModel{State: types.StringValue("simple")}
	// The live controller document carries identity-certificate fields that
	// go-unifi does not model; the raw merge must preserve them verbatim.
	data := map[string]any{
		"identity_certificate_all_users": true,
		"identity_certificate_groups":    []any{},
	}

	sslInspectionModelToData(m, data)

	if data["state"] != "simple" {
		t.Fatalf("state = %v", data["state"])
	}
	if data["identity_certificate_all_users"] != true {
		t.Fatal("unmodeled identity_certificate_all_users was clobbered")
	}
	if _, present := data["identity_certificate_groups"]; !present {
		t.Fatal("unmodeled identity_certificate_groups was dropped")
	}
}

func Test_sslInspectionModelToData_nullLeavesRemoteValue(t *testing.T) {
	m := &settingSslInspectionModel{State: types.StringNull()}
	data := map[string]any{"state": "advanced"}

	sslInspectionModelToData(m, data)

	if data["state"] != "advanced" {
		t.Fatalf("null state overwrote remote value: %v", data["state"])
	}
}

func Test_sslInspectionSettingToModel(t *testing.T) {
	m := sslInspectionSettingToModel(&settings.SslInspection{State: "off"})
	if m.State.ValueString() != "off" {
		t.Fatalf("state = %v", m.State)
	}
	empty := sslInspectionSettingToModel(&settings.SslInspection{})
	if !empty.State.IsNull() {
		t.Fatalf("empty state should map to null, got %v", empty.State)
	}
}

func Test_settingResource_Schema_sslInspection(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["ssl_inspection"]; !ok {
		t.Fatal("schema is missing the ssl_inspection section attribute")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_sslInspection|Test_settingResource_Schema_sslInspection' -count=1`
Expected: compile FAILURE — `undefined: settingSslInspectionModel` etc.

- [ ] **Step 3: Create `unifi/setting_section_ssl_inspection.go`**

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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingSslInspectionModel is the nested ssl_inspection block. The state
// attribute name and values align with filipowm's unifi_setting_ssl_inspection
// for config portability. The controller's identity-certificate scoping
// fields are not modeled; the raw merge preserves them across updates.
type settingSslInspectionModel struct {
	State types.String `tfsdk:"state"`
}

var sslInspectionAttrTypes = map[string]attr.Type{
	"state": types.StringType,
}

type sslInspectionSection struct{}

func (sslInspectionSection) key() string { return "ssl_inspection" }

func (sslInspectionSection) attrTypes() map[string]attr.Type { return sslInspectionAttrTypes }

func (sslInspectionSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "SSL/TLS inspection settings. Controller-managed identity-certificate " +
			"scoping fields are preserved across updates.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"state": schema.StringAttribute{
				MarkdownDescription: "SSL inspection mode: `off`, `simple`, or `advanced`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("off", "simple", "advanced"),
				},
			},
		},
	}
}

func (sslInspectionSection) get(m *settingResourceModel) types.Object { return m.SslInspection }

func (sslInspectionSection) set(m *settingResourceModel, obj types.Object) { m.SslInspection = obj }

func (sslInspectionSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingSslInspectionModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	sslInspectionModelToData(&m, data)
	return diags
}

// sslInspectionModelToData writes only the user-set fields into the raw
// section document; unset fields — including the controller's unmodeled
// identity_certificate_* fields — keep their remote values.
func sslInspectionModelToData(m *settingSslInspectionModel, data map[string]any) {
	if !m.State.IsNull() && !m.State.IsUnknown() {
		data["state"] = m.State.ValueString()
	}
}

func (sslInspectionSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.SslInspection](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(sslInspectionAttrTypes), diags
		}
		diags.AddError("Error Reading SSL Inspection Setting", err.Error())
		return types.ObjectNull(sslInspectionAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, sslInspectionAttrTypes, sslInspectionSettingToModel(setting))
}

func sslInspectionSettingToModel(s *settings.SslInspection) settingSslInspectionModel {
	return settingSslInspectionModel{State: util.StringValueOrNull(s.State)}
}
```

- [ ] **Step 4: Register the section**

`unifi/setting_section.go` registry (after `netflowSection{}`):

```go
	sslInspectionSection{},
```

`unifi/setting_resource.go` model (after `Netflow`):

```go
	SslInspection types.Object   `tfsdk:"ssl_inspection"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_sslInspection|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_ssl_inspection.go unifi/setting_section_ssl_inspection_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add ssl_inspection section"
```

(Body should note: `state` aligns with filipowm; the controller's identity_certificate_* fields are unmodeled and preserved by the raw merge.)

---

## Tranche B — GATED on the go-unifi PR 0 bump

> **PRECONDITION for Tasks 4–6:** these sections need typed read structs that only exist after the go-unifi PR 0 lands: `settings.GlobalNetwork`, `settings.UsgGeo`, `settings.Ipsec`. Before starting Task 4, verify:
>
> ```bash
> go doc github.com/ubiquiti-community/go-unifi/unifi/settings GlobalNetwork
> go doc github.com/ubiquiti-community/go-unifi/unifi/settings UsgGeo
> go doc github.com/ubiquiti-community/go-unifi/unifi/settings Ipsec
> ```
>
> - If all three resolve: proceed. Record the actual field names — the plan pins expected names below (go-unifi generator initialism conventions, cf. `DHCPSnoop`/`RADIUSProfileID`), but ONLY the typed read paths depend on them; the raw overlay uses JSON keys and is unaffected. If the generator produced different Go names (e.g. `IpFiltering` vs `IPFiltering`, `IKEv2…` vs `Ikev2…`), adjust the `…SettingToModel` functions and their tests accordingly — nothing else changes.
> - If they do not resolve: either bump `go.mod` to the go-unifi release containing PR 0, or add a development-only directive `replace github.com/ubiquiti-community/go-unifi => /Users/jamesb/projects/go-unifi` (MUST be dropped before release — note it in the final report), then `go mod tidy`.
> - If PR 0 has not landed at all: **skip Tasks 4–6 entirely** and continue at Task 7 with tranche A only. Do not stub, guess, or hand-write structs in this repo.

### Task 4 (GATED): `global_network` section

**Files:**
- Create: `unifi/setting_section_global_network.go`
- Create: `unifi/setting_section_global_network_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settings.GlobalNetwork` — expected shape from the live payload: `DefaultSecurityPosture string // json default_security_posture` (live value class: `ALLOW_ALL`).
- Produces: `settingGlobalNetworkModel`, `globalNetworkAttrTypes`, `globalNetworkSection`, `globalNetworkModelToData(m, data)`, `globalNetworkSettingToModel(s)`.
- No enum validator: only one live value observed; the controller's full value set is unverified.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_global_network_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_globalNetworkModelToData(t *testing.T) {
	m := &settingGlobalNetworkModel{
		DefaultSecurityPosture: types.StringValue("ALLOW_ALL"),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": "keep"}

	globalNetworkModelToData(m, data)

	if data["default_security_posture"] != "ALLOW_ALL" {
		t.Fatalf("default_security_posture = %v", data["default_security_posture"])
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_globalNetworkModelToData_nullLeavesRemoteValue(t *testing.T) {
	m := &settingGlobalNetworkModel{DefaultSecurityPosture: types.StringNull()}
	data := map[string]any{"default_security_posture": "ALLOW_ALL"}

	globalNetworkModelToData(m, data)

	if data["default_security_posture"] != "ALLOW_ALL" {
		t.Fatal("null posture overwrote remote value")
	}
}

func Test_globalNetworkSettingToModel(t *testing.T) {
	m := globalNetworkSettingToModel(&settings.GlobalNetwork{
		DefaultSecurityPosture: "ALLOW_ALL",
	})
	if m.DefaultSecurityPosture.ValueString() != "ALLOW_ALL" {
		t.Fatalf("default_security_posture = %v", m.DefaultSecurityPosture)
	}
	empty := globalNetworkSettingToModel(&settings.GlobalNetwork{})
	if !empty.DefaultSecurityPosture.IsNull() {
		t.Fatalf("empty posture should map to null, got %v", empty.DefaultSecurityPosture)
	}
}

func Test_settingResource_Schema_globalNetwork(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["global_network"]; !ok {
		t.Fatal("schema is missing the global_network section attribute")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_globalNetwork|Test_settingResource_Schema_globalNetwork' -count=1`
Expected: compile FAILURE — `undefined: settingGlobalNetworkModel` etc. (If instead the failure is `undefined: settings.GlobalNetwork`, the tranche precondition was not met — stop and re-check the gate.)

- [ ] **Step 3: Create `unifi/setting_section_global_network.go`**

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

// settingGlobalNetworkModel is the nested global_network block: site-wide
// network defaults (currently the default security posture).
type settingGlobalNetworkModel struct {
	DefaultSecurityPosture types.String `tfsdk:"default_security_posture"`
}

var globalNetworkAttrTypes = map[string]attr.Type{
	"default_security_posture": types.StringType,
}

type globalNetworkSection struct{}

func (globalNetworkSection) key() string { return "global_network" }

func (globalNetworkSection) attrTypes() map[string]attr.Type { return globalNetworkAttrTypes }

func (globalNetworkSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site-wide network defaults.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"default_security_posture": schema.StringAttribute{
				MarkdownDescription: "Default security posture for new networks (e.g. `ALLOW_ALL`).",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func (globalNetworkSection) get(m *settingResourceModel) types.Object { return m.GlobalNetwork }

func (globalNetworkSection) set(m *settingResourceModel, obj types.Object) { m.GlobalNetwork = obj }

func (globalNetworkSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingGlobalNetworkModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	globalNetworkModelToData(&m, data)
	return diags
}

// globalNetworkModelToData writes only the user-set fields into the raw
// section document; unset fields keep their remote values.
func globalNetworkModelToData(m *settingGlobalNetworkModel, data map[string]any) {
	if !m.DefaultSecurityPosture.IsNull() && !m.DefaultSecurityPosture.IsUnknown() {
		data["default_security_posture"] = m.DefaultSecurityPosture.ValueString()
	}
}

func (globalNetworkSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.GlobalNetwork](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(globalNetworkAttrTypes), diags
		}
		diags.AddError("Error Reading Global Network Setting", err.Error())
		return types.ObjectNull(globalNetworkAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, globalNetworkAttrTypes, globalNetworkSettingToModel(setting))
}

func globalNetworkSettingToModel(s *settings.GlobalNetwork) settingGlobalNetworkModel {
	return settingGlobalNetworkModel{
		DefaultSecurityPosture: util.StringValueOrNull(s.DefaultSecurityPosture),
	}
}
```

- [ ] **Step 4: Register the section**

`unifi/setting_section.go` registry (after `sslInspectionSection{}`):

```go
	globalNetworkSection{},
```

`unifi/setting_resource.go` model (after `SslInspection`):

```go
	GlobalNetwork types.Object   `tfsdk:"global_network"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_globalNetwork|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_global_network.go unifi/setting_section_global_network_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add global_network section"
```

---

### Task 5 (GATED): `usg_geo` section

**Files:**
- Create: `unifi/setting_section_usg_geo.go`
- Create: `unifi/setting_section_usg_geo_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settings.UsgGeo` — expected shape from the live payload and go-unifi nested-object conventions (single nested objects are pointers, cf. `settings.Usg.DNSVerification *SettingUsgDNSVerification`): `IPFiltering *SettingUsgGeoIPFiltering // json ip_filtering` with `Action string // block|allow`, `Countries string // comma-separated ISO codes, e.g. "NZ,AU"`, `Enabled bool`, `TrafficDirection string // both|ingress|egress`. Enums grounded in the equivalent `settings.Usg` fields `geo_ip_filtering_block // block|allow` and `geo_ip_filtering_traffic_direction // ^(both|ingress|egress)$`.
- Produces: `settingUsgGeoModel`, `settingUsgGeoIPFilteringModel`, `usgGeoAttrTypes`, `usgGeoIPFilteringAttrTypes`, `usgGeoSection`, `usgGeoModelToData(ctx, m, data, diags)`, `usgGeoSettingToModel(ctx, s, diags)`.
- `countries` stays a comma-separated string, matching the raw controller document (live shows `""`) and the `settings.Usg` regex `^([A-Z]{2})?(,[A-Z]{2}){0,149}$` — not a set; splitting/joining would invent ordering semantics the API doesn't have documented.
- The nested `ip_filtering` overlay merges INTO the existing nested map (unmodeled nested keys preserved) rather than replacing it.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_usg_geo_test.go`:

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

func Test_usgGeoModelToData(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	filtering, d := types.ObjectValueFrom(ctx, usgGeoIPFilteringAttrTypes,
		settingUsgGeoIPFilteringModel{
			Action:           types.StringNull(),
			Countries:        types.StringValue("NZ,AU"),
			Enabled:          types.BoolValue(true),
			TrafficDirection: types.StringValue("both"),
		})
	if d.HasError() {
		t.Fatal(d)
	}
	m := &settingUsgGeoModel{IPFiltering: filtering}

	// Both top-level and nested unmodeled fields must survive the merge, and
	// a null nested attribute must not clobber the remote nested value.
	data := map[string]any{
		"unmodeled_field": "keep",
		"ip_filtering": map[string]any{
			"action":          "block",
			"nested_unmodeled": true,
		},
	}

	usgGeoModelToData(ctx, m, data, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}

	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
	nested, ok := data["ip_filtering"].(map[string]any)
	if !ok {
		t.Fatalf("ip_filtering = %T", data["ip_filtering"])
	}
	if nested["action"] != "block" {
		t.Fatal("null action overwrote remote nested value")
	}
	if nested["nested_unmodeled"] != true {
		t.Fatal("nested unmodeled field was clobbered")
	}
	if nested["countries"] != "NZ,AU" || nested["enabled"] != true ||
		nested["traffic_direction"] != "both" {
		t.Fatalf("nested fields wrong: %v", nested)
	}
}

func Test_usgGeoModelToData_nullObjectLeavesRemoteValues(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := &settingUsgGeoModel{IPFiltering: types.ObjectNull(usgGeoIPFilteringAttrTypes)}
	data := map[string]any{"ip_filtering": map[string]any{"action": "block"}}

	usgGeoModelToData(ctx, m, data, &diags)

	nested := data["ip_filtering"].(map[string]any)
	if nested["action"] != "block" {
		t.Fatal("null ip_filtering object overwrote remote values")
	}
}

func Test_usgGeoSettingToModel(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	m := usgGeoSettingToModel(ctx, &settings.UsgGeo{
		IPFiltering: &settings.SettingUsgGeoIPFiltering{
			Action:           "block",
			Countries:        "NZ",
			Enabled:          true,
			TrafficDirection: "both",
		},
	}, &diags)
	if diags.HasError() {
		t.Fatal(diags)
	}
	var f settingUsgGeoIPFilteringModel
	diags.Append(m.IPFiltering.As(ctx, &f, basetypes.ObjectAsOptions{})...)
	if f.Action.ValueString() != "block" || f.Countries.ValueString() != "NZ" ||
		!f.Enabled.ValueBool() || f.TrafficDirection.ValueString() != "both" {
		t.Fatalf("ip_filtering = %+v", f)
	}

	empty := usgGeoSettingToModel(ctx, &settings.UsgGeo{}, &diags)
	if !empty.IPFiltering.IsNull() {
		t.Fatalf("nil IPFiltering should map to a null object, got %v", empty.IPFiltering)
	}
}

func Test_settingResource_Schema_usgGeo(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["usg_geo"]; !ok {
		t.Fatal("schema is missing the usg_geo section attribute")
	}
}
```

(Add `"github.com/hashicorp/terraform-plugin-framework/types/basetypes"` to the imports for the `As` call.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_usgGeo|Test_settingResource_Schema_usgGeo' -count=1`
Expected: compile FAILURE — `undefined: settingUsgGeoModel` etc. (If the failure is `undefined: settings.UsgGeo` or a mismatched field name like `IPFiltering`, reconcile with the actual generated struct per the tranche precondition — fix the test's typed literals, not the raw-merge assertions.)

- [ ] **Step 3: Create `unifi/setting_section_usg_geo.go`**

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
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingUsgGeoModel is the nested usg_geo block: gateway GeoIP filtering.
type settingUsgGeoModel struct {
	IPFiltering types.Object `tfsdk:"ip_filtering"`
}

type settingUsgGeoIPFilteringModel struct {
	Action           types.String `tfsdk:"action"`
	Countries        types.String `tfsdk:"countries"`
	Enabled          types.Bool   `tfsdk:"enabled"`
	TrafficDirection types.String `tfsdk:"traffic_direction"`
}

var (
	usgGeoIPFilteringAttrTypes = map[string]attr.Type{
		"action":            types.StringType,
		"countries":         types.StringType,
		"enabled":           types.BoolType,
		"traffic_direction": types.StringType,
	}
	usgGeoAttrTypes = map[string]attr.Type{
		"ip_filtering": types.ObjectType{AttrTypes: usgGeoIPFilteringAttrTypes},
	}
)

type usgGeoSection struct{}

func (usgGeoSection) key() string { return "usg_geo" }

func (usgGeoSection) attrTypes() map[string]attr.Type { return usgGeoAttrTypes }

func (usgGeoSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Gateway GeoIP filtering settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"ip_filtering": schema.SingleNestedAttribute{
				MarkdownDescription: "GeoIP-based traffic filtering.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"action": schema.StringAttribute{
						MarkdownDescription: "Whether the country list is blocked or allowed: `block` or `allow`.",
						Optional:            true,
						Computed:            true,
						Validators: []validator.String{
							stringvalidator.OneOf("block", "allow"),
						},
					},
					"countries": schema.StringAttribute{
						MarkdownDescription: "Comma-separated ISO 3166-1 alpha-2 country codes (e.g. `NZ,AU`).",
						Optional:            true,
						Computed:            true,
					},
					"enabled": schema.BoolAttribute{
						MarkdownDescription: "Enable GeoIP filtering.",
						Optional:            true,
						Computed:            true,
					},
					"traffic_direction": schema.StringAttribute{
						MarkdownDescription: "Filtered traffic direction: `both`, `ingress`, or `egress`.",
						Optional:            true,
						Computed:            true,
						Validators: []validator.String{
							stringvalidator.OneOf("both", "ingress", "egress"),
						},
					},
				},
			},
		},
	}
}

func (usgGeoSection) get(m *settingResourceModel) types.Object { return m.UsgGeo }

func (usgGeoSection) set(m *settingResourceModel, obj types.Object) { m.UsgGeo = obj }

func (usgGeoSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingUsgGeoModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	usgGeoModelToData(ctx, &m, data, &diags)
	return diags
}

// usgGeoModelToData writes only the user-set fields into the raw section
// document. The nested ip_filtering object merges into the existing nested
// map so unmodeled nested fields keep their remote values too.
func usgGeoModelToData(
	ctx context.Context,
	m *settingUsgGeoModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if m.IPFiltering.IsNull() || m.IPFiltering.IsUnknown() {
		return
	}
	var f settingUsgGeoIPFilteringModel
	diags.Append(m.IPFiltering.As(ctx, &f, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return
	}
	nested, _ := data["ip_filtering"].(map[string]any)
	if nested == nil {
		nested = map[string]any{}
	}
	if !f.Action.IsNull() && !f.Action.IsUnknown() {
		nested["action"] = f.Action.ValueString()
	}
	if !f.Countries.IsNull() && !f.Countries.IsUnknown() {
		nested["countries"] = f.Countries.ValueString()
	}
	if !f.Enabled.IsNull() && !f.Enabled.IsUnknown() {
		nested["enabled"] = f.Enabled.ValueBool()
	}
	if !f.TrafficDirection.IsNull() && !f.TrafficDirection.IsUnknown() {
		nested["traffic_direction"] = f.TrafficDirection.ValueString()
	}
	data["ip_filtering"] = nested
}

func (usgGeoSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.UsgGeo](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(usgGeoAttrTypes), diags
		}
		diags.AddError("Error Reading USG Geo Setting", err.Error())
		return types.ObjectNull(usgGeoAttrTypes), diags
	}
	model := usgGeoSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(usgGeoAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, usgGeoAttrTypes, model)
}

func usgGeoSettingToModel(
	ctx context.Context,
	s *settings.UsgGeo,
	diags *diag.Diagnostics,
) settingUsgGeoModel {
	if s.IPFiltering == nil {
		return settingUsgGeoModel{IPFiltering: types.ObjectNull(usgGeoIPFilteringAttrTypes)}
	}
	obj, d := types.ObjectValueFrom(ctx, usgGeoIPFilteringAttrTypes, settingUsgGeoIPFilteringModel{
		Action:           util.StringValueOrNull(s.IPFiltering.Action),
		Countries:        util.StringValueOrNull(s.IPFiltering.Countries),
		Enabled:          types.BoolValue(s.IPFiltering.Enabled),
		TrafficDirection: util.StringValueOrNull(s.IPFiltering.TrafficDirection),
	})
	diags.Append(d...)
	return settingUsgGeoModel{IPFiltering: obj}
}
```

- [ ] **Step 4: Register the section**

`unifi/setting_section.go` registry (after `globalNetworkSection{}`):

```go
	usgGeoSection{},
```

`unifi/setting_resource.go` model (after `GlobalNetwork`):

```go
	UsgGeo        types.Object   `tfsdk:"usg_geo"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_usgGeo|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_usg_geo.go unifi/setting_section_usg_geo_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add usg_geo section"
```

(Body should note: nested ip_filtering merges into the existing nested map so unmodeled nested keys survive; countries stays a comma-separated string per the controller schema regex.)

---

### Task 6 (GATED): `ipsec` section

**Files:**
- Create: `unifi/setting_section_ipsec.go`
- Create: `unifi/setting_section_ipsec_test.go`
- Modify: `unifi/setting_section.go` (registry slice), `unifi/setting_resource.go` (model field)

**Interfaces:**
- Consumes: `settingSection`, `settings.Ipsec` — expected shape from the live payload: `Ikev2ReauthenticationMethod string // json ikev2_reauthentication_method` (live value class: `make-before-break`; verify the generated Go name per the tranche precondition — the generator may emit `IKEv2ReauthenticationMethod`).
- Produces: `settingIpsecModel`, `ipsecAttrTypes`, `ipsecSection`, `ipsecModelToData(m, data)`, `ipsecSettingToModel(s)`.
- No enum validator: only one live value observed.

- [ ] **Step 1: Write the failing tests**

Create `unifi/setting_section_ipsec_test.go`:

```go
package unifi

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func Test_ipsecModelToData(t *testing.T) {
	m := &settingIpsecModel{
		Ikev2ReauthenticationMethod: types.StringValue("make-before-break"),
	}
	// Pre-existing raw fields must be preserved, not clobbered.
	data := map[string]any{"unmodeled_field": "keep"}

	ipsecModelToData(m, data)

	if data["ikev2_reauthentication_method"] != "make-before-break" {
		t.Fatalf("ikev2_reauthentication_method = %v", data["ikev2_reauthentication_method"])
	}
	if data["unmodeled_field"] != "keep" {
		t.Fatal("unmodeled_field was clobbered")
	}
}

func Test_ipsecModelToData_nullLeavesRemoteValue(t *testing.T) {
	m := &settingIpsecModel{Ikev2ReauthenticationMethod: types.StringNull()}
	data := map[string]any{"ikev2_reauthentication_method": "make-before-break"}

	ipsecModelToData(m, data)

	if data["ikev2_reauthentication_method"] != "make-before-break" {
		t.Fatal("null method overwrote remote value")
	}
}

func Test_ipsecSettingToModel(t *testing.T) {
	m := ipsecSettingToModel(&settings.Ipsec{
		Ikev2ReauthenticationMethod: "make-before-break",
	})
	if m.Ikev2ReauthenticationMethod.ValueString() != "make-before-break" {
		t.Fatalf("method = %v", m.Ikev2ReauthenticationMethod)
	}
	empty := ipsecSettingToModel(&settings.Ipsec{})
	if !empty.Ikev2ReauthenticationMethod.IsNull() {
		t.Fatalf("empty method should map to null, got %v", empty.Ikev2ReauthenticationMethod)
	}
}

func Test_settingResource_Schema_ipsec(t *testing.T) {
	r := &settingResource{}
	var resp fwresource.SchemaResponse
	r.Schema(context.Background(), fwresource.SchemaRequest{}, &resp)
	if _, ok := resp.Schema.Attributes["ipsec"]; !ok {
		t.Fatal("schema is missing the ipsec section attribute")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./unifi/ -run 'Test_ipsec|Test_settingResource_Schema_ipsec' -count=1`
Expected: compile FAILURE — `undefined: settingIpsecModel` etc. (A `settings.Ipsec` field-name mismatch means the generator used a different initialism — adjust the typed literals per the tranche precondition.)

- [ ] **Step 3: Create `unifi/setting_section_ipsec.go`**

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

// settingIpsecModel is the nested ipsec block: site-wide IPsec/IKEv2
// behavior (currently the IKEv2 reauthentication method).
type settingIpsecModel struct {
	Ikev2ReauthenticationMethod types.String `tfsdk:"ikev2_reauthentication_method"`
}

var ipsecAttrTypes = map[string]attr.Type{
	"ikev2_reauthentication_method": types.StringType,
}

type ipsecSection struct{}

func (ipsecSection) key() string { return "ipsec" }

func (ipsecSection) attrTypes() map[string]attr.Type { return ipsecAttrTypes }

func (ipsecSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site-wide IPsec settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"ikev2_reauthentication_method": schema.StringAttribute{
				MarkdownDescription: "IKEv2 reauthentication method (e.g. `make-before-break`).",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func (ipsecSection) get(m *settingResourceModel) types.Object { return m.Ipsec }

func (ipsecSection) set(m *settingResourceModel, obj types.Object) { m.Ipsec = obj }

func (ipsecSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingIpsecModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	ipsecModelToData(&m, data)
	return diags
}

// ipsecModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values.
func ipsecModelToData(m *settingIpsecModel, data map[string]any) {
	if !m.Ikev2ReauthenticationMethod.IsNull() && !m.Ikev2ReauthenticationMethod.IsUnknown() {
		data["ikev2_reauthentication_method"] = m.Ikev2ReauthenticationMethod.ValueString()
	}
}

func (ipsecSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Ipsec](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(ipsecAttrTypes), diags
		}
		diags.AddError("Error Reading IPsec Setting", err.Error())
		return types.ObjectNull(ipsecAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, ipsecAttrTypes, ipsecSettingToModel(setting))
}

func ipsecSettingToModel(s *settings.Ipsec) settingIpsecModel {
	return settingIpsecModel{
		Ikev2ReauthenticationMethod: util.StringValueOrNull(s.Ikev2ReauthenticationMethod),
	}
}
```

- [ ] **Step 4: Register the section**

`unifi/setting_section.go` registry (after `usgGeoSection{}`):

```go
	ipsecSection{},
```

`unifi/setting_resource.go` model (after `UsgGeo`):

```go
	Ipsec         types.Object   `tfsdk:"ipsec"`
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./unifi/ -run 'Test_ipsec|Test_settingResource_Schema' -count=1`
Expected: PASS.

- [ ] **Step 6: Full unit suite + commit**

Run: `go build ./... && go test ./unifi/ -count=1 -short 2>&1 | tail -5`
Expected: PASS.

```bash
git add unifi/setting_section_ipsec.go unifi/setting_section_ipsec_test.go unifi/setting_section.go unifi/setting_resource.go
git commit -m "feat(setting): add ipsec section"
```

---

### Task 7: Acceptance tests against the docker demo controller

**Files:**
- Modify: `unifi/setting_section_snmp_test.go`, `unifi/setting_section_netflow_test.go`, `unifi/setting_section_ssl_inspection_test.go` (append acceptance tests)
- Modify (only if tranche B was implemented): `unifi/setting_section_global_network_test.go`, `unifi/setting_section_usg_geo_test.go`, `unifi/setting_section_ipsec_test.go`

**Interfaces:**
- Consumes: `preCheck(t)`, `testAccProtoV6ProviderFactories` (see `unifi/provider_test.go`), `resource.Test` from terraform-plugin-testing.
- Sensitive attributes are still checkable with `TestCheckResourceAttr` (sensitivity affects display, not state); all secret-shaped values are synthetic `tfacc-…` strings.

- [ ] **Step 1: Append tranche A acceptance tests**

To `unifi/setting_section_snmp_test.go` (add import `"github.com/hashicorp/terraform-plugin-testing/helper/resource"`; alias if it collides with `fwresource` — the file already imports the framework package under that alias):

```go
func TestAccSettingResource_snmp(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_snmp(true, "tfacc-ro"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "snmp.enabled", "true",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "snmp.community", "tfacc-ro",
					),
				),
			},
			{
				Config: testAccSettingConfig_snmp(false, "tfacc-ro2"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "snmp.enabled", "false",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "snmp.community", "tfacc-ro2",
					),
				),
			},
		},
	})
}

func testAccSettingConfig_snmp(enabled bool, community string) string {
	return fmt.Sprintf(`
resource "unifi_setting" "test" {
  snmp = {
    enabled   = %t
    community = %q
  }
}
`, enabled, community)
}
```

(Add `"fmt"` to that file's imports.)

To `unifi/setting_section_netflow_test.go` (same import notes):

```go
func TestAccSettingResource_netflow(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_netflow(2055),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "netflow.enabled", "false",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "netflow.server", "192.0.2.10",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "netflow.port", "2055",
					),
				),
			},
			{
				Config: testAccSettingConfig_netflow(2056),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "netflow.port", "2056",
				),
			},
		},
	})
}

func testAccSettingConfig_netflow(port int) string {
	return fmt.Sprintf(`
resource "unifi_setting" "test" {
  netflow = {
    enabled = false
    server  = "192.0.2.10"
    port    = %d
    version = 10
  }
}
`, port)
}
```

To `unifi/setting_section_ssl_inspection_test.go`:

```go
func TestAccSettingResource_sslInspection(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccSettingConfig_sslInspection("off"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "ssl_inspection.state", "off",
				),
			},
			{
				Config: testAccSettingConfig_sslInspection("simple"),
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "ssl_inspection.state", "simple",
				),
			},
		},
	})
}

func testAccSettingConfig_sslInspection(state string) string {
	return `
resource "unifi_setting" "test" {
  ssl_inspection = {
    state = "` + state + `"
  }
}
`
}
```

- [ ] **Step 2: Append tranche B acceptance tests (SKIP this step if Tasks 4–6 were skipped)**

To `unifi/setting_section_global_network_test.go`:

```go
func TestAccSettingResource_globalNetwork(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_setting" "test" {
  global_network = {
    default_security_posture = "ALLOW_ALL"
  }
}
`,
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "global_network.default_security_posture", "ALLOW_ALL",
				),
			},
		},
	})
}
```

To `unifi/setting_section_usg_geo_test.go`:

```go
func TestAccSettingResource_usgGeo(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_setting" "test" {
  usg_geo = {
    ip_filtering = {
      enabled           = false
      action            = "block"
      countries         = "NZ"
      traffic_direction = "both"
    }
  }
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "usg_geo.ip_filtering.enabled", "false",
					),
					resource.TestCheckResourceAttr(
						"unifi_setting.test", "usg_geo.ip_filtering.countries", "NZ",
					),
				),
			},
		},
	})
}
```

To `unifi/setting_section_ipsec_test.go`:

```go
func TestAccSettingResource_ipsec(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "unifi_setting" "test" {
  ipsec = {
    ikev2_reauthentication_method = "make-before-break"
  }
}
`,
				Check: resource.TestCheckResourceAttr(
					"unifi_setting.test", "ipsec.ikev2_reauthentication_method", "make-before-break",
				),
			},
		},
	})
}
```

- [ ] **Step 3: Start the demo controller and run**

Check `unifi/provider_test.go` `preCheck` for the exact env vars, then:

```bash
docker compose up -d
# wait for healthy:
docker compose ps
```

Run: `TF_ACC=1 go test ./unifi/ -run 'TestAccSettingResource_(snmp|netflow|sslInspection|globalNetwork|usgGeo|ipsec)' -v -count=1 -timeout 15m` (with the `UNIFI_API`/`UNIFI_USERNAME`/`UNIFI_PASSWORD`/`UNIFI_INSECURE` values `preCheck` expects — read them from `unifi/provider_test.go` / CI workflow, do not guess).
Expected: PASS, or documented skips per the contingency below.

**Contingency:** if the demo controller rejects a section (e.g. `api.err.InvalidPayload`, `api.err.InvalidObject`, or a 400 because the simulated controller lacks the feature — `ssl_inspection`, `usg_geo`, and `global_network` are gateway/UDM-class features and are the most likely to be refused), add a skip-guard at the top of that acceptance test following the existing pattern in `unifi/setting_resource_test.go` (`TestAccSettingResource_dohCustomServers`):

```go
	// <key> requires a real gateway-class controller; the demo/simulation
	// controller rejects the section.
	if os.Getenv("UNIFI_SKIP_CONTAINER") == "" {
		t.Skip("<key> requires a real controller; set UNIFI_SKIP_CONTAINER to run")
	}
```

(add `"os"` to imports). Record which sections were skipped in the final report. Additional SNMP-specific contingency: if the SNMP test fails with `inconsistent result after apply` on `snmp.community` (controller not echoing the community on read-back), extend `preserveSnmpPassword` to also carry `community` from the prior object (rename it `preserveSnmpSecrets`, update its unit test), and change the community read in `snmpSettingToModel` to `types.StringNull()` — do NOT weaken the unit tests' raw-merge assertions.

- [ ] **Step 4: Commit**

```bash
git add unifi/setting_section_*_test.go
git commit -m "test(setting): acceptance coverage for monitoring & security sections"
```

(Body lists which sections got demo-controller skip-guards and why.)

---

### Task 8: Docs, changelog, final verification

**Files:**
- Modify: `examples/resources/unifi_setting/resource.tf` (add the new sections to the example)
- Modify: `CHANGELOG.md` (Unreleased → Features)
- Generated: `docs/resources/setting.md` (via `go generate ./...`)

- [ ] **Step 1: Extend the example**

Append to `examples/resources/unifi_setting/resource.tf` (read it first; match its commenting style and how PRs 1–2 folded their sections in — keep one example block per theme). Omit the tranche B sections if Tasks 4–6 were skipped:

```terraform
# Monitoring & security settings. SNMP credentials are sensitive — source
# them from variables, never literals.
variable "snmp_community" {
  type      = string
  sensitive = true
}

resource "unifi_setting" "monitoring" {
  site = "default"

  snmp = {
    enabled   = true
    community = var.snmp_community
  }

  netflow = {
    enabled = true
    server  = "192.0.2.10"
    port    = 2055
    version = 10
  }

  ssl_inspection = {
    state = "off"
  }

  # Requires a gateway-class controller (UDM/USG):
  global_network = {
    default_security_posture = "ALLOW_ALL"
  }

  usg_geo = {
    ip_filtering = {
      enabled           = true
      action            = "block"
      countries         = "KP"
      traffic_direction = "both"
    }
  }

  ipsec = {
    ikev2_reauthentication_method = "make-before-break"
  }
}
```

Note: if the example file's existing convention is one `unifi_setting` resource per site, fold these sections into the existing block instead — follow the file, not this snippet.

- [ ] **Step 2: Regenerate docs**

Run: `go generate ./...`
Expected: `docs/resources/setting.md` gains the new section attribute documentation (with `community`/`password` rendered as sensitive). Inspect the diff: `git diff --stat docs/`.

- [ ] **Step 3: Changelog**

Add under `## [Unreleased]` → `### ✨ Features` in `CHANGELOG.md` (match the existing bolded-lede prose style). If tranche B was skipped, trim the entry to the three ungated sections and drop the gated clause:

```markdown
- **`unifi_setting`: new `snmp`, `netflow`, `ssl_inspection`, `global_network`, `usg_geo`, and `ipsec` sections.** Monitoring and security parity for the consolidated settings resource: the SNMP v1/v2c community and SNMPv3 agent credentials (`community` and `password` are sensitive; the password maps to the controller's write-only `x_password` and is never read back), the NetFlow/IPFIX exporter (collector, version 5/9/10, sampling), SSL inspection state (`off`/`simple`/`advanced`, attribute-compatible with the filipowm provider's `unifi_setting_ssl_inspection`), the site default security posture, gateway GeoIP filtering (`ip_filtering` block with country list and traffic direction), and the IKEv2 reauthentication method. All sections use the raw read-modify-write merge, so controller fields the SDK does not model (e.g. `ssl_inspection`'s identity-certificate scoping) are preserved verbatim.
```

- [ ] **Step 4: Full verification**

Run, in order:

```bash
go build ./...
go vet ./...
go test ./unifi/ -count=1 2>&1 | tail -5
```

Expected: all clean, tests PASS. If the repo has a lint config (`.golangci.yml`), also run `golangci-lint run ./unifi/...` if the tool is installed — do not install tools globally to satisfy this. If a `replace` directive was added for tranche B, confirm it is still present and flagged: `grep replace go.mod` — it must be called out in the final report as a pre-release removal item.

- [ ] **Step 5: Commit**

```bash
git add examples/resources/unifi_setting/resource.tf docs/ CHANGELOG.md
git commit -m "docs(setting): document monitoring & security sections"
```

- [ ] **Step 6: STOP — do not push**

The branch stays local. Report completion with: sections added (and whether tranche B executed or was gated out), test results (including any demo-controller skips), whether a go.mod `replace` is in place, and the diff stat. James reviews before anything is posted publicly. Manual `tofu plan` validation against the live UDM is a James-driven step after review — not part of this plan.

---

## Self-review notes

- Spec coverage (PR 3 scope only): `snmp` ✓ (Task 1, community + v3 password Sensitive per spec), `netflow` ✓ (Task 2), `ssl_inspection` ✓ (Task 3, filipowm `state` alignment), gated `global_network`/`usg_geo`/`ipsec` ✓ (Tasks 4–6 behind an explicit go-unifi PR 0 precondition with a documented skip path), docker-only acceptance ✓ (Task 7), docs + changelog ✓ (Task 8), no public push ✓ (Task 8 Step 6).
- Interface contract: every section implements exactly the PR 1 Task 1 `settingSection` method set (`key`/`attrTypes`/`schemaAttribute`/`get`/`set`/`overlay`/`read`) and registers in `settingSections`; the engine (`applySections`/`readSections`) is consumed, never modified. Model fields `Snmp`/`Netflow`/`SslInspection`/`GlobalNetwork`/`UsgGeo`/`Ipsec` match each section's `get`/`set`.
- Deviation from the plain per-field pattern, intentional: `snmpSection.set` preserves the configured password instead of installing the read-back value. `x_password` is write-only on the controller (repo precedent: `mgmt.ssh_password`, `setting_resource.go` ~line 2402); doing it in `set` keeps the fix inside the section without changing the engine or the interface signature. `community` is read back normally (no `x_` prefix ⇒ the controller returns it); Task 7 carries a contingency to move it into the preserve path if the demo controller proves otherwise.
- Raw-key gotchas encoded in tests: `enabledV3` stays camelCase and `x_password` keeps its prefix on the wire while the TF attributes are `enabled_v3`/`password`; the usg_geo overlay merges into the nested `ip_filtering` map rather than replacing it (nested unmodeled keys asserted).
- Tranche B typed names are pinned assumptions (`settings.GlobalNetwork.DefaultSecurityPosture`, `settings.UsgGeo.IPFiltering *SettingUsgGeoIPFiltering{Action, Countries, Enabled, TrafficDirection}`, `settings.Ipsec.Ikev2ReauthenticationMethod`) derived from the live payload plus the generator's conventions (`DHCPSnoop`/`RADIUSProfileID` initialisms; pointer single-nested objects like `settings.Usg.DNSVerification`). Only the typed read paths and their tests depend on these; the precondition block requires verifying with `go doc` before Task 4 and adjusting there.
- Validators are limited to enums documented in generated structs (`netflow` sampling/version/port, `ssl_inspection` state, `usg_geo` action/direction via the `settings.Usg` geo regexes). `default_security_posture` and `ikev2_reauthentication_method` are unvalidated: one observed value each, full value set unverified.
- Live-payload note: the reference UDM's `snmp` document only carries `enabled`/`enabledV3` (SNMP unconfigured there), so `community`/`username`/`x_password` come from the generated struct, not observation — the raw merge makes absent keys a non-issue.
