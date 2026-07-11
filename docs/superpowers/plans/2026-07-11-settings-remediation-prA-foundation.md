# PR-A: Settings Lifecycle Foundation — Implementation Plan (rev 2)

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
- Migration proof: request-level golden tests (Task 8) stay green through every migration task; the legacy converters are retained until the final cutover (Task 24c). Behavior not on the permitted-delta list must be byte-identical to `origin/main`.
- Verification per task: `gofmt -w`, `go build ./...`, `go vet ./unifi/`, `go test ./unifi/...`, `git diff --check`. TF_ACC tests are demo-controller-only.
- **Permitted deltas** (the ONLY behavior changes allowed in PR-A): (1) present-empty string/array round-trips instead of collapsing to null; (2) malformed remote scalar/list raises a diagnostic instead of truncating/dropping; (3) writes preserve all unmodeled controller keys; (4) one snapshot per op replaces per-section reads; (5) import populates `site` + hydrates all sections. Each golden that changes for delta (1) cites the delta in its commit.

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
  - `overlayString(out, key, class, v types.String)` — if `class.writesToPUT()` then `putString`; else no-op. Analogous `overlayInt64/overlayBool/overlayStringList`.

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
  - **Transport (real adapter):** a test that marshals a `RawSetting{Data:{"key":"mgmt","x_ssh_enabled":true,"unmodeled":"keep"}}` via the SAME path `UpdateSetting` uses (`json.Marshal(&rs)`) and asserts the produced JSON body contains `unmodeled":"keep"` and `x_ssh_enabled":true` — proving the merged map (incl. unmodeled keys) reaches the wire. (Pure marshal test; no HTTP.)

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

- [ ] **Step 2: Run** `go test ./unifi/ -run 'TestFakeClient|TestRawSettingMarshal'` → FAIL.
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
- `func bestEffortState(prior, plan settingResourceModel, put map[string]bool, sections []settingSection) settingResourceModel` — start from `prior`; for each section where `put[key]` is true, copy that section's attribute from `plan` (the values that were sent); secrets always taken from `prior`. Never includes a not-PUT section's `plan` value.

- [ ] **Step 1: Failing tests** (via the fake + two stub sections passed as the `sections` param):
  1. `TestEngine_noWriteBeforeReconcileError`: a stub `overlay` returns a diagnostic ⇒ `fake.puts` is empty.
  2. `TestEngine_oneSnapshotOnRead`: `readSections` triggers exactly one `ListSettings` (count on the fake).
  3. `TestEngine_partialApplyReReads`: section `a` PUTs ok, `b` injected-fail ⇒ error returned AND `readSections` after shows `a`'s new value, `b`'s old value.
  4. `TestEngine_preservesUnmodeledKeys`: overlay sets one key; the recorded PUT `Data` still contains a pre-existing unmodeled key from the snapshot.
  5. `TestBestEffortState_excludesUnattempted`: `put={a:true}`, plan has new `a` and `b` ⇒ result has plan's `a`, prior's `b`.
  6. `TestBestEffortState_secretsFromPrior`: a secret attr comes from `prior` even for a PUT section.

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
		out = bestEffortState(prior, plan, put, sections) // C2.4 second failure
		d.AddWarning("settings read-back failed after apply",
			"state written best-effort from applied values; run `terraform refresh`")
	}
	if putErr != nil {
		d.AddError("settings apply failed", putErr.Error())
	}
	return out, d
}
```

Implement `readSections`, `listSnapshot`, and `bestEffortState` in full per the contracts above.

- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(setting): engine — reconcile-before-mutate, partial-apply, best-effort recovery (C2.2/2.4/2.5)`.

---

## Task 8: Framework partial-state-on-error verification

**Files:** Create `unifi/setting_framework_state_test.go`

**Purpose (blocking spec verification):** determine empirically whether the plugin-framework/Terraform persists a `resp.State` set alongside an `Update` **error** diagnostic. The C2.4 contract ("state written best-effort") depends on this.

- [ ] **Step 1:** Write a minimal framework-level test: a throwaway resource whose `Update` sets a known state value AND appends an error diagnostic; drive it through `resource.Test` with a step that `ExpectError`s and a follow-up `RefreshState`/plan assertion (or use `fwserver` directly) to observe whether the errored state is persisted.
- [ ] **Step 2: Run** and record the actual behavior.
- [ ] **Step 3: Conform the engine to the result:**
  - If Core **persists** errored state (expected — Terraform's partial-apply model): keep Task 7's `applySections` returning `(out, error)` and have the resource set state before the error (Task 24b).
  - If Core **discards** errored state: change the partial/second-failure paths to surface the failure as a **warning + written state** (not a hard error) so state persists, and document the deviation in the spec's C2.4 (flag to the maintainer). Do NOT silently lose the recovery state.
- [ ] **Step 4: Run** the conforming engine tests → PASS.
- [ ] **Step 5: Commit** `test(setting): verify framework state persistence on Update error; conform C2.4`.

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

**Structural templates** (write once, reused): Task 10 (`auto_speedtest`) is the **scalar** template; Task 20 (`syslog`) is the **list** template; Task 23 (`mgmt`) is the **nested-object + nested-list** template. Later tasks of the same shape follow the matching template.

### Task 10 (scalar template): `auto_speedtest`
- Files: `unifi/setting_section_auto_speedtest.go` (+ test). Legacy converter authority: `setting_resource.go` (auto_speedtest converters). Ownership: `enabled`→ownerManaged, `cron_expr`→ownerManaged.
- Steps 1–5 as the fully-worked example: failing decode/overlay round-trip + present-empty + preservation test → implement `decode` (uses `decodeBool`/`decodeString` with the class from `ownership()`, base = `snap.section` Data) and `overlay` (base = `snap.dataCopy("auto_speedtest")`, apply `overlayBool`/`overlayString`) + `schemaAttribute()` identical to today + `ownership()` map → assert golden reproduced → commit `refactor(setting): migrate auto_speedtest onto the engine`.

### Tasks 11–22: per-section specifics

For each: cite the converter, port field-for-field, apply ownership, assert golden. Full ownership maps (attr path → class); anything not listed is `ownerManaged`:

| Task | Section | go-unifi type | Ownership (non-Managed leaves) | Template |
|---|---|---|---|---|
| 11 | `country` | `settings.Country` | — | scalar |
| 12 | `dpi` | `settings.Dpi` | — | scalar |
| 13 | `lcm` | `settings.Lcm` | — | scalar |
| 14 | `network_optimization` | `settings.NetworkOptimization` | — | scalar |
| 15 | `ntp` | `settings.Ntp` | — | scalar |
| 16 | `syslog` (list template) | `settings.Rsyslogd` | `contents` list ownerManaged (empty-list round-trip = permitted delta 1) | list |
| 17 | `doh` | `settings.Doh` | `server_names` list ownerManaged | list |
| 18 | `ips` | `settings.Ips` | `enabled_categories`, `enabled_networks` lists ownerManaged | list |
| 19 | `igmp_snooping` | `settings.IgmpSnooping` | `network_ids` list ownerManaged | list |
| 20 | `radius` | `settings.Radius` | `x_secret`→ownerWriteOnlySecret; ports/enabled ownerManaged | scalar+secret |
| 21 | `usg` | `settings.Usg` | nested `dns_verification` children ownerManaged; scalars ownerManaged | nested-object |
| 22 | `mgmt` (nested template) | `settings.Mgmt` | `ssh_password`→ownerWriteOnlySecret; `x_mgmt_key`→ownerWriteOnlySecret (if in schema); `ssh_keys` (public keys, ListNested)→ownerManaged; all other flags ownerManaged | nested-object + nested-list |

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
- Modify `setting_resource.go`: `Schema` builds section attrs from `orderedSections(settingSections)`; Create/Update/Read call `applySections`/`readSections` with `settingSections` + `realSettingsClient{r.client}` + `resolveSite`. **Legacy converters remain** (unused by the new path but still compiled for goldens).
- Test `TestSettingSchema_equivalence`: the assembled schema equals `origin/main`'s for all 13 sections — attribute names, types, Optional/Computed/Sensitive, defaults, validators, and plan modifiers (deep-compare the schema tree, not just names).
- Commit `refactor(setting): drive lifecycle through the section engine (legacy retained)`.

### Task 24b: Site-aware import + hydration
- `ImportState`: parse the import ID as a **site name** (not `site:id`); validate non-empty/unambiguous (diagnostic otherwise); set `site` + `id`; mark for full hydration. `Read` uses `onlyConfigured=false` when all section attrs are null (imported) to hydrate every registered section.
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

**Resolved from codex prA-plan-review turn 1:** C1 now encoded in codec + `ownership()` (blocking 1); `bestEffortState` corrected + tested (blocking 2); goldens retained until 24c (blocking 3); full ownership maps + templates + ssh fix (blocking 4); framework-state verification task (blocking 5); deterministic ordered registry + sections-as-param, no global test race (important); PUT-body transport test (important); Task 24 split + schema-equivalence (important); deep-copy snapshot, deduped goldens, snapshot/refresh wording (minor).
