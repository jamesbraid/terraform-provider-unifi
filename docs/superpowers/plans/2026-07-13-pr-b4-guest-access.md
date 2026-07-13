# PR-B4: `guest_access` Settings Section — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Revision note (rev. 2):** this plan was revised after an independent codex
review returned NEEDS-WORK on rev. 1. Changes: (1) secret leaves are now
`Optional + Computed + Sensitive`, matching the actually-shipped
`radius.secret` precedent, not the stale Optional+Sensitive-only C1 table
reading; (2) four operationally-necessary fields (`auth_url`, `ec_enabled`,
`custom_ip`, `redirect_https`) are promoted from preserved to modeled; (3)
the modeled/preserved split is now 56/41 (was 52/45), 18 secrets unchanged;
(4) `allSectionAttrsNull`/`allSectionsNullModel` wiring is now an explicit
task (Task 5a); (5) the secret test matrix (Task 3) is now a full
table-driven test over all 18 leaves, not a sampled subset; (6) the secret
carry helper is section-local (`carryGuestAccessSecrets` in
`setting_section_guest_access.go`), not a new shared `setting_engine.go`
function — this also reverses rev. 1's Key Decision 2 recommendation in
favor of its own documented fallback, specifically to avoid PR-A-file churn.
See `docs/superpowers/specs/2026-07-13-pr-b4-guest-access-design.md`
(rev. 2) for full rationale on every point.

**Goal:** Add the `guest_access` settings section to `unifi_setting`, modeling
the 56-field operational core (18 secrets) and preserving the 41-field
portal-template surface by read-modify-write, per
`docs/superpowers/specs/2026-07-13-pr-b4-guest-access-design.md`. New section
only — **zero changes to `setting_engine.go` or any other shared PR-A file**
(rev. 2: the secret-carry helper is section-local; see Task 3).

**Architecture:** One new `settingSection` implementation
(`guestAccessSection` in `unifi/setting_section_guest_access.go`), registered
via `init()`, following the existing 13-section contract exactly (`key`,
`attrName`, `schemaAttribute`, `decode`, `overlay`, `carryBestEffort`,
`isConfigured`). Non-secret leaves use the existing class-free codec
(`decodeString`/`overlayString`, `decodeBool`/`overlayBool`,
`decodeInt64`/`overlayInt64`, `decodeStringList`/`overlayStringList`). The 18
secret leaves use the existing top-level `WriteOnlySecret` pattern inline
(`radius`-style decode-always-prior / overlay-delete-on-unset,
**Optional+Computed+Sensitive** schema flags — see spec Key Decision 2a),
fanned out through a new **section-local** `carryGuestAccessSecrets` helper
(spec Key Decision 2b) in `unifi/setting_section_guest_access.go` that loops
the existing `carrySecretObject` from `unifi/setting_engine.go` — that
shared file is read, not modified. The 41 unmodeled fields are never decoded
into state; they survive every write via the section's own
`snap.dataCopy("guest_access")` RMW base, exactly like `mgmt`'s
`alert_enabled`/`boot_sound`/etc.

**Tech Stack:** Go, terraform-plugin-framework v1.19.0, existing
`unifi/setting_*` engine (no changes to `setting_snapshot.go`,
`setting_section.go`'s interface, `setting_engine.go`, or the engine's
lifecycle methods).

## Provenance

Spec: `docs/superpowers/specs/2026-07-13-pr-b4-guest-access-design.md`
(rev. 2; field table verified by script against
`github.com/ubiquiti-community/go-unifi@v1.33.43-.../unifi/settings/guest_access.generated.go`).
Parent design: `docs/superpowers/specs/2026-07-11-settings-remediation-design.md`
§367. Built on `settings-expansion-v2` foundation as simplified by
`docs/superpowers/plans/2026-07-12-settings-simplification-prA.md` (tip
`0ff93708`) — the class-free codec, `carrySecretObject`, and the 13-section
registry this plan extends.

## Global Constraints

- **Depends only on PR-A. Introduces no dependency on any other Bn tranche.**
  Nothing in B1/B2/B3/B5 is referenced.
- **No breaking schema change to the 13 existing sections.** This PR is
  strictly additive: a new top-level `guest_access` attribute on
  `unifi_setting`, a new `GuestAccess types.Object` field on
  `settingResourceModel`, and a new **explicit clause added to
  `allSectionAttrsNull()`** (Task 5a — rev. 2 adds this; rev. 1 omitted it,
  which would have left a freshly-imported resource's hydration gate wrong
  for the new section). **Confirmed (pre-verified against the live file, not
  left as a Task 1 open question):** `unifi/setting_schema_equiv_test.go`
  iterates a fixed, hardcoded `settingSectionAttrNames` (13 names) and only
  looks up each by name in the built schema — it never asserts the schema's
  *total* attribute count, so a 14th attribute (`guest_access`) is invisible
  to it. It stays green **unmodified**; do not add `guest_access` to
  `settingSectionAttrNames` or touch this file at all.
- **`unifi/setting_migration_inventory_test.go` is OUT OF SCOPE — do not add
  `guest_access` to it.** Its own docstring identifies it as "the PR-A
  settings-migration inventory" — a closed historical record of the 13
  *legacy* sections migrated onto the new engine, hard-pinned by
  `TestMigrationInventoryCoversAllSections`'s `len(...) != 13` assertion.
  `guest_access` is a new section, not a legacy migration; adding it to that
  list would be a category error and would break the test's own stated
  purpose (confirmed by reading the file: `settingMigrationInventory` and
  `settingSectionSchemaAttrNames` are both hardcoded 13-entry lists with no
  connection to the live `settingSections` registry — this closed inventory
  does NOT reflect the live model's field set, and no claim to the contrary
  appears anywhere in this plan or spec). `unifi/setting_golden_test.go`
  gets a new `TestGolden_guest_access` regardless (every section needs a
  PUT-body golden), but the migration-inventory table/list stays untouched.
- **Secrets are frozen to the existing pattern, with the Computed flag
  corrected (rev. 2).** All 18 secret leaves are `WriteOnlySecret`-class
  top-level string attributes: **`Optional`, `Computed`, AND `Sensitive`**
  (matching `radius.secret`, NOT `mgmt.ssh_password`'s Optional-only-without-
  Computed variance — see spec Key Decision 2a for why Computed is
  framework-required for a Computed parent object's secret children whose
  decode always supplies a provider-computed value). Decode always reads
  from `priorModel.<Field>` (never the API — the controller returns a mask,
  never the real value); overlay deletes the wire key when config is
  null/unknown and writes verbatim (including explicit empty) when
  configured. No `*_wo`/native-write-only attributes. No `echoed` fields
  (none of guest_access's secrets are known to echo). Secret values never
  appear in logs, diagnostics, or comments.
- **Preserve-by-default for the 41 unmodeled fields.** `overlay()` starts
  from `snap.dataCopy("guest_access")`; only the 56 modeled fields are ever
  written by this section's overlay. The 41 preserved fields are never
  decoded into `settingGuestAccessModel` and never appear in
  plan/state/diagnostics.
- **Every task leaves the branch building and green.** `go build ./...`,
  `go vet ./unifi/`, `gofmt -l unifi/` clean, `go test ./unifi/...` all pass
  at every task tip.
- **Privacy:** no real payment credentials, RADIUS secrets, OAuth
  client IDs/secrets, or hostnames in any test/example/fixture. Use the
  synthetic values from the spec's "Privacy-safe synthetic fixtures" section
  (RFC 5737/1918/2606 ranges only; never a real-looking value, never the
  word "public").
- **Field-count contract:** the modeled/preserved/secret split MUST match
  the spec's mechanically-verified **56/41/18** exactly. Task 1 re-derives
  this split from the live go-unifi struct as a checked-in test, not a
  one-off script — if go-unifi's `GuestAccess` struct has drifted since the
  spec was written (new/removed/renamed fields), that is a STOP-and-reconcile
  condition, not a silent adjustment.
- **Zero changes to `unifi/setting_engine.go`.** Rev. 2's Key Decision 2b:
  the secret-carry helper (`carryGuestAccessSecrets`) is section-local in
  `unifi/setting_section_guest_access.go` and calls the existing
  `carrySecretObject` (unmodified) 18 times in a loop. If any task step in
  this plan appears to require touching `setting_engine.go`, STOP — that is
  a plan-changing fact, not a minor adjustment to make in-flight.

## Current-State Reference (read before starting)

- `docs/superpowers/specs/2026-07-13-pr-b4-guest-access-design.md` (rev. 2)
  — the field table (modeled/preserved/secret), validators, fixture
  conventions, and the `carryGuestAccessSecrets` design (read in full before
  Task 1).
- `unifi/setting_section_dpi.go` — simplest template: flat scalars, no
  secrets, no nested objects. Base structure for `guest_access`'s non-secret
  leaves.
- `unifi/setting_section_radius.go` — the one-secret-leaf template
  (`secret`→`x_secret`), including its schema block (`Optional + Computed +
  Sensitive`, ~lines 92-104) and its `carryBestEffort`'s `carrySecretObject`
  call (~lines 215-219) — the exact schema-flag and carry-call precedent
  Task 3 repeats 18 times.
- `unifi/setting_section_mgmt.go` — a second one-secret-leaf template
  (`ssh_password`, ~lines 132-137) plus RMW-preserved unmodeled top-level
  fields (`alert_enabled`, `boot_sound`, etc.) — the direct precedent for
  guest_access's 41 preserved fields (same mechanism: `snap.dataCopy(key)`
  base, only modeled fields overlaid). **Note the schema-flag variance**:
  `mgmt.ssh_password` is `Optional + Sensitive` WITHOUT `Computed` — this is
  a pre-existing inconsistency in the codebase, not a second valid pattern.
  `guest_access`'s 18 secrets follow `radius.secret`'s flags (with Computed),
  not `mgmt.ssh_password`'s.
- `unifi/setting_engine.go:199-249` — `carrySecretObject`, the function
  `carryGuestAccessSecrets` calls in a loop. **Read-only reference — this
  file is not modified by this plan** (rev. 2; rev. 1 would have added a
  generalized `carrySecretObjectMulti` here, which the codex review flagged
  as unnecessary PR-A-file churn for a section-local concern).
- `unifi/setting_codec.go` — the class-free codec: `decodeString/Bool/
  Int64/StringList` + `overlayString/Bool/Int64/StringList` (lines ~259-320,
  post-simplification signatures — no `class`/`ownership` parameter).
- `unifi/setting_section.go` — the 7-method `settingSection` interface (no
  change needed; `guestAccessSection` implements it as-is).
- `unifi/setting_resource.go:305-322` — `settingResourceModel`; Task 5 adds
  `GuestAccess types.Object \`tfsdk:"guest_access"\`` here. `Schema()`
  (line ~496) derives attributes from `orderedSections(settingSections)`
  automatically — no separate schema-wiring edit needed beyond registration.
- `unifi/setting_resource.go:648-665` — `allSectionAttrsNull()`, an explicit
  13-clause boolean check (`m.AutoSpeedtest.IsNull() && ... `). Task 5a adds
  a 14th clause (`&& m.GuestAccess.IsNull()`) — this function is NOT derived
  from the section registry, so a new section is silently invisible to the
  post-import hydration gate unless this literal function body is edited.
- `unifi/setting_engine_lifecycle_test.go:32-48` — `allSectionsNullModel()`,
  the test fixture returning a `settingResourceModel` with all 13 section
  fields set to `types.ObjectNull(<attrTypes>)`. Task 5a adds a 14th
  `GuestAccess: types.ObjectNull(guestAccessAttrTypes)` line. Referenced from
  `unifi/setting_engine_lifecycle_test.go`,
  `unifi/setting_engine_capability_scope_test.go`,
  `unifi/setting_fix_c_regression_test.go`, and
  `unifi/setting_resource_test.go` — all of these compile against this one
  fixture, so it must be updated in the same commit as the model-field
  addition (Task 5) or earlier callers referencing `GuestAccess` will not
  compile.
- `unifi/setting_resource_test.go:834-848` — `TestAllSectionAttrsNull_gate`,
  the direct unit test for `allSectionAttrsNull()`: asserts `true` for
  `allSectionsNullModel()` and `false` once any one section (currently Dpi)
  is populated. Task 5a extends this test with a `guest_access`-specific
  case: build a model where every section is null EXCEPT `GuestAccess`, and
  assert `allSectionAttrsNull()` returns `false` — proving the new clause is
  actually wired into the `&&` chain, not just present as dead code alongside
  an already-true expression.
- `unifi/setting_golden_test.go` — PUT-body golden oracle; Task 6 adds
  `goldenGuestAccess` + `TestGolden_guest_access`.
- `unifi/setting_migration_inventory_test.go` — **read but do not modify**
  (see Global Constraints).
- `unifi/setting_schema_equiv_test.go` — **read to confirm it does not need
  a `guest_access` entry** (see Global Constraints) before Task 1.
- `unifi/firewall_policy_resource.go` (~line 360) and
  `unifi/site_to_site_vpn_resource.go` (~lines 229-230) — the codebase's
  existing `listvalidator.ValueStringsAre(...)` precedent for validating
  every element of a string list. No existing `unifi_setting` section
  validates a string list per-element yet (`ips.enabled_categories` has no
  validator at all) — `restricted_dns_servers` is the first, so follow these
  two non-setting resources' composition, not an `unifi_setting`-internal
  one (there isn't one).
- `examples/resources/unifi_setting/resource.tf` — existing example shape;
  Task 7 adds a `guest_access` block.
- `CHANGELOG.md` `[Unreleased]` section — Task 7 adds an entry in the
  existing bug-fix/feature narrative style (see the `unifi_ap_group`/
  `unifi_device` entries for tone: lead with the user-visible capability,
  then the mechanism, then any caveat).

## File Structure (what changes)

- Create: `unifi/setting_section_guest_access.go` — the section
  implementation (schema, decode, overlay, carryBestEffort, isConfigured,
  and the section-local `carryGuestAccessSecrets` helper).
- Create: `unifi/setting_section_guest_access_test.go` — unit tests: golden
  reproduction, secret matrix (all 18 leaves, exhaustive), decode-roundtrip,
  RMW preservation of the 41 unmodeled fields, not-configured gating.
- Modify: `unifi/setting_resource.go` — add `GuestAccess types.Object
  \`tfsdk:"guest_access"\`` to `settingResourceModel` (Task 5); add a
  `guest_access` clause to `allSectionAttrsNull()` (Task 5a).
- Modify: `unifi/setting_engine_lifecycle_test.go` — add
  `GuestAccess: types.ObjectNull(guestAccessAttrTypes)` to
  `allSectionsNullModel()` (Task 5a).
- Modify: `unifi/setting_resource_test.go` — extend
  `TestAllSectionAttrsNull_gate` with a guest_access-populated case
  (Task 5a).
- Modify: `unifi/setting_golden_test.go` — add `goldenGuestAccess` constant +
  `TestGolden_guest_access` (Task 6).
- Modify: `examples/resources/unifi_setting/resource.tf` — add a
  `guest_access` example block (Task 7).
- Modify: `CHANGELOG.md` `[Unreleased]` — add an entry (Task 7).
- Do NOT modify: `unifi/setting_engine.go` (rev. 2 — see Global
  Constraints), `unifi/setting_migration_inventory_test.go`,
  `unifi/setting_section.go` (interface), `unifi/setting_snapshot.go`,
  `unifi/setting_codec.go` (no new codec primitive is needed — all 56
  modeled fields are scalars/lists the existing codec already handles),
  `unifi/setting_schema_equiv_test.go`.

---

### Task 1: Field-split contract test (no production code)

**Why:** The spec's 56/41/18 split is a claim against a third-party
generated file that can drift on a go-unifi bump. Before writing any schema
or decode/overlay code, pin the split as an executable fact so any future
go-unifi version bump that changes `GuestAccess`'s fields fails loudly here
first, not as a silent schema gap discovered later.

**Files:**
- Create: `unifi/setting_section_guest_access_test.go` (this task only adds
  the field-split test to it; later tasks append to the same file).

**Interfaces:**
- Produces: `guestAccessModeledFields map[string]bool` (Go field name →
  true) and `guestAccessSecretFields map[string]bool` (subset), as
  package-level vars in the new section file (Task 2 defines them for real
  use by decode/overlay comments/tests; this task's test asserts their
  cardinality and membership against the spec's tables).

- [ ] **Step 1: Write the failing contract test** — using `reflect` over a
  zero-value `settings.GuestAccess{}` (imported read-only from
  `github.com/ubiquiti-community/go-unifi/unifi/settings`) to enumerate
  every `json:"..."` struct tag, then assert:
  - total field count (excluding embedded `BaseSetting`) is 97;
  - exactly 56 are in a hardcoded `wantModeled` set (copied from the spec's
    "Modeled" table Go field names — including the four rev. 2 promotions:
    `AuthUrl`, `EcEnabled`, `CustomIP`, `RedirectHttps`);
  - exactly 18 of those 56 are in a hardcoded `wantSecret` set (copied from
    the spec's 18-name list);
  - the remaining 41 (97 − 56) match a hardcoded `wantPreserved` set (or,
    equivalently, assert `wantModeled ∪ wantPreserved == all fields` and
    `wantModeled ∩ wantPreserved == ∅` — don't hand-list `wantPreserved`
    twice, derive it as the complement to avoid the same drift the spec's
    own prose had before its numbers were script-verified).

```go
// TestGuestAccessFieldSplit pins the spec's modeled/secret/preserved split
// against the live go-unifi struct so a future go-unifi bump that adds,
// removes, or renames a GuestAccess field fails here first, not as a
// silent schema gap. See docs/superpowers/specs/2026-07-13-pr-b4-guest-access-design.md
// (rev. 2: 56 modeled / 18 secret / 41 preserved).
func TestGuestAccessFieldSplit(t *testing.T) {
	typ := reflect.TypeOf(settings.GuestAccess{})
	all := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Anonymous { // BaseSetting
			continue
		}
		all[f.Name] = true
	}
	if len(all) != 97 {
		t.Fatalf("settings.GuestAccess has %d fields, want 97 — go-unifi drifted; reconcile the spec before proceeding", len(all))
	}

	wantModeled := map[string]bool{ /* 56 names, copied from the spec's rev. 2 Modeled table */ }
	wantSecret := map[string]bool{ /* 18 names, subset of wantModeled */ }

	if len(wantModeled) != 56 {
		t.Fatalf("wantModeled has %d entries, want 56", len(wantModeled))
	}
	if len(wantSecret) != 18 {
		t.Fatalf("wantSecret has %d entries, want 18", len(wantSecret))
	}
	for name := range wantSecret {
		if !wantModeled[name] {
			t.Errorf("wantSecret contains %q which is not in wantModeled", name)
		}
	}
	for name := range wantModeled {
		if !all[name] {
			t.Errorf("wantModeled contains %q which is not a settings.GuestAccess field", name)
		}
	}
	preserved := 0
	for name := range all {
		if !wantModeled[name] {
			preserved++
		}
	}
	if preserved != 41 {
		t.Errorf("preserved field count = %d, want 41", preserved)
	}
}
```

- [ ] **Step 2: Run to verify it fails** — expect a compile error (no
  `guest_access` test file existed) or, once the file compiles with empty
  `wantModeled`/`wantSecret` maps, a count-mismatch failure.

Run: `go test ./unifi/ -run TestGuestAccessFieldSplit -v`

- [ ] **Step 3: Fill in `wantModeled`/`wantSecret`** from the spec's tables
  verbatim (56 and 18 names respectively — double-check the four rev. 2
  promotions, `AuthUrl`/`EcEnabled`/`CustomIP`/`RedirectHttps`, are present
  in `wantModeled` and absent from any leftover "preserved" hand-list).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./unifi/ -run TestGuestAccessFieldSplit -v`
Expected: PASS. If it fails on the 97/56/41/18 counts, STOP — either the
spec's tables have a transcription error or go-unifi has drifted; fix the
spec file, not the test's expected numbers, unless go-unifi is confirmed to
have changed (check the module's version pin in `go.mod` against the one
cited in the spec).

- [ ] **Step 5: Sanity-check the Global Constraints' pre-verified reading**
  of `unifi/setting_schema_equiv_test.go` and
  `unifi/setting_migration_inventory_test.go` against the file contents at
  this tip (both were read in full during planning; re-confirm nothing
  changed since — a `git log -1 --format=%H -- unifi/setting_schema_equiv_test.go
  unifi/setting_migration_inventory_test.go` compared against the plan's
  authoring context is sufficient. If either file's shape has changed,
  STOP and re-read before proceeding — this is a plan-changing fact, not a
  silent override).

- [ ] **Step 6: Commit**

```bash
git add unifi/setting_section_guest_access_test.go
git commit -m "setting: pin guest_access field-split contract (56/41/18)"
```

---

### Task 2: Core scalars — schema, decode, overlay, no secrets yet

**Why:** Establish the section skeleton and the 38 non-secret modeled leaves
first (auth mode + auth_url, portal/expiry/network incl. custom_ip/
ec_enabled, both redirect flags, RADIUS, OAuth enable/id, payment
enable/gateway-selector/sandbox-toggles), deferring the 18 secrets to Task 3
so this task's diff is reviewable as "a dpi-shaped section, just bigger" —
matching the task brief's suggested split (core scalars; secrets;
nested/list fields — adapted here to core scalars; secrets, since
guest_access's only list is `restricted_dns_servers`, folded into this task
as a plain string list, not a nested-object list requiring the third split).

**Files:**
- Create: `unifi/setting_section_guest_access.go`.
- Modify: `unifi/setting_section_guest_access_test.go` — golden
  reproduction and decode-roundtrip tests for the non-secret leaves only
  (secrets added in Task 3's tests).

**Interfaces:**
- Produces: `guestAccessSection struct{}`, `settingGuestAccessModel` (38
  non-secret fields for now), `guestAccessAttrTypes map[string]attr.Type`
  (38 entries for now), the 7 `settingSection` methods.
- Consumes: existing `decodeString/Bool/Int64/StringList`,
  `overlayString/Bool/Int64/StringList`, `snap.dataCopy`, `int64validator`,
  `stringvalidator`, `listvalidator` (all already imported by sibling
  sections or by `firewall_policy_resource.go`/`site_to_site_vpn_resource.go`
  for `listvalidator` specifically).

- [ ] **Step 1: Write the failing golden-reproduction test** — seed a
  representative `settingGuestAccessModel` (all 38 non-secret fields set to
  spec-listed synthetic values, including the four rev. 2 fields
  `auth_url`, `ec_enabled`, `custom_ip`, `redirect_https`) plus an RMW base
  map containing a handful of the 41 preserved fields (e.g.
  `portal_customized_bg_color`, `template_engine`, `wechat_shop_id`) with
  synthetic non-real values, call `overlay()`, and assert the resulting
  `RawSetting.Data` (a) contains every configured modeled field at its
  correct wire key, (b) contains the seeded preserved fields UNCHANGED
  (byte-identical), (c) contains no secret keys yet (none configured in
  this task's test model — secrets are added to the struct in Task 3, so
  this task's model has no secret fields at all).

```go
func TestGuestAccessSection_GoldenReproduction(t *testing.T) {
	snap := rawSettings{"guest_access": settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "guest_access"},
		Data: map[string]any{
			// A sample of the 41 preserved fields, present on the
			// "controller" and expected to survive overlay untouched.
			"template_engine":            "angular",
			"portal_customized_bg_color": "#112233",
			"wechat_shop_id":             "shop-example-001",
		},
	}}
	model := settingResourceModel{GuestAccess: /* built via types.ObjectValueFrom from a populated settingGuestAccessModel, including auth_url/ec_enabled/custom_ip/redirect_https */}

	rs, configured, diags := guestAccessSection{}.overlay(context.Background(), model, settingResourceModel{}, snap)
	// assert !diags.HasError(), configured == true
	// assert rs.Data["auth"] == "hotspot" (or whatever synthetic value), etc. for all 38
	// assert rs.Data["auth_url"] == "https://auth.example.internal/guest"
	// assert rs.Data["custom_ip"] == "192.0.2.10"
	// assert rs.Data["ec_enabled"] == true (or configured bool)
	// assert rs.Data["redirect_https"] == <configured bool>, distinct from rs.Data["redirect_to_https"]
	// assert rs.Data["template_engine"] == "angular" (preserved, untouched)
	// assert rs.Data["portal_customized_bg_color"] == "#112233" (preserved, untouched)
	// assert rs.Data["wechat_shop_id"] == "shop-example-002" (preserved, untouched)
}
```

- [ ] **Step 2: Run to verify it fails** — compile error (no
  `guest_access.go` yet).

Run: `go test ./unifi/ -run TestGuestAccessSection -v`

- [ ] **Step 3: Write `unifi/setting_section_guest_access.go`** — the
  `dpi`-shaped skeleton, sized up to 38 fields. Key details to get right
  (from the spec's table):
  - Wire keys with a trailing underscore: `allowed_subnet_`,
    `restricted_subnet_` — not a typo, copy verbatim from go-unifi's json
    tag.
  - `redirect_to_https` and `redirect_https` are two INDEPENDENT boolean
    leaves — do not conflate them or assume one implies the other; no
    cross-field validator ties them together (see spec Key Decision 3).
  - `restricted_dns_servers` is a `types.List` of `types.StringType` — use
    `decodeStringList`/`overlayStringList`, matching how `syslog`'s
    `contents` already handles a string list (grep for `decodeStringList`
    usage in an existing section for the exact calling shape before writing
    this one); layer `listvalidator.ValueStringsAre(stringvalidator.RegexMatches(...))`
    on top per the `firewall_policy_resource.go`/`site_to_site_vpn_resource.go`
    precedent (Current-State Reference above) — this is the first
    `unifi_setting` section to validate a string list per-element, so there
    is no in-package precedent to copy, only the two non-setting resources'.
  - `expire`/`expire_number`/`expire_unit` are three independent leaves
    (not a nested object) — matches go-unifi's flat struct shape.
  - Validators per the spec's Key Decision 3 table: `auth`
    (`stringvalidator.OneOf`), `expire_unit` (`int64validator.OneOf(1, 60,
    1440)`), `expire_number` (`int64validator.Between(1, 1000000)`),
    `portal_hostname` (`stringvalidator.RegexMatches`), `custom_ip`
    (`stringvalidator.RegexMatches`, dotted-quad-or-empty — rev. 2 field),
    `restricted_dns_servers` per-element regex (see above), `radius_auth_type`
    (`OneOf`), `radius_disconnect_port` (`Between(1, 65535)`), `gateway`
    (`OneOf`). `auth_url` has no validator (free-form).
  - `schemaAttribute()` returns a `SingleNestedAttribute`, `Optional +
    Computed`, no `UseStateForUnknown` (per spec's "Schema shape" — matches
    `radius`, not `mgmt`).
  - For now, `settingGuestAccessModel` has 38 fields; Task 3 adds the 18
    secret fields to the same struct (one struct, two tasks populating it —
    acceptable since Task 2's tip does not yet register secrets in the
    schema, so nothing references the not-yet-added fields).
  - `carryBestEffort` for this task: plain copy (`dst.GuestAccess =
    plan.GuestAccess`) — Task 3 replaces this with the
    `carryGuestAccessSecrets` call once secrets exist.
  - `init()` calls `registerSection(guestAccessSection{})`.
  - `key()` / `attrName()` both return `"guest_access"`.

- [ ] **Step 4: Add the decode-roundtrip test** — snap → decode → model;
  assert every one of the 38 leaves round-trips (including the four rev. 2
  fields), and that none of the 41 preserved fields appear anywhere in
  `model.GuestAccess`'s attribute set (i.e. `guestAccessAttrTypes` has
  exactly 38 keys at this task's tip).

- [ ] **Step 5: Run tests**

Run: `go build ./... && go vet ./unifi/ && go test ./unifi/ -run 'GuestAccess' -v`
Expected: PASS.

- [ ] **Step 6: Full suite regression check** — confirm the 13 existing
  sections are untouched.

Run: `go test ./unifi/... 2>&1 | tail -20`
Expected: PASS, zero golden diffs on the 13 existing sections.

- [ ] **Step 7: Commit**

```bash
git add unifi/setting_section_guest_access.go unifi/setting_section_guest_access_test.go
git commit -m "setting: add guest_access core scalars (no secrets yet)"
```

---

### Task 3: Secrets — 18 leaves, section-local `carryGuestAccessSecrets`

**Why:** Isolate the secret surface as its own reviewable diff, per the task
brief's explicit ask for a secrets-focused task and the spec's Key Decision
2a/2b. This is the highest-risk task (secret-clobber/leak potential) and
gets the most test scrutiny. **Rev. 2: this task no longer touches
`unifi/setting_engine.go` at all** — the carry helper is section-local.

**Files:**
- Modify: `unifi/setting_section_guest_access.go` — add the 18 secret fields
  to `settingGuestAccessModel`/`guestAccessAttrTypes`/schema/decode/overlay;
  add the `carryGuestAccessSecrets` helper and `guestAccessSecretLeaves`
  var; replace `carryBestEffort`'s plain copy with a call to it.
- Modify: `unifi/setting_section_guest_access_test.go` — the exhaustive
  secret matrix test (below) + updated golden/roundtrip tests now covering
  all 56 fields.
- Do NOT modify: `unifi/setting_engine.go`, `unifi/setting_section_radius.go`,
  `unifi/setting_section_mgmt.go`. `carrySecretObject` is called, not
  changed; `radius`/`mgmt` keep their existing single-leaf call sites
  untouched.

**Interfaces:**
- Produces (both in `unifi/setting_section_guest_access.go`):
  - `guestAccessSecretLeaves []string` — the 18 tfsdk attribute names, in
    the same order as the spec's secret list.
  - `carryGuestAccessSecrets(plan, prior types.Object, secretLeaves []string) (types.Object, diag.Diagnostics)`
    — loops `carrySecretObject` (from `setting_engine.go`, unmodified) once
    per leaf, threading the accumulating result as arg1 but ALWAYS the
    original `prior` parameter as arg2 on every iteration (see spec Key
    Decision 2b for why this is correctness-critical, not stylistic — a
    later leaf's null/unknown-in-plan substitution must read the true
    original prior value, not an already-mutated intermediate).

- [ ] **Step 1: Write the failing exhaustive secret-matrix test** — a
  table-driven test over **all 18 modeled secret leaves**, not a sampled
  subset. For each leaf, three independent cases (per the task's required
  matrix):

  1. **preserve/null** — config leaf is null, prior has a real value →
     decode/carry yields prior's value, never null, never the wire's masked
     placeholder.
  2. **preserve/unknown** — config leaf is unknown (e.g. a computed
     upstream reference not yet resolved), prior has a real value → same
     outcome as null: prior's value is kept (`IsUnknown()` is treated
     exactly like `IsNull()` per `carrySecretObject`'s documented contract).
  3. **rotate/non-empty** — config leaf is a new non-empty value, distinct
     from prior → overlay writes the NEW value verbatim to the wire key;
     carry keeps the NEW (plan) value, not prior.

  A fourth case, **rotate/empty** (config leaf is explicit `""`, distinct
  from prior, proving a real credential can be intentionally cleared), is
  added ONLY for secret leaves whose go-unifi struct/controller behavior
  does not impose a minimum length that would make an empty string
  unreachable through a validated Terraform config. **Check each of the 18
  leaves' go-unifi field comment/regex for a length-lower-bound before
  adding this case** (mirroring the B3 resolution referenced in the task
  brief: a min-length validator makes `""` unreachable through config, so do
  not construct an invalid model for that leaf). None of `guest_access`'s
  18 secret leaves carry a go-unifi regex/length constraint in
  `guest_access.generated.go` (unlike, e.g., `radius.secret`'s
  `stringvalidator.LengthBetween(1, 48)`, which is a `radius`-specific
  validator not present on any `guest_access` field) — so **this spec adds
  no length validator to any of the 18 secret leaves, and rotate/empty is
  exercised for all 18**. If a future revision adds a length validator to
  any leaf, that leaf's rotate/empty case must be removed at the same time
  (a mechanical link the test's own doc comment should flag).

  Table shape (18 leaves × up to 4 cases = up to 72 sub-tests; write the
  actual table, not a placeholder):

```go
// TestGuestAccessSection_SecretMatrix exhaustively exercises every one of
// the 18 modeled secret leaves independently: preserve/null,
// preserve/unknown, and rotate/non-empty are exercised for all 18;
// rotate/empty is exercised for all 18 as well since none of guest_access's
// secret leaves carry a go-unifi length-lower-bound (unlike radius.secret's
// LengthBetween(1,48) — see this test's table below for the one-time check
// per leaf). Each sub-test also asserts every OTHER (sibling) secret leaf in
// the same object is unaffected by the leaf under test, proving
// carryGuestAccessSecrets's per-leaf independence.
func TestGuestAccessSection_SecretMatrix(t *testing.T) {
	type secretCase struct {
		leaf          string // tfsdk attribute name
		wireKey       string // controller wire key
		priorValue    string
		newValue      string // used for rotate/non-empty
	}
	leaves := []secretCase{
		{"password", "x_password", "prior-password", "new-password"},
		{"facebook_app_secret", "x_facebook_app_secret", "prior-fb-secret", "new-fb-secret"},
		{"google_client_secret", "x_google_client_secret", "prior-google-secret", "new-google-secret"},
		{"wechat_app_secret", "x_wechat_app_secret", "prior-wechat-app-secret", "new-wechat-app-secret"},
		{"wechat_secret_key", "x_wechat_secret_key", "prior-wechat-secret-key", "new-wechat-secret-key"},
		{"paypal_username", "x_paypal_username", "prior-paypal-user", "new-paypal-user"},
		{"paypal_password", "x_paypal_password", "prior-paypal-pass", "new-paypal-pass"},
		{"paypal_signature", "x_paypal_signature", "prior-paypal-sig", "new-paypal-sig"},
		{"stripe_api_key", "x_stripe_api_key", "prior-stripe-key", "new-stripe-key"},
		{"authorize_loginid", "x_authorize_loginid", "prior-authorize-login", "new-authorize-login"},
		{"authorize_transactionkey", "x_authorize_transactionkey", "prior-authorize-txn", "new-authorize-txn"},
		{"quickpay_merchantid", "x_quickpay_merchantid", "prior-quickpay-merchant", "new-quickpay-merchant"},
		{"quickpay_apikey", "x_quickpay_apikey", "prior-quickpay-key", "new-quickpay-key"},
		{"quickpay_agreementid", "x_quickpay_agreementid", "prior-quickpay-agreement", "new-quickpay-agreement"},
		{"merchantwarrior_merchantuuid", "x_merchantwarrior_merchantuuid", "prior-mw-uuid", "new-mw-uuid"},
		{"merchantwarrior_apikey", "x_merchantwarrior_apikey", "prior-mw-key", "new-mw-key"},
		{"merchantwarrior_apipassphrase", "x_merchantwarrior_apipassphrase", "prior-mw-pass", "new-mw-pass"},
		{"ippay_terminalid", "x_ippay_terminalid", "prior-ippay-terminal", "new-ippay-terminal"},
	}
	if len(leaves) != 18 {
		t.Fatalf("test table has %d leaves, want 18", len(leaves))
	}

	for _, tc := range leaves {
		t.Run(tc.leaf+"/preserve_null", func(t *testing.T) {
			// config leaf null, prior has tc.priorValue -> decode/carry yields
			// tc.priorValue; overlay deletes tc.wireKey from the PUT body;
			// every sibling leaf's prior value is also asserted unchanged.
		})
		t.Run(tc.leaf+"/preserve_unknown", func(t *testing.T) {
			// config leaf unknown, prior has tc.priorValue -> same outcome as
			// preserve_null (IsUnknown treated exactly like IsNull).
		})
		t.Run(tc.leaf+"/rotate_non_empty", func(t *testing.T) {
			// config leaf = tc.newValue (distinct from tc.priorValue) -> overlay
			// writes tc.wireKey = tc.newValue verbatim; carry keeps tc.newValue;
			// every sibling leaf unaffected.
		})
		t.Run(tc.leaf+"/rotate_empty", func(t *testing.T) {
			// config leaf = "" (distinct from tc.priorValue, and reachable
			// through config because this leaf has no go-unifi length lower
			// bound) -> overlay writes tc.wireKey = "" verbatim (a real
			// rotate-to-empty, not a delete); carry keeps "" from plan, not
			// tc.priorValue; every sibling leaf unaffected.
		})
	}
}

func TestGuestAccessSection_SecretDecodeNeverReadsAPI(t *testing.T) {
	// mirrors TestRadiusSection_Decode's "masked wire value must not leak"
	// shape, generalized across all 18 leaves: seed data[wireKey] = "MASKED"
	// for every leaf and a distinct priorModel value per leaf; assert
	// decode() yields every prior value, never "MASKED", for all 18.
}

func TestGuestAccessSection_CarryBestEffortSecrets(t *testing.T) {
	// build a plan with SOME of the 18 secrets null/unknown and others
	// rotated (including at least one explicit empty-string rotation), a
	// dst seeded with distinct prior values for all 18, call
	// carryBestEffort, and assert: null/unknown-in-plan leaves keep dst's
	// (prior) value; set-in-plan leaves (including explicit empty string)
	// take plan's value; every non-secret leaf comes from plan. This is the
	// test that specifically exercises carryGuestAccessSecrets's
	// multi-leaf-in-one-object chaining (see spec Key Decision 2b) — assert
	// AT LEAST ONE leaf that is null-in-plan and appears AFTER a
	// rotated-in-plan leaf in guestAccessSecretLeaves' order still correctly
	// resolves to prior (not to the rotated leaf's sibling value or to a
	// zero value) — this is the regression test for the "must pass ORIGINAL
	// prior, not the accumulating out, as arg2" chaining bug the spec calls
	// out explicitly.
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./unifi/ -run 'GuestAccess' -v`

- [ ] **Step 3: Add the 18 secret fields to `guestAccessSection`** — schema
  (`Optional + Computed + Sensitive`, per leaf — see spec Key Decision 2a;
  this is the corrected flag set, NOT `mgmt.ssh_password`'s
  Optional+Sensitive-without-Computed), model struct fields,
  `guestAccessAttrTypes`, decode (each: `x := priorModel.X`, never from
  `data`), overlay (each: delete-on-null/unknown, verbatim-on-set including
  explicit empty).

```go
var guestAccessSecretLeaves = []string{
	"password", "facebook_app_secret", "google_client_secret",
	"wechat_app_secret", "wechat_secret_key",
	"paypal_username", "paypal_password", "paypal_signature",
	"stripe_api_key", "authorize_loginid", "authorize_transactionkey",
	"quickpay_merchantid", "quickpay_apikey", "quickpay_agreementid",
	"merchantwarrior_merchantuuid", "merchantwarrior_apikey", "merchantwarrior_apipassphrase",
	"ippay_terminalid",
} // tfsdk names, not Go field names — double-check against the schema's attribute keys; 18 entries

// carryGuestAccessSecrets threads plan.GuestAccess through carrySecretObject
// once per leaf in secretLeaves, accumulating the rebuilt object across
// iterations. CRITICAL: arg2 to carrySecretObject is ALWAYS the original
// prior object passed into this function, never the accumulating "out" —
// see the loop body for why substituting "out" would silently lose a later
// leaf's real prior value. carrySecretObject itself (unifi/setting_engine.go)
// is unmodified by this PR.
func carryGuestAccessSecrets(plan, prior types.Object, secretLeaves []string) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	if plan.IsNull() || plan.IsUnknown() {
		return prior, diags
	}

	out := plan
	for _, leaf := range secretLeaves {
		var d diag.Diagnostics
		// out accumulates each processed leaf's substitution, but prior is
		// threaded from the ORIGINAL parameter on every iteration — passing
		// "out" as arg2 here would make a not-yet-processed leaf see plan's
		// value (already copied into out) as if it were "prior", losing the
		// real prior value for that leaf if it is null/unknown in plan.
		out, d = carrySecretObject(out, prior, leaf)
		diags.Append(d...)
	}
	return out, diags
}

func (guestAccessSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	obj, diags := carryGuestAccessSecrets(plan.GuestAccess, dst.GuestAccess, guestAccessSecretLeaves)
	dst.GuestAccess = obj
	return diags
}
```

- [ ] **Step 4: Run tests**

Run: `go build ./... && go vet ./unifi/ && go test ./unifi/ -run 'GuestAccess' -v`
Expected: PASS, all up-to-72 secret-matrix sub-tests green.

- [ ] **Step 5: Full regression**

Run: `go test ./unifi/... 2>&1 | tail -25`
Expected: PASS. `radius`/`mgmt` goldens byte-identical (proof that
`carrySecretObject` itself was never touched — rev. 2 requires zero drift
here, not just "behavior-preserving").

- [ ] **Step 6: Confirm `setting_engine.go` is untouched**

Run: `git diff --stat unifi/setting_engine.go`
Expected: empty output. If this file shows any diff, STOP — Global
Constraints require zero changes to it; something in Step 3 leaked into the
wrong file.

- [ ] **Step 7: Grep for accidental secret logging** — confirm no `fmt.*`,
  `tflog.*`, or diagnostic message in the new file interpolates a secret
  value (only field *names*, never values, may appear in diagnostics).

Run: `grep -n 'Password\|Secret\|Apikey\|Signature' unifi/setting_section_guest_access.go | grep -iv 'field\|leaf\|tfsdk\|json\|wire'`
Expected: manually eyeball every hit — none should be a `%v`/`%s`-style
interpolation of the secret's *value* (field names in error message strings
like `"field %q: ..."` are fine; the values themselves must never appear).

- [ ] **Step 8: Commit**

```bash
git add unifi/setting_section_guest_access.go unifi/setting_section_guest_access_test.go
git commit -m "setting: add guest_access secret surface (18 leaves, carryGuestAccessSecrets)"
```

---

### Task 4: RMW preservation + golden-repro for the 41 unmodeled fields

**Why:** The task brief specifically calls for a "golden-repro (RMW-seed the
large preserved surface)" test — prove the 41 preserved fields survive a
realistic multi-field write untouched, not just the 2-3 sampled in Task 2's
smoke test.

**Files:**
- Modify: `unifi/setting_section_guest_access_test.go`.

**Interfaces:** none new — this task is pure test coverage over Task 2/3's
production code.

- [ ] **Step 1: Write the failing preservation test** — seed an RMW base
  containing ALL 41 preserved fields with distinct synthetic non-real
  values (a realistic snapshot of "what a controller with a fully
  customized guest portal looks like"), configure a partial subset of the
  56 modeled fields (not all — proving unconfigured modeled fields don't
  spuriously touch the base either, matching `Managed` class semantics: cfg
  null omits from PUT, controller keeps its value), call `overlay()`,
  and assert:
  - all 41 preserved keys are present in `rs.Data` with their EXACT seeded
    values (byte-identical — use `reflect.DeepEqual` or a value-by-value
    loop, not spot-checks). **Explicitly include and separately assert**
    `x_facebook_wifi_gw_secret` (the 19th credential-like field, deliberately
    preserved not modeled — spec Key Decision 1) round-trips byte-identical
    even though it is never in state — this is the direct proof that
    preserve-by-default protects a credential-shaped preserved field without
    ever putting that secret in Terraform state;
  - the configured modeled subset's keys are present with the NEW
    (configured) values;
  - unconfigured modeled fields' wire keys are either absent or retain
    the base's prior value — whichever `overlay*`'s existing null-handling
    contract already guarantees (verify against `overlayString`'s actual
    behavior for a null `types.String` rather than assuming; this must
    match the `Managed` class row in the parent design's decision matrix:
    "cfg null: omit from PUT (controller keeps value)").

```go
func TestGuestAccessSection_PreservesUnmodeledFields(t *testing.T) {
	preserved := map[string]any{
		"portal_customized":                 true,
		"portal_customized_bg_color":        "#334455",
		"portal_customized_welcome_text":    "Example welcome copy",
		"portal_customized_languages":       []any{"en", "fr"},
		"template_engine":                   "angular",
		"voucher_customized":                false,
		"wechat_shop_id":                    "shop-example-002",
		"x_facebook_wifi_gw_secret":         "preserved-fb-wifi-gw-secret-not-in-state",
		// ... all 41, generated from the spec's preserved-field list, not
		// hand-typed one at a time — build this map programmatically from
		// the same wantModeled-complement logic Task 1's test used, paired
		// with a synthetic-value generator (string->"example-<field>",
		// bool->false, number->1, list->["a","b"]), to guarantee full
		// coverage without manual transcription drift.
	}
	base := map[string]any{}
	for k, v := range preserved {
		base[k] = v
	}
	snap := rawSettings{"guest_access": settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "guest_access"},
		Data:        base,
	}}

	// model configures only e.g. auth/portal_enabled/expire_number — a
	// partial, realistic subset.

	rs, _, diags := guestAccessSection{}.overlay(context.Background(), model, settingResourceModel{}, snap)
	// assert !diags.HasError()
	for k, want := range preserved {
		got, ok := rs.Data[k]
		if !ok {
			t.Errorf("preserved field %q dropped from overlay output", k)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("preserved field %q = %v, want unchanged %v", k, got, want)
		}
	}
	// Direct, separate assertion (not folded into the loop above) so a
	// future refactor can't accidentally delete this specific check while
	// pruning the generic preserved map:
	if got := rs.Data["x_facebook_wifi_gw_secret"]; got != "preserved-fb-wifi-gw-secret-not-in-state" {
		t.Errorf("x_facebook_wifi_gw_secret = %v, want byte-identical round-trip of the preserved (never-in-state) credential", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails or passes** — this test should
  actually PASS immediately if Task 2/3 built `overlay()` correctly (RMW via
  `snap.dataCopy` is inherited from the shared pattern, not hand-rolled per
  field) — this task is a coverage addition, not a new behavior. If it
  fails, that's a real defect in Task 2/3's overlay, not a test bug — fix
  the section code, not the test.

Run: `go test ./unifi/ -run TestGuestAccessSection_PreservesUnmodeledFields -v`

- [ ] **Step 3: Add the "not configured" test** — `model.GuestAccess` is
  null/unknown → `overlay()` returns `configured == false` and an empty
  `RawSetting{}`, matching `isConfigured()`'s contract and every sibling
  section's `TestXxxSection_NotConfigured`-shaped test.

```go
func TestGuestAccessSection_NotConfigured(t *testing.T) {
	model := settingResourceModel{GuestAccess: types.ObjectNull(guestAccessAttrTypes)}
	_, configured, diags := guestAccessSection{}.overlay(context.Background(), model, settingResourceModel{}, rawSettings{})
	if configured {
		t.Error("overlay() configured = true, want false for null GuestAccess")
	}
	if diags.HasError() {
		t.Errorf("unexpected diags: %v", diags)
	}
	if guestAccessSection{}.isConfigured(model) {
		t.Error("isConfigured() = true, want false for null GuestAccess")
	}
}
```

- [ ] **Step 4: Add the decode-roundtrip test covering all 56 fields** (Task
  2 only covered 38) — snap with all 56 modeled fields present on the wire,
  decode, assert every leaf round-trips including all 18 secrets reading
  from prior (not the wire).

- [ ] **Step 5: Run full section test suite**

Run: `go test ./unifi/ -run GuestAccess -v`
Expected: PASS, every sub-test green.

- [ ] **Step 6: Full regression**

Run: `go test ./unifi/... 2>&1 | tail -20`

- [ ] **Step 7: Commit**

```bash
git add unifi/setting_section_guest_access_test.go
git commit -m "setting: cover guest_access RMW preservation and not-configured gating"
```

---

### Task 5: Wire into `settingResourceModel` + top-level golden + schema/example/changelog

**Why:** The section is fully implemented and unit-tested in isolation
(Tasks 1-4); this task makes it reachable through the actual resource
(`settingResourceModel`), adds the centralized golden oracle entry
(`setting_golden_test.go`), and ships the user-facing artifacts (example,
changelog) the task brief requires.

**Files:**
- Modify: `unifi/setting_resource.go` — add `GuestAccess types.Object
  \`tfsdk:"guest_access"\`` to `settingResourceModel`.
- Modify: `unifi/setting_golden_test.go` — add `goldenGuestAccess` +
  `TestGolden_guest_access`.
- Modify: `examples/resources/unifi_setting/resource.tf`.
- Modify: `CHANGELOG.md`.

**Interfaces:** none new.

- [ ] **Step 1: Add the model field**

```go
type settingResourceModel struct {
	ID            types.String   `tfsdk:"id"`
	Site          types.String   `tfsdk:"site"`
	AutoSpeedtest types.Object   `tfsdk:"auto_speedtest"`
	Country       types.Object   `tfsdk:"country"`
	Dpi           types.Object   `tfsdk:"dpi"`
	GuestAccess   types.Object   `tfsdk:"guest_access"`
	Lcm           types.Object   `tfsdk:"lcm"`
	// ... unchanged
}
```

(Field order in the struct is cosmetic — alphabetical-ish grouping already
exists loosely; insert wherever reads cleanest, it has no wire effect.)

- [ ] **Step 2: Build, confirm `Schema()` picks it up automatically**

Run: `go build ./... && go test ./unifi/ -run TestSectionStructuralCoverage -v`
Expected: PASS — this is the gate-10-descended structural test
(`modelHasField` reflection) that would fail if the model field were missing
or misnamed relative to `attrName()`.

- [ ] **Step 3: Add the centralized golden** — copy a representative model
  from the section's own golden-reproduction test (Task 2/3) into
  `setting_golden_test.go`'s `goldenGuestAccess` constant +
  `TestGolden_guest_access`, matching the file's existing per-section
  pattern (`assertPUTBodyMatchesGolden`, stripping the routing `key` field).

```go
const goldenGuestAccess = `{...56-field JSON body with synthetic values...,"key":""}`

func TestGolden_guest_access(t *testing.T) {
	// mirrors TestGolden_radius / TestGolden_mgmt shape
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./unifi/... 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Add the example block** to
  `examples/resources/unifi_setting/resource.tf`, in the "combined" example
  (alongside `mgmt`/`radius`/`usg`) AND optionally its own
  `guest_access_only` resource block (matching the file's existing
  `radius_only` pattern) — using ONLY the spec's synthetic fixture values
  (`guest.example.internal`, `10.20.30.0/24`, `192.0.2.1`/`198.51.100.1`,
  `192.0.2.10` (custom_ip), `https://auth.example.internal/guest`
  (auth_url), `example-app-id-123`, `sandbox-user`, placeholder secret
  strings clearly marked as placeholders in a comment, e.g. `# replace with
  a real secret — never commit one`).

- [ ] **Step 6: Add the CHANGELOG entry** under `[Unreleased]`, matching the
  existing narrative style (lead with the capability, name the mechanism,
  flag the secret-handling caveat) — see the `unifi_ap_group`/`unifi_device`
  entries in `CHANGELOG.md` for tone/length. Example shape:

```markdown
- **`unifi_setting`: add the `guest_access` section (guest portal auth,
  expiry, RADIUS, OAuth SSO, and payment-gateway configuration).** Models
  the operational core — auth mode and its custom-auth endpoint, portal
  enable/redirect (including both HTTPS-redirect flags and the alternate
  custom-IP portal address), guest-session expiry, subnet/DNS restriction,
  RADIUS-backed auth, Facebook/Google/WeChat SSO connection settings, and
  full payment-gateway credentials for all 6 supported gateways (PayPal,
  Stripe, Authorize.Net, Quickpay, MerchantWarrior, ippay) — 56 of the
  section's 97 controller fields, 18 of them secrets. The portal
  *template/styling* surface (colors, fonts, logo, background image,
  welcome/success/ToS copy, language list — 41 fields) is deliberately not
  modeled: it's normally authored once through the controller's own
  guest-portal editor, and every unmodeled field is preserved verbatim on
  every update (read-modify-write), never at risk of being cleared. All 18
  secrets follow the existing write-only pattern (`radius.secret`): never
  read back from the controller (which only returns a mask), preserved
  across applies that don't touch them, and never re-sent as a mask.
```

- [ ] **Step 7: Final full verification**

Run:
```sh
gofmt -l unifi/            # empty
go build ./...             # ok
go vet ./unifi/             # clean
go test ./unifi/...        # all pass
git diff --check           # clean
git diff --stat unifi/setting_engine.go   # empty — confirms rev. 2's constraint held for the whole plan
```

Focused gates:
- `go test ./unifi/ -run 'Golden|SchemaEquiv|StructuralCoverage'` — new
  golden present and green; existing 13 sections' schema-equivalence golden
  untouched; structural coverage includes `guest_access`.
- `go test ./unifi/ -run 'GuestAccess'` — full section test suite green.
- `go test ./unifi/ -run TestMigrationInventoryCoversAllSections` — still
  asserts exactly 13 (confirms Global Constraints' "do not touch" held).
- `go test ./unifi/ -run TestAllSectionAttrsNull_gate` — still green after
  Task 5a's extension (see below) — confirms the hydration gate is wired for
  the new section, not just compiling.

- [ ] **Step 8: Commit**

```bash
git add unifi/setting_resource.go unifi/setting_golden_test.go examples/resources/unifi_setting/resource.tf CHANGELOG.md
git commit -m "setting: wire guest_access into the resource, add golden/example/changelog"
```

---

### Task 5a: `allSectionAttrsNull` / `allSectionsNullModel` wiring (rev. 2 — new task)

**Why:** `allSectionAttrsNull()` (`unifi/setting_resource.go:648-665`) is an
explicit, hand-written 13-clause boolean expression, NOT derived from the
`settingSections` registry. It gates `Read`'s post-import hydration
behavior (see `unifi/setting_resource.go`'s `Read` doc comment): when every
section attribute is null (the shape `ImportState` produces), `Read`
hydrates ALL registered sections as Computed; otherwise it only refreshes
the sections the user configured. **Without this task, a freshly-imported
resource that has `guest_access` as the only configured section would be
mis-classified**: `allSectionAttrsNull` would report `true` (since it never
even looks at `GuestAccess`), causing `Read` to take the "hydrate everything"
branch even when `guest_access` genuinely is configured and the model is not
actually all-null — the exact bug rev. 1 would have shipped by omitting this
task. This must land in the same commit as Task 5's model-field addition, or
between Task 5 and Task 6, so the branch never has a tip where the new
`GuestAccess` field exists on the model but is invisible to this gate.

**Files:**
- Modify: `unifi/setting_resource.go` — extend `allSectionAttrsNull()`.
- Modify: `unifi/setting_engine_lifecycle_test.go` — extend
  `allSectionsNullModel()`.
- Modify: `unifi/setting_resource_test.go` — extend
  `TestAllSectionAttrsNull_gate`.

**Interfaces:** none new — both functions being extended already exist.

- [ ] **Step 1: Write the failing gate-extension test** — extend
  `TestAllSectionAttrsNull_gate` (`unifi/setting_resource_test.go:834-848`)
  with a case that isolates `GuestAccess` specifically: start from
  `allSectionsNullModel()` (all null, asserted true, unchanged from the
  existing test body), then set ONLY `GuestAccess` to a non-null object and
  assert `allSectionAttrsNull()` now returns `false`. This must fail before
  Step 3's production edit (either a compile error, since
  `allSectionsNullModel()` doesn't yet have a `GuestAccess` field to
  reference, or a false-negative pass because the untouched
  `allSectionAttrsNull` doesn't look at `GuestAccess` at all and would
  wrongly still report `true`).

```go
// Appended to TestAllSectionAttrsNull_gate in unifi/setting_resource_test.go
func TestAllSectionAttrsNull_gate(t *testing.T) {
	if !allSectionAttrsNull(allSectionsNullModel()) {
		t.Error("allSectionAttrsNull(all-null model) = false, want true")
	}

	ctx := context.Background()
	partial := allSectionsNullModel()
	partial.Dpi = dpiObject(t, ctx, true, false)
	if allSectionAttrsNull(partial) {
		t.Error("allSectionAttrsNull(model with Dpi configured) = true, want false")
	}

	// rev. 2: guest_access-specific case — isolates the new section's
	// clause in allSectionAttrsNull rather than relying on the pre-existing
	// Dpi case above to prove the whole function still works.
	guestAccessOnly := allSectionsNullModel()
	guestAccessOnly.GuestAccess = /* a non-null, non-unknown types.Object built from guestAccessAttrTypes */
	if allSectionAttrsNull(guestAccessOnly) {
		t.Error("allSectionAttrsNull(model with only GuestAccess configured) = true, want false — guest_access clause missing from allSectionAttrsNull")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./unifi/ -run TestAllSectionAttrsNull_gate -v`

- [ ] **Step 3: Extend `allSectionsNullModel()`**
  (`unifi/setting_engine_lifecycle_test.go:32-48`) with a 14th field:

```go
func allSectionsNullModel() settingResourceModel {
	return settingResourceModel{
		AutoSpeedtest: types.ObjectNull(autoSpeedtestAttrTypes),
		Country:       types.ObjectNull(countryAttrTypes),
		Dpi:           types.ObjectNull(dpiAttrTypes),
		GuestAccess:   types.ObjectNull(guestAccessAttrTypes),
		Lcm:           types.ObjectNull(lcmAttrTypes),
		NetworkOpt:    types.ObjectNull(networkOptimizationAttrTypes),
		Ntp:           types.ObjectNull(ntpAttrTypes),
		Syslog:        types.ObjectNull(syslogAttrTypes),
		Doh:           types.ObjectNull(dohAttrTypes),
		Ips:           types.ObjectNull(ipsAttrTypes),
		Mgmt:          types.ObjectNull(mgmtAttrTypes),
		Radius:        types.ObjectNull(radiusAttrTypes),
		USG:           types.ObjectNull(usgAttrTypes),
		IgmpSnooping:  types.ObjectNull(igmpSnoopingAttrTypes),
	}
}
```

- [ ] **Step 4: Extend `allSectionAttrsNull()`**
  (`unifi/setting_resource.go:659-665`) with a 14th clause:

```go
func allSectionAttrsNull(m settingResourceModel) bool {
	return m.AutoSpeedtest.IsNull() && m.Country.IsNull() && m.Dpi.IsNull() &&
		m.GuestAccess.IsNull() &&
		m.Lcm.IsNull() && m.NetworkOpt.IsNull() && m.Ntp.IsNull() &&
		m.Syslog.IsNull() && m.Doh.IsNull() && m.Ips.IsNull() &&
		m.Mgmt.IsNull() && m.Radius.IsNull() && m.USG.IsNull() &&
		m.IgmpSnooping.IsNull()
}
```

Update the function's doc comment's "explicit 13-field check" language to
"explicit 14-field check" and note `guest_access` was added here
deliberately (rev. 2), not derived from the registry — the same caveat the
existing comment already states for a hypothetical "15th section" applies
identically to any future 15th.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./unifi/ -run 'TestAllSectionAttrsNull_gate|GuestAccess' -v`
Expected: PASS.

- [ ] **Step 6: Full regression** — confirm every OTHER test file that
  references `allSectionsNullModel()` (`unifi/setting_engine_lifecycle_test.go`
  itself, `unifi/setting_engine_capability_scope_test.go`,
  `unifi/setting_fix_c_regression_test.go`) still compiles and passes
  unmodified — they should, since they only ever read the fixture's already
  existing 13 fields and never construct it positionally (Go struct literals
  in this fixture are keyed, not positional, so adding a 14th key does not
  break any existing caller).

Run: `go test ./unifi/... 2>&1 | tail -25`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add unifi/setting_resource.go unifi/setting_engine_lifecycle_test.go unifi/setting_resource_test.go
git commit -m "setting: wire guest_access into allSectionAttrsNull hydration gate"
```

---

## Verification (whole plan)

```sh
gofmt -l unifi/
go build ./...
go vet ./unifi/
go test ./unifi/...
git diff --check
git diff --stat unifi/setting_engine.go   # must be empty
```

Focused gates that MUST stay green throughout every task:
- `go test ./unifi/ -run 'Golden|MigrationInventory|SchemaEquiv'` — the 13
  existing sections' byte-identical PUT bodies and schema; migration
  inventory still pinned at exactly 13.
- `go test ./unifi/ -run 'Lifecycle|CapabilityScope|BestEffort'` — the
  shared engine's lifecycle/fail-closed/recovery behavior, unaffected by an
  additive section.
- `go test ./unifi/ -run TestAllSectionAttrsNull_gate` — the post-import
  hydration gate correctly includes `guest_access` (Task 5a).
- `go test ./unifi/ -run GuestAccess` — the new section's own suite:
  field-split contract (Task 1), golden reproduction (Task 2/5), exhaustive
  secret matrix (Task 3), RMW preservation + not-configured + full
  decode-roundtrip (Task 4).

Expected net effect: one new `unifi/setting_section_guest_access.go` (~380-480
lines given 56 modeled fields plus the section-local secret-carry helper, in
line with `mgmt`'s ~360 lines for a smaller field count plus a nested list),
**zero changes to `unifi/setting_engine.go`**, a small, explicit 14th-clause
edit to `allSectionAttrsNull()` and its test fixture, zero changes to any of
the 13 existing sections' behavior, zero changes to the engine's
lifecycle/capability/codec mechanisms.

## Self-Review notes

- **Coverage:** field-split contract test first (T1) so schema/decode/overlay
  (T2) and secrets (T3) are built against a pinned, verified split rather
  than re-deriving it ad hoc; RMW preservation gets its own dedicated
  full-surface test (T4) beyond the smoke coverage folded into T2/T3;
  wiring + golden + example + changelog (T5), then the hydration-gate
  wiring (T5a) once the model field exists, once the section is proven
  correct in isolation.
- **Compile-safety per tip:** T2 introduces `guestAccessSection` with only
  38 fields and a plain-copy `carryBestEffort` — deliberately not yet
  registered against any secret-bearing test, so it compiles and passes
  standalone. T3 is the one task that must add fields, schema, decode,
  overlay, AND `carryBestEffort` together (the secret leaves don't
  half-exist) — atomic like PR-A's Task 2, for the same reason (a
  half-added secret leaf referenced by schema but not decode, or vice
  versa, would not compile or would silently misbehave).
- **Ordering:** T1 (contract) before T2 (schema uses the pinned field
  names) before T3 (secrets need the schema's non-secret leaves to exist
  first, so `carryBestEffort`'s "every other leaf comes from plan" has
  something to copy) before T4 (coverage over T2+T3's finished surface)
  before T5 (wiring — needs the section fully correct before it's reachable
  through the actual resource) before T5a (hydration-gate wiring — needs
  `GuestAccess` to exist as a model field, added in T5, before it can be
  referenced in `allSectionAttrsNull`/`allSectionsNullModel`). T4 could in
  principle run interleaved with T2/T3 rather than after, but keeping it
  last matches the task brief's explicit "core scalars; secrets;
  nested/list fields" three-way split read as "core; secrets;
  coverage/wiring."
- **Risk ranking:** T3 (secrets) highest — a mistake here is a real secret
  leak or clobber risk, not just a wrong test, and rev. 2's chaining
  correctness requirement (always pass ORIGINAL prior as arg2, never the
  accumulating out) is the single most likely place for a subtle bug; T1
  (field-split), T5 (golden/wiring), and T5a (hydration gate) medium — all
  are "did the paperwork/wiring match the code" categories, not behavior
  risk, though T5a's omission in rev. 1 shows this category is not
  risk-free; T2/T4 lower — mechanical repetition of an already-proven
  pattern (dpi/radius/mgmt) and pure test coverage, respectively.
- **Deliberately deferred / out of scope:** the 41 preserved fields (Key
  Decision 1's cut — promotable later as a small additive PR per-field, four
  of rev. 1's originally-preserved fields already promoted in rev. 2);
  `WriteOnlyAttribute`/ephemeral-resource native write-only support (frozen
  by the PR-A simplification plan, not reopened here); cross-field
  discriminator validation e.g. gateway-requires-payment_enabled or
  auth=custom-requires-auth_url (C4 framework deleted as YAGNI in PR-A Task
  4; not reintroduced for one section); any go-unifi SDK change (none
  needed — confirmed in the parent design's PR-0 audit, "no SDK dependency
  after PR-0"); any change to `unifi/setting_engine.go` (rev. 2's explicit
  reversal of rev. 1's Key Decision 2 recommendation, to minimize
  shared-PR-A-file churn per the integration constraint).
