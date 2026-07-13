# PR-D: `unifi_nat` Resource — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Status:** draft — revised per independent codex DESIGN review of the spec.
**This plan is not authorized for execution yet.** Task 0 below now records
a real, unresolved implementation blocker (a required PR-V amendment) that
did not exist as a checked precondition in the prior revision. Do not start
Task 1 until Task 0's precondition is actually satisfied in the PR-V code
(not just decided in the abstract).

**Goal:** Add a new v2 CRUD+list resource, `unifi_nat`, managing single NAT
rules (DNAT/SNAT/MASQUERADE) on the gateway, built entirely on PR-V's shared
lifecycle helpers (`unifi/v2_resource.go`) — no hand-rolled
Configure/timeout/NotFound/identity/import boilerplate. Implements the
discriminator contract (C4) for both the rule-type (`type`) and
filter-type (`filter_type`) axes, surfaces predefined/non-editable rules
with a non-mutating capability precheck (§5.6/§6.1), reconciles state from a
fresh read after every mutation (§5.2), and implements `list.ListResource`
per PR-V's recorded eligibility rule.

**Spec of record:**
`docs/superpowers/specs/2026-07-13-pr-d-nat-resource-design.md`. That spec's
schema-shape decisions (resource name, discriminator blocks, field
placement, `rule_index` cardinality) are now **settled** — see its
"Decisions resolved in this revision" section. The **one remaining
precondition** is not a design decision but a code dependency: PR-V does
not yet have a composite-identity import helper, and `unifi_nat` cannot
implement C3's composite-`id` contract without one (spec §8). Task 0 below
is a precondition check for that, not a decision gate over open schema
questions.

**Tech stack:** Go, terraform-plugin-framework v1.19.0,
`github.com/ubiquiti-community/go-unifi` (frozen at v1.33.43-...,
effectively 9.5.21 wire semantics — see spec §6a/§6b for fields flagged
needs-live-confirmation). Flat `unifi/` package. Consumes
`unifi/v2_resource.go` (PR-V, already merged in this worktree) **plus the
composite-identity import amendment this plan requires as a precondition**
(spec §8).

## Global Constraints

- **Build on PR-V; do not reinvent lifecycle.** Every CRUD method uses
  `v2Configure`, `resolveV2Site`, `v2Timeout`/`v2TimeoutOp`,
  `v2IsNotFound`, `v2FinishRead`, the composite-identity import helper (once
  it exists — spec §8), `v2SetIdentityAndState[T]`, and `objectAsOptions[T]`
  for nested-object decode, from `unifi/v2_resource.go`. A code review
  finding a hand-inlined timeout-extraction block, a hand-inlined
  `site := ...; if site == ""` block, a hand-inlined identity-then-state
  write, or NAT-local composite-id parsing logic duplicating what the PR-V
  amendment provides, is a defect, not a style nit — PR-V exists
  specifically so PR-D doesn't repeat that boilerplate (parent spec
  §379-388). **`objectListAsOptions` is not used anywhere in this
  resource** — `firewall_group_ids` is a flat `types.Set` of strings,
  decoded directly via `Set.ElementsAs`, not a list of nested objects (spec
  §9); do not introduce `objectListAsOptions` here unless a real
  `ListNestedAttribute` (multi-field elements) is added to the schema,
  which it is not.
- **TDD throughout.** Every task follows failing-test → run → implement →
  run → commit. Unit tests (conversion functions, discriminator
  validators/plan-modifiers, precheck logic) do not require `TF_ACC` or a
  live controller. Acceptance tests are gated behind `TF_ACC=1` and run
  only against the ephemeral docker demo controller
  (`unifi/provider_test.go`'s `TestMain`), per the parent spec's "Live
  validation is a separate, manual step" policy.
- **godoc + golangci-lint clean.** Every exported symbol (`NewNatResource`,
  `NewNatListResource`) has a doc comment. Every non-obvious wire mapping
  (discriminator normalization, precheck rationale, port string-not-int64
  choice, the post-mutation reconcile-read) carries a comment explaining
  *why*, not a narration of process. `golangci-lint run ./unifi/...` (or the
  repo's configured lint target) must be clean before the final commit of
  each task.
- **This is a new resource: new schema is expected**, not a
  settings-goldens/byte-identical constraint. There is no legacy
  `unifi_nat` to preserve compatibility with. C1's ownership taxonomy does
  not directly apply (that's `unifi_setting`'s codec) — model each field's
  Optional/Computed/Required shape from the wire contract in the spec
  directly.
- **Real DNAT/SNAT acceptance coverage is mandatory**, not
  MASQUERADE-only (parent spec §390, spec §11). A PR that only exercises
  MASQUERADE in `TF_ACC` tests does not meet the acceptance contract for
  this PR, regardless of unit-test coverage.
- **Post-mutation reconciliation is mandatory** (spec §12, new in this
  revision). Create and Update must each call `GetNat` on the
  mutation-returned id and build state from that read, not from the
  `CreateNat`/`UpdateNat` response body directly. This is a specified
  behavior with its own test (Task 8), not an implicit assumption folded
  into the identity+state write tail.
- **Privacy-safe fixtures.** No real ObjectIDs/IPs/hostnames anywhere in
  this PR's tests, examples, or commit messages. Use RFC 5737/3849
  documentation ranges for any IP-shaped example value. No `/Users/...`
  in commit messages.
- **Verification per task:** `gofmt -w unifi/`, `go build ./...`,
  `go vet ./unifi/...`, `go test ./unifi/...` (non-acceptance), and where
  a task's step says so, `TF_ACC=1 go test ./unifi/... -run <TestAccNat...>`
  against the demo controller. `git diff --check` before each commit.
- **Site identity:** composite `<site>:<id>` persisted `id`, `site`
  derived from it, `RequiresReplace` on `site` — per spec §8. This is a
  divergence from `firewall_policy`/`port_forward`'s bare-id+sibling-site
  shape; do not copy their `ImportState`/site-handling verbatim — use the
  PR-V composite-identity import helper (spec §8) and derive `site` from
  the persisted `id` via `parseSiteID` (`unifi/site.go`) on every
  Read/Update/Delete, not from a separately-stored `site` attribute value.
  **This helper does not exist yet** — see Task 0.
- **No `masquerade` block.** `type = "MASQUERADE"` has no corresponding
  nested attribute; its fields are the shared top-level ones
  (`out_interface`, `protocol`, `ip_version`, `pppoe_use_base_interface`,
  `setting_preference`). Do not add an empty marker block "for symmetry"
  with `dnat`/`snat` — spec §4.1 explicitly rejects this.

## File Structure

New: `unifi/nat_resource.go` (schema, model, CRUD, ImportState, discriminator
validators/plan-modifiers, conversion functions, precheck), `unifi/nat_resource_test.go`
(unit tests: schema, model AttributeTypes, conversion round-trips,
discriminator validator/plan-modifier, precheck, Metadata/IdentitySchema/
Configure, post-mutation reconcile), `unifi/nat_resource_acc_test.go` (TF_ACC
acceptance tests — split into its own file following the size of
`firewall_policy_resource_test.go`; merge into `nat_resource_test.go` if the
repo's convention is one test file per resource — confirm against
`port_forward_resource_test.go`, which keeps unit+acceptance together,
before creating a second file. Default: **one file**,
`unifi/nat_resource_test.go`, matching `port_forward_resource_test.go`'s
convention, unless it grows unwieldy).

Modified: `unifi/v2_resource.go` (the composite-identity import
amendment — spec §8; not owned by this PR's task list, but Task 0 must
confirm it has landed before Task 1 starts), `unifi/provider.go` (register
`NewNatResource` in `Resources()`, `NewNatListResource` in
`ListResources()`), `CHANGELOG.md` (one `[Unreleased]` entry),
`examples/resources/unifi_nat/resource.tf` + `import.sh` (new, following the
existing `examples/resources/unifi_<name>/` layout — verify exact layout
with `ls examples/resources/unifi_port_forward/` before Task 8),
`docs/resources/nat.md` (generated via the repo's doc-gen tooling — confirm
tool, likely `tfplugindocs`, via `Makefile`/`go generate` directives before
Task 8; do not hand-write if generated elsewhere).

---

## Task 0: Precondition check — PR-V composite-identity import helper

**Files:** none from this PR's own scope; this task *verifies* a dependency,
it does not implement one (the PR-V amendment is a different PR's work).

Spec §8 establishes that `v2ImportState` as currently written
(`unifi/v2_resource.go:249-262`) writes the **bare** id to the `id`
attribute with `site` as an independent sibling — the
`firewall_policy`/`port_forward` shape — and cannot produce the **composite**
`<site>:<id>` `id`-with-derived-`site` shape C3 mandates for `unifi_nat`.
This is a hard blocker for Task 8 (CRUD) and Task 9 (ImportState), not a
style preference.

- [ ] **Step 1:** Confirm whether PR-V has been amended (in this worktree or
  a merged successor) with a composite-identity import helper or mode (spec
  §8's two named options: a second helper like `v2ImportStateComposite`, or
  a mode parameter on `v2ImportState`). Check `unifi/v2_resource.go` for its
  current exported surface and `unifi/v2_resource_test.go` for coverage of
  either shape.
- [ ] **Step 2:** If the amendment does not exist yet: **stop here.** This
  plan's Task 8/9 cannot be written against a helper that doesn't exist.
  Either (a) file/flag the PR-V amendment as its own prerequisite unit of
  work (owned by whoever maintains PR-V, not invented ad hoc inside this
  PR's `nat_resource.go`), and wait for it to land, or (b) if a maintainer
  explicitly directs this PR to implement the amendment itself as part of
  D's own branch (e.g. because PR-V is still open and taking changes), do
  that as an explicit, separately-committed, separately-reviewed change to
  `unifi/v2_resource.go` with its own tests — never as inline NAT-local
  ID-parsing logic in `nat_resource.go`. Either path must be resolved
  before Task 1.
- [ ] **Step 3:** Once the helper exists (by whichever path), record its
  exact name/signature here (update this task's text) so Tasks 8/9
  reference the real symbol, not a placeholder name.
- [ ] **Step 4:** No commit for this task (verification/dependency-tracking
  only) unless Step 2's option (b) path was taken, in which case that PR-V
  change gets its own commit in its own right, separate from this plan's
  Task 1+ commits.

*This task has no automated test — it is a dependency gate, not a decision
gate (the prior revision's Task 0 was a decision gate over six open
questions; those are now resolved in the spec, see the spec's "Decisions
resolved in this revision" section). Do not start Task 1 until this gate is
actually satisfied in code.*

---

## Task 1: Model types + `AttributeTypes()` for nested blocks

**Files:** Create `unifi/nat_resource.go` (model + attribute-type methods
only — no schema/CRUD yet), `unifi/nat_resource_test.go`

**Interfaces — Produces:** `natModel` (top-level tfsdk model, mirrors spec
§5's schema sketch), `natDnatModel`, `natSnatModel` (**no
`natMasqueradeModel`** — spec §4.1 settled: MASQUERADE has no nested block),
`natFilterModel` (shared shape for `destination_filter`/`source_filter` —
one Go type reused for both, since `NatDestinationFilter`/`NatSourceFilter`
are wire-identical), each with an `AttributeTypes() map[string]attr.Type`
method, following `firewallPolicyEndpointModel.AttributeTypes()`
(`unifi/firewall_policy_resource.go:113-127`) as the template for how a
nested block's Go type declares its own `types.ObjectType`/`types.SetType`
shape for use in `types.ObjectValueFrom`/`ObjectAsOptions` calls elsewhere.
`natDnatModel`/`natSnatModel`'s filter field and `natFilterModel`'s
`firewall_group_ids` field are the two places this task must get right:
`destination_filter`/`source_filter` are `types.Object`; `firewall_group_ids`
is `types.Set` with `ElementType: types.StringType`, not a nested-object
list.

- [ ] **Step 1: Failing test** — for each model type, a test constructing a
  zero-value instance, converting it through `types.ObjectValueFrom(ctx,
  M{}.AttributeTypes(), m)`, and asserting no diagnostics and that
  round-tripping back via `objectAsOptions[M]` (from `v2_resource.go`)
  reproduces the same zero value. Mirrors
  `Test_portForwardWanModel_AttributeTypes` (`unifi/port_forward_resource_test.go:628`)
  extended to also exercise `objectAsOptions` as the decode path, since
  PR-D is the first real consumer of that helper outside its own unit
  tests. Additionally test that `natFilterModel.firewall_group_ids` decodes
  via direct `types.Set.ElementsAs`, not through any `objectAsOptions`/
  `objectListAsOptions` call — a set of bare strings needs no per-element
  struct decode.
- [ ] **Step 2: Run** `go test ./unifi/ -run Test_nat.*AttributeTypes` → FAIL (types don't exist).
- [ ] **Step 3: Implement** the model structs and `AttributeTypes()` methods
  only — no `Schema`/CRUD methods yet, so this task is a small, reviewable
  slice.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: `gofmt -w`, `go vet ./unifi/...`, commit** `unifi: add unifi_nat model types`.

---

## Task 2: Schema (flat fields, no discriminator logic yet)

**Files:** Modify `unifi/nat_resource.go`, `unifi/nat_resource_test.go`

**Interfaces — Produces:** `natResource` struct (`client *Client`),
`Metadata`, `IdentitySchema`, `Schema` methods, following
`firewallPolicyResource`'s method shapes
(`unifi/firewall_policy_resource.go:129-398`) but content per spec §5's
schema sketch. No validators or plan modifiers for the discriminator yet
(Task 4/5) — this task gets the attribute tree/types/defaults/`OneOf` enums
for non-discriminator fields correct and compiling first.
`protocol`/`ip_version`/`pppoe_use_base_interface`/`setting_preference` are
schema'd as **top-level attributes only** (spec §4.3) — do not add them
inside `dnat`/`snat` as well. `rule_index` is `Computed` **only** (no
`Optional`) — spec §5/§6a item 5.

- [ ] **Step 1: Failing test** — `Test_natResource_Metadata` (asserts
  `resp.TypeName == "unifi_nat"`), `Test_natResource_IdentitySchema`
  (asserts `id` is `RequiredForImport`), `Test_natResource_Schema` (asserts
  presence, type, and required/optional/computed shape of every top-level
  attribute from spec §5 — table-driven over attribute name → expected
  shape, mirroring `Test_portForwardResource_Schema`
  (`unifi/port_forward_resource_test.go:798`); explicitly assert
  `rule_index` is `Computed` and NOT `Optional`, and that no `masquerade`
  attribute/block exists in the schema at all).
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** `Metadata`/`IdentitySchema`/`Schema`. Every
  `schema.StringAttribute`/`Int64Attribute`/`BoolAttribute` carries a
  `MarkdownDescription`; every field spec §6a flags as
  needs-live-confirmation gets that caveat in its description text (not
  just the spec doc) so `docs/resources/nat.md` inherits it.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: `gofmt -w`, `go vet ./unifi/...`, commit** `unifi: add unifi_nat schema`.

---

## Task 3: `Configure` via `v2Configure`

**Files:** Modify `unifi/nat_resource.go`, `unifi/nat_resource_test.go`

- [ ] **Step 1: Failing test** — `Test_natResource_Configure`, mirroring
  `Test_portForwardResource_Configure`
  (`unifi/port_forward_resource_test.go:831`): nil `ProviderData` → no
  error, `r.client` untouched; wrong-type `ProviderData` → error
  diagnostic; correct `*Client` → `r.client` set.
- [ ] **Step 2: Run** → FAIL (method doesn't exist).
- [ ] **Step 3: Implement**:
  ```go
  func (r *natResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
      r.client = v2Configure(req, resp)
  }
  ```
  One line — this is exactly the boilerplate PR-V exists to remove (see
  `v2Configure`'s doc comment, `unifi/v2_resource.go:71-103`, which names
  `firewall_policy_resource.go`/`firewall_zone_resource.go`'s duplicated
  body as the defect it centralizes).
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `unifi: wire unifi_nat Configure through v2Configure`.

---

## Task 4: Discriminator normalization — rule `type` (C4/§4.1)

**Files:** Modify `unifi/nat_resource.go`, `unifi/nat_resource_test.go`

**Interfaces — Produces:** a `resource.ResourceWithValidateConfig`
implementation (`ValidateConfig` method) rejecting: a populated `snat`
block when `type != "SNAT"`; a populated `dnat` block when `type !=
"DNAT"`; **and** a missing `dnat` block when `type = "DNAT"` (respectively
`snat`/`SNAT`) — DNAT/SNAT's own block is *required*, not
optional-with-defaults, since there is no meaningful DNAT/SNAT rule without
a translation-target block (spec §4.1's settled answer to the prior
revision's decision #2). There is no MASQUERADE-block case to validate,
because there is no `masquerade` block. Plus a plan-modifier (custom
`planmodifier.Object` on `dnat`/`snat`, or a resource-level `ModifyPlan` —
choose based on which is more testable in isolation) that nulls the
inactive block (`snat` when `type` is `DNAT`, `dnat` when `type` is `SNAT`,
both when `type` is `MASQUERADE`) before validation runs.

- [ ] **Step 1: Failing test** — table-driven `Test_natResource_typeDiscriminator_validate`:
  `type=DNAT` + populated `snat` block → error diagnostic;
  `type=DNAT` + populated `dnat` block only → no error;
  `type=DNAT` + **no `dnat` block at all** → error diagnostic (block
  required when active);
  `type=MASQUERADE` + populated `dnat` or `snat` → error;
  `type=MASQUERADE` + neither block populated → no error (this is the only
  type with no required block). Plus
  `Test_natResource_typeDiscriminator_planModifier`: prior state
  `type=SNAT` with a populated `snat` block, planned config changes to
  `type=DNAT` with a new `dnat` block and no `snat` block in config → the
  modified plan's `snat` attribute is null (not carried over stale from
  prior state), matching spec §4.1's "stale prior-state children cleared
  ... before validation" requirement and the direct regression test for
  "NAT stale selectors" (traceability §4.2).
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** `ValidateConfig` and the plan modifier(s).
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: `gofmt -w`, `go vet ./unifi/...`, commit** `unifi: normalize unifi_nat rule-type discriminator (type)`.

---

## Task 5: Discriminator normalization — filter `filter_type` (C4/§4.2)

**Files:** Modify `unifi/nat_resource.go`, `unifi/nat_resource_test.go`

Same mechanism as Task 4, one level deeper: applies to `filter_type` inside
`destination_filter`/`source_filter` — a shared `natFilterModel`-scoped
validator/plan-modifier reused for both nested locations (don't duplicate
the logic per block; the two filters are wire-identical shapes per spec
§4.2).

- [ ] **Step 1: Failing test** — table-driven over `filter_type` ∈
  {`NONE`, `ADDRESS_AND_PORT`, `FIREWALL_GROUPS`, `NETWORK_CONF`} ×
  each of `address`/`port`/`firewall_group_ids`/`network_conf_id`
  configured, asserting: owned combination → no error; unowned
  combination → error. Plus a plan-modifier test:
  `destination_filter.filter_type` transitions `FIREWALL_GROUPS` →
  `NETWORK_CONF` with a stale `firewall_group_ids` in prior state and no
  `firewall_group_ids` in new config → modified plan nulls/empties
  `firewall_group_ids`. This is the acceptance-coverage requirement from
  spec §11 made concrete at the unit level first.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: `gofmt -w`, `go vet ./unifi/...`, commit** `unifi: normalize unifi_nat filter discriminator (filter_type)`.

---

## Task 6: Conversion functions — `modelToNat` / `natToModel`

**Files:** Modify `unifi/nat_resource.go`, `unifi/nat_resource_test.go`

**Interfaces — Produces:** `modelToNat(ctx, model natModel) (*unifi.Nat, diag.Diagnostics)`
and `natToModel(ctx, n *unifi.Nat, model *natModel) diag.Diagnostics`,
following `modelToFirewallPolicy`/`firewallPolicyToModel`
(`unifi/firewall_policy_resource.go:778-826`, `:953-1003`) as the shape
template, but using `objectAsOptions[T]` (`unifi/v2_resource.go:264-298`)
for every nested-**object** decode (`dnat`, `snat`, and the filter block
one level deeper) instead of the hand-rolled `obj.As(ctx, &m,
basetypes.ObjectAsOptions{})` calls `firewall_policy_resource.go` uses —
this is the concrete fix for the "`objectAsOptions`-in-NAT defect" the
parent spec names as one of PR-V's reasons for existing (parent spec
§379-388). `firewall_group_ids` decodes via a direct `types.Set.ElementsAs`
into `[]string` — **not** `objectListAsOptions`, which is for
`ListNestedAttribute`-shaped (multi-field-element) lists that don't exist
anywhere in this schema (spec §9). `modelToNat` serializes only the active
discriminator's block into the flat wire fields plus the shared top-level
fields (§4.3's `protocol`/`ip_version`/`pppoe_use_base_interface`/
`setting_preference`, read once, not per-block) per spec §4.1/§4.2's
"outbound normalization" rule; `natToModel` populates only the matching
block and nulls the other/other-filter-type fields per spec §4.1/§4.2's
"inbound... normalize stale values out of state" rule.

- [ ] **Step 1: Failing test** — round-trip tests per rule type: build a
  `natModel` with `type=DNAT` and a populated `dnat` block (including a
  nested `destination_filter` with `filter_type=ADDRESS_AND_PORT`), run
  `modelToNat`, assert the resulting `*unifi.Nat`'s flat fields
  (`IPAddress`, `Port`, `DestinationFilter.Address`, etc.) match; then run
  `natToModel` on that `*unifi.Nat` and assert the round-tripped model
  equals the original (modulo any lossy string/int64 conversion, tested
  explicitly for `port`). Repeat for SNAT and MASQUERADE (MASQUERADE case:
  assert both `dnat` and `snat` end up null, and the shared top-level
  fields — `out_interface`, `protocol`, etc. — round-trip with no block
  involved at all). Also test the "controller returns stale non-owned
  filter fields" case from spec §4.2's inbound-normalization rule: a
  `*unifi.Nat` with `filter_type=NETWORK_CONF` but non-empty
  `FirewallGroupIDs` set (a wire-legal-but-inactive combination) →
  `natToModel` produces a model with `firewall_group_ids` null/empty, not
  the stale value. Also test that `firewall_group_ids` round-trips through
  a plain `types.Set` decode (assert no `objectListAsOptions` call site
  exists for this field — a code-shape assertion, not just a value
  assertion, since spec §9 flags this as a previously-wrong claim worth
  actively guarding against regressing).
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.** Port fields use string↔`*int64` conversion
  (validate against the documented `[1-9][0-9]{0,4}` pattern at the schema
  validator level, Task 2 — this task just does the numeric conversion,
  nil-safe both directions). `invert_address`/`invert_port` are always
  sent (no `omitempty` on the wire struct) — `modelToNat` must not
  conditionally omit them.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: `gofmt -w`, `go vet ./unifi/...`, commit** `unifi: add unifi_nat model<->API conversion`.

---

## Task 7: Predefined/NoEdit/NoDelete precheck (§5.6/§6.1)

**Files:** Modify `unifi/nat_resource.go`, `unifi/nat_resource_test.go`

**Interfaces — Produces:** `natMutationPrecheck(op string, noEdit,
noDelete bool) diag.Diagnostics` (or two narrower functions
`natUpdatePrecheck`/`natDeletePrecheck` — prefer whichever keeps call
sites in Update/Delete a single `if`) that returns a populated
diagnostics value when the operation is blocked, nil otherwise. Pure
function, no I/O — this is the "non-mutating" part of §6.1: it inspects
already-decoded state fields (`no_edit`/`no_delete`, populated by the last
Read via `natToModel`), never issues a fresh request.

**Test-scope note (per review, spec §7):** this task's unit test proves the
*predicate* is correct. It does **not** by itself prove Update/Delete's
method bodies actually skip `UpdateNat`/`DeleteNat` when the precheck
fires — that is Task 8's job, and Task 8's own test must independently
verify request-non-invocation (via an injectable seam) or explicitly defer
that proof to acceptance/live validation (spec §11's predefined-rule
acceptance test). Do not let this task's green test be cited later as
proof of the request-skipping behavior; it isn't.

- [ ] **Step 1: Failing test** — `natMutationPrecheck` table: `noEdit=true`
  on an update-precheck call → non-nil diagnostics with a clear summary
  naming the rule id and `attr_no_edit`; `noEdit=false` → nil;
  analogous for `noDelete`/delete-precheck. Assert the diagnostic message
  is useful (names the constraint), not generic.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: `gofmt -w`, `go vet ./unifi/...`, commit** `unifi: add unifi_nat predefined-rule mutation precheck`.

---

## Task 8: Create/Read/Update/Delete/ImportState via PR-V helpers (+ post-mutation reconcile)

**Files:** Modify `unifi/nat_resource.go`, `unifi/nat_resource_test.go`

**Precondition:** Task 0's PR-V composite-identity import helper exists and
its exact name/signature is recorded there. This task cannot be written
concretely (the `ImportState` body below is illustrative, not final) until
that precondition is satisfied.

**Interfaces — Produces:** `Create`, `Read`, `Update`, `Delete`,
`ImportState` methods. Every method's timeout handling goes through
`v2Timeout(ctx, model.Timeouts, v2TimeoutCreate|Read|Update|Delete)`; every
site resolution goes through `resolveV2Site` (Create's configured-site case)
or `parseSiteID` on the persisted composite `id` (Read/Update/Delete, per
spec §8); `ImportState` delegates to the PR-V composite-identity helper from
Task 0, parameterized with `r.client.Site`, e.g. (name illustrative pending
Task 0):
```go
func (r *natResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
    v2ImportStateComposite(ctx, req, resp, r.client.Site) // exact name per Task 0
}
```
`Read`'s NotFound/error handling goes through `v2FinishRead`; `Delete`'s
NotFound handling goes through a direct `v2IsNotFound(err)` check (matching
`firewallPolicyResource.Delete`'s "ignore NotFound, surface everything
else" pattern — no framework helper needed there since Delete has no
state-removal step, only a diagnostic-or-not branch); every successful
Create/Read/Update ends with `v2SetIdentityAndState[natModel](ctx,
resp.Identity, resp.State, model.ID, &model)` instead of the
hand-written `SetAttribute`-then-`State.Set` pair.

**Post-mutation reconciliation (spec §12) — mandatory, not optional:**
- **Create**: after `CreateNat(ctx, site, nat)` returns successfully (using
  the returned object's `ID`), call `GetNat(ctx, site, created.ID)` and
  build the model from **that** result via `natToModel`, not from
  `created` directly. If `GetNat` fails immediately after a successful
  `CreateNat`, this is an error diagnostic on Create (do not silently fall
  back to the `CreateNat` echo body).
- **Update**: after `UpdateNat(ctx, site, nat)` returns successfully, call
  `GetNat(ctx, site, id)` and build the model from that read, not from
  `UpdateNat`'s response.
- This is a deliberate departure from both `firewallPolicyResource.Create`
  (`unifi/firewall_policy_resource.go:454-466`, uses the `CreateFirewallPolicy`
  echo body via `firewallPolicyToModel(ctx, created, &plan)`) and
  `portForwardResource.Create`
  (`unifi/port_forward_resource.go:369-378`, same pattern via
  `r.portForwardToModel(ctx, createdPortForward, &data, site)`) — **do not
  copy either template's Create/Update body verbatim**; copy their
  timeout/site/error-handling shape, but insert the extra `GetNat` call
  before building state.

Per Global Constraints and spec §8: `site` is *derived from* the persisted
composite `id` via `parseSiteID` (not read from a separately-stored `site`
attribute the way `firewall_policy`/`port_forward` do it) in
Read/Update/Delete; Create derives `site` from the configured value via
`resolveV2Site` (there is no persisted `id` yet at Create time).

Update and Delete call the Task 7 precheck functions using the
last-known-state's `no_edit`/`no_delete` values *before* calling
`UpdateNat`/`DeleteNat` — if the precheck returns diagnostics, append them
and return immediately, never issuing the PUT/DELETE request.

- [ ] **Step 1: Failing test** — unit tests that can run without a live
  controller: `ImportState` behavior via a fake/minimal
  `resource.ImportStateRequest`/`Response` pair (mirroring
  `v2_resource_test.go`'s `newV2TestImportStateResponse` helper — reuse it
  directly if its shape fits the composite-id helper's signature, or
  extend it minimally if not), asserting both `<id>` and `<site>:<id>`
  import grammars produce a **composite `id`** attribute write (not a bare
  id — this is the concrete regression test for spec §8's
  contradiction-resolution) and a correctly-derived `site`. A
  precheck-wiring test: construct a `natResource` with a fake client (or
  skip the actual client call via an interface seam if one is introduced —
  see Step 3 note) and assert Update/Delete short-circuit with the
  precheck's diagnostic when state's `no_edit`/`no_delete` is true,
  **without invoking the client's Update/Delete method** — if
  `*Client`/`unifi.ApiClient` isn't already fake-able at this granularity
  elsewhere in the package, introduce the minimal seam needed to assert
  non-invocation (do not settle for "the diagnostic looks right" without
  also asserting the client method was never called; that gap is exactly
  what spec §7's test-scope-honesty note warns against). A
  post-mutation-reconcile test: a fake/injectable client whose
  `CreateNat`/`UpdateNat` response body differs from what its
  `GetNat` would return for the same id, asserting the state written by
  Create/Update matches the `GetNat` shape, not the mutate-response shape
  — this is the direct regression test for spec §12; a test that can't
  distinguish the two sources does not satisfy this task.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** the five methods, including the reconcile-read
  calls. Reuse `modelToNat`/`natToModel` (Task 6) for all wire conversion;
  do not duplicate conversion logic inline in Create/Update.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: `gofmt -w`, `go vet ./unifi/...`, commit** `unifi: implement unifi_nat CRUD via PR-V helpers`.

---

## Task 9: `list.ListResource` (§5.9/§10)

**Files:** Modify `unifi/nat_resource.go`, `unifi/nat_resource_test.go`,
`unifi/provider.go`

**Interfaces — Produces:** `NewNatResource() resource.Resource`,
`NewNatListResource() list.ListResource` (both backed by `*natResource`,
per the existing one-struct-two-constructors pattern —
`unifi/firewall_policy_resource.go:47-53`), `natListConfigModel`,
`natListFilterModel`, `ListResourceConfigSchema`, `List` methods —
structurally identical to `firewallPolicyResource`'s implementation
(`unifi/firewall_policy_resource.go:1071-1221`), adapted: filter names
`type`/`enabled`/`description` (spec §10) instead of firewall policy's
`name`/`action`/`enabled`; each list result's identity `id` is the
composite `<site>:<id>` (matching the managed resource's own identity
shape, per spec §8/§10 — this is the one place this task's shape most
diverges from the firewall-policy template, since that resource's list
identity is the bare id).

- [ ] **Step 1: Failing test** — assert `var _ list.ListResource =
  &natResource{}` and `var _ list.ListResourceWithConfigure =
  &natResource{}` compile-time checks exist; `Test_natResource_ListResourceConfigSchema`
  asserts the `site`/`filter` shape; a unit test for the post-filter logic
  (given a slice of `unifi.Nat`, assert filtering by `type`/`enabled`/
  `description` narrows correctly) that doesn't require a live controller,
  mirroring how `firewall_policy_resource_test.go` likely tests its own
  filter logic (check for an existing pattern there before inventing a
  new one); a list-identity test asserting the emitted identity `id`
  matches the composite `<site>:<id>` shape, not a bare id.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.** Register both constructors in
  `unifi/provider.go`'s `Resources()`/`ListResources()`.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: `gofmt -w`, `go vet ./unifi/...`, commit** `unifi: add unifi_nat list resource and provider registration`.

---

## Task 10: Acceptance tests — DNAT, SNAT, MASQUERADE, transitions, precheck, reconcile

**Files:** `unifi/nat_resource_test.go` (or `nat_resource_acc_test.go` per
the File Structure note above — decide before this task, don't split
mid-task)

Gated `TF_ACC=1`, demo-controller-only (`unifi/provider_test.go`'s
`TestMain`/`runAcceptanceTests`), following
`TestAccPortForward_basic`'s structure
(`unifi/port_forward_resource_test.go:20-70`: `resource.Test` +
`PreCheck`/`ProtoV6ProviderFactories`/`Steps`, a create+check step, an
import+`ImportStateVerify` step).

- [ ] **Step 1:** `TestAccNat_dnatAddressAndPort` — DNAT rule,
  `destination_filter.filter_type = "ADDRESS_AND_PORT"`, asserts
  `ip_address`/`port`/`invert_address`/`invert_port` round-trip; import +
  `ImportStateVerify` (both `<id>` and `<site>:<id>` import forms, since
  spec §8's composite-id contract is this PR's own novel identity shape and
  deserves direct acceptance coverage, not just unit coverage).
- [ ] **Step 2:** `TestAccNat_dnatFirewallGroups` — DNAT rule,
  `destination_filter.filter_type = "FIREWALL_GROUPS"`, provisions a
  companion `unifi_firewall_group` inline in the test config (mirroring
  `TestAccPortForward_sourceLimitingFirewallGroup`'s pattern —
  `unifi/port_forward_resource_test.go:169`), asserts
  `firewall_group_ids` round-trips as a set.
- [ ] **Step 3:** `TestAccNat_snatNetworkConf` — SNAT rule,
  `source_filter.filter_type = "NETWORK_CONF"`, provisions a companion
  `unifi_network`, asserts `network_conf_id` round-trips.
- [ ] **Step 4:** `TestAccNat_masquerade` — MASQUERADE smoke test (happy
  path, minimal fields, no nested block since none exists) — retained as
  one test among several, not the only one, per spec §11 / Global
  Constraints.
- [ ] **Step 5:** `TestAccNat_typeTransition` — create a rule as one type
  (e.g. SNAT), apply an update changing `type` to DNAT with a new `dnat`
  block, assert the plan has no stale `snat` attribute leakage and the
  apply succeeds — direct regression coverage for spec §4.1's
  discriminator-transition requirement.
- [ ] **Step 6:** `TestAccNat_filterTypeTransition` — within one rule type,
  transition `filter_type` (e.g. `ADDRESS_AND_PORT` → `FIREWALL_GROUPS`),
  assert stale fields clear — regression coverage for spec §4.2 / parent
  spec's "NAT stale selectors" traceability item.
- [ ] **Step 7:** Investigate whether the demo docker controller seeds any
  predefined/`is_predefined`/`attr_no_edit` NAT rule. If yes:
  `TestAccNat_predefinedImportOnly` — import it, assert Read succeeds,
  assert an attempted Update/Delete against it fails with the Task 7
  precheck's diagnostic. Per spec §7/§11's test-scope-honesty requirement:
  state explicitly in the test's doc comment which proof this test relies
  on — either (a) an injectable-seam assertion that `UpdateNat`/`DeleteNat`
  was never invoked, or (b) a controller-side assertion (e.g. re-fetch the
  rule and confirm it is byte-identical to before the attempted apply) —
  do not just assert the diagnostic text and call that sufficient proof of
  non-invocation. If no such rule exists on the demo controller, mark this
  test `t.Skip("demo controller ships no predefined NAT rule; covered by
  unit test in Task 7 + flagged for live-controller validation")` and note
  it in spec §6b as confirmed unavailable in the demo environment — do not
  silently drop the coverage, downgrade it explicitly.
- [ ] **Step 8:** `TestAccNat_postMutationReconcile` (or fold into Step 1
  as an additional assertion): assert that after Create and after Update,
  the resulting state is provably sourced from a fresh read, not the
  mutation response — at the acceptance level this is harder to observe
  directly than at the unit level (Task 8's test already covers the code
  path with an injectable double); this step's job is to confirm the real
  `CreateNat`/`UpdateNat`/`GetNat` sequence against the demo controller
  doesn't error and produces the expected final state, as an end-to-end
  sanity check of Task 8's implementation, not a re-proof of the reconcile
  behavior itself (that proof lives in Task 8's unit test).
- [ ] **Step 9: Run** `TF_ACC=1 go test ./unifi/... -run TestAccNat -timeout 20m`
  → PASS (or explicit skip per Step 7).
- [ ] **Step 10: Commit** `unifi: add unifi_nat acceptance tests (DNAT/SNAT/MASQUERADE)`.

---

## Task 11: Examples + docs + changelog

**Files:** `examples/resources/unifi_nat/resource.tf`,
`examples/resources/unifi_nat/import.sh` (confirm exact filenames/layout
against `examples/resources/unifi_port_forward/` before writing),
`docs/resources/nat.md` (confirm generation tool/command — likely
`tfplugindocs generate`, check `Makefile`/`go generate` — regenerate rather
than hand-author if so), `CHANGELOG.md`.

- [ ] **Step 1:** Write `resource.tf` showing at minimum one DNAT and one
  SNAT example (not MASQUERADE-only, matching this PR's own coverage
  principle), using RFC 5737/3849 documentation-range IPs. No `masquerade`
  block in any example (spec §4.1 — it doesn't exist).
- [ ] **Step 2:** Write `import.sh` showing both `<id>` and `<site>:<id>`
  import forms, both producing the composite `id` in state (spec §8).
- [ ] **Step 3:** Regenerate/write `docs/resources/nat.md`.
- [ ] **Step 4:** Add one `[Unreleased]` `### ✨ Features` entry to
  `CHANGELOG.md` in house style (what users could not do before, what they
  can do now, any state/compat notes — e.g. "no `unifi_nat` resource
  existed; NAT rules could only be read via ... [if a data source exists —
  check] or managed by hand in the UI").
- [ ] **Step 5: Commit** `unifi: add unifi_nat examples, docs, and changelog entry`.

---

## Task 12: Whole-branch review

- [ ] **Step 1:** Run the full non-acceptance suite:
  `gofmt -l unifi/` (empty output), `go build ./...`, `go vet ./unifi/...`,
  `go test ./unifi/...`.
- [ ] **Step 2:** Run `TF_ACC=1 go test ./unifi/... -run TestAccNat -timeout 20m`
  one more time at the branch tip (not just per-task) to catch any
  cross-task interaction.
- [ ] **Step 3:** Run `golangci-lint run ./unifi/...` (or repo's configured
  invocation) clean.
- [ ] **Step 4:** Privacy sweep of every file this PR touched: no real
  ObjectIDs, no non-`192.0.2.0/24`/`198.51.100.0/24`/`203.0.113.0/24`
  IPs, no `/Users/...` in any commit message on this branch.
- [ ] **Step 5:** Re-check spec §6a/§6b's uncertain-wire-field lists against
  what was actually implemented — confirm every §6a item still carries its
  needs-live-confirmation caveat in the shipped schema descriptions, not
  just the spec doc, and confirm every §6b item is still an open
  live-validation item (not silently resolved without evidence).
- [ ] **Step 6:** Confirm no drive-by `unifi_nat_rule` → `unifi_nat`
  corrections were needed in files this PR itself touched (this PR's own
  new files should never say `unifi_nat_rule`); if the PR-V amendment
  (Task 0) touched `unifi/v2_resource.go`'s existing `unifi_nat_rule`
  prose incidentally, correct it there as a drive-by (spec §1) — do not
  edit `docs/superpowers/specs/2026-07-11-settings-remediation-design.md`
  from this PR, that file is out of scope.
- [ ] **Step 7:** Use `codex-review` (per parent spec's review process,
  §495-511) at the branch tip against PR-V's merge base.
- [ ] **Step 8:** Completion report per parent spec §10: merge-gate
  disposition (§5.2 post-mutation reconcile, §5.6 predefined handling,
  §5.9 list-resource, §6.1 capability precheck — all D-owned gates from
  the traceability table), which of spec §6a/§6b's uncertain fields remain
  unresolved and need the live-validation pass named in the parent spec's
  "Live validation" section, exact verification commands + results.
