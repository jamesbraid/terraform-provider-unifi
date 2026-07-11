# PR-A: Settings Lifecycle Foundation — Implementation Plan (rev 5)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `unifi_setting` resource's two ad-hoc lifecycle paths with one snapshot-driven engine built on an explicit, ownership-aware codec, and migrate all 13 existing sections onto it with no user-facing behavior change beyond the named permitted deltas.

**Architecture:** A single `ListSettings` snapshot per operation is decoded/overlaid by per-section handlers implementing one `settingSection` interface that declares an `ownership()` map. A shared codec branches on each field's ownership class (C1) for null/empty/clearing/secret/preservation. Writes are raw read-modify-write merges over a deep copy of the section's snapshot object; the write path reconciles all sections before the first PUT and reconciles state from a fresh snapshot after. The engine takes its section list and settings client as parameters, so tests inject fakes without touching global state.

**Tech Stack:** Go, terraform-plugin-framework v1.19.0, `github.com/ubiquiti-community/go-unifi` (via `jamesbraid/go-unifi v1.34.1` replace). Flat `unifi/` package.

## Global Constraints

- Spec of record: `docs/superpowers/specs/2026-07-11-settings-remediation-design.md`. This plan implements **only PR-A** (gates 1–5, 7, 10 fully; the *framework* half of gates 6, 8, 9). It adds NO new user-facing section.
- Public `unifi_setting` schema attribute names, types, defaults, validators, sensitivity, and plan modifiers are unchanged for all 13 sections → no state-schema migration. A schema-equivalence test enforces this (Task 24a). Any deviation is a plan failure.
- One codec, one engine; no section performs I/O; no per-section null/empty handling.
- Reconciliation before mutation: no controller write occurs before every configured section has decoded and produced its candidate overlay without error.
- Universal preservation: every write is a raw merge over a **deep copy** of the section's snapshot object; unmodeled keys are never dropped.
- Malformed remote data raises a diagnostic and aborts; never silently normalized. Present-empty ≠ absent.
- The engine functions take `(sections []settingSection, client settingsClient, ...)` — no global mutable registry is read inside the engine, so tests never race on global state.
- Deterministic order: the engine iterates sections sorted by `key()`.
- Migration proof: request-level golden tests (Task 9) stay green through every migration task; the legacy converters are retained until the final cutover (Task 24c). Behavior not on the permitted-delta list must be byte-identical to `origin/main`.
- Verification per task: `gofmt -w`, `go build ./...`, `go vet ./unifi/`, `go test ./unifi/...`, `git diff --check`. TF_ACC tests are demo-controller-only.
- **Permitted deltas** (the ONLY behavior changes allowed in PR-A): (1) present-empty string/array round-trips instead of collapsing to null; (2) malformed remote scalar/list raises a diagnostic instead of truncating/dropping; (3) writes preserve all unmodeled controller keys; (4) one snapshot per op replaces per-section reads; (5) import populates `site` + hydrates all sections; (6) a write-only secret with **null** config now omits the masked/stale secret from the PUT — the legacy typed converters could re-send `x_secret`/`x_ssh_password` from state (a masked value re-sent risks clearing the real secret); a configured value or explicit empty is unchanged. Each golden that changes for delta (1) or (6) cites the delta in its commit.

---

## File Structure

New (flat `unifi/`): `setting_ownership.go`, `setting_codec.go` (low-level + ownership-aware layer), `setting_snapshot.go`, `setting_capability.go`, `setting_section.go` (interface + registry + ordering), `setting_engine.go` (readSections/applySections/bestEffortState), `setting_client.go` (settingsClient + real adapter), `setting_discriminator.go` (C4 framework), `site.go` (resolveSite/parseSiteID), and `setting_section_<name>.go` × 13.

Modified: `setting_resource.go` (Schema from registry; CRUD+Import via engine; legacy code deleted in 24c).

Test files mirror each source; plus `setting_fake_client_test.go`, `setting_golden_test.go`, `setting_engine_lifecycle_test.go`, `setting_framework_state_test.go`.

---

## Task 1: Ownership taxonomy

**Files:** Create `unifi/setting_ownership.go`, `unifi/setting_ownership_test.go`

**Interfaces — Produces:** `type ownershipClass int`; constants `ownerManaged, ownerCoManaged, ownerComputed, ownerWriteOnlySecret, ownerGeneratedSecret, ownerPreservedUnmanaged`; methods `writesToPUT() bool`, `readsFromAPI() bool`, `isSecret() bool`, `usesStateForUnknown() bool`.

- [ ] **Step 1: Failing test** — table over all six classes asserting each predicate (writesToPUT true for Managed/CoManaged/WriteOnlySecret; readsFromAPI false for WriteOnlySecret/PreservedUnmanaged; isSecret for the two secret classes; usesStateForUnknown for CoManaged/Computed/GeneratedSecret).

```go
func TestOwnershipClassPolicy(t *testing.T) {
	cases := []struct{ c ownershipClass; writes, reads, secret, sfu bool }{
		{ownerManaged, true, true, false, false},
		{ownerCoManaged, true, true, false, true},
		{ownerComputed, false, true, false, true},
		{ownerWriteOnlySecret, true, false, true, false},
		{ownerGeneratedSecret, false, true, true, true},
		{ownerPreservedUnmanaged, false, false, false, false},
	}
	for _, tc := range cases {
		if tc.c.writesToPUT() != tc.writes || tc.c.readsFromAPI() != tc.reads ||
			tc.c.isSecret() != tc.secret || tc.c.usesStateForUnknown() != tc.sfu {
			t.Errorf("class %d policy mismatch", tc.c)
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./unifi/ -run TestOwnershipClassPolicy` → FAIL.
- [ ] **Step 3: Implement** the enum + four predicates. `writesToPUT`: Managed/CoManaged/WriteOnlySecret → true. `readsFromAPI`: all except WriteOnlySecret/PreservedUnmanaged. `isSecret`: the two secret classes. `usesStateForUnknown`: CoManaged/Computed/GeneratedSecret.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(setting): field-ownership taxonomy (C1)`.

---

## Task 2: Codec — low-level typed accessors + ownership-aware layer

**Files:** Create `unifi/setting_codec.go`, `unifi/setting_codec_test.go`

**Interfaces — Produces:**
- Low-level (read a `map[string]any` value; present-empty is a value, absent/null is TF-null, wrong/fractional type is a diagnostic): `codecString/codecBool/codecStringList(ctx?) → (types.X, diag.Diagnostics)`, `codecInt64` (fractional → diagnostic).
- Low-level setters (write present incl. empty, skip null/unknown): `putString/putInt64/putBool(out, key, v)`, `putStringList(ctx, out, key, v) diags`.
- **Ownership-aware layer** (the C1 encoding):
  - `decodeString(data, key, class, prior types.String) (types.String, diag.Diagnostics)` — if `!class.readsFromAPI()` return `prior` (preserve write-only secret); else `codecString`. Analogous `decodeInt64/decodeBool/decodeStringList`.
  - `overlayString(out, key, class, v types.String)` — branch on class:
    - **write-only secret** (`class == ownerWriteOnlySecret`, i.e. `!readsFromAPI()`): if `v` is null/unknown, **`delete(out, key)`** — the copied snapshot holds a masked/stale secret that must NOT be re-sent; omitting it makes the controller keep its stored value. If `v` is a value (incl. empty) it is written (a configured empty is an intentional clear/rotate-to-empty).
    - **Managed/CoManaged:** write `v` if present (incl. empty); if null/unknown, leave the copied snapshot value in place (preserve the controller value).
    - **Computed/GeneratedSecret/Preserved (`!writesToPUT()`):** no write and no delete — the snapshot's own value is left as-is (these are controller-owned and read-back, so the base value is the truth).
    - Analogous `overlayInt64/overlayBool/overlayStringList/overlayObject/overlayObjectList` apply the same branching. **The delete-on-null write-only-secret rule is the ONE place overlay removes a key from the copied base** — Task 20/22 MUST test it with a masked/empty secret value present in the snapshot and null config, asserting the key is absent from the PUT body.
  - **Nested shapes** (for `SingleNestedAttribute`/`ListNestedAttribute` sections — usg, mgmt, ips lists, doh):
    - `decodeObject(ctx, data map[string]any, key string, childOwnership map[string]ownershipClass, prior types.Object, attrTypes map[string]attr.Type) (types.Object, diag.Diagnostics)` — reads the nested map, recursing each child per its class (a nested `WriteOnlySecret` is preserved from `prior`'s object); `overlayObject(ctx, out, key, childOwnership, cfg types.Object) diags` writes children whose class `writesToPUT()`.
    - `decodeObjectList(ctx, data, key string, elemOwnership map[string]ownershipClass, prior types.List, elemType attr.Type) (types.List, diag.Diagnostics)` / `overlayObjectList(...)` — element order follows the API; a per-element write-only-secret leaf is preserved from the matching prior element (hence the `prior` param), and its overlay applies the same delete-on-null rule as above.
    - **Duration/number:** a field the schema exposes as int seconds uses `decodeInt64`/`overlayInt64`; a field the existing converter parses from/to a duration string is ported verbatim from the cited converter (see the migration table's converter column).
    - **Echoed secrets:** PR-A has no echoing secret (mgmt/radius secrets are write-only/masked → always preserve prior). The spec's `echoed` opt-in is a per-field annotation added by the first section that actually echoes (PR-B); adding it to PR-A now would be unused surface (YAGNI). `decodeString` for a write-only secret therefore always returns `prior`.

- [ ] **Step 1: Failing tests** — the contract AND the ownership layer:
  - `codecString({"k":""})` → StringValue("") not null; `codecString({})` → null.
  - `codecInt64({"k":1.9})` → diagnostic.
  - `putString` writes "" but skips null.
  - `decodeString(data,"k",ownerWriteOnlySecret, prior=Value("keep"))` returns "keep" and never touches `data`.
  - `overlayString(out,"k",ownerComputed, Value("x"))` writes nothing; `overlayString(out,"k",ownerManaged, Value(""))` writes "".

- [ ] **Step 2: Run** `go test ./unifi/ -run TestCodec` → FAIL.
- [ ] **Step 3: Implement** low-level codec exactly as in rev 1 (string/int64/bool/list with malformed→diag, empty≠absent), plus the ownership-aware wrappers above. Full code for the ownership layer:

```go
func decodeString(data map[string]any, key string, class ownershipClass, prior types.String) (types.String, diag.Diagnostics) {
	if !class.readsFromAPI() {
		return prior, nil // preserve write-only secret from prior state (C1, R2)
	}
	return codecString(data, key)
}

func overlayString(out map[string]any, key string, class ownershipClass, v types.String) {
	if class.writesToPUT() {
		putString(out, key, v)
	}
}
```

(Int64/Bool/StringList follow the same two-line pattern.)

- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(setting): ownership-aware value codec (C1: empty!=absent, malformed diag, secret preservation)`.

---

## Task 3: Snapshot type with deep copy

**Files:** Create `unifi/setting_snapshot.go`, `unifi/setting_snapshot_test.go`

**Interfaces — Produces:** `type rawSettings struct{ byKey map[string]settings.RawSetting }`; `newRawSettings([]settings.RawSetting) rawSettings`; `func (s rawSettings) section(key) (settings.RawSetting, bool)`; `func (s rawSettings) has(key) bool`; `func (s rawSettings) dataCopy(key string) map[string]any` — a **deep** copy (recursive for nested maps/slices) of the section's `Data`, used as the overlay base so a nested mutation cannot corrupt the snapshot.

- [ ] **Step 1: Failing test** — lookup works; and `dataCopy` deep-copies (mutating a nested map in the copy does not change the snapshot).

```go
func TestRawSettings_dataCopyIsDeep(t *testing.T) {
	rs := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "x"},
		Data:        map[string]any{"nested": map[string]any{"a": float64(1)}},
	}})
	cp := rs.dataCopy("x")
	cp["nested"].(map[string]any)["a"] = float64(2)
	orig, _ := rs.section("x")
	if orig.Data["nested"].(map[string]any)["a"] != float64(1) {
		t.Fatalf("dataCopy must be deep; snapshot was mutated")
	}
}
```

- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** with a recursive `deepCopyAny(any) any` (handles `map[string]any` and `[]any`).
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(setting): authoritative snapshot with deep-copy overlay base (C2.1)`.

---

## Task 4: Capability taxonomy

*(unchanged from rev 1)* Create `unifi/setting_capability.go` + test: `capabilityState` enum (`capSupported/capUnsupported/capUnmaterialized/capUnauthorized/capUnknown`), `sectionCapability(snap, key)` (present→Supported else Unsupported; per-section refinement later), `(c capabilityState) configuredError(section) diag.Diagnostics` failing closed for Unsupported/Unauthorized/Unknown.
- [ ] Steps 1–5 as rev 1. Commit `feat(setting): capability taxonomy failing closed (C6)`.

---

## Task 5: `settingsClient` seam + real adapter + fake + PUT-body transport test

**Files:** Create `unifi/setting_client.go`, `unifi/setting_fake_client_test.go`, and add a transport test in `unifi/setting_client_test.go`.

**Interfaces — Produces:** `type settingsClient interface { ListSettings(ctx, site) ([]settings.RawSetting, error); UpdateRawSetting(ctx, site string, s settings.RawSetting) error }`; `realSettingsClient{c *Client}` (`UpdateRawSetting` calls `r.c.UpdateSetting(ctx, site, &s)` — `*RawSetting` satisfies `settings.Setting` via `BaseSetting`, and `RawSetting.MarshalJSON` PUTs the merged `Data`); `fakeSettingsClient` with `sections`, `failList`, `failUpdateOn`, and recorded `puts`.

- [ ] **Step 1: Failing tests**
  - Fake: update-then-list round-trip; injected update failure returns error.
  - **Marshal (unit):** a test that marshals a `RawSetting{Data:{"key":"mgmt","x_ssh_enabled":true,"unmodeled":"keep"}}` via `json.Marshal(&rs)` and asserts the body contains `unmodeled":"keep"` and `x_ssh_enabled":true`.
  - **Adapter (httptest):** stand up an `httptest.Server`, construct a real go-unifi `ApiClient`/`*Client` pointed at its URL, call `realSettingsClient{c}.UpdateRawSetting(ctx,"default", rs)`, and assert the captured request is `PUT`, path ends `/set/setting/mgmt`, and the decoded body contains the merged keys (including the unmodeled one). This proves the full `realSettingsClient → UpdateSetting → HTTP` path, not just marshalling.

```go
func TestRawSettingMarshalPreservesUnmodeled(t *testing.T) {
	rs := settings.RawSetting{BaseSetting: settings.BaseSetting{Key: "mgmt"},
		Data: map[string]any{"key": "mgmt", "x_ssh_enabled": true, "unmodeled": "keep"}}
	b, err := json.Marshal(&rs)
	if err != nil { t.Fatal(err) }
	s := string(b)
	if !strings.Contains(s, `"unmodeled":"keep"`) || !strings.Contains(s, `"x_ssh_enabled":true`) {
		t.Fatalf("merged map not marshalled to wire: %s", s)
	}
}
```

- [ ] **Step 2: Run** `go test ./unifi/ -run 'TestFakeClient|TestRawSettingMarshal|TestSettingClientAdapter'` → FAIL.
- [ ] **Step 3: Implement** the interface + real adapter (`unifi/setting_client.go`) and the fake (`_test.go`).
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(setting): settingsClient seam, fake with fault injection, PUT-body transport test (C2.8)`.

---

## Task 6: `settingSection` interface (with `ownership()`) + registry + deterministic order

**Files:** Create `unifi/setting_section.go`, `unifi/setting_section_test.go`

**Interfaces — Produces:**
```go
type settingSection interface {
	key() string
	attrName() string
	schemaAttribute() schema.Attribute
	ownership() map[string]ownershipClass                 // attr path -> class; every schema leaf present
	decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics
	overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics)
	capability(snap rawSettings) capabilityState
	// carryBestEffort is ADDED IN TASK 7 (not built in Task 6). Task 6 ships the 7
	// methods above; Task 7 is the consumer (bestEffortState) that reveals the need
	// and extends the interface. Listed here so Tasks 10-22 implement all 8 methods.
	carryBestEffort(dst *settingResourceModel, plan, prior settingResourceModel) diag.Diagnostics
}
var settingSections []settingSection
func registerSection(s settingSection)                    // append
func orderedSections(in []settingSection) []settingSection // returns a copy sorted by key()
```

- [ ] **Step 1: Failing tests**
  - `TestRegistryKeysUnique`: unique `key()` and `attrName()` across `settingSections`; none empty.
  - `TestOrderedSectionsDeterministic`: `orderedSections` returns sections sorted by `key()` regardless of input order (gate: deterministic PUT order).
  - `TestSectionOwnershipCoversSchema` (structural, gate 10): for each section, every leaf attribute path in `schemaAttribute()` appears in `ownership()`, and vice-versa. (Walk the schema.Attribute tree; compare key sets.)

- [ ] **Step 2: Run** → FAIL (undefined) — for the ownership/registry tests, they become meaningful once sections register (Tasks 9–21); at this task they compile and pass trivially over the empty registry, and are re-exercised as sections land. Keep them.
- [ ] **Step 3: Implement** the interface, `registerSection`, and `orderedSections` (copy + `sort.Slice` by `key()`).
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(setting): settingSection interface with ownership() + deterministic ordered registry (C2, gate 10)`.

---

## Task 7: The engine — read, write, and best-effort recovery

**Files:** Create `unifi/setting_engine.go`, `unifi/setting_engine_test.go`

**Interfaces — Produces (all take an explicit `sections []settingSection` — no global read):**
- `func readSections(ctx, sections []settingSection, client settingsClient, site string, prior settingResourceModel, model *settingResourceModel, onlyConfigured bool) diag.Diagnostics` — one `ListSettings`; per configured (or all, import) section: capability check (fail closed if configured+unsupported), then `decode(snap, prior, model)`.
- `func applySections(ctx, sections []settingSection, client settingsClient, site string, plan, prior settingResourceModel) (settingResourceModel, diag.Diagnostics)` — snapshot; reconcile ALL configured overlays (abort before any PUT on error); PUT in `orderedSections` order, recording successes; re-read canonical snapshot into state; on re-read failure use `bestEffortState`.
- `func bestEffortState(prior, plan settingResourceModel, put map[string]bool, sections []settingSection) (settingResourceModel, diag.Diagnostics)` — start from `prior`; for each section where `put[key]` is true, call `s.carryBestEffort(&out, plan, prior)`. Non-PUT sections keep `prior` entirely (never touched). Returns the assembled model + any diagnostics from the object rebuilds.

  **Why a per-section method and not decode/overlay reuse** (codex-validated, conv `besteffort-mechanism`): this needs a per-LEAF choice between `plan` and `prior` (rotated secret → plan, unset secret → prior), which `decode` cannot express — `decode` uses ONE uniform `prior` source, so `prior=plan` gets rotation right but null wrong, and `prior=prior` gets null right but rotation wrong. `overlay` yields a PUT body (and deletes null secrets), not a TF object. Generic code also cannot assign `out.<SectionField> = plan.<SectionField>` without reflection over `tfsdk` tags (rejected as fragile). Hence each section owns the copy of its own `types.Object` field.

- `carryBestEffort(dst *settingResourceModel, plan, prior settingResourceModel) diag.Diagnostics` — each section implementation (Tasks 10-22) sets ITS OWN field in `dst`:
  - **Non-secret sections:** one line — `dst.<Field> = plan.<Field>`.
  - **Secret sections** (mgmt `ssh_password`, radius `secret`): `dst.<Field>, d = bestEffortObject(plan.<Field>, prior.<Field>, s.ownership())`.
- `func bestEffortObject(planObj, priorObj types.Object, own map[string]ownershipClass) (types.Object, diag.Diagnostics)` — shared engine helper. Rebuild an object from `planObj`'s attribute types and values, replacing ONLY each `ownerWriteOnlySecret` leaf that is **null OR unknown** in `planObj` with `priorObj`'s value; every other leaf comes from `planObj` verbatim. **Codex-validated traps that MUST be encoded + tested:** (1) treat `IsUnknown()` exactly like `IsNull()` — `overlay` deletes BOTH, so best-effort retains `prior` for both; (2) a configured **empty string** secret (`types.StringValue("")`) WAS sent (rotate-to-empty) → keep `plan`'s empty value, do NOT fall back to `prior`; (3) if `planObj` itself is null/unknown, return `priorObj` unchanged — never manufacture a known object from a null section; (4) thread diagnostics out (structurally can't fail for same-schema objects, but make the invariant explicit + tested).

- [ ] **Step 1: Failing tests** (via the fake + two stub sections passed as the `sections` param):
  1. `TestEngine_noWriteBeforeReconcileError`: a stub `overlay` returns a diagnostic ⇒ `fake.puts` is empty.
  2. `TestEngine_oneSnapshotOnRead`: `readSections` triggers exactly one `ListSettings` (count on the fake).
  3. `TestEngine_partialApplyReReads`: section `a` PUTs ok, `b` injected-fail ⇒ error returned AND `readSections` after shows `a`'s new value, `b`'s old value.
  4. `TestEngine_preservesUnmodeledKeys`: overlay sets one key; the recorded PUT `Data` still contains a pre-existing unmodeled key from the snapshot.
  5. `TestBestEffortState_excludesUnattempted`: `put={a:true}`, plan has new `a` and `b` ⇒ result has plan's `a`, prior's `b`.
  6. `TestBestEffortState_secretRotationRetained`: uses stub sections (each implementing `carryBestEffort`) — for a PUT section, a *set* (rotated) secret in `plan` is retained; a *null* secret in `plan` falls back to `prior`'s secret; a non-PUT section keeps `prior`.
  7. `TestBestEffortObject_secretLeafMatrix` (direct unit test of the helper with real `types.Object` values — the codex-validated matrix): null secret → prior; non-empty secret → plan; **empty-string secret → plan** (rotate-to-empty, NOT prior); **unknown secret → prior** (treated like null); non-secret sibling leaf → plan; and a null/unknown parent object → returns `prior` object unchanged.

```go
func TestBestEffortState_excludesUnattempted(t *testing.T) {
	prior := modelWith("a", "old"); prior = setSection(prior, "b", "old")
	plan := modelWith("a", "new"); plan = setSection(plan, "b", "new")
	got := bestEffortState(prior, plan, map[string]bool{"a": true}, testSections)
	if sectionVal(got, "a") != "new" || sectionVal(got, "b") != "old" {
		t.Fatalf("best-effort must use plan for PUT sections and prior for the rest")
	}
}
```

- [ ] **Step 2: Run** `go test ./unifi/ -run 'TestEngine|TestBestEffort'` → FAIL.
- [ ] **Step 3: Implement.** `applySections` (corrected second-failure path):

```go
func applySections(ctx context.Context, sections []settingSection, client settingsClient, site string, plan, prior settingResourceModel) (settingResourceModel, diag.Diagnostics) {
	var d diag.Diagnostics
	ordered := orderedSections(sections)
	snap, err := listSnapshot(ctx, client, site)
	if err != nil {
		d.AddError("read settings failed", err.Error())
		return prior, d
	}
	type pending struct{ s settingSection; rs settings.RawSetting }
	var todo []pending
	for _, s := range ordered {
		rs, configured, sd := s.overlay(ctx, plan, prior, snap)
		d.Append(sd...)
		if configured {
			todo = append(todo, pending{s, rs})
		}
	}
	if d.HasError() {
		return prior, d // reconcile-before-mutate: nothing written
	}
	put := map[string]bool{}
	var putErr error
	for _, p := range todo {
		if err := client.UpdateRawSetting(ctx, site, p.rs); err != nil {
			putErr = fmt.Errorf("section %q: %w", p.s.key(), err)
			break
		}
		put[p.s.key()] = true
	}
	out := plan
	if rd := readSections(ctx, sections, client, site, plan, &out, false); rd.HasError() {
		var bd diag.Diagnostics
		out, bd = bestEffortState(prior, plan, put, sections) // C2.4 second failure
		d.Append(bd...)
		d.AddWarning("settings read-back failed after apply",
			"state written best-effort from applied values; run `terraform refresh`")
	}
	if putErr != nil {
		d.AddError("settings apply failed", putErr.Error())
	}
	return out, d
}
```

Implement `readSections`, `listSnapshot`, `bestEffortState`, and the shared `bestEffortObject` helper in full per the contracts above. Also EXTEND the `settingSection` interface (from Task 6) with the `carryBestEffort` method and add it to the engine's stub sections in the test file. Real sections implement `carryBestEffort` in their own migration task (10-22).

**Watch-item for the reviewer (named risk, do not pre-resolve):** `applySections`'s post-apply re-read passes `onlyConfigured=false`, re-decoding ALL registered sections into state. If the `unifi_setting` schema does not mark every section Computed, this risks a Terraform "provider produced inconsistent result after apply" for sections the user did not configure. Implement as written (`false`) — this was codex-approved at the plan level — but the reviewer must assess whether it is safe given the schema, and it is carried to Task 24a (wiring) + live validation.

- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(setting): engine — reconcile-before-mutate, partial-apply, best-effort recovery (C2.2/2.4/2.5)`.

---

## Task 8: Framework partial-state-on-error verification

**Files:** Create `unifi/setting_framework_state_test.go`

**Settled behavior (verified in source):** the plugin-framework sets `resp.NewState = &updateResp.State` **unconditionally** at `fwserver/server_updateresource.go:154` — regardless of error diagnostics (the null-state guard at :157 only fires when there is *no* error). Terraform Core persists that returned state alongside the error (partial-apply). So the C2.4 contract holds as approved: on a partial/read-back failure the operation stays **failed (error)** AND the best-effort state is persisted. No warning fallback; no spec change.

This task is a **regression guard** that pins that behavior so a future framework bump cannot silently break C2.4.

- [ ] **Step 1: Write the failing test** — an **end-to-end** `resource.Test`: a throwaway resource whose `Update` sets a known state value AND appends an error diagnostic; the mutating step uses `ExpectError`, and a follow-up step (or `RefreshState` + a plan assertion) asserts the **persisted** state contains the value set during the errored apply — proving Terraform Core (not merely the framework's `NewState` return) persists it. (The `server_updateresource.go:154` reading is the mechanism; this test is the Core-level proof.)
- [ ] **Step 2: Run** → PASS (documents current framework behavior).
- [ ] **Step 3:** N/A (assertion of existing behavior).
- [ ] **Step 4:** If this test ever fails on a framework upgrade, **STOP and escalate to the maintainer** — converting the failed apply to a warning would flip "failed" to "success" and is a spec change, not an implementation choice.
- [ ] **Step 5: Commit** `test(setting): regression-guard framework state persistence on Update error (C2.4)`.

---

## Task 9: Golden characterization of the 13 legacy sections (retain legacy until 24c)

**Files:** Create `unifi/setting_golden_test.go`

- [ ] **Step 1:** For EACH of the 13 sections, one golden asserting the exact JSON PUT body the **current** legacy converter produces for a representative model (captured from `origin/main`). One `TestGolden_<section>` function per section (no duplicates). These call the legacy converters, which remain until Task 24c.
- [ ] **Step 2: Run** `go test ./unifi/ -run TestGolden` → PASS (baseline).
- [ ] **Step 5: Commit** `test(setting): golden characterization of 13 legacy wire outputs (pre-migration)`.

**Coverage inventory (checked-in):** add `unifi/setting_migration_inventory_test.go` — a table `map[section][]op` asserting each of the 13 sections has both a golden and (after its migration task) a registered section whose overlay reproduces the golden body. This is a test, not a review-guide note.

---

## Tasks 10–22: Migrate the 13 legacy sections (one task each)

Each task ADDS a `settingSection` implementation and registers it, WITHOUT deleting the legacy converter (deletion is Task 24c). Each task asserts the new section's `overlay` reproduces the section's golden body (same expected bytes) and its `decode` round-trips. The **authoritative field↔raw-key mapping is the existing converter**, cited by line; port it verbatim and apply the ownership tags below.

Each section MUST also implement `carryBestEffort(dst *settingResourceModel, plan, prior settingResourceModel) diag.Diagnostics` (the 8th interface method added in Task 7): a non-secret section is a one-line `dst.<Field> = plan.<Field>; return nil`; a **secret** section (mgmt `ssh_password`, radius `secret`) is `dst.<Field>, d := bestEffortObject(plan.<Field>, prior.<Field>, s.ownership()); return d`. The two secret sections (Tasks 21/22 for usg/mgmt-shape and the radius task) additionally add a `carryBestEffort` test covering the codex secret matrix (null→prior, non-empty→plan, empty→plan, unknown→prior, sibling→plan).

**Structural templates** (fully worked with real code; reused by same-shape tasks): Task 10 (`auto_speedtest`) = **scalar**; Task 16 (`syslog`) = **list**; Task 21 (`usg`) = **nested-object** (`dns_verification`); Task 22 (`mgmt`) = **nested-list** (`ssh_keys`) **+ secret** (`ssh_password`). Each other task states its shape and follows the matching worked template.

The **converter** column cites the current `setting_resource.go` read (decode) and overlay (write) call sites, which name the `<attr>ModelToSetting` / `<attr>SettingToModel` converter methods and the go-unifi type each section uses. Locate those converter methods by name — they hold the field↔raw-key mapping — and port each field onto `decode`/`overlay`. The golden (Task 9) is the **hard exactness oracle**: if `overlay`'s body ≠ the golden byte-for-byte, a field was missed. This makes the mapping fully verifiable without transcribing all 13 field lists into the plan.

### Task 10 (scalar template): `auto_speedtest`
- Files: `unifi/setting_section_auto_speedtest.go` (+ test). Legacy converter authority: `setting_resource.go` (auto_speedtest converters). Ownership: `enabled`→ownerManaged, `cron_expr`→ownerManaged.
- Steps 1–5 as the fully-worked example: failing decode/overlay round-trip + present-empty + preservation test → implement `decode` (uses `decodeBool`/`decodeString` with the class from `ownership()`, base = `snap.section` Data) and `overlay` (base = `snap.dataCopy("auto_speedtest")`, apply `overlayBool`/`overlayString`) + `schemaAttribute()` identical to today + `ownership()` map → assert golden reproduced → commit `refactor(setting): migrate auto_speedtest onto the engine`.

### Tasks 11–22: per-section specifics

For each: cite the converter, port field-for-field, apply ownership, assert golden. Full ownership maps (attr path → class); anything not listed is `ownerManaged`:

| Task | Section | go-unifi type | Converter (decode / overlay lines) | Ownership (non-Managed leaves) | Template |
|---|---|---|---|---|---|
| 11 | `country` | `settings.Country` | 1947 / 1386–1397, 1677–1688 | — | scalar |
| 12 | `dpi` | `settings.Dpi` | 1964 / 1399–1410, 1690–1701 | — | scalar |
| 13 | `lcm` | `settings.Lcm` | 1981 / 1412–1423, 1703–1714 | — | scalar |
| 14 | `network_optimization` | `settings.NetworkOptimization` | 1998 / 1425–1436, 1716–1727 | — | scalar |
| 15 | `ntp` | `settings.Ntp` | 2017 / 1438–1449, 1729–1740 | — | scalar |
| 16 | `syslog` (list, worked) | `settings.Rsyslogd` | 2034 / 1451–1465, 1742–1756 | `contents` list ownerManaged (empty-list round-trip = permitted delta 1) | list |
| 17 | `doh` | `settings.Doh` | 2059 / 1467–1482, 1758–1773 | `server_names` list ownerManaged | list |
| 18 | `ips` | `settings.Ips` | 2084 / 1484–1499, 1775–1790 | `enabled_categories`, `enabled_networks` lists ownerManaged | list |
| 19 | `igmp_snooping` | `settings.IgmpSnooping` | 2283 / 1565–1592, 1856–1881 | `network_ids` list ownerManaged | list |
| 20 | `radius` | `settings.Radius` | 2136 / 1526–1549, 1817–1840 | `x_secret`→ownerWriteOnlySecret; ports/enabled ownerManaged | scalar+secret |
| 21 | `usg` (nested-object, worked) | `settings.Usg` | 2174 / 1551–1563, 1842–1854 | nested `dns_verification` children ownerManaged; scalars ownerManaged | nested-object |
| 22 | `mgmt` (nested-list + secret, worked) | `settings.Mgmt` | 2110 / 1501–1524, 1792–1815 | `ssh_password`→ownerWriteOnlySecret; `x_mgmt_key`→ownerWriteOnlySecret (only if in schema); `ssh_keys` (public keys, ListNested)→ownerManaged; all other flags ownerManaged | nested-list + secret |

**Worked tasks (16, 21, 22) show real `decode`/`overlay` code** using the nested/list codec helpers from Task 2 — Task 16 the list shape (`contents`), Task 21 the nested-object shape (`dns_verification` via `decodeObject`/`overlayObject`), Task 22 the nested-list shape (`ssh_keys` via `decodeObjectList`/`overlayObjectList`) plus the secret leaf (`ssh_password` preserved when config-null). Same-shape non-worked tasks reproduce the matching worked template with their table row's specifics.

**Secret-section goldens (Tasks 20 `radius`, 22 `mgmt`) — two scenarios:**
1. **secret set:** the golden PUT body is **byte-identical** to the legacy converter's (both send the configured secret).
2. **secret null + masked/stale value present in the snapshot fixture:** the new overlay **omits** the key (permitted delta 6); this golden reflects the NEW behavior and its commit cites delta 6. The legacy converter would have re-sent the masked value here — that is exactly the leak this fixes. The fixture MUST include the masked secret key so the delete path is exercised (a fixture that omits the remote key would not catch a regression).

**Secret tasks (20, 22) additionally test:** a null secret config preserves the prior-state secret (`decode` returns prior) and the secret never appears in the golden PUT body when config is null; a set secret is written. Only `ssh_password`/`x_secret`/`x_mgmt_key` are secret — **`ssh_keys` are public and are `ownerManaged`**.

Each task's commit: `refactor(setting): migrate <section> onto the engine`, citing a permitted delta if a golden changed.

---

## Task 23: Engine lifecycle test suite (§8)

**Files:** Create `unifi/setting_engine_lifecycle_test.go`

- [ ] Add stateful tests over the fake using the real registered sections: multiple sections in one op; only-configured written; failure before first write; failure after partial writes + retry convergence; universal preservation across a full apply; empty vs absent; explicit-clear vs stop-managing (null config leaves controller value; empty config clears); malformed remote → diagnostic aborts; write-only secret after refresh and after failed mutation (never cleared); import hydrates all sections and a no-config re-plan is clean. Commit `test(setting): engine lifecycle coverage (§8)`.

---

## Task 24: Discriminator framework + site helper + cutover (split)

### Task 24-fw: Discriminator framework (C4)
Create `unifi/setting_discriminator.go` (+ test): `requireChildrenFor(...) validator.Object` and `clearInactiveChildren(...) planmodifier.Object`, unit-tested standalone. Commit `feat(setting): reusable discriminator validator + plan modifier (C4 framework)`.

### Task 24-site: `resolveSite` + `parseSiteID`
Create `unifi/site.go` (+ test): `resolveSite(configured, def) string`; `parseSiteID(importID, def) (site, id string, err error)` (splits `site:id`, else `def,importID`; empty/ambiguous → error). Commit `feat: shared site resolution + composite-id parsing (C3)`.

### Task 24a: Wire the engine (schema + lifecycle), legacy retained
- **First, capture the legacy schema fixture (before rewiring):** add `normalizeSchemaAttr(ctx, name string, a schema.Attribute) normAttr` capturing name, type string, Required/Optional/Computed/Sensitive, the concrete default value rendered (if any), and each validator's and plan-modifier's `.Description(ctx)` (validators/plan-modifiers are not reflect-comparable — their descriptions are). Run the CURRENT (legacy) `Schema()` and snapshot the 13 sections' normalized form to a checked-in golden `unifi/testdata/setting_schema_legacy.json`.
- Modify `setting_resource.go`: `Schema` builds section attrs from `orderedSections(settingSections)`; Create/Update/Read call `applySections`/`readSections` with `settingSections` + `realSettingsClient{r.client}` + `resolveSite`. **Legacy converters remain** (unused by the new path but still compiled for goldens).
- Test `TestSettingSchema_equivalence`: normalize the NEW registry-built schema and assert it equals the `setting_schema_legacy.json` golden — so the comparison stays executable after the legacy `Schema` code is deleted in 24c.
- **Behavioral schema tests (stronger than description comparison):** for each section that carries them, add a focused test — a value a validator must reject (e.g. an invalid enum/cron), a default that must apply when config omits the field, and a plan-modifier no-churn case (`UseStateForUnknown` leaves a prior value unchanged on an unrelated edit). Description equality alone does not prove validator/modifier behavior.
- Commit `refactor(setting): drive lifecycle through the section engine; schema-equivalence golden (legacy retained)`.

### Task 24b: Site-aware import + hydration
- `ImportState`: parse the import ID as a **site name** (settings import is NOT the `site:id` composite used by NAT/CF); reject empty or `:`-containing input with a diagnostic; set `site` + `id` = the site name; leave all section attributes null. **Hydration marker:** there is NO separate flag — `Read` hydrates every registered section (`onlyConfigured=false`) exactly when all registered section attributes are null in state (the imported shape); otherwise it reads only configured sections. This invariant is documented in a comment on `Read`.
- Tests: `TestSettingImport_setsSiteAndHydrates` (via fake through the injected client seam); `TestSettingImport_nonDefaultSite`; and an acceptance-level `TestAccSettingImport_cleanPlan` asserting a no-config plan after import is empty.
- Commit `feat(setting): site-aware import that hydrates all sections (C3, gate 6)`.

### Task 24c: Delete legacy + repoint goldens
- Delete all legacy per-section converters/read/write from `setting_resource.go`. Repoint the Task 9 goldens to drive each section's `overlay` against the same fixtures with the **same immutable expected bodies** (now the only producer). Run the full suite + goldens.
- Commit `refactor(setting): remove legacy settings lifecycle; goldens drive section overlays`.

**Framework-state conformance (from Task 8):** in 24a/24b set `resp.State` before appending any `applySections` error per the verified behavior; the acceptance import test and a partial-apply test confirm state persistence.

---

## Task 25: Ownership-boundary notes + changelog

Annotate SDK-gap raw mappings `// TODO(go-unifi): <retirement condition>`; record permanent-vs-temporary. Add one `CHANGELOG.md` (Unreleased) entry in house style covering the internal unification and the five permitted deltas. Commit `docs(setting): ownership boundary + changelog for lifecycle foundation`.

---

## Self-Review

1. **Spec coverage:** gate 1 → Tasks 6,10–22,24; gate 2 → Task 7; gate 3 → Task 7; gate 4 → Tasks 3(deep copy),7,10; gate 5 → Tasks 1,2,6(`ownership()`),10–22; gate 6-fw → 24-site,24b; gate 7 → Task 7 + best-effort + Task 8 verification; gate 8-fw → Tasks 4 + secret classes (2,20,22); gate 9-fw → Task 24-fw; gate 10 → Tasks 6(structural),9,23. C2.4 second-failure → Task 7 `bestEffortState` + Task 8. C2.8 seam → Task 5. Permitted-delta proof → Task 9 goldens retained through 24c.
2. **Placeholder scan:** migration Tasks 11–22 cite the authoritative converter + full ownership maps + a golden equality check per section, with three structural templates (scalar/list/nested) worked in Tasks 10/16/22 — no "similar to" gaps. The engine's `readSections`/`listSnapshot`/`bestEffortState` are specified by signature + contract + the shown `applySections` body and are pinned by Task 7's six tests.
3. **Type consistency:** interface methods `key/attrName/schemaAttribute/ownership/decode(prior)/overlay/capability` consistent across Tasks 6,7,10–24. `ownershipClass` predicates (`writesToPUT/readsFromAPI/isSecret/usesStateForUnknown`) consistent across 1,2,10–22. `settingsClient` (`ListSettings/UpdateRawSetting`), engine signatures with explicit `sections []settingSection`, and `rawSettings`/`dataCopy` consistent throughout.

**Resolved from codex prA-plan-review turn 1:** C1 encoded in codec + `ownership()`; `bestEffortState` corrected + tested; goldens retained until 24c; ownership maps + templates + ssh fix; framework-state verification; deterministic ordered registry + sections-as-param; PUT-body transport test; Task 24 split + schema-equivalence; deep-copy snapshot.

**Resolved from turn 3:** NEW BLOCKING (null write-only secret leaking from the copied snapshot into the PUT) fixed — `overlayString` for a write-only secret now `delete`s the key on null config (the one place overlay removes a key), configured-empty still clears; Task 20/22 test it with a masked secret present in the snapshot. `decodeObjectList` gained a `prior` param for nested-secret preservation. Echoed-secret opt-in scoped out of PR-A (no echoing secret; YAGNI, deferred to first consumer). Converter citations reframed to name the `<attr>ModelToSetting` methods with the golden as the hard oracle. Task 8 tightened to an end-to-end `resource.Test` proving Core persistence. Schema-equivalence gains focused behavioral tests (validator reject / default applies / plan-modifier no-churn).

**Resolved from turn 2:** (2) `bestEffortState` secret rule fixed — a PUT section's *rotated* secret is retained, a null secret falls back to prior, non-PUT sections keep prior. (5) Framework state-on-error settled from source (`server_updateresource.go:154` sets NewState unconditionally) → op stays failed AND best-effort state persists; no warning fallback, no spec change; Task 8 is now a regression guard that escalates on framework change. (1) nested-object/nested-list/duration ownership-aware codec helpers added (Task 2); usg + mgmt worked with real code. (4) converter decode/overlay line citations added per section; golden is the exactness oracle. httptest adapter test added (Task 5). Import hydration marker specified (all-null attrs, no flag). Schema-equivalence uses a pre-cutover normalized golden. Template task-number references corrected (10 scalar, 16 list, 21 nested-object, 22 nested-list).
