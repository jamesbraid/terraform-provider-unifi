# PR-A Settings Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Simplify the `unifi_setting` engine on the `settings-expansion-v2` branch from a heavy typed-ownership machine toward the industry-norm "thin, preserve-by-default" shape — **without any breaking schema change** — keeping the lifecycle core that the settings review legitimately requires.

**Architecture:** The write path already does raw-map preserve-by-default (`base := snap.dataCopy(key)`, then overlay only configured fields). This plan removes the *overbuild* layered on top: the ownership-class dispatch threaded through the codec, the unused 4-of-6 ownership classes, the unused discriminator framework, the speculative 5-state capability model, over-strict inbound decoding, and the C1/C2.4/C4/codex process comments. The two write-only secrets (`mgmt.ssh_password`, `radius.secret`) and their state remain exactly as shipped in `v0.99.0` — no native write-only migration, no deprecation window.

**Tech Stack:** Go, terraform-plugin-framework v1.19.0, go-unifi settings SDK (`RawSetting{ Data map[string]any }`).

## Provenance

Co-authored with an independent codex review (conversation `settings-architecture-review`, turns 1-4) and grounded in two research passes (`.superpowers/sdd/research-unifi-ecosystem.md`, `.superpowers/sdd/research-blob-api-patterns.md`); architecture verdict in `.superpowers/sdd/architecture-findings.md`. Key external facts: the UniFi controller settings API is undocumented; go-unifi's codegen source froze at controller 9.5.21; the industry norm (azapi/restful/restapi, HashiCorp's own thin-provider principle) is preserve-by-default, not exhaustive typing.

## Global Constraints

- **No breaking schema change.** The public schema stays byte-identical to `v0.99.0` and to the current `settings-expansion-v2` schema. The schema-equivalence golden (`unifi/testdata/setting_schema_legacy.json`, `unifi/setting_schema_equiv_test.go`) MUST stay green after every task. This is a refactor of internals, not of the user-facing contract.
- **Secrets are frozen.** `mgmt.ssh_password` and `radius.secret` remain nested, stateful, `Sensitive` string attributes with their existing decode-preserves-prior / overlay-delete-on-mask behavior. No `*_wo` attributes. No state upgrader change.
- **Every task leaves the branch building and green.** `go build ./...`, `go vet ./unifi/`, `gofmt -l unifi/` clean, `go test ./unifi/...` all pass at every task tip. A task must not depend on a later task to restore correctness OR to compile (the review's independent-merge invariant, applied per task).
- **Golden PUT bodies are the hard regression gate.** The per-section golden tests (`unifi/setting_golden_test.go`, `unifi/setting_migration_inventory_test.go`, and each `unifi/setting_section_*_test.go`) assert byte-level PUT payloads. They MUST stay green **unmodified** — this refactor changes no wire behavior. An unexplained golden diff is a defect, not an expected update.
- **Preserve exact diagnostic wording that tests assert.** Notably `Settings section not supported` (asserted at `unifi/setting_engine_capability_scope_test.go:101-109`). Do not reword a diagnostic without intentionally updating its test in the same task.
- **Privacy:** no real ObjectIDs, no `America/Vancouver`, no `192.168.2.1/24`, no real color hex, no personal mDNS/hostname fixtures in any added test data.
- **Only two ownership classes are in use** across all 13 sections: `ownerManaged` (137 uses) and `ownerWriteOnlySecret` (7 uses, only in `mgmt`/`radius`, both top-level scalars). `ownerCoManaged`, `ownerComputed`, `ownerGeneratedSecret`, `ownerPreservedUnmanaged` are applied to **zero** fields (verified 2026-07-12).

## Current-State Reference (read before starting)

- `unifi/setting_codec.go` (836 lines) — the code being slimmed. Low-level `codec*`/`put*` typed accessors (42-234); ownership-aware `decode*`/`overlay*` wrappers that branch on `ownershipClass` (247-382); generalized nested codec with `own`/`ownPrefix`/`ownershipFor`/`leafPath` (411-836).
- `unifi/setting_engine.go` (252 lines) — the lifecycle core to KEEP: `listSnapshot`, `readSections`, `applySections`, `bestEffortState`, `joinDiagMessages`; `bestEffortObject` (195-252) is replaced in Task 2.
- `unifi/setting_snapshot.go` — `rawSettings` snapshot; `has(key)` already exists (32-35); `dataCopy(key)`, `section(key)`.
- `unifi/setting_section.go` (81 lines) — the `settingSection` interface (9 methods incl. `ownership()`, `capability()`, `carryBestEffort()`).
- `unifi/setting_ownership.go` (81 lines) — the 6-class `ownershipClass` enum + `writesToPUT()`/`readsFromAPI()`/`isSecret()` predicates.
- `unifi/setting_capability.go` (99 lines) — the 5-state `capabilityState` model.
- `unifi/setting_discriminator.go` (240 lines) — unused validator/plan-modifier framework (zero production callers).
- `unifi/setting_section_dpi.go` — the simplest section (managed scalars only): the template for the non-secret transformation.
- `unifi/setting_section_mgmt.go` — the mgmt section: secret `ssh_password`→wire `x_ssh_password`, plus separate `x_ssh_*` remaps and the nested `ssh_keys`→`x_ssh_keys` list with controller date/fingerprint blanking (307-332).
- `unifi/setting_section_radius.go` — secret `secret`→wire `x_secret`.
- `unifi/setting_section_test.go` — the gate-10 structural coverage tests (`leafPaths`, `ownershipCoverageMismatches`) that assert every schema leaf has an `ownership()` entry.

## File Structure (what changes)

- `unifi/setting_codec.go` — MODIFY: make inbound `codec*` readers tolerant of remote type drift (Task 1); drop the ownership-class parameter from the `decode*`/`overlay*` layer and drop `own`/`ownPrefix`/`ownershipFor`/`leafPath` from the nested codec (Task 2). Net: ~836 → ~450 lines.
- `unifi/setting_engine.go` — MODIFY: propagate post-apply read warnings (Task 1); replace `bestEffortObject` with `carrySecretObject` (Task 2); gate on `snap.has(key)` instead of `capability()` (Task 3).
- `unifi/setting_ownership.go`, `unifi/setting_ownership_test.go` — DELETE (Task 2).
- `unifi/setting_discriminator.go`, `unifi/setting_discriminator_test.go` — DELETE (Task 4).
- `unifi/setting_capability.go` — MODIFY/gut: collapse `capabilityState` to the presence check; keep the exact fail-closed error (Task 3).
- `unifi/setting_section.go` — MODIFY: drop `ownership()` (Task 2) and `capability()` (Task 3) from the interface (9 → 7 methods).
- `unifi/setting_section_*.go` (all 13) — MODIFY: drop `ownership()` and `capability()`; non-secret sections call the class-free codec; `mgmt`/`radius` handle their one secret leaf inline (Task 2/3).
- `unifi/setting_section_test.go` — MODIFY: replace the ownership-coverage assertion with a structural registry/schema/model coverage assertion (Task 2).
- All production files — MODIFY: strip process comments; retain domain invariants (Task 5).

---

### Task 1: Make inbound decode tolerant of remote type drift

**Why:** For an undocumented, version-drifting API, a remote value whose *type* has drifted must NOT fail a whole `terraform refresh`. Today the low-level readers `AddError` on a type mismatch (`setting_codec.go:49-55,68-74,88-102,146-163`), hard-failing the read. Correct behavior: a **present, wrong-typed** value is a warning and the field retains its prior typed value (null on first import); unrelated sections still hydrate. Outbound config validation stays strict; silent numeric truncation stays forbidden (warn, don't truncate). **Absence/JSON-null still decodes to Terraform null** — a controller clearing a managed field must clear state; only *type drift* retains prior.

**Files:**
- Modify: `unifi/setting_codec.go:42-169` (low-level `codec*` readers), `:242-287` (the `decode*` wrappers — must pass `prior` through to compile), `:457-470` (`decodeObject` non-map), `:722-769` (`decodeObjectList` malformed element/list).
- Modify: `unifi/setting_engine.go:137-152` (`applySections` post-apply warning propagation).
- Modify: `unifi/setting_engine_lifecycle_test.go:541-571` (test pinning the old over-strict behavior).
- Test: `unifi/setting_codec_test.go`, `unifi/setting_engine_lifecycle_test.go`.

**Interfaces:**
- Produces: `codecString(data, key, prior types.String) (types.String, diag.Diagnostics)` and analogues — each low-level reader now takes the `prior` typed value; **absent/null → null; present well-typed → remote value; present wrong-typed → warning + prior**. (The `decode*` wrappers' prior-fallback is folded down here; the wrappers themselves are removed in Task 2.)

- [ ] **Step 1: Write the failing tests** — three cases: type drift warns+retains prior; type drift with null prior yields null; absence yields null (NOT prior).

```go
// in unifi/setting_codec_test.go
func TestCodecBool_typeDriftWarnsAndRetainsPrior(t *testing.T) {
	data := map[string]any{"enabled": "true"} // controller returned a STRING for a bool field
	prior := types.BoolValue(true)
	got, diags := codecBool(data, "enabled", prior)
	if diags.HasError() {
		t.Fatalf("type drift must not be an error, got: %v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("type drift must produce a warning")
	}
	if !got.Equal(prior) {
		t.Fatalf("type drift must retain prior %v, got %v", prior, got)
	}
}

func TestCodecBool_typeDriftNullPriorYieldsNull(t *testing.T) {
	data := map[string]any{"enabled": "true"}
	got, diags := codecBool(data, "enabled", types.BoolNull()) // import: no prior
	if diags.HasError() || !got.IsNull() {
		t.Fatalf("type drift with null prior must yield null, no error; got %v %v", got, diags)
	}
}

func TestCodecBool_absenceYieldsNullNotPrior(t *testing.T) {
	got, diags := codecBool(map[string]any{}, "enabled", types.BoolValue(true)) // key absent
	if diags.HasError() || !got.IsNull() {
		t.Fatalf("absence must clear to null (not retain prior); got %v %v", got, diags)
	}
}
```

Add a `hasWarning(diag.Diagnostics) bool` test helper if none exists.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./unifi/ -run 'TestCodecBool_' -v`
Expected: FAIL — `codecBool` has no `prior` parameter and `AddError`s on mismatch.

- [ ] **Step 3: Make the low-level readers tolerant** — absent/null → null; wrong-typed → warning + prior. Example:

```go
// codecBool reads a bool field. Absent or JSON null -> Terraform null (a
// controller clearing a managed field must clear state). A present value of a
// non-bool type is remote type drift: a WARNING (not error), retaining prior
// so a single drifted field never fails refresh.
func codecBool(data map[string]any, key string, prior types.Bool) (types.Bool, diag.Diagnostics) {
	var diags diag.Diagnostics
	raw, ok := data[key]
	if !ok || raw == nil {
		return types.BoolNull(), diags
	}
	b, ok := raw.(bool)
	if !ok {
		diags.AddWarning(
			"Settings value type drift",
			fmt.Sprintf("field %q: expected bool, got %T; retaining last-known value", key, raw),
		)
		return prior, diags
	}
	return types.BoolValue(b), diags
}
```

Apply the same shape to `codecString`, `codecInt64`, `codecGoDuration`, `codecStringList`. **Fractional numbers warn-and-retain** (do not silently truncate `1.9`). For `codecStringList`, a non-array value or non-string element warns and retains prior.

**Same step — update the `decode*` wrappers to compile.** The still-existing ownership-aware wrappers (`setting_codec.go:242-287`) call `codec*(data, key)` and must now pass `prior`, keeping their secret branch until Task 2 removes it:

```go
func decodeBool(data map[string]any, key string, class ownershipClass, prior types.Bool) (types.Bool, diag.Diagnostics) {
	if !class.readsFromAPI() {
		return prior, nil
	}
	return codecBool(data, key, prior)
}
```

Do this for `decodeString`, `decodeInt64`, `decodeGoDuration`, `decodeStringList` too. Update the nested codec's calls (`decodeObjectFields`/`decodeObjectList`) to pass their already-derived `priorChild`/prior element into the tolerant readers.

- [ ] **Step 4: Tolerate malformed containers** — in `decodeObject` (`:457-470`), a present non-map value warns and returns the **prior object** (not `ObjectUnknown`). In `decodeObjectList` (`:722-769`), a present non-array, or a non-object element, warns and returns the **prior list**; do not build a partial list. Keep "unsupported nested attribute type" (`:561-565,682-685`) as a hard **error** — that is a provider/schema defect, not remote drift.

- [ ] **Step 5: Propagate post-apply read warnings** — `applySections` currently only inspects `rd` when `rd.HasError()` (`setting_engine.go:137-152`), silently dropping type-drift warnings from the post-apply re-read. Add the else branch:

```go
rd := readSections(ctx, sections, client, site, plan, &out, false)
if rd.HasError() {
	// existing C2.4 best-effort path (unchanged)
	...
} else {
	d.Append(rd...) // surface type-drift warnings from the post-apply read
}
```

- [ ] **Step 6: Update the lifecycle test** — rewrite `setting_engine_lifecycle_test.go:541-571` to assert the new contract: a malformed remote field on refresh produces a **warning**, the section keeps its prior value, the operation **succeeds**, and a *valid configured value still overwrites* a malformed prior remote on apply.

- [ ] **Step 7: Run tests**

Run: `go test ./unifi/... 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add unifi/setting_codec.go unifi/setting_codec_test.go unifi/setting_engine.go unifi/setting_engine_lifecycle_test.go
git commit -m "setting: tolerate remote type drift on read (warn, retain prior)"
```

---

### Task 2: Collapse ownership (atomic) — class-free codec, inline secrets, best-effort, structural test

**Why:** Only `ownerManaged` and `ownerWriteOnlySecret` are ever applied, and no nested leaf is a secret. Collapse the ownership machinery in ONE compile-safe change: the generic + nested codec become class-free (managed default), the two secret sections handle their single secret leaf inline, `bestEffortObject` is replaced by a secret-only `carrySecretObject`, `carryBestEffort` drops its `prior` param, `ownership()` leaves the interface, `setting_ownership.go` is deleted, and the gate-10 coverage test is replaced. This is deliberately one indivisible task: deleting the ownership enum while `bestEffortObject`/`carryBestEffort` still reference it would not compile.

**Files:**
- Modify: `unifi/setting_codec.go:236-836` (drop `class` from `decode*`/`overlay*`; drop `own`/`ownPrefix`/`ownershipFor`/`leafPath` from the nested codec).
- Modify: `unifi/setting_engine.go:183-252` (replace `bestEffortObject` with `carrySecretObject`; `bestEffortState` calls the simplified `carryBestEffort`).
- Modify: `unifi/setting_section.go` (drop `ownership()`; change `carryBestEffort` signature).
- Modify: all 13 `unifi/setting_section_*.go` (drop `ownership()`; class-free codec; simplified `carryBestEffort`; `mgmt`/`radius` inline secret decode/overlay + secret carry).
- Modify: `unifi/setting_section_test.go` (replace ownership-coverage test).
- Delete: `unifi/setting_ownership.go`, `unifi/setting_ownership_test.go`.
- Modify (test files that reference ownership symbols — must change in THIS atomic task or the tip won't compile): `unifi/setting_codec_test.go` (many synthetic ownership/nested-secret tests — delete or rewrite as class-free codec tests), `unifi/setting_engine_test.go` (ownership-bearing stub sections + direct `bestEffortObject` tests — rewrite/remove), `unifi/setting_section_mgmt_test.go`, `unifi/setting_section_radius_test.go`, `unifi/setting_section_test.go`, `unifi/setting_engine_lifecycle_test.go`. Obsolete *generic* ownership tests are removed; mgmt/radius secret behavior stays covered by the section + lifecycle tests.
- Test (must stay green, unmodified): `unifi/setting_golden_test.go`, `unifi/setting_migration_inventory_test.go`.

**Interfaces:**
- Consumes: Task 1's tolerant `codec*(data, key, prior)` readers.
- Produces: class-free `decodeString(data, key, prior)`, `overlayString(out, key, v)`, nested `decodeObject(ctx, data, key, prior, attrTypes)` / `overlayObject(ctx, out, key, cfg)` (no `own`/`ownPrefix`); `carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics`; `carrySecretObject(plan, prior types.Object, secretLeaf string) (types.Object, diag.Diagnostics)`.

- [ ] **Step 1: Write the failing structural-coverage test first** — the gate-10 `leafPaths`/`ownershipCoverageMismatches` test in `setting_section_test.go` asserts every schema leaf has an `ownership()` entry; that premise is being removed. Replace it with a structural assertion (unique key, schema attribute present, matching model `tfsdk` field). Write it now so it fails to compile against the not-yet-removed code path, driving the change:

```go
// TestSectionStructuralCoverage asserts each registered section is wired: unique
// key, a schema attribute, and a matching model field. It does NOT prove
// decode/overlay behavior — the per-section golden and lifecycle tests do that.
func TestSectionStructuralCoverage(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range settingSections {
		if seen[s.key()] {
			t.Errorf("duplicate section key %q", s.key())
		}
		seen[s.key()] = true
		if s.schemaAttribute() == nil {
			t.Errorf("section %q has no schema attribute", s.key())
		}
		if !modelHasField(s.attrName()) { // reflect over settingResourceModel tfsdk tags
			t.Errorf("section %q attrName %q has no settingResourceModel field", s.key(), s.attrName())
		}
	}
}
```

Add the `modelHasField(name string) bool` helper (reflect over `settingResourceModel`'s `tfsdk:` tags). Delete the old `leafPaths`/`ownershipCoverageMismatches`/`TestSectionOwnershipCoversSchema` in the same edit.

- [ ] **Step 2: Make the generic codec class-free** — collapse `decodeString/Bool/Int64/GoDuration/StringList` to thin `prior`-passing wrappers over the Task-1 `codec*` readers (drop the `class` arg and `!class.readsFromAPI()` branch). Collapse `overlayString/Bool/Int64/GoDuration/StringList` to the managed path (drop the `ownerWriteOnlySecret` delete branch and the `writesToPUT()` guard):

```go
func overlayBool(out map[string]any, key string, v types.Bool) { putBool(out, key, v) }
```

- [ ] **Step 3: Make the nested codec class-free** — delete `ownershipFor` and `leafPath`; remove `own`/`ownPrefix` params from `decodeObject`/`decodeObjectFields`/`decodeObjectList`/`overlayObject`/`overlayObjectFields`/`overlayObjectList`; the type-dispatch switch calls the class-free `decode*`/`overlay*` directly. **Keep** the fresh-element-build rule in `overlayObjectList` (its correctness invariant) and its `priorChild` derivation for tolerant decode.

- [ ] **Step 4: Inline the two secrets** — replace the ownership-class secret handling with explicit inline code. `mgmt.decode`: `sshPassword := priorModel.SSHPassword` (never read from `data`; the controller returns a mask). `mgmt.overlay`, independent of the `x_ssh_*` remaps and the `x_ssh_keys` overlay (write it before or after `overlayObjectList`):

```go
if !m.SSHPassword.IsNull() && !m.SSHPassword.IsUnknown() {
	base["x_ssh_password"] = m.SSHPassword.ValueString()
} else {
	delete(base, "x_ssh_password") // never replay a read-back mask
}
```

`radius` is the identical pattern for `secret`→`x_secret`.

- [ ] **Step 5: Replace `bestEffortObject` with `carrySecretObject`** — an explicit value-returning helper (no pointer mutation, no argument-eval-order reliance):

```go
// carrySecretObject rebuilds plan's section object but keeps prior's secret
// leaf when plan's is null/unknown (write-only secrets are never in the
// controller read-back). Traps preserved: unknown treated as null; a known
// empty-string secret is kept from plan; a null/unknown plan object returns
// prior unchanged; every non-secret leaf comes from plan.
func carrySecretObject(plan, prior types.Object, secretLeaf string) (types.Object, diag.Diagnostics) { ... }
```

- [ ] **Step 6: Simplify `carryBestEffort`** — drop `prior` from the interface signature. Non-secret sections: `dst.Dpi = plan.Dpi`. Secret sections read prior from `dst` (which `bestEffortState` seeds as `prior`):

```go
func (mgmtSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	out, diags := carrySecretObject(plan.Mgmt, dst.Mgmt, "ssh_password")
	dst.Mgmt = out
	return diags
}
```

Delete `bestEffortObject`.

- [ ] **Step 7: Drop `ownership()` from the interface and all 13 sections; delete `setting_ownership.go` + test.**

```bash
git rm unifi/setting_ownership.go unifi/setting_ownership_test.go
```

- [ ] **Step 8: Run the full suite — goldens MUST be byte-identical**

Run: `go build ./... && go vet ./unifi/ && go test ./unifi/... 2>&1 | tail -25`
Expected: PASS with ZERO golden diffs (this refactor changes no wire behavior — configured-secret goldens at `setting_golden_test.go:327` (mgmt) and `:394` (radius) stay identical; the null/masked-secret deletion tests at `setting_engine_lifecycle_test.go:721-748` stay green). If any golden changed, STOP and find the behavior drift.

- [ ] **Step 9: Commit**

```bash
git add -A unifi/
git commit -m "setting: collapse ownership to managed default; inline the two secrets"
```

---

### Task 3: Collapse capability to snapshot presence

**Why:** `capabilityState` is a 5-state enum whose implementation admits it can only tell present from absent (`setting_capability.go:42-57`). Replace it with the already-existing `snap.has(key)` presence check, preserving fail-closed semantics: a *configured* section missing from the snapshot is a hard error (exact wording `Settings section not supported`); an *unconfigured/import-sweep* section missing is silently skipped.

**Files:**
- Modify: `unifi/setting_engine.go:57-74,115-118` (the `capability()` call sites).
- Modify: `unifi/setting_section.go` (drop `capability()` from the interface).
- Modify: all 13 `unifi/setting_section_*.go` (drop `capability()`).
- Delete/gut: `unifi/setting_capability.go` (remove `capabilityState`; keep the fail-closed error text if still referenced, else inline it).
- Modify (test refs to `capabilityState`/`capability()` — must change in THIS task to compile): `unifi/setting_capability_test.go` (remove/rewrite), and the fake `capability()` methods on stub sections in `unifi/setting_engine_test.go` / `unifi/setting_section_test.go`.
- Test: `unifi/setting_engine_capability_scope_test.go`.

**Interfaces:**
- Consumes: `rawSettings.has(key) bool` (already exists at `setting_snapshot.go:32-35` — do NOT re-add it).
- Produces: engine-internal presence gating; `settingSection` loses `capability()`.

- [ ] **Step 1: Confirm the green baseline**

Run: `go test ./unifi/ -run 'CapabilityScope|Capability' -v 2>&1 | tail -20`
Expected: PASS (asserts fail-closed-when-configured with wording `Settings section not supported`, skip-when-swept).

- [ ] **Step 2: Replace the capability gate in `readSections`** — preserve the exact diagnostic summary:

```go
for _, s := range sections {
	if !snap.has(s.key()) {
		if onlyConfigured {
			diags.AddError(
				"Settings section not supported",
				fmt.Sprintf("section %q is not present on this controller", s.key()),
			)
		}
		continue
	}
	diags.Append(s.decode(ctx, snap, prior, model)...)
}
```

(Match the current detail text if the test asserts it too — check `setting_engine_capability_scope_test.go:101-109` and keep whatever it pins.)

- [ ] **Step 3: Replace the capability gate in `applySections`** — same presence check before overlay; a configured-but-absent section records the fail-closed error and the aggregate `HasError()` aborts before any PUT (reconcile-before-mutate preserved).

- [ ] **Step 4: Drop `capability()` from the interface and all 13 sections; delete `capabilityState`.**

- [ ] **Step 5: Run tests**

Run: `go test ./unifi/... 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add -A unifi/
git commit -m "setting: gate sections on snapshot presence, drop capability enum"
```

---

### Task 4: Delete the unused discriminator framework

**Why:** `setting_discriminator.go` (240 lines) has zero production callers — built for future NAT/firewall PRs (`setting_discriminator.go:19-22`). YAGNI: reintroduce a focused validator/plan-modifier with the first NAT/firewall consumer.

**Files:**
- Delete: `unifi/setting_discriminator.go`, `unifi/setting_discriminator_test.go`.

- [ ] **Step 1: Confirm no production references**

Run: `grep -rn 'discriminator\|requireChildrenFor\|clearInactiveChildren' unifi/ --include='*.go' | grep -v _test.go | grep -v setting_discriminator.go`
Expected: no output.

- [ ] **Step 2: Delete**

```bash
git rm unifi/setting_discriminator.go unifi/setting_discriminator_test.go
```

- [ ] **Step 3: Build, vet, test**

Run: `go build ./... && go vet ./unifi/ && go test ./unifi/... 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git commit -am "setting: drop unused discriminator scaffolding (defer to first NAT/firewall consumer)"
```

---

### Task 5: Strip process comments; keep domain invariants

**Why:** Production comments reference internal process taxonomy meaningless to a repo reader: `C1`/`C2.4`/`C4`/`gate-N`/`Task 16b`/`codex whole-branch review finding 3`. Rewrite each in domain terms. RETAIN the genuine domain invariants (fresh snapshot before PUT; raw base preserves unmodeled controller fields; list position cannot carry controller metadata safely; masked secret wire values must not be replayed).

**Files:**
- Modify: `unifi/setting_engine.go`, `unifi/setting_codec.go`, `unifi/setting_section*.go`, and any other production file with the flagged phrases.

- [ ] **Step 1: Find the process residue**

Run: `grep -rnE 'C1\b|C2\.[0-9]|\bC4\b|gate[ -][0-9]|Task 16b|Task 19b|codex|whole-branch' unifi/ --include='*.go' | grep -v _test.go`
Expected: a finite list of comment lines.

- [ ] **Step 2: Rewrite each** — replace the process reference with the domain reason. Example (`overlayObjectList` doc): drop "codex whole-branch review finding 3: mgmt.ssh_keys' controller-assigned date/fingerprint were mis-attached" and keep "List position is not a stable element identity; a fresh element is built per config entry so controller-owned per-element metadata is never re-attached to the wrong element."

- [ ] **Step 3: Verify none remain, build**

Run: `grep -rnE 'C1\b|C2\.[0-9]|\bC4\b|gate[ -][0-9]|Task 16b|Task 19b|codex|whole-branch' unifi/ --include='*.go' | grep -v _test.go; go build ./...`
Expected: no grep output; build OK.

- [ ] **Step 4: Commit**

```bash
git commit -am "setting: replace process-taxonomy comments with domain invariants"
```

---

## Verification (whole plan)

After all tasks:

```sh
gofmt -l unifi/            # empty
go build ./...             # ok
go vet ./unifi/            # clean
go test ./unifi/...        # all pass
git diff --check           # clean
```

Focused gates that MUST stay green throughout:
- `go test ./unifi/ -run 'Golden|MigrationInventory|SchemaEquiv'` — byte-identical PUT bodies + schema (no breaking change).
- `go test ./unifi/ -run 'Lifecycle|CapabilityScope|BestEffort'` — lifecycle, fail-closed, and C2.4 recovery preserved.

Expected net effect: ~1,700 lines of framework → roughly half; four dead ownership classes, the 5-state capability enum, and the 240-line discriminator gone; inbound decode tolerant of drift; the two secrets and the public schema unchanged.

## Self-Review notes

- **Coverage:** tolerant read + warning propagation (T1); ownership collapse + inline secrets + best-effort + structural-test replacement, all atomic (T2); capability collapse (T3); discriminator delete (T4); comment strip (T5). Kept core (snapshot, config-scoped overlay, reconcile-before-mutate, import, abort-on-GET-failure, the `carryBestEffort` section-local boundary) is preserved, not touched.
- **Compile-safety per tip:** T2 is deliberately atomic — the ownership enum, `bestEffortObject`, `carryBestEffort`'s signature, `ownership()`, and the coverage test all change together so no intermediate tip references a deleted symbol.
- **Ordering:** T1 (behavioral contract change) before T2 (mechanical class-strip against the settled contract); T3/T4/T5 independent. No task depends on a later one for correctness OR compilation.
- **Type consistency:** `codec*` gain `prior` (T1); `decode*`/`overlay*` lose `class` and the nested codec loses `own`/`ownPrefix` (T2); `carryBestEffort` drops `prior` and `carrySecretObject` replaces `bestEffortObject` (T2) — each within a single task.
- **Risk ranking:** T1 (behavior change — guard with the drift/absence tests) and T2 (touches all 13 sections + codec + engine — goldens are the hard byte-identical gate) highest; T3 medium (fail-closed wording + reconcile-before-mutate must survive); T4/T5 low.
