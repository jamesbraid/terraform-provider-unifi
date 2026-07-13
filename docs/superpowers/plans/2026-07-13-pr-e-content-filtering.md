# PR-E: Content-Filtering Resource — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `unifi_content_filtering`, a v2 resource for per-site
content-filtering profiles, built entirely on `unifi/v2_resource.go` (PR-V),
applying the parent design's enum compatibility policy (§5.4), collection
ownership (C1/§5.3), and capability precheck (C6/§6.1). **Whether it ships
as a collection-shaped resource (CRUD + list, per-item `site:id` identity —
the working assumption used throughout this plan) or as a per-site singleton
(no list resource, no per-item identity, `unifi_setting`/BGP-shaped) is
conditional on live-controller capture proof — see spec's "Site identity +
list resource" section and Task 3's Step 0 gate.** This plan is written
against the collection-shaped assumption because that is the spec's leading
candidate, not because it is confirmed.

**BLOCKED status, capture-first:** the resource's wire shape (go-unifi
struct, JSON field names, v2 endpoint path) does not exist in
`go-unifi@v1.33.43-0.20260706191309-bc63776a9ebf` — verified by exhaustive
grep across the module (see spec's WIRE SHAPE STATUS section). **Tasks 5–9
(schema, converter, CRUD, list, acceptance tests) cannot start until the
spec's "Live-controller capture plan" produces a sanitized live payload.**
This plan is revised per an independent design review (verdict: NEEDS-WORK on
the prior draft) that found Tasks 2–4 had, in places, written an unverified
guess as though it were mergeable, decided scaffolding. The corrected
framing: **Task 1 is genuinely not blocked** (the `safe_search` `OneOf`
literals are named directly in the parent spec, independent of any other
wire-shape question). **Tasks 2, 3, and 4 are NOT "not blocked" — they are
hypothesis/design work whose *artifacts* (a candidate model, a conditional
identity design, a capability-shape sketch) are useful now, but none of them
produce mergeable production schema, CRUD, or Read code ahead of the
capture.** See each task's revised framing below. Tasks 5–9 remain fully
blocked as before.

Spec of record: `docs/superpowers/specs/2026-07-13-pr-e-content-filtering-design.md`.

## Global Constraints

- Builds ONLY on PR-V (`unifi/v2_resource.go`): `v2Configure`, `v2Timeout`/
  `v2DefaultTimeout`, `v2SetIdentityAndState`, `v2FinishRead`, `v2IsNotFound`,
  `v2ImportState`, `resolveV2Site` apply unconditionally. `objectAsOptions`/
  `objectListAsOptions` apply only where the real schema has a matching
  nested-object shape — per the spec's corrected helper-applicability note,
  `objectAsOptions` decodes a single `types.Object` (applies to `schedule`
  if it stays a single nested object) and `objectListAsOptions` decodes a
  `types.List` of nested objects (applies only if capture shows a field is
  actually a list-of-objects, e.g. multiple schedule ranges) — **neither
  applies to the scalar `network_ids`/`client_macs` lists or a flat-string
  `safe_search` list**, which convert via plain `list.ElementsAs` instead.
  Do not force either helper onto a scalar list field for the sake of using
  "every PR-V helper." No resource-specific reimplementation of anything
  PR-V already provides. Must compile and behave identically whether or not
  PR-D (`unifi_nat_rule`) is merged — no shared types, no cross-references to
  PR-D's files.
- TDD: every task's Go code starts with a failing test. Converter/schema
  tests can run without a live controller (table-driven, in-package);
  acceptance tests are gated `TF_ACC` and demo-controller-only per repo
  convention, EXCEPT where Task 8 notes the demo controller may not carry
  this feature (see Task 8's capability-skip design).
- godoc: every exported type/function carries a doc comment in the style
  already used in `unifi/v2_resource.go` and `unifi/firewall_policy_resource.go`
  (full sentences, states the contract, cites the design section it
  implements).
- golangci-lint clean: `golangci-lint run ./unifi/...` (or repo's configured
  invocation) with no new findings.
- Enum `OneOf` policy (§5.4, decided): `safe_search` engine values and
  `schedule` mode are closed `OneOf` sets with a provider-bump doc note;
  `restricted_categories` is open (no `OneOf`), following the
  `ips.enabled_categories` precedent.
- Collection type: scope selectors (`network_ids`, `client_macs`) are
  `types.List`, matching `firewall_policy`'s equivalent selector family —
  NOT `types.Set` (that's `ap_group`'s membership-collection convention, a
  different selector family). Decided in the spec; do not revisit without
  updating the spec first. **`client_macs` element type defaults to plain
  `types.String`**, matching `firewall_policy_resource.go`'s actual
  `ClientMACs`/`NetworkIDs` fields (verified: both are
  `types.ListType{ElemType: types.StringType}`, not `hwtypes.MACAddress`) —
  `hwtypes.MACAddress` is an option to adopt only if the live-controller
  capture's edit tests (capture plan step (d)) show the controller
  normalizes MAC case/separators in a way that would otherwise churn the
  plan; it is not an established convention to follow by default (see spec's
  "Client + network scope semantics" section).
- Privacy-safe fixtures: any acceptance-test fixture (network IDs, MACs,
  profile names) uses synthetic values, never real household/controller
  data, per the parent spec's privacy-scrub discipline (§1).
- Verification per task: `gofmt -w`, `go build ./...`, `go vet ./unifi/...`,
  `go test ./unifi/...`, `git diff --check`.

## File Structure

New (flat `unifi/`): `content_filtering_resource.go` (schema, model, CRUD,
list — mirrors `firewall_policy_resource.go`/`port_forward_resource.go`
structure), `content_filtering_resource_test.go` (unit/converter tests),
`content_filtering_resource_acc_test.go` (TF_ACC lifecycle tests, split out
per repo convention if `firewall_policy`/`port_forward` do so — confirm
against their actual file split before creating).

Modified: `unifi/provider.go` (register `NewContentFilteringResource` /
`NewContentFilteringListResource`), `docs/resources/content_filtering.md`
(generated via the repo's doc-gen tooling — do not hand-write), `CHANGELOG.md`,
`examples/resources/unifi_content_filtering/` and
`examples/list-resources/unifi_content_filtering/` (mirroring
`examples/resources/unifi_firewall_policy/` structure).

---

## Task 1: Ownership tags + enum validators (not blocked)

**Files:** Create `unifi/content_filtering_ownership.go` (or fold into the
main resource file if small — decide at implementation time; keep it a
separate, independently testable unit either way),
`unifi/content_filtering_ownership_test.go`.

**Produces:** the `OneOf` validator sets for `safe_search` engines
(`GOOGLE`, `YOUTUBE`, `BING` — literal values are already known from parent
spec §399/§5.4, independent of the rest of the wire shape) and for
`schedule` mode (placeholder literals; see Task 1b). A doc-comment constant
or comment block recording the provider-bump note text (spec's "Provider-bump
doc note" paragraph) for reuse in both the resource godoc and generated docs.

- [ ] **Step 1: Failing test** — a table test asserting the `safe_search`
      `stringvalidator.OneOf(...)` (or `listvalidator`-wrapped equivalent, once
      Task 2 settles single-vs-list) accepts `GOOGLE`/`YOUTUBE`/`BING` and
      rejects an arbitrary unknown string with a diagnostic, mirroring the
      validator-behavior test pattern used in `unifi/firewall_policy_resource_test.go`
      for `action`/`protocol`.
- [ ] **Step 2: Run** → FAIL (validator not yet defined).
- [ ] **Step 3: Implement** the `safe_search` `OneOf` validator constant/var.
      Leave the `schedule` mode `OneOf` as a `// TODO(content-filtering): fill
      in real mode literals once live-controller capture lands (see spec's
      WIRE SHAPE STATUS)` — do NOT guess literals; a validator with an empty
      or placeholder set is acceptable scaffolding here ONLY if it's marked
      unused/unreachable until Task 6 wires it, with a compile-time-visible
      TODO, not silently shipped as if final.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(content_filtering): safe-search OneOf validator + provider-bump doc note (§5.4)`.

### Task 1b: BLOCKED sub-item — schedule mode literals

Cannot complete until live-controller capture provides the real enum
values. Tracked here so it is not silently forgotten inside Task 1: once
literals are known, extend Task 1's test + validator, and remove the TODO.

---

## Task 2 [HYPOTHESIS-ONLY — not schema/model code, not mergeable production scaffolding]: Candidate Terraform model shape

**Reclassified from the prior draft's "not blocked / mergeable scaffolding"
framing.** The prior draft fixed unknown cardinalities — safe_search
list-vs-scalar, schedule object-vs-ranges — into a concrete Terraform state
model (a `contentFilteringModel` struct with `tfsdk` tags) and called it
"not blocked," on the reasoning that it "compiles standalone" and is tested
against a hand-built fake. That reasoning does not hold: a `tfsdk`-tagged
model struct with `AttributeTypes()` methods *is* the state schema, wired
into a real `schema.Schema` and persisted as Terraform state the moment
Task 6 attaches it to a resource. Locking in the wrong cardinality here does
not surface as a compile error later — it surfaces as a state-schema
migration for every user who applied against the guessed shape, exactly the
outcome the spec's WIRE SHAPE STATUS section says this PR must avoid.

**What this task actually produces:** a **candidate model, written down for
review and to give the capture something concrete to confirm or refute** —
not a struct that compiles into the resource, not a file that Task 6 attaches
to a `schema.Schema`, and not something a table test can "pass" in the sense
of certifying correctness (a tfsdk-tag/schema-agreement test only proves
internal self-consistency of a guess, not that the guess matches the
controller). Write the candidate as a **design note plus a
non-production sketch** (e.g. a `_test.go`-adjacent scratch file, or inline
in this plan/spec, clearly marked `// CANDIDATE — do not wire into a real
resource.Schema until live-controller capture confirms this shape`), not as
`unifi/content_filtering_model.go` or any file Task 6 would import.

- [ ] **Step 1:** Write the candidate `contentFilteringModel` shape
      (`safe_search` = `types.List(String)` of `OneOf` engines; `schedule` =
      nested `types.Object` with `mode` + time-bound fields) as a documented
      proposal, explicitly labeled non-final, citing spec Open Decisions
      #2/#3 and "Riskiest assumptions" #3/#4.
- [ ] **Step 2:** If a compiling sketch is useful for internal-consistency
      review, it may live in a scratch/throwaway file or a `_test.go` that
      asserts only "this candidate's tfsdk tags are self-consistent" — never
      committed as the production model file, never referenced by Task 6
      without being re-derived against the actual capture first.
- [ ] **Step 3: Do NOT commit** a production model file from this task. If a
      scratch sketch was written for review, discard it or keep it clearly
      out of the `unifi/` package (e.g. under a `docs/` or throwaway path) —
      it is a design artifact, not a merge candidate. **No commit message,
      no PR diff, introduces `contentFilteringModel` (or any nested model
      type) into `unifi/*.go` as a result of this task.** That only happens
      in Task 6, after Task 5's converter is unblocked by real capture
      evidence, and only with the shape corrected against that evidence.

---

## Task 3 [CONDITIONAL on proof of per-profile IDs]: Site identity via PR-V

**Reclassified from the prior draft's unconditional "not blocked."** The
prior draft implemented `<site>:<id>` per-item import identity as though it
were a settled fact, on the strength of the parent spec naming
`unifi_content_filtering` alongside `unifi_firewall_policy` in a PR list.
That is not proof the controller exposes multiple independently-addressable
profile objects with their own stable IDs, as opposed to one per-site
settings object (the `unifi_setting`/BGP singleton shape) with no per-item
identity at all — see the spec's "Site identity + list resource" section
(now marked CONDITIONAL) and "Riskiest assumptions" #1–2. Writing and
merging `v2ImportState` wiring, a `site`/`id` schema pair, and an import-
parsing test *now* commits to the collection-shape assumption before the
capture that would confirm or refute it exists.

**This task does not start until the live-controller capture (spec's
"Live-controller capture plan," steps (b) and (c)) confirms**: the
discovery/list response is an array of independently-addressable profile
objects, AND creating a second synthetic profile yields a second stable,
distinct controller-issued ID coexisting with the first (not a client-side
UUID the provider would have to invent, not an array index, not a
singleton overwrite). If the capture instead shows a per-site singleton,
this task is replaced entirely by a BGP/`unifi_setting`-shaped identity
(no `list.ListResource`, no per-item import, update-in-place against a
fixed per-site resource) — a different task, not a variant of this one.

**Once that proof exists, this task's original shape applies exactly:**

**Files:** Create `unifi/content_filtering_resource.go` (start the file;
Tasks 4–7 continue filling it in), test in
`unifi/content_filtering_resource_test.go`.

**Produces:** `ImportState` wired to `v2ImportState` exactly as PR-V's doc
comment prescribes; `site`/`id` schema attributes matching the C3 table
(`id` = canonical `<site>:<id>`, `site` derived, `RequiresReplace` on
`site` change).

- [ ] **Step 0 (gate, must pass before Step 1):** confirm the live-controller
      capture's steps (b)/(c) evidence establishes per-profile controller IDs
      (spec "Riskiest assumptions" #1–2). If not yet captured, this task
      remains blocked — do not proceed to Step 1 on the strength of the
      parent spec's naming alone.
- [ ] **Step 1: Failing test** — `TestContentFilteringImportState` table:
      bare `<id>` under a non-default provider default site normalizes to
      `<default-site>:<id>` (§5.5 equivalence requirement — run this test
      with a provider configured with a **non-default** default site, per
      spec); `<site>:<id>` passes through; empty and multi-colon input
      produce an error diagnostic. Mirror
      `unifi/ap_group_resource_test.go`'s or `firewall_policy_resource_test.go`'s
      existing import-parsing test shape if one exists — check first, reuse
      structure.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** `func (r *contentFilteringResource) ImportState(...)
      { v2ImportState(ctx, req, resp, r.client.Site) }` verbatim per PR-V's
      doc comment; `site` schema attribute `Optional+Computed` +
      `stringplanmodifier.RequiresReplace()`; `id` schema attribute
      `Computed` + identity.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(content_filtering): site identity via PR-V (C3)`.

---

## Task 4 [documentation/design only — no production Read stub]: Capability precheck design

**The prior draft's Task 4 is removed.** It proposed a production `Read`
method stub — an explicit compile-time placeholder or a panic/"not
implemented" body wired against a fabricated client interface method — to
exercise `v2FinishRead`'s NotFound handling ahead of the real SDK call. On
review this is neither meaningful coverage nor safely mergeable:
`v2FinishRead` (`unifi/v2_resource.go`) is **already independently unit-
tested in PR-V** — it is generic, table-testable against a fake `err`
without needing a resource-shaped caller at all, and content-filtering adds
no new behavior to it. A `Read` method stub whose only job is to call an
already-tested helper, wired against a client method that doesn't exist,
produces a shipped panic/TODO body in `unifi/*.go` for no test-coverage
benefit — exactly the kind of "neither meaningful coverage nor safely
mergeable" scaffolding the review flagged. It also risks the panic/stub
being left behind or miswired once the real SDK call lands, since nothing
forces its removal.

**Revised scope: this task is design documentation only, no Go code.**
Record, as a design note (this section, or a comment in the spec — not a
`.go` file):

- The C6 capability classification this resource uses: NotFound →
  `Unmaterialized`/absent via `v2FinishRead` (already correct and already
  tested by PR-V, requires no new code here); Method/endpoint unsupported →
  `Unsupported`; 401/403 → `Unauthorized`, fail not skip. (Spec's
  "Capability precheck" section, unchanged by this revision.)
- The *design* of the non-mutating acceptance-test precheck (read-only
  List/GET before any mutating test step; skip-with-reason on
  `Unsupported`; fail, not skip, on `Unauthorized`/transient) — as prose,
  to be turned into real code only in Task 8 once Task 7's List method
  exists for real.

**Read is wired for real — with an actual GET call and typed error
surface — only in Task 6, after Task 5's converter and the real SDK/HTTP
call exist.** No `Read` method, stub client interface, or placeholder body
is created by this task. If a reviewer wants to see `v2FinishRead`'s
NotFound behavior exercised, point to PR-V's own existing test coverage
(`unifi/v2_resource_test.go` or equivalent) rather than adding a redundant,
unwired copy here.

- [ ] **Step 1 (documentation only):** write the capability-classification
      note and the non-mutating precheck design above into this plan (or a
      short design-note file under `docs/superpowers/`), citing the spec's
      C6 section. No test, no `.go` file, no commit touching `unifi/*.go`.

---

## Task 5 [BLOCKED on wire-shape capture]: SDK converter

**Cannot start.** Requires the real go-unifi struct (or hand-rolled
request/response types once the endpoint/JSON shape is known) to write
`contentFilteringModelToSDK`/`sdkToContentFilteringModel` converters. Do not
scaffold plausible-looking field mappings — an incorrect guess here is worse
than an explicit gap, per the task's constraint against inventing wire
shapes.

**Unblock precondition:** spec's WIRE SHAPE STATUS path (1) or (2) resolved,
and the resulting struct/endpoint reviewed against this plan's Task 2 model
shape (updating Task 2's model if the real shape differs from the
recommended guess).

**When unblocked, this task's shape (fill in for real once unblocked):**
TDD converter tests using golden fixtures (privacy-safe synthetic values per
Global Constraints), one test per C1 ownership behavior from the spec's
decision matrix (`Managed` null→omit, empty→explicit-clear, value→send;
remote read always adopts) — mirroring the pattern `unifi/setting_codec_test.go`
or `firewall_policy_resource_test.go`'s conversion tests use.

---

## Task 6 [BLOCKED on Task 5]: Schema + CRUD wiring

**Cannot start.** Depends on Task 5's converter. Once unblocked: full
`Schema()` (assembling Tasks 1–4's pieces), `Create`/`Read`/`Update`/`Delete`
using PR-V's `v2Configure`, `v2Timeout`, `v2SetIdentityAndState`,
`v2FinishRead`, `objectAsOptions`/`objectListAsOptions` for nested
`safe_search`/`schedule` — mirroring `firewall_policy_resource.go`'s
Create/Read/Update/Delete structure exactly, substituting the content-
filtering SDK calls from Task 5.

Post-mutation reconcile (§5.2, PR-V convention): Create/Update re-read from
the controller (or trust the mutation response only per whatever PR-V's
established D/E convention turns out to be for the sibling PR — check
PR-D's implementation if it has landed first, since parent spec §399 notes
D and E are mergeable in either order and should follow the same PR-V
convention, not diverge).

TDD: failing CRUD tests against a fake client first, per repo convention
(check whether `firewall_policy_resource_test.go` uses a fake client/mock
transport or `httptest` — mirror whichever this package's existing v2
resources use, do not introduce a third test-double style).

---

## Task 7 [BLOCKED on Task 6, and on the same collection-shape proof Task 3 needs]: List resource (§5.9, PR-V's recorded rule)

**Cannot start** (needs Task 6's Read/converter). This task exists at all
only **if** the capture confirms collection-shape per Task 3's Step 0 gate —
if the capture instead shows a per-site singleton, this task is dropped
entirely (no `list.ListResource` for a singleton, per PR-V's own §5.9 rule,
which excludes BGP/`unifi_setting` on exactly this basis). Once unblocked
and collection-shape is confirmed: `contentFilteringListConfigModel` (`site`
+ `filter`, mirroring `firewallPolicyListConfigModel`/
`portForwardListConfigModel` exactly), `List` method,
`NewContentFilteringListResource()` constructor, registration in
`provider.go`'s `ListResources()`. Per PR-V's already-recorded §5.9 rule,
this is required for a collection-shaped resource and inapplicable to a
singleton — the rule itself is not re-litigated here, only which side of it
this resource falls on.

---

## Task 8 [BLOCKED on Task 7]: Acceptance tests (TF_ACC)

**Cannot start.** Once unblocked:

- Non-mutating capability precheck (Task 4's documented design, made real):
  skip with reason if the demo controller image lacks content filtering
  (`Unsupported`), fail (not skip) on `Unauthorized`/transient.
- Full lifecycle: create/read/update/delete against the ephemeral demo
  controller (`TestMain`-managed per session memory
  `acceptance-tests-colima-docker-host.md` — `DOCKER_HOST` under Colima).
- Cases from the spec's C1 ownership section, **testing the hypothesis, not
  assuming it**: `network_ids`/`client_macs` null (hypothesized
  stop-managing, controller value retained) vs explicit `[]` (hypothesized
  clears scope) — both must be present and distinct tests, not one
  conflated case, and the test's job is partly to confirm or refute which
  of the spec's three candidate wire semantics (full-replacement, PATCH-like,
  read-overlay-required) the controller actually implements, per capture
  plan step (e) and "Riskiest assumptions" #6.
- `safe_search`/`schedule` `OneOf` rejection at plan time (unit-level,
  doesn't need TF_ACC — can run as a plan-only `resource.Test` with
  `ExpectError`).
- Import equivalence under a non-default provider default site (§5.5).
- List resource: at least one `resource.Test`-style list-config test
  matching `firewall_policy`'s list-resource test pattern.
- **If the demo controller genuinely cannot exercise this feature** (parent
  spec's "Live validation" section notes gateway resources like this may
  need the cloud UDM test bed, not the demo controller) — document that
  explicitly in the test file's package doc and in the PR's completion
  report, same as PR-D is expected to for NAT. Do not silently skip without
  a documented reason.

---

## Task 9 [BLOCKED on Task 8]: Docs, example, changelog

**Cannot start** (needs the real schema to generate accurate docs). Once
unblocked:

- `examples/resources/unifi_content_filtering/{resource,import}.tf` and
  `examples/list-resources/unifi_content_filtering/` mirroring
  `examples/resources/unifi_firewall_policy/` structure.
- Regenerate `docs/resources/content_filtering.md` via the repo's doc-gen
  tooling (check `Makefile`/`tools/` for the exact command used by
  `docs/resources/setting.md` — do not hand-write; this repo's docs are
  generated).
- `CHANGELOG.md` Unreleased entry, house style (matching the entries already
  in `CHANGELOG.md`'s `[Unreleased]` section for #359/#361): what users
  could not previously do, controller behavior, provider behavior, the
  `OneOf` provider-bump note for `safe_search`/`schedule`, state/import
  notes. No dev-report residue, no process language (§9.2/§9.3 comment-
  hygiene discipline applied to the changelog too).
- Resource godoc on `contentFilteringResource` and its `Schema()` method
  citing this plan's spec sections, matching the style already used in
  `unifi/v2_resource.go` and `firewall_policy_resource.go`.

---

## Self-Review

1. **Spec coverage:** collection ownership (C1/§5.3) → Task 1 (enum tags,
   real code) + Task 5 (converter behavior + the null/empty wire-semantics
   hypothesis, both blocked); enum policy (§5.4) → Task 1 (safe search, not
   blocked) + Task 1b (schedule mode, blocked on literals); client+network
   scope semantics → spec's Open Decision #4 and "Riskiest assumptions" #5,
   surfaced as a TF_ACC test in Task 8 (blocked); capability (C6/§6.1) →
   Task 4 (design documentation only, no code) + Task 8 (real precheck,
   blocked); site identity (C3) → Task 3, **conditional** on the capture
   proving per-profile controller IDs exist (not unconditionally "not
   blocked" — see Task 3's Step 0 gate); list resource (§5.9) → Task 7
   (blocked on CRUD, and itself conditional on the same collection-shape
   proof Task 3 needs).
2. **What is genuinely not blocked and should start immediately:** Task 1
   (safe-search `OneOf` validator — the literal values are named directly
   in the parent spec, independent of every other open question). That is
   the only task in this revision producing mergeable, tested production
   code today. Tasks 2 and 4 produce *design artifacts* (a candidate model
   write-up, a capability-classification note) worth doing now for review
   value, but neither lands schema, model, or Read code in `unifi/*.go`.
   Task 3 (site identity) does not start at all until the capture proves
   per-profile IDs exist — it is gated, not merely "conditional scaffolding
   that compiles standalone."
3. **What is blocked and why:** Tasks 3 (pending per-profile-ID proof) and
   5–9 all require either the live-controller capture or an intermediate
   unblock it produces (hand-rolled wire types, confirmed cardinalities,
   confirmed collection-vs-singleton shape). None may be scaffolded with
   guessed field names, guessed cardinalities, or a guessed identity model
   per the task's explicit constraint — a wrong guess produces silent
   runtime failures or a state-schema migration against a real controller,
   which is worse than an honest capture-blocked gap. This revision
   specifically removes two places where the prior draft had scaffolded
   past this constraint: Task 2's production model struct, and Task 4's
   production Read stub.
4. **Placeholder scan:** every blocked task states its exact unblock
   precondition (Task 3: capture proof of per-profile IDs; Task 5:
   wire-shape capture generally; Tasks 6–9: the prior blocked task). No task
   says "similar to X" without naming which file (`firewall_
   policy_resource.go`/`port_forward_resource.go`) and which method to
   mirror.
5. **Type consistency (naming convention, not a commitment to these types
   existing yet):** *if* implemented, `contentFilteringModel`/
   `contentFilteringSafeSearchModel`/`contentFilteringScheduleModel`/
   `contentFilteringListConfigModel`/`contentFilteringListFilterModel`
   naming should follow the exact convention of `firewallPolicyModel`/
   `firewallPolicyEndpointModel`/`firewallPolicyListConfigModel`/
   `firewallPolicyListFilterModel`. Task 2's candidate write-up may use
   these names for illustration, but per Task 2's revised scope, none of
   these types are created as real `unifi/*.go` code before Task 6 — this
   item records a naming convention for later, not evidence that Tasks 2–7
   are already producing consistent, mergeable code today.
6. **Independence from PR-D:** no task references NAT types, NAT files, or
   assumes NAT has landed. Task 6 explicitly calls out checking PR-D's
   implementation only to confirm shared PR-V *convention* adherence
   (post-mutation reconcile pattern), never to import or depend on PR-D
   code.
