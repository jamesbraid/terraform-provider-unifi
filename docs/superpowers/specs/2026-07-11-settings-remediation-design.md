# Settings Expansion Remediation — Design

Date: 2026-07-11
Status: draft — revision 3 (addresses codex `spec-review` turns 1–2)

## Goal

Resolve every finding in `SETTINGS_EXPANSION_REVIEW.md` and re-deliver the
settings-expansion work as an upstream-ready series of independently reviewable
and mergeable PRs. This supersedes `review/settings-expansion`: the defects are
architectural, so the branch is rebuilt on a common lifecycle rather than
patched in place.

## Provenance and why a rebuild

The review was independently verified against the branch before this design was
written; all load-bearing claims confirmed (two lifecycle engines; legacy
writes commit before registry sections are decoded; per-section independent
`ListSettings`/`GetSetting`; import sets only `id`; empty-string and
malformed-number handling; NAT/firewall stale-selector serialization;
conversion diagnostics not halting state writes; `objectAsOptions` cross-file
coupling). A privacy sweep found two ObjectIDs the review missed —
`689ff798c4b72577507ae001`, `68945578bfcb5d2e51dd0f10`, both in
`firewall_policy_resource_test.go`.

SDK audit results (`jamesbraid/go-unifi` `provider-prereqs` @ v1.34.1) that
shape this design:

- `RawSetting` unmarshals into `BaseSetting` + `Data map[string]any`. Settings
  reads therefore never unmarshal a typed struct; the guest_access
  `Expire` string-vs-number defect is mooted by the raw-snapshot engine, not
  worked around.
- Ownership flags (`attr_hidden`/`attr_no_delete`/`attr_no_edit`) already exist
  on `BaseSetting` and on `nat.generated.go`. §5.6 (predefined/non-editable NAT
  rules) is a provider-side surfacing task; no SDK change.
- No optimistic-concurrency primitive exists (no version/ETag/rev on
  `BaseSetting`/`RawSetting`; `UpdateSetting` is a full PUT). Concurrency
  policy is therefore last-write-wins from the operation's single snapshot,
  with documented non-atomicity (see R1).

**Conclusion: PR-0 requires no go-unifi change.** The provider builds on the
existing v1.34.1. PR-0 is retained only as an explicit audit record.

The old branch remains as `review/settings-expansion` (archived). New work is
on `settings-expansion-v2`, branched from `main` (`039d97c6`, upstream v0.55.0).

## Scope

In: review §1–§10 — correctness/privacy (T1), architectural unification (T2),
restack into the A–F/V series (T3).

Out: mechanical `docs/**` regeneration (removed during work, regenerated once
at the end); the `jamesbraid/go-unifi` fork `replace` (expected during
co-development).

## Acceptance contract: the ten merge gates

Each gate splits into a **framework** obligation (the mechanism, owned once)
and an **adoption** obligation (each consumer using it). A gate holds when its
framework exists AND every resource merged so far adopts it. No later PR may
regress it.

| # | Gate | Framework owner | Adoption |
|---|------|-----------------|----------|
| 1 | One internal lifecycle per `unifi_setting` section | PR-A | all sections migrated in A; new sections in B follow it |
| 2 | One authoritative snapshot per operation | PR-A | A |
| 3 | Reconciliation complete before first mutation | PR-A | A |
| 4 | Universal preservation of unmodeled fields | PR-A | A (all sections) |
| 5 | Explicit value-ownership / clearing / unmanage semantics (C1) | PR-A | A; every B tranche tags its fields; C/D/E for their models |
| 6 | Site-aware import | PR-A (setting) + PR-V (v2 identity helper) | A (setting), D, E |
| 7 | Partial-apply and retry behavior | PR-A | A |
| 8 | Shared secret + capability policy (C1 + capability taxonomy) | PR-A | B3, B4 (secrets), B5, D, E (capability) |
| 9 | One discriminator contract (C4) | PR-A (mechanism) | B2, C, D, E |
| 10 | Whole-resource lifecycle tests | each PR | each PR |

## Core architecture

### C1. Field-ownership taxonomy — the value-semantics contract (gate 5)

Every schema leaf is tagged with exactly one ownership class. The shared codec
branches on class; no field carries ad-hoc null/empty/clearing logic.

**Field absence vs section absence.** A key missing *within* a present section
maps to Terraform null — a normal nullable/defaulted field, resolved per class
below. A whole *section* missing from the snapshot is a capability question
(C6), never silently treated as a set of null fields. "Remote absent" in the
matrix below is always field-level.

**Classes and their Terraform encoding:**

| Class | TF flags | Owner | Read-back |
|-------|----------|-------|-----------|
| `Managed` | Optional + Computed | user when set, controller when omitted | adopt remote |
| `CoManaged` | Optional + Computed + UseStateForUnknown | user and controller (controller mutates out-of-band) | adopt remote; churn documented |
| `Computed` | Computed | controller only | adopt remote |
| `WriteOnlySecret` | Optional + Sensitive (not Computed) | user | preserve prior; adopt only if field is `echoed` |
| `GeneratedSecret` | Computed + Sensitive + UseStateForUnknown | controller | adopt once; never user-set |
| `PreservedUnmanaged` | not in schema | controller | raw-merge preserved verbatim |

**Decision matrix** (config → outbound → inbound-state → diagnostic). "cfg
null" = attribute absent from configuration; "cfg empty" = present zero value.

| Class | cfg null | cfg empty | cfg value | cfg unknown | remote read → state |
|-------|----------|-----------|-----------|-------------|---------------------|
| `Managed` | omit from PUT (controller keeps value) | send empty (explicit clear) | send value | defer (UseStateForUnknown) | remote value |
| `CoManaged` | omit; keep prior | send empty | send value | defer | remote value; no churn on unrelated edits |
| `Computed` | n/a | n/a | n/a | defer | remote value |
| `WriteOnlySecret` | omit; **preserve prior state secret** | send empty (explicit clear/rotate-to-empty) | send value (set/rotate) | defer | prior state value; if remote masked/empty and no prior → null |
| `GeneratedSecret` | n/a | n/a | n/a | defer | remote value once; preserved after |
| `PreservedUnmanaged` | — | — | — | — | never dropped from the raw object |

**Contract rules the codec enforces (all classes):**

- **Present-empty ≠ absent.** A remote empty string/array is a value; a
  configured empty is serialized. (§3.1; retires the test pinning the broken
  behavior.)
- **Malformed remote is a diagnostic, not a normalization.** A fractional
  number where an int is modeled, a wrong scalar/container type, or a
  non-string list member raises an error and aborts state/remote mutation. No
  truncation, no member-dropping. (§3.2.)
- **JSON null vs Terraform null:** remote JSON `null` or an absent key maps to
  Terraform null; the codec distinguishes it from a present empty value.
- **Unknown config values are never serialized as literals;** Computed/
  CoManaged/secret classes use `UseStateForUnknown`.
- **Stop-managing vs clear:** removing an attribute from config (cfg null)
  reverts a `Managed` field to reading the controller value and stops sending
  it — the controller value is retained. Setting it to an explicit empty (cfg
  empty) sends the empty and clears it on the controller. These are distinct
  and separately tested.
- **One raw codec** for every registry section — no hand-rolled per-section
  null/empty checks (retires the `locale`/`snmp` variants). (§3.3.)
- **CoManaged drift:** a `CoManaged` field pinned in config re-asserts that
  value on every apply (last-writer = the apply); if the controller changed it
  out-of-band, refresh adopts the controller value into state and the next plan
  shows drift against config — intended. `UseStateForUnknown` only suppresses
  churn on *unrelated* edits, never on the field's own configured change.

**Secret sub-cases (resolving §2.8):**

- *Echoed vs masked:* by default a `WriteOnlySecret` never trusts the API and
  preserves the prior state value. A field known to echo its value carries an
  `echoed` annotation; only then is a real remote value adopted, while an
  empty/masked placeholder still preserves prior. Masking patterns are
  documented per field.
- *Rotation:* a changed `WriteOnlySecret` config value is an ordinary write; no
  special path.
- *Import without secret:* an imported resource has no prior state and the API
  returns no secret → the field is null; the user supplies it in config on the
  first managed apply. Documented.
- *Diagnostic redaction:* secret values never appear in diagnostics or logs.
- *Fail-closed (R2):* a secret is never overwritten with empty as a side effect
  of a failed apply; if neither a prior value nor an API value is available, the
  codec preserves prior (or null on import) and never emits a clearing PUT
  unless the user configured cfg-empty.

Adding a secret means tagging a field `WriteOnlySecret`/`GeneratedSecret`, never
extending a handwritten inventory.

### C2. One settings lifecycle engine (gates 1–4, 7)

`unifi_setting` keeps its consolidated public surface; one internal engine
serves all sections. Sections perform no I/O:

```
type settingSection interface {
    key() string
    schema() schema.Attribute
    ownership() map[string]ownershipClass          // C1
    decode(snap RawSettings, into *model) diag.Diagnostics
    overlay(cfg, prior model, snap RawSettings) (settings.RawSetting, diag.Diagnostics)
    capability(snap RawSettings) capabilityState   // C6
}
```

Orchestration (owned by the resource):

1. **One snapshot.** A single `ListSettings` per Create/Update/Read is passed
   to every section. `guest_access` and `magic_site_to_site_vpn` decode the
   shared snapshot (their independent reads were unnecessary). **Snapshot
   exception list: empty** — no section fetches independently for read/decode.
   Capability probes are the one *named* exception to "no independent fetch"
   (C6): they run in the pre-mutation capability stage, are enumerated
   per-section (**currently empty**), and any probe failure occurs before the
   first PUT. Any future read/probe exception is added to the relevant list
   with justification. (§2.3.)
2. **Reconcile before mutate.** All configured sections decode, validate, and
   produce candidate `RawSetting`s before the first PUT. Any conversion or
   validation error aborts before any controller write. (§2.2; §5.1 for
   settings decode.)
3. **Universal preservation.** Every write is a raw read-modify-write over the
   section's snapshot object: overlay only the fields C1 says to send; preserve
   all other keys verbatim, for legacy and new sections alike. (§2.4.)
4. **Partial-apply algorithm (§2.5):** PUT sections in deterministic registry
   order. On the first failure at section *k*: sections `1..k-1` are live,
   `k..n` untouched. Then perform **one final `ListSettings`** and decode all
   sections into state, so state reflects post-op reality (secrets preserved
   per C1). Surface a section-qualified error diagnostic. State is always the
   post-operation truth, making retry idempotent: the next apply re-attempts
   `k..n` from real current state. `unifi_setting` is documented as
   non-atomic. **If the final `ListSettings` itself fails** after one or more
   PUTs, state cannot be read back: write best-effort state = prior state with
   each successfully-PUT section replaced by the candidate values that were
   sent (secrets preserved per C1, unmodeled keys kept from prior), emit a
   *distinct* diagnostic instructing a `terraform refresh`, and mark the
   operation failed. The next refresh reconciles from a fresh snapshot; no
   secret is cleared by this path (R2).
5. **Post-mutation reconciliation source.** The final `ListSettings` after the
   PUT loop is canonical for state on both success and partial failure (also
   answers §5.2 for settings). No trusting of PUT response bodies.
6. **Legacy migration (§2.1).** All 13 typed sections move onto this engine;
   legacy Create/Update/Read code is deleted. **Migration is proved
   non-regressive by:** (a) request-level golden tests capturing each legacy
   section's current wire output *before* migration; (b) a
   permitted-delta list naming the intentional behavior changes (empty-string
   collapse → distinct; malformed-normalize → diagnostic; secret silent
   fallback → diagnostic; selective→universal preservation) with new
   assertions for the corrected behavior; (c) a coverage inventory mapping each
   legacy section × operation to its replacement codec/ownership. Behavior not
   on the permitted-delta list must be byte-identical.
7. **State compatibility.** The migration preserves every existing attribute
   name and type, so no state-schema upgrade is required for the 13 legacy
   sections; the engine swap is internal. Any attribute change is called out
   per-section and carries its own `UpgradeState` and serialized-old-state
   fixtures. The `unifi_setting` schema version bumps only if an attribute
   changes.
8. **Testability seam.** The engine depends on an injectable settings-client
   interface (a fake transport), so tests can force one-snapshot assertions,
   no-writes-before-reconcile, deterministic partial failure, masked secrets,
   malformed remote, controller normalization, and NotFound without a live
   controller. (Supports the §8 coverage list.)
9. **Structural registry tests (§2.10, gate 10):** unique keys;
   schema/model/ownership agreement; decode↔overlay round-trips; capability
   presence; complete resource wiring.
10. **Ownership boundary (§2.11):** each raw mapping is annotated permanent
    provider behavior or temporary SDK gap with a retirement condition.

### C3. Site identity and import (gate 6)

One contract; exact syntax:

| Resource | Import ID accepted | Persisted `id` | `site` | Change `site` |
|----------|--------------------|----------------|--------|---------------|
| `unifi_setting` | `<site>` (or `default`) | the site name | set from import | RequiresReplace |
| `unifi_nat_rule` | `<id>` or `<site>:<id>` | canonical `<site>:<id>` | derived from `id` | RequiresReplace |
| `unifi_content_filtering` | `<id>` or `<site>:<id>` | canonical `<site>:<id>` | derived from `id` | RequiresReplace |

- `unifi_setting` import sets `site` and hydrates **every registered section**
  into Computed state; hydrated values are observed (Computed + UseStateFor
  Unknown), not forced into configuration, so a subsequent plan with no config
  for them produces no diff. Adding config for a field opts it into management.
  This resolves both "import reads nothing" and "import silently manages
  observed config." (§2.7.)
- For NAT and content filtering the persisted Terraform `id` is the canonical
  `<site>:<id>`; `site` and the bare object id are computed from it. Every code
  path — import, Read, Update, Delete, timeouts, state upgrade — consumes this
  one composite identity, so no path can treat the object id as globally
  unique. (§5.5.)
- `<id>` (no prefix) on import uses the provider default site and is provably
  equivalent to `<default-site>:<id>`; both normalize to the same persisted
  `<site>:<id>`. Invalid/ambiguous input is a diagnostic, never a silent
  default. A shared `resolveSite` helper is used by all three; Read never
  rewrites identity.
- Equivalence tests run under a provider configured with a **non-default**
  default site. (§5.5.)

### C4. Discriminator contract (gate 9)

One mechanism wherever valid children depend on another field (NAT `type`, NAT
`filter_type`, firewall `matching_target`/`matching_target_type`/
`port_matching_type`, guest auth/payment provider, mDNS `mode` vs services,
`setting_preference`):

- **Contradictory config → plan-time error.** Configuring a child the active
  discriminator does not own is rejected by a validator (not deferred to the
  controller).
- **Stale prior-state children → cleared by a plan modifier** when the
  discriminator changes, *before* validation, so a legitimate transition (e.g.
  `FIREWALL_GROUPS`→`NETWORK_CONF`, `IP`→`APP`) does not error on leftover
  state. (§4.2, §4.3.)
- **Outbound normalization** serializes only the active discriminator's
  children; the firewall APP path clears stale *target data*, not just
  `matching_target_type`.
- **Imported/controller stale values** are normalized out of state on read when
  inactive; the discriminator is authoritative.
- **Shapes per discriminator value** are modeled explicitly (NAT
  DNAT/SNAT/MASQUERADE each have a defined valid field set), not a flat bag.
  (§4.4.)
- **Transitions are in-place updates** unless a controller constraint requires
  recreate (none identified). An unknown discriminator value defers child
  validation to apply.

### C5/C6 are folded into PR-V and the capability taxonomy below.

### C6. Capability taxonomy (gates 8 adoption, §2.9, §6.1, §6.2)

`capabilityState` ∈ {`Supported`, `Unsupported`, `Unmaterialized`,
`Unauthorized`, `Unknown`}. Source of truth:

- **Settings:** the snapshot's key presence establishes only that the section
  *exists*. Distinguishing `Supported` (present, real values) from
  `Unmaterialized` (present, defaults only) is decided from the section's own
  fields, or from an enumerated pre-mutation probe where the fields are
  insufficient (the C2.1 named exception; currently empty). A key absent on a
  product/version that should carry it → `Unsupported`.
- **v2 resources:** typed API errors — NotFound (`Unmaterialized`/absent),
  method/endpoint unsupported (`Unsupported`), 401/403 (`Unauthorized`).

Behavior:

- Configured + `Unsupported`/`Unauthorized` → predictable error diagnostic
  (never silent-null). (§2.9.)
- Not configured + `Unsupported` → omitted from state.
- `Unmaterialized` → treated as defaults; configuring materializes it.
- `Unknown` (capability indeterminate, e.g. a transient error): a *configured*
  section fails closed with a retryable diagnostic; a non-configured section is
  ignored.
- Test outcomes: `Unsupported` → skip with reason; `Unauthorized`/transient →
  fail, not skip. Prechecks are non-mutating; where write capability is the
  thing under test, the mutation lives in the managed test lifecycle, not an
  out-of-band probe. (§6.1, §6.2.)

## The PR series

Each PR is independently mergeable, leaves `main` production-safe, and carries
its own tests, example, changelog entry, and a compact review guide (single
purpose; invariants relied on; new invariant introduced; files with the
substantive logic; controller assumptions; focused risk tests). Within a PR,
commits run contract/tests → implementation → example/changelog; exploratory
and scaffolding commits are squashed out.

### PR-0 · go-unifi audit (no change)

Audit record: the SDK already provides NAT ownership flags and raw-snapshot
reads; `Expire` typing is mooted by the raw engine; no missing symbol. The
provider builds on `jamesbraid/go-unifi v1.34.1`. No go-unifi PR. Removes the
B4/D SDK dependency.

### PR-A · Settings lifecycle foundation — gates 1–5, 7, 10; framework for 6, 8, 9

Delivers C1 (taxonomy + one codec), C2 (engine: one snapshot, reconcile-before-
mutate, universal preservation, partial-apply, post-mutation reconcile, fake-
transport seam, structural registry tests, ownership-boundary annotations), C3
import for `unifi_setting`, and the C4/C6 *mechanisms* (validators/plan-
modifiers/capability types) with no new user sections. Migrates the 13 existing
sections under golden + permitted-delta + coverage-inventory tests. No
user-facing schema change → no state migration. Production-safe tip: existing
sections behave identically except for the named permitted deltas.

### PR-B · Settings tranches (adopt the foundation) — gates 6, 8, 9 per section

The 18 new sections, each tranche a separate PR that depends only on PR-A,
introduces no dependency on another Bn, and carries its own schema, C1 tags,
codec usage, lifecycle behavior, privacy-safe fixtures, example, tests, and
changelog. No tranche introduces a behavior a later tranche silently replaces.

- **B1 simple user-managed:** `locale`, `global_switch`, `global_nat`,
  `dashboard`, `traffic_flow`, `ether_lighting`, `netflow`, `ssl_inspection`,
  `ipsec`, `usg_geo`, `global_network`.
- **B2 preservation / co-managed + discriminators:** `mdns` (mode vs services,
  C4), `teleport` (enabled/subnet coupling), `magic_site_to_site_vpn`
  (`GeneratedSecret`), `radio_ai` (`CoManaged`, churn-documented).
- **B3 secret-bearing:** `snmp` (`WriteOnlySecret` community + v3).
- **B4 guest portal:** `guest_access` alone (large schema; payment/auth
  `WriteOnlySecret` surface; no SDK dependency after PR-0).
- **B5 capability-gated:** `provider_capabilities` (runtime C6 behavior).

### PR-C · Firewall APP matching — gate 9 adoption; §4.3/§4.5/§5.7/§5.8

Discriminator-transition normalization for endpoints (C4), semantic port
validation (reject 0, >65535, reversed ranges — §4.5), set-vs-list collection
semantics (§5.7), and state upgrade from serialized old-state fixtures (§5.8).
Self-contained; no dependency on V/D/E. Privacy scrub of
`firewall_policy_resource_test.go` (incl. the two missed IDs) lands here.

### PR-V · Shared v2-resource infrastructure — §5.10/§5.11

A dedicated small PR introducing `unifi/v2_resource.go`: the shared v2
lifecycle template (site resolution via C3, timeout preservation, conversion-
halts-state per §5.1, NotFound, post-mutation reconcile convention per §5.2,
identity) and `objectAsOptions`. It also records the §5.9 list-resource
eligibility rule that D and E each apply. **Both PR-D and PR-E depend only on
PR-V, not on each other** — resolving the D/E coupling, the
`objectAsOptions`-in-NAT defect, and the list-resource cross-dependency. D and
E are mergeable in either order once V is in.

### PR-D · NAT resource — §4.2/§4.4/§5.2/§5.6/§5.9/§6.1

Rule types as distinct shapes (C4/§4.4), filter discriminator normalization
(C4/§4.2), predefined/non-editable rules surfaced from existing SDK
`attr_*`/`nat.NoEdit` flags with predictable Read/Update/Delete (§5.6),
non-mutating capability precheck (C6/§6.1), site identity (C3), built on PR-V.
Real DNAT/SNAT acceptance coverage, not MASQUERADE-only. Applies PR-V's
recorded list-resource rule.

### PR-E · Content-filtering resource — §5.3/§5.4/§5.2/§5.9/§6.1

Collection ownership per C1 (§5.3), enum compatibility policy (below, §5.4),
client+network scope semantics documented from live behavior, capability (C6),
site identity (C3), built on PR-V, applies PR-V's list-resource rule. Compiles
and behaves identically whether or not PR-D is merged (both depend only on
PR-V).

## Cross-cutting policies and resolved decisions

- **Privacy scrub (§1).** Per owning PR, replace with synthetic values:
  `block shield DNS`; ObjectIDs `5dbaa47ea7986c04d72d4f5e`,
  `6068a1508bf47808f667f3e8`, `689ff798c4b72577507ae001`,
  `68945578bfcb5d2e51dd0f10`; color `0544ff`; `America/Vancouver`; the
  Home-Assistant/Sonos mDNS fixtures; the `192.168.2.1/24` teleport subnet.
  Comments describe the invariant, not the environment. A final
  case-insensitive sweep (24-char hex IDs, absolute user paths, household
  names, non-example domains, public addresses, credential-shaped strings) over
  retained tree and commit messages is a completion gate. The rebuilt series
  carries no `/Users/...` commit messages.
- **Enum compatibility policy (§5.4) — decided.** Safe-search
  (`GOOGLE`/`YOUTUBE`/`BING`) and content-filter schedule modes are closed,
  controller-validated sets: validate with `OneOf` and reject unknowns at plan
  time (better UX than a 400). Each carries a doc/changelog note that new
  controller values require a provider bump. Open, controller-evolving fields
  (if any are found) accept any string and defer to the controller — decided
  per field in the owning plan, defaulting to `OneOf` for anything the
  controller strictly validates.
- **List-resource decision (§5.9) — decided as a rule, owned by PR-V.** PR-V
  audits the repository's existing v2 resources (e.g. `firewall_policy`) and
  records one eligibility rule: if those expose a framework `ListResource`, NAT
  and content-filtering do too; if not, both are excluded under that rule. PR-D
  and PR-E each apply the *already-recorded* rule independently, so neither
  depends on the other.
- **Collection semantics (§5.7).** Order-meaningful → list; set-membership →
  set. Equivalent selector families use the same type across resources.
- **Changelog discipline (§F, §9.1).** One entry per PR, house style (what
  users could not do, controller behavior, provider behavior, state/compat/
  migration effects), no dev-report residue. The false "a failed settings read
  prevents any write" claim is removed. Changelog stays correct for any
  merged prefix/subset.
- **Comment hygiene (§9.2, §9.3).** Remove process residue (`Task 5's
  obligation`, `observed live`, internal names). Comments encode controller
  quirks and non-obvious wire mappings; lifecycle/capability/secret facts are
  structural, not narrated.
- **Validation gaps (§7).** SNMP password length, guest expiry, safe-search/
  schedule, firewall port semantics, and conditional NAT/firewall fields are
  enforced under C1/C4, not one-off checks.

## Risks and mitigations

- **R1 — concurrent-write races.** The controller settings API has no ETag/
  version; a whole-object PUT is last-write-wins. Mitigation: one snapshot per
  op minimizes the window; raw-merge preserves unmodeled keys; non-atomicity is
  documented; there is no conflict-retry to offer, and the design does not
  pretend otherwise.
- **R2 — secret loss after partial failure.** A failed apply must never clear a
  secret. Rule: `WriteOnlySecret` state comes from prior state; a masked/empty
  API value with no prior → null (import only); the codec never emits a clearing
  PUT unless the user configured cfg-empty. Tested via the fake transport.
- **R3 — capability detection limits.** Where `ListSettings` cannot distinguish
  unsupported from unmaterialized, an explicit, documented probe is permitted;
  otherwise C6 maps typed errors. No collapsing into one skip/diagnostic.
- **R4 — testability.** Without the fake-transport seam (C2.8) the §8 lifecycle
  cases cannot be tested deterministically; the seam is part of PR-A, not an
  afterthought.

## Testing strategy (§8)

Converter tests are retained but insufficient. Each PR adds stateful lifecycle
coverage for the behavior it introduces, including transitions from previously
merged state, using the fake transport. Series-wide coverage:
legacy+registry in one op; multiple registry sections in one op; one snapshot;
only-configured-sections written; failure before first write; failure after
partial writes; refresh/retry convergence; universal preservation; empty vs
absent; explicit clear vs stop-managing; malformed remote → diagnostic;
write-only secrets after refresh and after failed mutation; masked/echoed
secrets; unsupported vs absent sections; import into a non-default site;
removal of a configured section; upgrade from legacy-only state; NAT rule-type
and filter-type transitions; firewall target/port transitions; content-filter
omitted vs explicitly-empty collections; controller normalization after
mutation; malformed API results halting identity/state writes; predefined-NAT
import and attempted mutation; site and timeout preservation; NotFound.

## Live validation (distinct from automated tests)

Automated acceptance tests run **only** against the ephemeral docker demo
controller and never touch any real UDM. **Live validation is a separate,
manual step:** `tofu plan` (read-mostly) against the real UDM via
`~/ansible/infra/unifi`, expecting a no-op or exactly the intended drift, with
deliberate `apply` at the maintainer's discretion. Gates: after **A+B**
(settings core) and after **V+D+E** (gateway resources, which the demo
controller cannot exercise). *Open: whether a non-production UDM/test site is
available, or live validation is plan-first against production — maintainer's
call; does not block planning.*

## Review process for this cycle

1. **Spec** → `codex-review ask --name spec-review`, iterate via `reply` until
   sign-off → maintainer hand-review. (Current step.)
2. **Each PR plan** → `writing-plans` (one plan per PR) →
   `codex-review ask --name pr<X>-plan-review` until clean.
3. **Each PR implementation** → `subagent-driven-development` (per-task +
   whole-branch internal review) complemented by
   `codex-review review --base <prev-tip> --name pr<X>-impl-review` at the tip.
4. **Completion report (§10)** per PR: disposition of every merge gate; the
   final value-ownership/clearing/import/site-identity contracts; partial-apply
   and retry behavior; secret and capability policies; permanent-vs-temporary
   raw mappings; lifecycle tests added; privacy-scan result; exact verification
   commands and results (`gofmt -w`, `go test ./unifi/...`, `go vet ./unifi/...`,
   `git diff --check`); any coordinated go-unifi work (none expected). Items are
   not complete because converter tests or the pre-existing suite pass.

## Traceability

| Review section | Addressed by |
|---|---|
| §1 privacy (+2 missed IDs) | cross-cutting scrub per owning PR; sweep gate |
| §2.1 two engines | C2 / PR-A |
| §2.2 reconcile before mutate | C2.2 / PR-A |
| §2.3 one snapshot | C2.1 / PR-A |
| §2.4 universal preservation | C1/C2.3 / PR-A |
| §2.5 partial-apply & retry | C2.4 / PR-A |
| §2.6 value semantics | C1 matrix / PR-A |
| §2.7 import | C3 / PR-A |
| §2.8 secret policy | C1 secret sub-cases / PR-A, B3, B4 |
| §2.9 capability ≠ absence | C6 / PR-A, B5 |
| §2.10 registry verifiable | C2.9 / PR-A |
| §2.11 provider/SDK boundary | C2.10 / PR-A |
| §3.1 empty-string round-trip | C1 / PR-A |
| §3.2 malformed normalization | C1 / PR-A |
| §3.3 inconsistent overlays | C1 one-codec / PR-A |
| §4.1 discriminator contract | C4 mechanism / PR-A |
| §4.2 NAT stale selectors | C4 / PR-D |
| §4.3 firewall stale selectors | C4 / PR-C |
| §4.4 NAT rule shapes | C4 / PR-D |
| §4.5 port validation | PR-C |
| §5.1 diagnostics halt state | C2.2 / PR-A; PR-V / D, E |
| §5.2 post-mutation reconcile | C2.5 / PR-A; PR-V / D, E |
| §5.3 content-filter ownership | C1 / PR-E |
| §5.4 content-filter enums | decided (OneOf) / PR-E |
| §5.5 site identity | C3 / PR-A, D, E |
| §5.6 predefined NAT rules | existing SDK flags / PR-D |
| §5.7 collection semantics | cross-cutting / PR-C, D, E |
| §5.8 historical schemas | PR-C |
| §5.9 list-resource decision | rule owned by PR-V; applied by PR-D, E |
| §5.10 objectAsOptions coupling | PR-V |
| §5.11 v2 lifecycle drift | PR-V |
| §6.1 non-mutating prechecks | C6 / PR-D, E |
| §6.2 runtime capability | C6 / PR-A, B5, D, E |
| §7 schema/validation gaps | C1/C4 / owning PRs |
| §8 test architecture | testing strategy + C2.8 seam / every PR |
| §9 changelog & comments | cross-cutting / every PR |
| §10 verification & report | review process / every PR |

## Remaining open items (non-blocking for planning)

- Whether the `codex-review` bridge is committed to the branch or kept as local
  tooling (maintainer preference).
- Live-validation target (non-production UDM vs plan-first against production).
