# Settings Expansion Remediation — Design

Date: 2026-07-11
Status: draft (pending codex review, then maintainer review)

## Goal

Resolve every finding in `SETTINGS_EXPANSION_REVIEW.md` and re-deliver the
settings-expansion work as an upstream-ready series of independently
reviewable and mergeable PRs. This supersedes `review/settings-expansion`:
the review-grade defects are architectural, so the branch is rebuilt on a
common lifecycle rather than patched in place.

## Provenance and why a rebuild

The review was independently verified against the branch code before this
design was written. All load-bearing claims were confirmed:

- Two lifecycle engines coexist in `setting_resource.go` (legacy typed PUTs →
  `applySections` registry → `readSettings`). Confirmed.
- Legacy sections commit writes before any registry section is decoded or
  validated (the review's "reconciliation before mutation" concern; its
  narrower wording about registry-PUT-before-preflight is imprecise, but the
  cross-engine ordering defect is real). Confirmed.
- No authoritative snapshot: `guest_access` and `magic_site_to_site_vpn` each
  issue their own `ListSettings`; every legacy section issues its own
  `GetSetting`. Confirmed.
- Import sets only `id`, never `site`; an imported resource hydrates nothing.
  Confirmed.
- Empty-string round-trip asymmetry, silent malformed-number truncation, NAT
  and firewall stale-selector serialization, conversion diagnostics not
  halting state writes, and `objectAsOptions` cross-file coupling. All
  confirmed.

A privacy sweep additionally found **two real-looking controller ObjectIDs the
review missed** — `689ff798c4b72577507ae001` and `68945578bfcb5d2e51dd0f10`,
both in `firewall_policy_resource_test.go`.

The old branch remains as `review/settings-expansion` (archived) for
old-vs-new diffing. New work lands on `settings-expansion-v2`, branched from
`main` (`039d97c6`, upstream v0.55.0).

## Scope

In scope: all review sections §1–§10 — correctness/privacy (T1),
architectural unification (T2), and restack into the independently-mergeable
PR series A–F (T3).

Out of scope: mechanical `docs/**` regeneration (removed during work,
regenerated once at the end); the `github.com/jamesbraid/go-unifi` fork
`replace` (expected while provider and SDK evolve together).

## Acceptance contract: the ten merge gates

Every gate must hold at the tip of the PR that owns it, and must not regress
in later PRs. The owning PR is named; consumers reuse the established
invariant.

| # | Gate | Owner | Consumers |
|---|------|-------|-----------|
| 1 | One internal lifecycle for every `unifi_setting` section | A | B* |
| 2 | One authoritative settings snapshot per operation | A | B* |
| 3 | Complete local reconciliation before first mutation | A | B* |
| 4 | Universal preservation of unmodeled remote fields | A | B* |
| 5 | Explicit value-ownership / omission / clearing / unmanage semantics | A | B*, C, D, E |
| 6 | Correct site-aware import behavior | A (setting) | D, E (identity) |
| 7 | Defined partial-apply and retry behavior | A | B* |
| 8 | Shared secret and controller-capability policies | A (framework) | B3, B4 (secrets), D, E (capability) |
| 9 | One provider-wide discriminator contract | A (mechanism) | B2, C, D, E |
| 10 | Whole-resource lifecycle tests, not only converter tests | every PR | — |

## Core architecture

### C1. Field-ownership taxonomy (the value-semantics contract) — gate 5

Every schema leaf declares exactly one ownership class. The shared codec
branches on class; no field gets ad-hoc null/empty/clearing handling. This is
the single mechanism that resolves §2.4, §2.6, §3.1, §3.2, §3.3, and the
secret half of §2.8.

| Class | Config | Absent-in-config | Empty vs absent remote | Clearing | Preserved on read |
|-------|--------|------------------|------------------------|----------|-------------------|
| `Managed` | Optional/Required | not written (absent ≠ empty) | distinct: present-empty round-trips, absent omits | explicit empty/zero is sent | echoes remote |
| `Defaulted` | Optional+Computed | controller default used | distinct | reset = omit → controller default | echoes remote |
| `Computed` | Computed | n/a | read-only | n/a | UseStateForUnknown |
| `CoManaged` | Optional+Computed | controller may change independently | distinct | last-writer; documented churn | UseStateForUnknown |
| `WriteOnlySecret` | Optional, Sensitive | not written | never read back | explicit clear vs preserve is distinguished | preserved from prior state, never from API |
| `PreservedUnmanaged` | not in schema | — | raw-merge preserved verbatim | — | never dropped |

Contract rules the codec enforces for all classes:

- **Present-empty ≠ absent.** A remote empty string/array is a value, not
  null; a configured empty is serialized. Fixes §3.1 (and retires the test
  that pins the broken behavior).
- **Malformed remote is an error, not a normalization.** A fractional number
  where an int is modeled, a wrong scalar/container type, or a non-string list
  member raises a diagnostic and aborts state/remote mutation. No silent
  truncation or member-dropping. Fixes §3.2.
- **One raw codec.** All registry sections share the same null/unknown/empty/
  malformed/collection/secret handling; no hand-rolled per-section variants
  (retires the `locale`/`snmp` bespoke checks). Fixes §3.3.
- **Secrets never silently fall back.** A secret-preservation failure produces
  a diagnostic; it never returns a fresh object that drops prior write-only
  material. Adding a secret means tagging a field `WriteOnlySecret`, not
  extending a handwritten inventory + prose. Fixes §2.8.

`Optional+Computed` is no longer a general-purpose compatibility setting; it
is the concrete encoding of `Defaulted`/`CoManaged` only.

### C2. One settings lifecycle engine — gates 1–4, 7

The `unifi_setting` resource keeps its consolidated public surface but runs
one internal engine for all sections. The `settingSection` interface is
redesigned so sections never perform I/O:

```
type settingSection interface {
    key() string
    schema() schema.Attribute
    ownership() map[string]ownershipClass   // C1 taxonomy
    decode(snapshot RawSettings, into *model) diag.Diagnostics   // read
    overlay(cfg model, snapshot RawSettings) (RawSetting, diag.Diagnostics) // write
    capability(snapshot RawSettings) capabilityState             // C4/§2.9
}
```

Orchestration (owned by the resource, not sections):

1. **One snapshot.** A single `ListSettings` per Create/Update/Read; the
   snapshot is passed to every section. No section fetches independently.
   `guest_access` and `magic_site_to_site_vpn` decode the shared snapshot; a
   genuinely distinct endpoint may remain an explicit, documented exception.
   Gate 2, §2.3.
2. **Reconcile before mutate.** All configured sections are decoded,
   validated, and overlaid into candidate `RawSetting`s before the first PUT.
   Any conversion/validation error aborts before any controller write. Gate 3,
   §2.2, and §5.1 applied to settings decode.
3. **Universal preservation.** Every write is a raw read-modify-write merge
   over the section's snapshot object; unmodeled keys are preserved verbatim
   for all sections, legacy and new. Gate 4, §2.4.
4. **Partial-apply contract.** PUTs run in a deterministic order; a failure
   yields a section-qualified error, writes the state that did land, and
   documents that `unifi_setting` is not atomic. Retry converges from a fresh
   snapshot. Behavior of write-only secrets and co-managed values after a
   failed op is defined. Gate 7, §2.5.
5. **Legacy migration.** All 13 existing typed sections move onto this engine;
   the legacy Create/Update/Read code is deleted. Characterization tests
   capturing each legacy section's current wire behavior are written **first**,
   so the migration is provably non-regressive. Gate 1, §2.1.
6. **Structural registry tests.** The registry is the authoritative manifest:
   tests prove unique keys, schema/model/ownership compatibility, decode↔overlay
   round-trips, capability presence, and complete resource wiring. §2.10, gate
   10.
7. **Ownership boundary.** Each raw mapping is annotated as permanent provider
   behavior or a temporary SDK gap with a retirement condition; typed and raw
   decoders for one section do not both remain sources of truth. §2.11.

### C3. Site identity and import — gate 6, §5.5

One contract for all site-scoped resources:

- `unifi_setting` imports **by site**: import sets the `site` attribute and a
  full read hydrates every *registered* section (not only configured ones), so
  an imported resource has real content and cannot silently read the default
  site. §2.7.
- `unifi_nat_rule` / `unifi_content_filtering` use a composite `site:id`
  identity persisted consistently in state; default-site and explicit-site
  imports are provably equivalent, tested under a provider configured with a
  non-default default site. §5.5.
- A single site-resolution helper is shared by all three; provider default is
  the documented fallback, never a silent site switch.

### C4. Discriminator contract — gate 9, §4.1

One mechanism applied everywhere a field's valid children depend on another
field (NAT `type`, NAT `filter_type`, firewall `matching_target` /
`matching_target_type` / `port_matching_type`, guest auth/payment provider,
mDNS `mode` vs service lists, controller `setting_preference`):

- Contradictory configuration is rejected at plan time (validators), not
  deferred to controller errors.
- Inactive fields are **not serialized** — outbound normalization drops
  children that the active discriminator does not own. Fixes §4.2 (NAT filter
  transitions) and §4.3 (firewall endpoint transitions); the firewall APP
  helper must clear stale *target data*, not only `matching_target_type`.
- Stale prior state does not remain active across a discriminator change.
- Imported/controller-populated stale fields normalize predictably.
- Rule/endpoint *shapes* are modeled per discriminator value, not as a flat
  bag of independently-optional fields. Fixes §4.4 (NAT rule types).

### C5. Shared v2-resource lifecycle template — §5.11

`unifi_nat_rule` and `unifi_content_filtering` share one lifecycle contract so
site resolution, timeout preservation, conversion-halts-state (§5.1),
NotFound, post-mutation reconciliation (§5.2), and identity do not drift per
resource. `objectAsOptions` and any other genuinely shared helper move to a
shared file so content filtering compiles without the NAT file (§5.10). The
template defines whether a v2 mutation response is canonical or requires a
post-mutation read (§5.2) — one convention, not per-resource guesswork.

## The PR series

Each PR is independently mergeable, leaves `main` production-safe, and carries
its own tests, example, changelog entry, and a compact review guide (single
purpose, invariants relied on, new invariant introduced, files with the
substantive logic, controller assumptions, focused risk tests). Commits within
a PR follow contract/tests → implementation → example/changelog; exploratory
and dependency scaffolding is squashed out.

### PR-0 · go-unifi SDK prerequisites (fork; tag `v1.35.0`)

SDK-level fixes that must not be provider workarounds:

- Fix `GuestAccess.Expire` typing (controller sends a JSON number; the field
  is typed `string`), retiring guest_access's raw-read workaround.
- Add NAT rule ownership flags (`predefined` / `hidden` / `editable` /
  `deletable` or the SDK's actual names) needed for §5.6.
- Enumerate any other symbol used by the provider PRs and confirm it exists.

Rebased on upstream go-unifi (already current at v1.34.1 + this delta).
Precedes PR-B4 and PR-D. Exact contents finalized by auditing the SDK before
PR-A starts.

### PR-A · Settings lifecycle foundation — gates 1–5, 7, 10

Delivers C1 (taxonomy + codec), C2 (engine, one snapshot, reconcile-before-
mutate, universal preservation, partial-apply, capability model), C3 import
for `unifi_setting`, structural registry tests, and the ownership-boundary
annotations. Migrates the 13 existing sections under characterization tests.
Adds no large new user-facing surface — its review purpose is architectural.

### PR-B · Settings tranches (on the foundation pattern) — gates 6, 8, 9 per section

The 18 new sections, grouped by shared lifecycle profile. Each tranche is a
separate PR with complete schema, codec, ownership tags, lifecycle behavior,
privacy-safe fixtures, example, tests, changelog. No tranche introduces a
temporary behavior a later tranche silently replaces.

- **B1 — simple user-managed:** `locale`, `global_switch`, `global_nat`,
  `dashboard`, `traffic_flow`, `ether_lighting`, `netflow`, `ssl_inspection`,
  `ipsec`, `usg_geo`, `global_network`.
- **B2 — preservation / co-managed + discriminators:** `mdns` (mode vs service
  lists), `teleport` (enabled/subnet coupling), `magic_site_to_site_vpn`
  (controller-generated secret), `radio_ai` (co-managed, churn-documented).
- **B3 — secret-bearing:** `snmp` (community + v3 credentials).
- **B4 — guest portal:** `guest_access` alone (large schema, payment/auth
  secret surface; depends on PR-0).
- **B5 — capability-gated:** `provider_capabilities` (runtime capability
  behavior per C4/§2.9, not test-skip-only).

### PR-C · Firewall APP matching — gate 9, §4.3/§4.5/§5.7/§5.8

Complete discriminator-transition behavior for endpoints (stale-selector
normalization), semantic port validation (reject 0, >65535, reversed ranges —
§4.5), set-vs-list collection semantics (§5.7), and state upgrade driven by
serialized old-state fixtures rather than a mutated current schema (§5.8). SDK
symbols already present. Self-contained; no dependency on D or E. Privacy
scrub of `firewall_policy_resource_test.go` (including the two IDs the review
missed) lands here.

### PR-D · NAT resource — §4.2/§4.4/§5.2/§5.6/§5.9/§6.1

Complete resource: rule types as distinct shapes (§4.4), filter discriminator
normalization (§4.2), predefined/non-editable rule representation using PR-0
flags with predictable Read/Update/Delete on firmware-managed rules (§5.6),
non-mutating capability precheck (§6.1), post-mutation reconciliation
convention (§5.2), list-resource decision (§5.9), site identity (C3),
conversion-halts-state (§5.1). Introduces C5 (shared v2 template) and relocates
`objectAsOptions` to shared infra (§5.10). A real DNAT/SNAT acceptance path,
not MASQUERADE-only (§D).

### PR-E · Content-filtering resource — §5.3/§5.4/§5.2/§5.9/§6.1

Complete resource: collection ownership per C1 (§5.3), enum compatibility
policy for safe-search and schedule modes (§5.4), client+network scope
semantics documented from live behavior, capability behavior, site identity
(C3), post-mutation reconcile and lifecycle via C5. Compiles and behaves
identically whether or not PR-D is merged first.

## Cross-cutting policies

- **Privacy scrub (§1).** Replace with unmistakably synthetic values, per file,
  in the PR that owns it: `block shield DNS`; the ObjectIDs
  `5dbaa47ea7986c04d72d4f5e`, `6068a1508bf47808f667f3e8`,
  `689ff798c4b72577507ae001`, `68945578bfcb5d2e51dd0f10`; color `0544ff`;
  `America/Vancouver`; the Home-Assistant/Sonos mDNS fixtures; the
  `192.168.2.1/24` teleport subnet. Comments describe the invariant, not the
  source environment. A final case-insensitive sweep (24-char hex IDs,
  absolute user paths, household names, non-example domains, public addresses,
  credential-shaped strings) is a completion gate across retained tree and
  retained commit messages. The rebuilt series carries no
  `/Users/jamesb/...`-bearing commit messages.
- **Changelog discipline (§F, §9.1).** Each PR adds only its own entry, in the
  repository's detailed house style (what users could not do, controller
  behavior, resulting provider behavior, state/compat/migration effects), free
  of dev-report residue (probe logs, SDK TODO narration, field inventories).
  The false "a failed settings read prevents any write" claim is removed. The
  changelog stays correct if only a prefix/subset of PRs is merged.
- **Comment hygiene (§9.2, §9.3).** Remove process residue (`Task 5's
  obligation`, `observed live`, internal names). Comments encode controller
  quirks, state semantics, security behavior, and non-obvious wire mappings;
  lifecycle/capability/secret facts are represented structurally, not narrated.
- **Collection semantics (§5.7).** Order-meaningful → list; set-membership →
  set. Equivalent selector families use the same type across resources.
- **List-resource decision (§5.9).** NAT and content filtering either follow
  the repo's modern list-resource pattern or are excluded by one explicit,
  consistent eligibility rule, stated in their review guides.
- **Capability is runtime behavior (§6.1, §6.2).** Prechecks are non-mutating;
  where write capability is the thing under test, the mutation belongs to the
  managed test lifecycle, not an out-of-band probe. Unsupported / unauthorized
  / rejected / transient failures do not collapse into one skip or one
  diagnostic.
- **Validation gaps (§7).** SNMP password length, guest expiry, content-filter
  safe-search/schedule, firewall port semantics, and the conditional
  NAT/firewall fields are enforced under C1/C4, not as one-off checks.

## Testing strategy (§8)

Converter tests are retained but insufficient. Each PR adds stateful lifecycle
coverage for the behavior it introduces, including transitions from
previously-merged state. Across the series the suite must cover: legacy+registry
sections in one operation; multiple registry sections in one operation; one
authoritative snapshot; only-configured-sections written; failure before first
write; failure after partial writes; refresh/retry convergence; universal
preservation; empty vs absent; explicit clearing vs stop-managing; malformed
remote; write-only secrets after refresh and after failed mutation; masked/
echoed secret responses; unsupported vs absent sections; import into a
non-default site; removal of a previously-configured section; upgrade from
legacy-only state; NAT rule-type and filter-type transitions; firewall target
and port transitions; content-filter omitted vs explicitly-empty collections;
controller normalization after mutation; malformed API results halting
identity/state writes; predefined-NAT import and attempted mutation; site and
timeout preservation; NotFound.

## Live-test gates

- **After A + B**, validate the settings core against the real UDM: no-op plan
  on already-managed sections, explicit-clear behavior, secret preservation
  across refresh, capability behavior on unsupported sections.
- **After D + E**, validate the new gateway resources (NAT DNAT/SNAT,
  content-filtering) against the UDM — they cannot be validated on the demo
  controller, which lacks the v2 endpoints.

Live testing uses the existing `~/ansible/infra/unifi` path; tests never mutate
the live UDM.

## Review process for this cycle

1. **Spec** → `scripts/codex-review ask --name spec-review` → address findings,
   `reply` until codex signs off → maintainer hand-review. (This step.)
2. **Each PR plan** → `writing-plans` produces one plan per PR →
   `codex-review ask --name pr<X>-plan-review` → address until clean.
3. **Each PR implementation** → `subagent-driven-development` with its internal
   per-task and whole-branch reviews, complemented by
   `codex-review review --base <prev-tip> --name pr<X>-impl-review` at the PR
   tip.
4. **go-unifi PR-0** lands and is tagged before the provider PR that needs it.
5. **Completion report (§10)** per PR: disposition of every merge gate; the
   final value-ownership, clearing, import, and site-identity contracts;
   partial-apply/retry behavior; secret and capability policies; which raw
   mappings are permanent vs temporary SDK gaps; lifecycle tests added; privacy
   scan result; exact verification commands and results
   (`gofmt -w`, `go test ./unifi/...`, `go vet ./unifi/...`,
   `git diff --check`); any coordinated go-unifi work. Items are not marked
   complete because converter tests or the pre-existing suite pass.

## Traceability

| Review section | Addressed by |
|---|---|
| §1 privacy (+2 missed IDs) | cross-cutting scrub, per owning PR; sweep gate |
| §2.1 two engines | C2 / PR-A |
| §2.2 reconcile before mutate | C2 / PR-A |
| §2.3 one snapshot | C2 / PR-A |
| §2.4 universal preservation | C1/C2 / PR-A |
| §2.5 partial-apply & retry | C2 / PR-A |
| §2.6 value semantics | C1 / PR-A |
| §2.7 import | C3 / PR-A |
| §2.8 secret policy | C1 / PR-A, B3, B4 |
| §2.9 capability ≠ absence | C2/C4 / PR-A, B5 |
| §2.10 registry verifiable | C2 / PR-A |
| §2.11 provider/SDK boundary | C2 / PR-A, PR-0 |
| §3.1 empty-string round-trip | C1 / PR-A |
| §3.2 malformed normalization | C1 / PR-A |
| §3.3 inconsistent overlays | C1 / PR-A |
| §4.1 discriminator contract | C4 / PR-A mechanism |
| §4.2 NAT stale selectors | C4 / PR-D |
| §4.3 firewall stale selectors | C4 / PR-C |
| §4.4 NAT rule shapes | C4 / PR-D |
| §4.5 port validation | PR-C |
| §5.1 diagnostics halt state | C2/C5 / PR-A, D, E |
| §5.2 post-mutation reconcile | C5 / PR-D, E |
| §5.3 content-filter ownership | C1 / PR-E |
| §5.4 content-filter enums | PR-E |
| §5.5 site identity | C3 / PR-A, D, E |
| §5.6 predefined NAT rules | PR-0, PR-D |
| §5.7 collection semantics | cross-cutting / PR-C, D, E |
| §5.8 historical schemas | PR-C |
| §5.9 list-resource decision | PR-D, E |
| §5.10 objectAsOptions coupling | C5 / PR-D |
| §5.11 v2 lifecycle drift | C5 / PR-D, E |
| §6.1 non-mutating prechecks | PR-D, E |
| §6.2 runtime capability | C2 / PR-A, B5, D, E |
| §7 schema/validation gaps | C1/C4 / owning PRs |
| §8 test architecture | testing strategy / every PR |
| §9 changelog & comments | cross-cutting / every PR |
| §10 verification & report | review process / every PR |

## Open items to finalize before plans

- PR-0 exact contents — audit `jamesbraid/go-unifi` for the `Expire` fix and
  NAT ownership flags; confirm no other missing symbols.
- Whether the `codex-review` bridge/script is committed to the branch or kept
  as local tooling.
- Live-test sequencing detail (single UDM window vs two) — maintainer's call.
