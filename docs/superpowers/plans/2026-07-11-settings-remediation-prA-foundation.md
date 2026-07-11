# PR-A: Settings Lifecycle Foundation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `unifi_setting` resource's two ad-hoc lifecycle paths with one snapshot-driven engine built on an explicit field-ownership codec, and migrate all 13 existing sections onto it with no user-facing behavior change beyond the named permitted deltas.

**Architecture:** A single `ListSettings` snapshot per operation is decoded/overlaid by per-section handlers implementing one `settingSection` interface. A shared codec branches on each field's ownership class (C1) for null/empty/clearing/secret/preservation behavior. Writes are raw read-modify-write merges that preserve unmodeled controller keys; the write path reconciles all sections before the first PUT and reconciles state from a fresh snapshot after. An injectable settings-client interface makes the whole lifecycle testable without a live controller.

**Tech Stack:** Go, terraform-plugin-framework v1.x, `github.com/ubiquiti-community/go-unifi` (via the `jamesbraid/go-unifi v1.34.1` replace). Flat `unifi/` package (follow the existing file-per-concern convention; no new subpackages).

## Global Constraints

- Spec of record: `docs/superpowers/specs/2026-07-11-settings-remediation-design.md`. This plan implements **only PR-A** (gates 1–5, 7, 10; the *framework* half of gates 6, 8, 9). It adds NO new user-facing section.
- The public `unifi_setting` schema attribute names and types are unchanged for all 13 sections → no state-schema migration. Any deviation is a plan failure.
- One codec, one engine: no section performs its own I/O; no per-section null/empty handling. (§2.3, §3.3.)
- Reconciliation before mutation: no controller write occurs before every configured section has decoded and produced its candidate overlay. (§2.2.)
- Universal preservation: every write is a raw merge over the section's snapshot object; unmodeled keys are never dropped. (§2.4.)
- Malformed remote data raises a diagnostic and aborts; it is never silently normalized. Present-empty ≠ absent. (§3.1, §3.2.)
- Migration is proved by request-level golden tests + a permitted-delta list + a section×operation coverage inventory. Behavior not on the permitted-delta list must be byte-identical to `origin/main`.
- Verification per task: `gofmt -w`, `go build ./...`, `go vet ./unifi/`, `go test ./unifi/...`, `git diff --check`. TF_ACC tests are demo-controller-only.
- Permitted deltas (the ONLY behavior changes allowed in PR-A):
  1. present-empty string/array now round-trips instead of collapsing to null;
  2. malformed remote scalar/list now raises a diagnostic instead of truncating/dropping;
  3. writes now preserve all unmodeled controller keys (previously section-dependent);
  4. one snapshot per op replaces per-section reads;
  5. import populates `site` + hydrates all sections (previously id-only passthrough).

---

## File Structure

New files (flat `unifi/` package):

- `unifi/setting_ownership.go` — `ownershipClass` type + per-class policy predicates.
- `unifi/setting_codec.go` — the shared codec: typed getters/setters that branch on ownership class; malformed→diagnostic; present-empty≠absent.
- `unifi/setting_snapshot.go` — `rawSettings` (wraps `[]settings.RawSetting`, indexed by key) + section lookup + `capabilityState`.
- `unifi/setting_capability.go` — `capabilityState` enum + mapping from snapshot/errors.
- `unifi/setting_section.go` — `settingSection` interface + `settingSections` registry slice + registry helpers.
- `unifi/setting_engine.go` — `applySections` (write path, partial-apply) and `readSections` (read path) orchestration over a `settingsClient`.
- `unifi/setting_client.go` — `settingsClient` interface (List/Update) + real adapter over `*Client`.
- `unifi/setting_discriminator.go` — reusable discriminator validators/plan-modifiers (C4 framework; no PR-A consumers).
- `unifi/site.go` — `resolveSite` shared helper (used here; reused by PR-V/D/E).
- `unifi/setting_section_<name>.go` × 13 — one migrated section each.

Modified:

- `unifi/setting_resource.go` — model unchanged; `Schema` builds section attributes from the registry; Create/Update/Read/Delete/ImportState call the engine; **all legacy per-section Create/Update/Read/converter code deleted**.

Test files mirror each source file (`*_test.go`), plus:

- `unifi/setting_fake_client_test.go` — in-memory `settingsClient` fake with fault injection.
- `unifi/setting_golden_test.go` — request-level golden tests capturing each legacy section's current wire output (written BEFORE migration).

---

## Task 1: Ownership taxonomy

**Files:**
- Create: `unifi/setting_ownership.go`
- Test: `unifi/setting_ownership_test.go`

**Interfaces:**
- Produces: `type ownershipClass int`; constants `ownerManaged, ownerCoManaged, ownerComputed, ownerWriteOnlySecret, ownerGeneratedSecret, ownerPreservedUnmanaged`; `func (c ownershipClass) sendsOnAbsentConfig() bool`; `func (c ownershipClass) readsFromAPI() bool`; `func (c ownershipClass) isSecret() bool`; `func (c ownershipClass) usesStateForUnknown() bool`.

- [ ] **Step 1: Write the failing test**

```go
// unifi/setting_ownership_test.go
package unifi

import "testing"

func TestOwnershipClassPolicy(t *testing.T) {
	cases := []struct {
		c                                            ownershipClass
		sendsOnAbsent, readsAPI, secret, stateForUnk bool
	}{
		{ownerManaged, false, true, false, false},
		{ownerCoManaged, false, true, false, true},
		{ownerComputed, false, true, false, true},
		{ownerWriteOnlySecret, false, false, true, false},
		{ownerGeneratedSecret, false, true, true, true},
		{ownerPreservedUnmanaged, false, false, false, false},
	}
	for _, tc := range cases {
		if tc.c.sendsOnAbsentConfig() != tc.sendsOnAbsent {
			t.Errorf("%v sendsOnAbsentConfig=%v want %v", tc.c, tc.c.sendsOnAbsentConfig(), tc.sendsOnAbsent)
		}
		if tc.c.readsFromAPI() != tc.readsAPI {
			t.Errorf("%v readsFromAPI=%v want %v", tc.c, tc.c.readsFromAPI(), tc.readsAPI)
		}
		if tc.c.isSecret() != tc.secret {
			t.Errorf("%v isSecret=%v want %v", tc.c, tc.c.isSecret(), tc.secret)
		}
		if tc.c.usesStateForUnknown() != tc.stateForUnk {
			t.Errorf("%v usesStateForUnknown=%v want %v", tc.c, tc.c.usesStateForUnknown(), tc.stateForUnk)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./unifi/ -run TestOwnershipClassPolicy`
Expected: FAIL (undefined `ownershipClass`).

- [ ] **Step 3: Write minimal implementation**

```go
// unifi/setting_ownership.go
package unifi

// ownershipClass classifies a settings field by who owns its value and how the
// codec must treat null/empty/clearing/secret/preservation. See spec C1.
type ownershipClass int

const (
	// ownerManaged: Optional+Computed. User owns when set; controller value
	// adopted when config is absent.
	ownerManaged ownershipClass = iota
	// ownerCoManaged: Managed, but the controller also mutates it out-of-band.
	ownerCoManaged
	// ownerComputed: controller sole owner, read-only.
	ownerComputed
	// ownerWriteOnlySecret: Optional+Sensitive, never read from the API,
	// preserved from prior state.
	ownerWriteOnlySecret
	// ownerGeneratedSecret: Computed+Sensitive, controller-generated.
	ownerGeneratedSecret
	// ownerPreservedUnmanaged: not in schema, raw-merge preserved verbatim.
	ownerPreservedUnmanaged
)

func (c ownershipClass) sendsOnAbsentConfig() bool { return false }

func (c ownershipClass) readsFromAPI() bool {
	switch c {
	case ownerWriteOnlySecret, ownerPreservedUnmanaged:
		return false
	default:
		return true
	}
}

func (c ownershipClass) isSecret() bool {
	return c == ownerWriteOnlySecret || c == ownerGeneratedSecret
}

func (c ownershipClass) usesStateForUnknown() bool {
	switch c {
	case ownerCoManaged, ownerComputed, ownerGeneratedSecret:
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./unifi/ -run TestOwnershipClassPolicy`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w unifi/setting_ownership.go unifi/setting_ownership_test.go
git add unifi/setting_ownership.go unifi/setting_ownership_test.go
git commit -m "feat(setting): field-ownership taxonomy (C1)"
```

---

## Task 2: Shared codec — string & int with the contract rules

**Files:**
- Create: `unifi/setting_codec.go`
- Test: `unifi/setting_codec_test.go`

**Interfaces:**
- Consumes: nothing (operates on `map[string]any` = a `settings.RawSetting.Data`).
- Produces:
  - `func codecString(data map[string]any, key string) (types.String, diag.Diagnostics)` — present-empty→StringValue(""); absent/JSON-null→StringNull; wrong type→diagnostic.
  - `func codecInt64(data map[string]any, key string) (types.Int64, diag.Diagnostics)` — integral number→Int64Value; fractional/non-numeric present→diagnostic; absent/null→Int64Null.
  - `func codecBool(data map[string]any, key string) (types.Bool, diag.Diagnostics)`.
  - `func codecStringList(ctx, data, key) (types.List, diag.Diagnostics)` — non-string member present→diagnostic.
  - `func setString(out map[string]any, key string, v types.String)` — writes present value incl. empty; skips null/unknown.
  - `func setInt64/setBool/setStringList` — analogous.

- [ ] **Step 1: Write the failing test** (the contract: empty≠absent; malformed→diag)

```go
// unifi/setting_codec_test.go
package unifi

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCodecString_presentEmptyIsValueNotNull(t *testing.T) {
	got, d := codecString(map[string]any{"k": ""}, "k")
	if d.HasError() {
		t.Fatalf("unexpected diag: %v", d)
	}
	if got.IsNull() || got.ValueString() != "" {
		t.Fatalf("present empty must be StringValue(\"\"), got %#v", got)
	}
}

func TestCodecString_absentIsNull(t *testing.T) {
	got, _ := codecString(map[string]any{}, "k")
	if !got.IsNull() {
		t.Fatalf("absent key must be null, got %#v", got)
	}
}

func TestCodecInt64_fractionalIsDiagnostic(t *testing.T) {
	_, d := codecInt64(map[string]any{"k": 1.9}, "k")
	if !d.HasError() {
		t.Fatalf("fractional number must raise a diagnostic, not truncate")
	}
}

func TestSetString_writesEmptyButSkipsNull(t *testing.T) {
	out := map[string]any{}
	setString(out, "a", types.StringValue(""))
	setString(out, "b", types.StringNull())
	if _, ok := out["a"]; !ok {
		t.Fatalf("empty string must be written")
	}
	if _, ok := out["b"]; ok {
		t.Fatalf("null must not be written")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./unifi/ -run TestCodec`
Expected: FAIL (undefined `codecString`).

- [ ] **Step 3: Write minimal implementation**

```go
// unifi/setting_codec.go
package unifi

import (
	"context"
	"fmt"
	"math"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// codecString reads key as a string. A present value (including "") is a value;
// an absent key or JSON null is Terraform null; a non-string present value is a
// diagnostic (never silently coerced). Spec C1 / §3.1 / §3.2.
func codecString(data map[string]any, key string) (types.String, diag.Diagnostics) {
	var d diag.Diagnostics
	v, ok := data[key]
	if !ok || v == nil {
		return types.StringNull(), d
	}
	s, ok := v.(string)
	if !ok {
		d.AddError("malformed setting value", fmt.Sprintf("%q: expected string, got %T", key, v))
		return types.StringNull(), d
	}
	return types.StringValue(s), d
}

// codecInt64 reads key as an integral number. JSON numbers decode to float64;
// a fractional value is malformed (diagnostic), not truncated. §3.2.
func codecInt64(data map[string]any, key string) (types.Int64, diag.Diagnostics) {
	var d diag.Diagnostics
	v, ok := data[key]
	if !ok || v == nil {
		return types.Int64Null(), d
	}
	f, ok := v.(float64)
	if !ok {
		d.AddError("malformed setting value", fmt.Sprintf("%q: expected number, got %T", key, v))
		return types.Int64Null(), d
	}
	if f != math.Trunc(f) {
		d.AddError("malformed setting value", fmt.Sprintf("%q: expected integer, got %v", key, f))
		return types.Int64Null(), d
	}
	return types.Int64Value(int64(f)), d
}

func codecBool(data map[string]any, key string) (types.Bool, diag.Diagnostics) {
	var d diag.Diagnostics
	v, ok := data[key]
	if !ok || v == nil {
		return types.BoolNull(), d
	}
	b, ok := v.(bool)
	if !ok {
		d.AddError("malformed setting value", fmt.Sprintf("%q: expected bool, got %T", key, v))
		return types.BoolNull(), d
	}
	return types.BoolValue(b), d
}

func codecStringList(ctx context.Context, data map[string]any, key string) (types.List, diag.Diagnostics) {
	var d diag.Diagnostics
	v, ok := data[key]
	if !ok || v == nil {
		return types.ListNull(types.StringType), d
	}
	raw, ok := v.([]any)
	if !ok {
		d.AddError("malformed setting value", fmt.Sprintf("%q: expected array, got %T", key, v))
		return types.ListNull(types.StringType), d
	}
	elems := make([]string, 0, len(raw))
	for i, m := range raw {
		s, ok := m.(string)
		if !ok {
			d.AddError("malformed setting value", fmt.Sprintf("%q[%d]: expected string, got %T", key, i, m))
			return types.ListNull(types.StringType), d
		}
		elems = append(elems, s)
	}
	lv, ld := types.ListValueFrom(ctx, types.StringType, elems)
	d.Append(ld...)
	return lv, d
}

func setString(out map[string]any, key string, v types.String) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	out[key] = v.ValueString()
}

func setInt64(out map[string]any, key string, v types.Int64) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	out[key] = v.ValueInt64()
}

func setBool(out map[string]any, key string, v types.Bool) {
	if v.IsNull() || v.IsUnknown() {
		return
	}
	out[key] = v.ValueBool()
}

func setStringList(ctx context.Context, out map[string]any, key string, v types.List) diag.Diagnostics {
	var d diag.Diagnostics
	if v.IsNull() || v.IsUnknown() {
		return d
	}
	var elems []string
	d.Append(v.ElementsAs(ctx, &elems, false)...)
	if d.HasError() {
		return d
	}
	arr := make([]any, len(elems))
	for i, s := range elems {
		arr[i] = s
	}
	out[key] = arr
	return d
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./unifi/ -run TestCodec && go test ./unifi/ -run TestSetString`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w unifi/setting_codec.go unifi/setting_codec_test.go
git add unifi/setting_codec.go unifi/setting_codec_test.go
git commit -m "feat(setting): shared value codec with empty!=absent and malformed diagnostics (C1/§3.1/§3.2)"
```

---

## Task 3: Snapshot type

**Files:**
- Create: `unifi/setting_snapshot.go`
- Test: `unifi/setting_snapshot_test.go`

**Interfaces:**
- Consumes: `settings.RawSetting` (`.Key`/`GetKey()`, `.Data map[string]any`, `.BaseSetting` with `NoEdit`/`Hidden`).
- Produces: `type rawSettings struct { byKey map[string]settings.RawSetting }`; `func newRawSettings([]settings.RawSetting) rawSettings`; `func (s rawSettings) section(key string) (settings.RawSetting, bool)`; `func (s rawSettings) has(key string) bool`.

- [ ] **Step 1: Write the failing test**

```go
// unifi/setting_snapshot_test.go
package unifi

import (
	"testing"

	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func TestRawSettings_lookup(t *testing.T) {
	rs := newRawSettings([]settings.RawSetting{
		{BaseSetting: settings.BaseSetting{Key: "mgmt"}, Data: map[string]any{"x_ssh_enabled": true}},
	})
	got, ok := rs.section("mgmt")
	if !ok || got.Data["x_ssh_enabled"] != true {
		t.Fatalf("expected mgmt section present, got %v ok=%v", got, ok)
	}
	if rs.has("absent") {
		t.Fatalf("absent section must not be reported present")
	}
}
```

- [ ] **Step 2: Run** `go test ./unifi/ -run TestRawSettings` → FAIL (undefined).

- [ ] **Step 3: Implement**

```go
// unifi/setting_snapshot.go
package unifi

import "github.com/ubiquiti-community/go-unifi/unifi/settings"

// rawSettings is one authoritative ListSettings result, indexed by section key.
// Sections decode from this; they never fetch. Spec C2.1.
type rawSettings struct {
	byKey map[string]settings.RawSetting
}

func newRawSettings(list []settings.RawSetting) rawSettings {
	m := make(map[string]settings.RawSetting, len(list))
	for _, s := range list {
		m[s.GetKey()] = s
	}
	return rawSettings{byKey: m}
}

func (s rawSettings) section(key string) (settings.RawSetting, bool) {
	v, ok := s.byKey[key]
	return v, ok
}

func (s rawSettings) has(key string) bool {
	_, ok := s.byKey[key]
	return ok
}
```

- [ ] **Step 4: Run** `go test ./unifi/ -run TestRawSettings` → PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w unifi/setting_snapshot.go unifi/setting_snapshot_test.go
git add unifi/setting_snapshot.go unifi/setting_snapshot_test.go
git commit -m "feat(setting): authoritative snapshot type (C2.1)"
```

---

## Task 4: Capability taxonomy

**Files:**
- Create: `unifi/setting_capability.go`
- Test: `unifi/setting_capability_test.go`

**Interfaces:**
- Produces: `type capabilityState int` (`capSupported, capUnsupported, capUnmaterialized, capUnauthorized, capUnknown`); `func sectionCapability(snap rawSettings, key string) capabilityState` (present→capSupported; absent→capUnsupported — refined per-section later); `func (c capabilityState) configuredError(section string) diag.Diagnostics` (Unsupported/Unauthorized/Unknown→error; else empty).

- [ ] **Step 1: Failing test**

```go
// unifi/setting_capability_test.go
package unifi

import "testing"

func TestSectionCapability(t *testing.T) {
	snap := newRawSettings(nil)
	if sectionCapability(snap, "mgmt") != capUnsupported {
		t.Fatalf("absent section must be capUnsupported")
	}
}

func TestConfiguredErrorFailsClosed(t *testing.T) {
	if !capUnsupported.configuredError("mgmt").HasError() {
		t.Fatalf("configured + unsupported must error")
	}
	if capSupported.configuredError("mgmt").HasError() {
		t.Fatalf("supported must not error")
	}
}
```

- [ ] **Step 2: Run** `go test ./unifi/ -run 'TestSectionCapability|TestConfiguredError'` → FAIL.

- [ ] **Step 3: Implement**

```go
// unifi/setting_capability.go
package unifi

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

type capabilityState int

const (
	capSupported capabilityState = iota
	capUnsupported
	capUnmaterialized
	capUnauthorized
	capUnknown
)

// sectionCapability decides support from snapshot key presence. Refinement of
// Supported vs Unmaterialized is a per-section concern (spec C6); the base rule
// is: present ⇒ Supported, absent ⇒ Unsupported.
func sectionCapability(snap rawSettings, key string) capabilityState {
	if snap.has(key) {
		return capSupported
	}
	return capUnsupported
}

// configuredError fails closed for a configured section that is not usable.
func (c capabilityState) configuredError(section string) diag.Diagnostics {
	var d diag.Diagnostics
	switch c {
	case capUnsupported:
		d.AddError("unsupported setting section",
			fmt.Sprintf("section %q is configured but not supported by this controller/product", section))
	case capUnauthorized:
		d.AddError("unauthorized setting section",
			fmt.Sprintf("section %q is configured but the account lacks permission", section))
	case capUnknown:
		d.AddError("setting capability undetermined",
			fmt.Sprintf("section %q capability could not be determined; retry", section))
	}
	return d
}
```

- [ ] **Step 4: Run** → PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w unifi/setting_capability.go unifi/setting_capability_test.go
git add unifi/setting_capability.go unifi/setting_capability_test.go
git commit -m "feat(setting): capability taxonomy failing closed for configured sections (C6)"
```

---

## Task 5: `settingsClient` interface + real adapter + fake

**Files:**
- Create: `unifi/setting_client.go`
- Test: `unifi/setting_fake_client_test.go`

**Interfaces:**
- Produces:
  - `type settingsClient interface { ListSettings(ctx, site) ([]settings.RawSetting, error); UpdateRawSetting(ctx, site string, s settings.RawSetting) error }`.
  - `type realSettingsClient struct { c *Client }` implementing it (`UpdateRawSetting` marshals the RawSetting via its `MarshalJSON` and PUTs through a raw setting update; see note).
  - `type fakeSettingsClient` (test-only) with in-memory sections + `failUpdateOn map[string]error` + `failList error` + recorded `puts []string` for fault injection.

**NOTE for implementer:** go-unifi exposes `(*ApiClient).UpdateSetting(ctx, site, settings.Setting)` (typed) and `ListSettings` (raw). PR-A writes raw merges, so `UpdateRawSetting` needs to PUT a `RawSetting`. `RawSetting` implements `settings.Setting` (via `BaseSetting`) and `MarshalJSON`, so `realSettingsClient.UpdateRawSetting` calls `c.UpdateSetting(ctx, site, &s)`. Verify `RawSetting` satisfies `settings.Setting` (it embeds `BaseSetting` which has `GetKey/SetKey`); if `UpdateSetting` re-marshals via the concrete type, confirm `RawSetting.MarshalJSON` is used. Add a golden test (Task 20) asserting the PUT body equals the merged map.

- [ ] **Step 1: Failing test** (fake round-trips + fault injection)

```go
// unifi/setting_fake_client_test.go
package unifi

import (
	"context"
	"errors"
	"testing"

	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func TestFakeClient_updateThenList(t *testing.T) {
	f := newFakeSettingsClient()
	f.set("mgmt", map[string]any{"x_ssh_enabled": false})
	err := f.UpdateRawSetting(context.Background(), "default",
		settings.RawSetting{BaseSetting: settings.BaseSetting{Key: "mgmt"}, Data: map[string]any{"x_ssh_enabled": true, "key": "mgmt"}})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := f.ListSettings(context.Background(), "default")
	rs := newRawSettings(list)
	got, _ := rs.section("mgmt")
	if got.Data["x_ssh_enabled"] != true {
		t.Fatalf("update not reflected: %v", got.Data)
	}
}

func TestFakeClient_faultInjection(t *testing.T) {
	f := newFakeSettingsClient()
	f.failUpdateOn = map[string]error{"radius": errors.New("boom")}
	err := f.UpdateRawSetting(context.Background(), "default",
		settings.RawSetting{BaseSetting: settings.BaseSetting{Key: "radius"}, Data: map[string]any{"key": "radius"}})
	if err == nil {
		t.Fatalf("expected injected failure")
	}
}
```

- [ ] **Step 2: Run** `go test ./unifi/ -run TestFakeClient` → FAIL.

- [ ] **Step 3: Implement** `unifi/setting_client.go` (real adapter) and the fake in the `_test.go` file.

```go
// unifi/setting_client.go
package unifi

import (
	"context"

	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// settingsClient is the injectable seam for the settings engine (spec C2.8).
type settingsClient interface {
	ListSettings(ctx context.Context, site string) ([]settings.RawSetting, error)
	UpdateRawSetting(ctx context.Context, site string, s settings.RawSetting) error
}

type realSettingsClient struct{ c *Client }

func (r realSettingsClient) ListSettings(ctx context.Context, site string) ([]settings.RawSetting, error) {
	return r.c.ListSettings(ctx, site)
}

func (r realSettingsClient) UpdateRawSetting(ctx context.Context, site string, s settings.RawSetting) error {
	return r.c.UpdateSetting(ctx, site, &s)
}
```

```go
// in unifi/setting_fake_client_test.go
type fakeSettingsClient struct {
	sections     map[string]map[string]any
	failList     error
	failUpdateOn map[string]error
	puts         []string
}

func newFakeSettingsClient() *fakeSettingsClient {
	return &fakeSettingsClient{sections: map[string]map[string]any{}, failUpdateOn: map[string]error{}}
}
func (f *fakeSettingsClient) set(key string, data map[string]any) { f.sections[key] = data }
func (f *fakeSettingsClient) ListSettings(_ context.Context, _ string) ([]settings.RawSetting, error) {
	if f.failList != nil {
		return nil, f.failList
	}
	var out []settings.RawSetting
	for k, d := range f.sections {
		cp := map[string]any{}
		for kk, vv := range d {
			cp[kk] = vv
		}
		out = append(out, settings.RawSetting{BaseSetting: settings.BaseSetting{Key: k}, Data: cp})
	}
	return out, nil
}
func (f *fakeSettingsClient) UpdateRawSetting(_ context.Context, _ string, s settings.RawSetting) error {
	if err := f.failUpdateOn[s.GetKey()]; err != nil {
		return err
	}
	f.puts = append(f.puts, s.GetKey())
	f.sections[s.GetKey()] = s.Data
	return nil
}
```

- [ ] **Step 4: Run** `go test ./unifi/ -run TestFakeClient` → PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w unifi/setting_client.go unifi/setting_fake_client_test.go
git add unifi/setting_client.go unifi/setting_fake_client_test.go
git commit -m "feat(setting): injectable settingsClient seam + in-memory fake with fault injection (C2.8)"
```

---

## Task 6: `settingSection` interface + registry

**Files:**
- Create: `unifi/setting_section.go`
- Test: `unifi/setting_section_test.go`

**Interfaces:**
- Consumes: `rawSettings`, `settingResourceModel` (existing, in setting_resource.go), `settings.RawSetting`.
- Produces:
  - ```go
    type settingSection interface {
        key() string
        attrName() string
        schemaAttribute() schema.Attribute
        decode(ctx context.Context, snap rawSettings, model *settingResourceModel) diag.Diagnostics
        overlay(ctx context.Context, model settingResourceModel, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics)
        capability(snap rawSettings) capabilityState
    }
    ```
    (`overlay` returns the merged RawSetting, a `configured bool` indicating whether the section is set and should be PUT, and diags.)
  - `var settingSections []settingSection` (registry).
  - `func registerSection(s settingSection)` (append + used by each section's `init()`).

- [ ] **Step 1: Failing test** — registry keys are unique and every section's `key()`/`attrName()` are non-empty.

```go
// unifi/setting_section_test.go
package unifi

import "testing"

func TestRegistryKeysUnique(t *testing.T) {
	seenKey := map[string]bool{}
	seenAttr := map[string]bool{}
	for _, s := range settingSections {
		if s.key() == "" || s.attrName() == "" {
			t.Fatalf("section has empty key/attrName: %#v", s)
		}
		if seenKey[s.key()] {
			t.Fatalf("duplicate section key %q", s.key())
		}
		if seenAttr[s.attrName()] {
			t.Fatalf("duplicate attr name %q", s.attrName())
		}
		seenKey[s.key()] = true
		seenAttr[s.attrName()] = true
	}
}
```

- [ ] **Step 2: Run** → initially PASS trivially (empty registry). To make it a real RED, add a temporary duplicate-detection unit by asserting `len(settingSections) >= 0` is insufficient; instead defer meaningful assertions to Task 8 when the first section registers. For now this task delivers the interface + registry compile surface.

  Adjust: the failing test for THIS task is a compile check — write `setting_section_test.go` referencing the interface method set:

```go
func TestSettingSectionInterfaceShape(t *testing.T) {
	var _ = func(s settingSection) (string, string) { return s.key(), s.attrName() }
}
```

Run: `go test ./unifi/ -run TestSettingSectionInterfaceShape` → FAIL (undefined `settingSection`).

- [ ] **Step 3: Implement** `unifi/setting_section.go`

```go
// unifi/setting_section.go
package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// settingSection is the one lifecycle contract for a unifi_setting section.
// Sections never perform I/O: they decode a shared snapshot and produce a
// merged RawSetting for the engine to PUT. Spec C2.
type settingSection interface {
	key() string
	attrName() string
	schemaAttribute() schema.Attribute
	decode(ctx context.Context, snap rawSettings, model *settingResourceModel) diag.Diagnostics
	overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics)
	capability(snap rawSettings) capabilityState
}

var settingSections []settingSection

func registerSection(s settingSection) { settingSections = append(settingSections, s) }
```

- [ ] **Step 4: Run** both tests → PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w unifi/setting_section.go unifi/setting_section_test.go
git add unifi/setting_section.go unifi/setting_section_test.go
git commit -m "feat(setting): settingSection interface + registry (C2, gate 10)"
```

---

## Task 7: The engine — read and write paths

**Files:**
- Create: `unifi/setting_engine.go`
- Test: `unifi/setting_engine_test.go`

**Interfaces:**
- Consumes: `settingsClient`, `settingSections`, `settingResourceModel`, `rawSettings`, ownership codec.
- Produces:
  - `func readSections(ctx, client settingsClient, site string, model *settingResourceModel, onlyConfigured bool) diag.Diagnostics` — one ListSettings; per-section capability check; decode configured (or all, when `onlyConfigured=false` for import).
  - `func applySections(ctx, client settingsClient, site string, plan, prior settingResourceModel) (settingResourceModel, diag.Diagnostics)` — reconcile ALL configured sections' overlays before the first PUT; PUT in registry order; on any PUT error stop; then re-read via `readSections(onlyConfigured=false)`; on re-read failure emit the best-effort state per C2.4; return the reconciled model.

- [ ] **Step 1: Failing tests** — the four load-bearing engine invariants, all via the fake:
  1. `TestEngine_noWriteBeforeReconcileError`: a section whose `overlay` returns a diagnostic ⇒ zero PUTs (`fake.puts` empty).
  2. `TestEngine_oneSnapshot`: read path calls `ListSettings` exactly once (wrap fake to count).
  3. `TestEngine_partialApplyReReads`: first section PUT ok, second injected-fail ⇒ error returned AND state reflects the first section's new value (re-read).
  4. `TestEngine_preservesUnmodeledKeys`: overlay sets one key; the PUT body still contains a pre-existing unmodeled key.

```go
// unifi/setting_engine_test.go — sketch of invariant (3); implementer writes 1,2,4 analogously.
func TestEngine_partialApplyReReads(t *testing.T) {
	f := newFakeSettingsClient()
	f.set("a", map[string]any{"key": "a", "v": float64(1)})
	f.set("b", map[string]any{"key": "b", "v": float64(1)})
	f.failUpdateOn = map[string]error{"b": errors.New("boom")}
	// two stub sections registered via a test-only registry override (see Step 3 note)
	// plan sets a.v=2 and b.v=2; expect: a PUT ok, b fails -> error, state.a.v==2
	// ...assert via readSections after applySections
}
```

- [ ] **Step 2: Run** `go test ./unifi/ -run TestEngine` → FAIL.

- [ ] **Step 3: Implement** `unifi/setting_engine.go`. Key algorithm (spec C2.2/2.4/2.5):

```go
func applySections(ctx context.Context, client settingsClient, site string, plan, prior settingResourceModel) (settingResourceModel, diag.Diagnostics) {
	var d diag.Diagnostics
	snap, err := listSnapshot(ctx, client, site)
	if err != nil {
		d.AddError("read settings failed", err.Error())
		return prior, d // abort before any write
	}
	// 1. reconcile ALL configured overlays before any PUT
	type pending struct {
		s  settingSection
		rs settings.RawSetting
	}
	var todo []pending
	for _, s := range settingSections {
		rs, configured, sd := s.overlay(ctx, plan, prior, snap)
		d.Append(sd...)
		if configured {
			todo = append(todo, pending{s, rs})
		}
	}
	if d.HasError() {
		return prior, d // no controller write on any conversion/validation error
	}
	// 2. PUT in registry order; stop at first failure
	var putErr error
	for _, p := range todo {
		if err := client.UpdateRawSetting(ctx, site, p.rs); err != nil {
			putErr = fmt.Errorf("section %q: %w", p.s.key(), err)
			break
		}
	}
	// 3. reconcile state from a fresh snapshot (canonical)
	out := plan
	if rd := readSections(ctx, client, site, &out, false); rd.HasError() {
		// C2.4 second-failure: best-effort state = plan (already the sent candidates); surface refresh diag
		d.Append(rd...)
		d.AddWarning("settings read-back failed after apply",
			"state may be stale; run `terraform refresh`")
	}
	if putErr != nil {
		d.AddError("settings apply failed", putErr.Error())
	}
	return out, d
}
```

(The `readSections` and `listSnapshot` helpers, and the exact best-effort merge for the second-failure path, are written in full here by the implementer following C2.4; secrets are preserved by the codec, not re-read.)

- [ ] **Step 4: Run** `go test ./unifi/ -run TestEngine` → PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w unifi/setting_engine.go unifi/setting_engine_test.go
git add unifi/setting_engine.go unifi/setting_engine_test.go
git commit -m "feat(setting): snapshot-driven engine with reconcile-before-mutate and partial-apply (C2.2/2.4/2.5)"
```

---

## Task 8: Golden characterization tests for the 13 legacy sections (BEFORE migration)

**Files:**
- Create: `unifi/setting_golden_test.go`

**Purpose:** Lock the current wire behavior of every legacy section so migration is provably non-regressive. Each golden asserts the exact PUT body the *current* (main) converter produces for a representative model, captured from `origin/main`'s converters.

**Interfaces:**
- Consumes: the existing `settingResource` converter methods (`autoSpeedtestModelToSetting`, etc.) — still present at this point.

- [ ] **Step 1:** For each of the 13 sections, write a golden test that builds a representative model, runs the current converter, marshals the resulting go-unifi setting to JSON, and asserts it equals a checked-in golden string. Example (auto_speedtest):

```go
func TestGolden_autoSpeedtest(t *testing.T) {
	r := &settingResource{}
	m := &settingAutoSpeedtestModel{Enabled: types.BoolValue(true), CronExpr: types.StringValue("0 3 * * *")}
	b, _ := json.Marshal(r.autoSpeedtestModelToSetting(m))
	const want = `{"key":"auto_speedtest",...}` // captured from origin/main
	if string(b) != want {
		t.Fatalf("wire drift:\n got %s\nwant %s", b, want)
	}
}
```

- [ ] **Step 2: Run** `go test ./unifi/ -run TestGolden` → PASS on current code (captures baseline).

- [ ] **Step 3–4:** N/A (characterization; no implementation).

- [ ] **Step 5: Commit**

```bash
gofmt -w unifi/setting_golden_test.go
git add unifi/setting_golden_test.go
git commit -m "test(setting): golden characterization of 13 legacy section wire outputs (pre-migration)"
```

**Coverage inventory (record in the PR review guide):** a table mapping each of the 13 sections × {decode, overlay} to its new section file, confirming every legacy branch has a replacement. The golden tests are updated in the migration tasks ONLY for a permitted delta, with the delta cited in the commit message.

---

## Tasks 9–21: Migrate the 13 legacy sections (one task each)

Each task migrates ONE section from the inline legacy converters to a
`settingSection` implementation, keeping the schema attribute identical. The
pattern is identical; **Task 9 is the fully-worked template**, and Tasks 10–21
follow it with the section-specific field map given in the table below.

### Task 9 (template): `auto_speedtest`

**Files:**
- Create: `unifi/setting_section_auto_speedtest.go`, `unifi/setting_section_auto_speedtest_test.go`
- Modify: `unifi/setting_resource.go` (remove the inline auto_speedtest converters/read/write once the section is wired; keep the schema attr moving into the section).

**Interfaces:**
- Produces: `type autoSpeedtestSection struct{}` implementing `settingSection`; registered via `func init() { registerSection(autoSpeedtestSection{}) }`.

- [ ] **Step 1: Failing test** — decode/overlay round-trip + present-empty + malformed, via the fake:

```go
func TestAutoSpeedtestSection_roundTrip(t *testing.T) {
	s := autoSpeedtestSection{}
	snap := newRawSettings([]settings.RawSetting{
		{BaseSetting: settings.BaseSetting{Key: "auto_speedtest"}, Data: map[string]any{"enabled": true, "cronExpr": "0 3 * * *"}},
	})
	var m settingResourceModel
	if d := s.decode(context.Background(), snap, &m); d.HasError() {
		t.Fatal(d)
	}
	// assert m.AutoSpeedtest object holds enabled=true, cron_expr="0 3 * * *"
	rs, configured, d := s.overlay(context.Background(), m, settingResourceModel{}, snap)
	if d.HasError() || !configured {
		t.Fatalf("overlay: configured=%v d=%v", configured, d)
	}
	if rs.Data["enabled"] != true {
		t.Fatalf("overlay lost enabled: %v", rs.Data)
	}
	// preservation: a pre-existing unmodeled key survives
	if _, ok := rs.Data["some_unmodeled_key"]; !ok {
		// seed snap with the key and assert it is preserved
	}
}
```

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** the section. `decode` reads from `snap.section("auto_speedtest").Data` via the codec into a nested object on the model; `overlay` starts from the section's snapshot `Data` (a copy — universal preservation), applies `set*` for each configured field per its ownership class, returns the merged RawSetting. `schemaAttribute()` returns the exact same `SingleNestedAttribute` the resource declares today (Optional+Computed+UseStateForUnknown). Field ownership tags:

  | field | class |
  |---|---|
  | enabled | ownerManaged |
  | cron_expr | ownerManaged |

- [ ] **Step 4: Run** section test + `go test ./unifi/ -run TestGolden_autoSpeedtest` (golden must still pass unless a permitted delta is cited). → PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w unifi/setting_section_auto_speedtest.go unifi/setting_section_auto_speedtest_test.go unifi/setting_resource.go
git add -A
git commit -m "refactor(setting): migrate auto_speedtest onto the section engine"
```

### Tasks 10–21: remaining sections

For each, repeat Task 9's five steps with these specifics (nested field →
ownership class). Complex sections (mgmt, radius, usg, igmp_snooping) carry the
noted nested structures; keep the existing schema shape verbatim.

| Task | Section (attr) | go-unifi type | Ownership notes |
|---|---|---|---|
| 10 | `country` | `settings.Country` | code=ownerManaged |
| 11 | `dpi` | `settings.Dpi` | enabled/fingerprinting=ownerManaged |
| 12 | `lcm` | `settings.Lcm` | scalars=ownerManaged |
| 13 | `network_optimization` | `settings.NetworkOptimization` | enabled=ownerManaged |
| 14 | `ntp` | `settings.Ntp` | mode=ownerManaged; server_N=ownerManaged |
| 15 | `syslog` | `settings.Rsyslogd` | `contents` list=ownerManaged (present-empty must round-trip) |
| 16 | `doh` | `settings.Doh` | state=ownerManaged; `server_names` list=ownerManaged |
| 17 | `ips` | `settings.Ips` | `enabled_categories`/`enabled_networks` lists=ownerManaged |
| 18 | `mgmt` | `settings.Mgmt` | `x_ssh_*` password/keys=ownerWriteOnlySecret; ssh_keys ListNested=ownerManaged; auto_upgrade etc.=ownerManaged |
| 19 | `radius` | `settings.Radius` | `x_secret`=ownerWriteOnlySecret; accounting/auth ports=ownerManaged; the object was Optional+Computed → keep, tag nested as ownerManaged (UseStateForUnknown lives on the object attr) |
| 20 | `usg` | `settings.Usg` | scalars=ownerManaged; nested dns_verification children=ownerManaged |
| 21 | `igmp_snooping` | `settings.IgmpSnooping` | `network_ids` list=ownerManaged |

Each task: golden stays green unless a permitted delta (empty-list round-trip in
syslog/doh/ips/igmp) is cited in the commit; secret fields (mgmt, radius) MUST
use `ownerWriteOnlySecret` and a test asserts a null config preserves the prior
state secret and never appears in the PUT body.

---

## Task 22: Discriminator framework (C4) — validators & plan modifiers only

**Files:**
- Create: `unifi/setting_discriminator.go`, `unifi/setting_discriminator_test.go`

**Interfaces:**
- Produces reusable, resource-agnostic helpers (no PR-A consumer):
  - `func requireChildrenFor(discriminator string, active map[string][]string) validator.Object` — errors if a configured child is not owned by the active discriminator value.
  - `func clearInactiveChildren(discriminator string, owned map[string][]string) planmodifier.Object` — nulls prior-state children when the discriminator changes, before validation.
- These are consumed by PR-B2/C/D/E; PR-A ships and unit-tests them standalone.

- [ ] **Step 1: Failing test** — a contradictory child errors; a discriminator change clears stale children. (Full table-driven test written here.)
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** the two helpers per spec C4.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(setting): reusable discriminator validator + plan modifier (C4 framework)`.

---

## Task 23: `resolveSite` shared helper

**Files:**
- Create: `unifi/site.go`, `unifi/site_test.go`

**Interfaces:**
- Produces: `func resolveSite(configured string, def string) string` (configured if non-empty else default) and `func parseSiteID(importID, defaultSite string) (site, id string, err error)` (splits `site:id`, else `defaultSite,importID`). Used by C3 import here and by PR-V/D/E.

- [ ] **Step 1–5:** TDD the two functions (equivalence of `id` vs `default:id`; invalid input → error). Commit `feat: shared site resolution + composite-id parsing (C3)`.

---

## Task 24: Wire the engine into the resource + site-aware import + delete legacy code

**Files:**
- Modify: `unifi/setting_resource.go` (Schema builds from registry; Create/Update/Read/Delete/ImportState call the engine; delete ALL remaining legacy per-section code).

**Interfaces:**
- Consumes: `settingSections`, `applySections`, `readSections`, `resolveSite`.

- [ ] **Step 1: Failing tests**
  - `TestSettingSchema_fromRegistry`: every registered section's `attrName()` appears in the resource schema.
  - `TestSettingImport_hydratesAllSections`: ImportState sets `site` and a subsequent `readSections(onlyConfigured=false)` over a fake with several sections populates all of them (via the engine, using the fake client injected through a test seam).
  - `TestSettingImport_nonDefaultSite`: importing `mysite` sets `site=mysite`, not the provider default.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement**
  - `Schema`: keep `id`/`site`/`timeouts`; then `for _, s := range settingSections { attrs[s.attrName()] = s.schemaAttribute() }`.
  - `Create`/`Update`: `site := resolveSite(plan.Site.ValueString(), r.client.Site)`; `out, d := applySections(ctx, realSettingsClient{r.client}, site, plan, state)`; set state from `out`.
  - `Read`: `readSections(ctx, realSettingsClient{r.client}, site, &state, onlyConfigured=true)`.
  - `ImportState`: parse import ID as a site name; set `site` and `id`; `resp.State.Set`. The subsequent `Read` uses `onlyConfigured=false` when state has no configured sections (imported) to hydrate all. (Introduce an internal flag: if all section attrs are null → hydrate all.)
  - `Delete`: unchanged no-op.
  - **Delete every legacy converter/read/write method** now unused.

- [ ] **Step 4: Run** full suite: `go build ./... && go vet ./unifi/ && go test ./unifi/...` → PASS; `go test ./unifi/ -run TestGolden` → PASS (baseline preserved except cited deltas).

- [ ] **Step 5: Commit** `refactor(setting): cut over to the section engine; site-aware import; remove legacy paths`.

**Verification item (codex spec-review):** in this task, confirm the plugin-framework retains the best-effort state written alongside an `Update` error diagnostic (write state THEN append the error). If it discards state on error, use `resp.State.Set` before adding the diagnostic. Add a test asserting state is retained when `applySections` returns an error+model.

---

## Task 25: Ownership-boundary annotations + changelog

**Files:**
- Create: `unifi/setting_ownership_boundary.md` (a short doc, or a comment block in `setting_codec.go`).
- Modify: `CHANGELOG.md` (Unreleased).

- [ ] **Step 1–4:** Annotate each raw mapping that exists only because the SDK lacks a typed field as `// TODO(go-unifi): <retirement condition>`; record permanent-vs-temporary in the boundary doc. Add ONE changelog entry describing the internal lifecycle unification and the five permitted deltas as user-visible behavior (empty round-trip, malformed→error, universal preservation, single snapshot, real import) in house style.

- [ ] **Step 5: Commit** `docs(setting): ownership boundary notes + changelog for lifecycle foundation`.

---

## Self-Review

*(run by the plan author before handing to codex; corrections applied inline)*

1. **Spec coverage (PR-A scope):** gate 1 → Tasks 6,9–24; gate 2 → Task 7 (one snapshot); gate 3 → Task 7 (reconcile-before-mutate); gate 4 → Tasks 7,9 (overlay preserves); gate 5 → Tasks 1,2 + per-section tags; gate 6 framework → Tasks 23,24 (import); gate 7 → Task 7 (partial-apply); gate 8 framework → Tasks 4 (capability) + secret classes in Tasks 1,2,18,19; gate 9 framework → Task 22; gate 10 → Tasks 6,8 + per-section tests. C2.8 seam → Task 5. C2.4 second-failure → Task 7. Permitted-delta proof → Task 8 + per-section. Ownership boundary → Task 25.
2. **Placeholder scan:** the section-migration tasks 10–21 use a field-map table rather than 13 full transcriptions — each row names the section, type, and per-field ownership, and every step mirrors the fully-worked Task 9, so an implementer has complete instructions without a "similar to" reference. Engine helpers `readSections`/`listSnapshot` are specified by contract + the shown `applySections` body; the implementer completes them under the Task 7 tests.
3. **Type consistency:** `settingSection` methods (`key/attrName/schemaAttribute/decode/overlay/capability`) are used identically in Tasks 6, 7, 9, 24. `ownershipClass` constants match across Tasks 1, 9–21. `settingsClient` (`ListSettings`/`UpdateRawSetting`) is consistent in Tasks 5, 7, 24. `rawSettings`/`newRawSettings`/`section`/`has` consistent in Tasks 3, 4, 7, 9.

**Open for codex plan-review:** whether Tasks 10–21 should each be their own SDD task (13 reviewer gates) or grouped (simple scalar sections batched); and whether the C2.4 best-effort second-failure merge needs its own dedicated test task rather than riding in Task 7.
