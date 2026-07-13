# PR-E: Content-Filtering Resource — Design

Date: 2026-07-13
Status: **draft, capture-first.** This is a design + playbook document, not an
implementation-ready spec. There is no go-unifi content-filtering struct,
endpoint, or field name anywhere in the SDK (see WIRE SHAPE STATUS). No
schema, model type, or converter code may land until a live-controller
capture (see "Live-controller capture plan" below) establishes the actual
collection shape, item identity, and field cardinalities. Everything in this
document short of that capture is either (a) a wire-shape-independent
structural decision carried over unmodified from the parent spec/PR-V, or (b)
an explicitly labeled hypothesis/candidate to be checked against the capture,
never a settled design.

Revision note (this pass): revised per an independent design review
(verdict: NEEDS-WORK) that found several places where an unverified guess had
been written as though it were a decided fact. Fixes applied: Task 2's model
demoted to hypothesis-only; Task 3's per-item import identity made
conditional on proof of per-profile controller IDs; the C1 null/empty write
semantics reclassified as a controller-behavior hypothesis; the
`objectAsOptions`/`objectListAsOptions` applicability claim corrected; the
`client_macs` MAC-custom-type rationale downgraded from "existing convention"
to "option to validate against capture" (verified against
`unifi/firewall_policy_resource.go`: its `client_macs`/`network_ids` are both
plain `types.List{ElemType: types.StringType}`, not `hwtypes.MACAddress`);
and a turn-key live-controller capture plan added so Tasks 5–9 have a
concrete unblock path instead of an open-ended "get a HAR someday."

Parent spec: `docs/superpowers/specs/2026-07-11-settings-remediation-design.md`
§399 (PR-E definition) + §5.3 (collection ownership), §5.4 (enum
compatibility), §5.2 (post-mutation reconcile), §5.9 (list-resource rule),
§6.1 (capability precheck), C3 (site identity).

## Goal

Introduce `unifi_content_filtering`, a collection-shaped v2 resource managing
per-site content-filtering profiles (safe-search enforcement, category/rule
blocking, and scheduling) scoped to clients and/or networks, built entirely on
`unifi/v2_resource.go` (PR-V). Independently mergeable before or after PR-D;
both depend only on PR-V (parent spec §399, §386–388).

This is a **design-level spec**. The exact wire shape (JSON field names, the
v2 endpoint path, request/response bodies) could not be determined from the
SDK and is flagged below rather than invented, per instruction. Sections that
do not depend on the wire shape — enum policy and capability-handling shape —
are fully specified and are not blocked. Ownership taxonomy and site identity
are specified as the leading candidate but are **conditional**: ownership's
wire-level null/empty behavior and identity's per-item model both depend on
facts only the live-controller capture (below) can establish — see "Riskiest
assumptions to validate" for the full list of what is and is not settled.

## WIRE SHAPE STATUS

**No content-filtering struct, endpoint, or JSON field name exists anywhere
in `go-unifi@v1.33.43-0.20260706191309-bc63776a9ebf`.** This was verified, not
assumed:

- `grep -rilE 'content.?filter|dns.?filter|safe.?search|web.?filter'` over
  `unifi/` (module root, all subpackages) returns exactly one file:
  `unifi/settings/ips.generated.go`. That file is the existing `unifi_setting`
  `ips` section (IPS/IDS threat management), already implemented in this repo
  (`unifi/setting_section_ips.go`). Its two matches —
  `AdvancedFilteringPreference` (`manual|disabled`) and
  `ContentFilteringBlockingPageEnabled` (bool) — are a toggle for whether the
  *IPS* subsystem shows a blocking page and an IPS filtering-preference
  switch. They are not a content-filtering profile: no category list, no
  safe-search field, no schedule, no client/network scope. **This PR does not
  touch them; they stay owned by `unifi_setting`'s `ips` section.**
- `grep -rn "^type.*Filter" unifi/*.go` finds only `NatDestinationFilter` and
  `NatSourceFilter` (NAT connection-tracking filters, unrelated to content
  filtering).
- No file in `unifi/settings/` is named or shaped for content filtering (full
  directory listing checked: `auto_speedtest`, `country`, `doh`, `dpi`,
  `ips`, `radius`, `usg`, etc. — no `content_filter`, `dns_filter`, or
  `parental_control` file).
- No v2 REST path in the SDK (`grep -rhoE '"v2/api/site/...'`) references
  filtering, DNS blocking, or safe-search.
- go-unifi is frozen at controller 9.5.21 codegen (per
  `docs/superpowers/specs/2026-07-11-settings-remediation-design.md` and prior
  session memory `unifi-settings-api-facts.md`). UniFi's content-filtering
  profile UI/API may postdate that codegen, may live under an endpoint the
  generator's `ace.jar` field dump didn't cover, or may be a v2-only endpoint
  never modeled in typed Go at all (the parent spec repeatedly notes the v2
  settings API is undocumented).

**Conclusion: the resource cannot be implemented against this SDK version as
of this spec.** Two unblocking paths, neither taken by this PR:

1. A live controller capture (authenticated HAR/devtools trace of the UniFi
   OS "Content Filtering" or "Traffic & Device / DPI" profile UI creating,
   editing, and deleting a profile) to learn the real endpoint and JSON
   shape, then either (a) hand-roll request/response structs in the provider
   the way `unifi/dns_record.go` supplements `dns_record.generated.go`, or
   (b) upstream the struct into `go-unifi` first (the PR-0 precedent: the
   parent spec's audit found no SDK gap for NAT, but explicitly allows for
   one here).
2. A go-unifi bump past this frozen SDK version, if a later upstream codegen
   run picks up the endpoint.

Everything below this section is written so that once (1) or (2) resolves the
wire shape, most of the **converter** (Terraform model ↔ SDK struct) and the
SDK-facing halves of Create/Read/Update/Delete can be filled in with minimal
rework — the enum policy and capability-handling shape are decided now and do
not change regardless of what the capture shows. The schema's exact attribute
cardinalities (Task 2) and the identity/list-resource model (this section and
"Site identity + list resource" below) are **not** decided yet: both are
explicitly conditional on the capture, and are written below as the leading
candidate/hypothesis, not a locked-in design, per the fixes applied in this
revision.

## Resource shape (design-level, wire-shape-independent) — collection-shape assumption pending capture

**Working assumption, not yet proven:** this design assumes
`unifi_content_filtering` is collection-shaped (many profiles per site, each
with its own id) — the same shape as `unifi_firewall_policy` and
`unifi_port_forward`, not a per-site singleton like `unifi_setting` or BGP.
This assumption comes from the parent spec naming the resource alongside
firewall_policy/NAT in its PR list and identity table, not from any captured
payload or SDK evidence (there is none — see WIRE SHAPE STATUS). **If proven
correct, it drives every structural decision below (list-resource, `site:id`
identity, per-item CRUD) exactly as written.** If the capture instead shows a
per-site singleton (one settings object per site, at most a couple of
list-valued fields inside it), the identity/list-resource/per-item-CRUD
decisions below do not apply and must be replaced with the singleton pattern
(`unifi_setting`/BGP shape) before implementation — see "Site identity + list
resource" below for the concrete fork.

### Schema (attribute-level, names indicative pending wire capture)

| Attribute | Type | Ownership (C1) | Notes |
|---|---|---|---|
| `id` | `types.String` (Computed, identity) | n/a | canonical `<site>:<id>`, C3 |
| `site` | `types.String` (Optional+Computed) | n/a | C3; RequiresReplace on change |
| `name` | `types.String` (Required) | `Managed` | profile display name |
| `enabled` | `types.Bool` (Optional+Computed) | `Managed` | profile on/off |
| `restricted_categories` | `types.List(String)` | `Managed` | content categories to block; **open** set — see enum policy |
| `safe_search` | nested Object or `types.List(String)` of engines | `Managed` | **closed** set — `GOOGLE`/`YOUTUBE`/`BING`, `OneOf` |
| `schedule` | nested Object: `mode` + time-range fields | `Managed` (mode: **closed** enum; time bounds: `Managed`, open) | schedule "mode" (e.g. `ALWAYS`/`CUSTOM`/off) is a **closed**, controller-validated set |
| `network_ids` | `types.List(String)` | `Managed` | networks this profile applies to — scope selector |
| `client_macs` | `types.List(String)` (element type: plain `types.String` by default, matching `firewall_policy`'s actual `client_macs`/`network_ids` shape — see "Client + network scope semantics" below for the `hwtypes.MACAddress` option and why it is not assumed) | `Managed` | clients this profile applies to — scope selector |
| `timeouts` | `timeouts.Value` | n/a | PR-V's `v2Timeout`, `v2DefaultTimeout` (20m) |

The exact attribute names, whether `safe_search` is a single enum or a list
of enabled engines, and whether `schedule` is a single nested block or a list
of time ranges are the parts that need live-controller capture — the table
above is the best-effort shape from the parent spec's own field references
(§399, §5.3, §5.4: "safe-search GOOGLE/YOUTUBE/BING and schedule modes") and
must be corrected against a real payload before implementation, not filled in
speculatively beyond what the parent spec already names.

### Collection ownership (C1, §5.3)

Every leaf above is tagged `Managed` (Optional+Computed, user-or-controller
owned, adopt-remote-on-omit) except `id`/`site` (identity, C3) and
`timeouts` (framework-owned, no ownership class). Rationale: a
content-filtering profile is a user-authored object end to end — unlike
`unifi_setting`'s sections there is no scalar the controller unilaterally
recomputes out-of-band (no `CoManaged` candidate identified), no secret
material (no `WriteOnlySecret`/`GeneratedSecret` leaf), and no field the
provider only ever reads (no bare `Computed`). If live-controller capture
reveals a controller-assigned field (e.g. an internal rule-priority integer
the UI does not expose), it is tagged `Computed` or `CoManaged` at that point
per the C1 decision matrix — not preemptively guessed here.

`restricted_categories`, `network_ids`, and `client_macs` are assigned to the
C1 `Managed` class (Optional+Computed, adopt-remote-on-omit) — that
classification itself is a design decision independent of wire shape and is
not in question. **What the decision matrix's null/empty row *produces on
the wire* for this resource — cfg null omits the field from the PUT and cfg
empty (`[]`) sends an explicit empty list, with distinct controller-observed
outcomes ("stop managing" vs "clear membership") — is a HYPOTHESIS, not an
established fact, and must be treated as such until the capture confirms it.**
The C1 matrix describes the Terraform-side contract this provider commits to
*sending*; it does not by itself prove the controller's content-filtering
endpoint *honors* that distinction the way, say, `unifi_firewall_policy`'s
PUT is already known to. Three different controller update semantics are
consistent with everything known so far — full-replacement PUT (omitted
fields are implicitly cleared, not preserved, which would break the
"stop-managing" half of `Managed` outright and require a read-modify-write
overlay in the converter), PATCH-like partial update (matches the assumed
behavior), or a read-overlay requirement (the provider must GET-merge-PUT
because the endpoint has no partial-update semantics at all) — and picking
the wrong one silently corrupts a user's other fields on their first apply.
**The capture plan's step (e) is designed specifically to distinguish these
three cases** by observing effective state after (i) populated→explicit-empty,
(ii) populated→field-omitted, and (iii) both selectors absent/empty on a
fresh profile. Until that evidence exists, this document commits only to the
Terraform-side `Managed` classification, not to the wire-level omit/empty
behavior described above.

### Enum compatibility policy (§5.4 — decided in the parent spec, applied here)

Per the parent spec's already-decided policy: closed, controller-validated
sets get `OneOf` (plan-time rejection, better UX than a 400) with a
doc/changelog note that a new controller value requires a provider bump.
Open, controller-evolving fields accept any string.

**Closed → `OneOf`:**

- `safe_search` engine values: `GOOGLE`, `YOUTUBE`, `BING` (named explicitly
  in parent spec §399/§5.4). These are safe-search-enforcement targets the
  controller validates against a fixed enum in its own UI dropdown — not a
  taxonomy the controller grows organically the way, say, IPS threat
  categories do.
- `schedule` mode: closed set of controller-defined scheduling modes (e.g.
  "always on" vs "custom time range" vs "disabled" — exact literal values are
  part of the wire-shape gap, but the *fact* that mode is a small fixed
  enum, not free text, is a structural certainty from how every other
  UniFi schedule-shaped field in this codebase behaves, and is called out by
  name in parent spec §399/§5.4 ("schedule modes")). `OneOf` once literals are
  known.

**Open → free string, no `OneOf`:** `restricted_categories`. Content-filter
category taxonomies (unlike safe-search engines) are exactly the kind of
controller-evolving list the parent spec's policy carves out — new categories
get added by Ubiquiti as threat/content classification evolves (directly
analogous to the existing `unifi_setting` `ips.enabled_categories`, which
this repo already models as an open `types.List(String)` with no `OneOf`,
despite having a long enumerated *comment* documenting the current known
values — see `unifi/settings/ips.generated.go`'s `EnabledCategories` doc
comment). `restricted_categories` follows that same precedent: open list,
each known category documented in a comment for discoverability, not
enforced as a validator.

**Default rule (parent spec):** anything the controller strictly validates
defaults to `OneOf` unless there's a specific reason (like
`ips.enabled_categories`'s precedent) to leave it open. `safe_search` and
`schedule.mode` are closed by explicit parent-spec instruction; everything
else in this table defaults open because no evidence of closed-set
controller validation exists for it.

**Provider-bump doc note (required on every `OneOf` field):** both the
resource's godoc and the Terraform docs (`docs/resources/content_filtering.md`,
generated) carry a note in the shape already used for other enum fields in
this codebase (e.g. `unifi_firewall_policy`'s `action`/`protocol`
validators): "Controller-validated set as of go-unifi
`v1.33.43-0.20260706191309-bc63776a9ebf` / controller 9.5.21-equivalent
codegen; a new controller-side value requires a provider bump to add it to
this list." The changelog entry for this PR states the same.

### Client + network scope semantics (from live behavior — flagged)

The parent spec (§399) calls for scope semantics "documented from live
behavior." No live controller was available in this session; the following
is the best-effort structural claim from the resource's shape and must be
confirmed during live-controller capture, not treated as verified:

- A content-filtering profile scopes to **networks** (`network_ids`) and/or
  **clients** (`client_macs`) independently — this mirrors
  `unifi_firewall_policy`'s `source`/`destination` `network_ids`/`client_macs`
  pattern (same underlying controller concept: a list of network object IDs
  and a list of client MAC addresses selecting what traffic a rule/profile
  applies to), not a discriminated single-select (C4) the way NAT's `type`
  or firewall's `matching_target` are. There is no evidence of a
  "matching_target"-style discriminator field for content filtering in
  anything the parent spec or the SDK exposes — profiles apply to the union
  of configured networks and clients, most likely (needs confirmation:
  whether an empty/omitted scope means "applies everywhere" or "applies
  nowhere until scoped" is exactly the kind of controller-default behavior
  that can only be learned by creating an unscoped profile against a real
  controller and observing what it does).
- **Collection type: `types.List`, not `types.Set`** (§5.7's "equivalent
  selector families use the same type across resources" rule). This repo
  already has two data points: `unifi_firewall_policy.source.network_ids` /
  `.client_macs` are `types.List` (order not meaningful, but List is the
  established convention for this exact selector shape); `unifi_ap_group.
  device_macs` is `types.Set` (a genuine membership-only collection with a
  `SizeAtLeast`/empty-allowed history, per commit `2c7623ca`). Content
  filtering's `network_ids`/`client_macs` are the *same selector concept* as
  firewall_policy's, not the *device-group-membership* concept ap_group
  models — so per §5.7 they take firewall_policy's type, `types.List`, for
  cross-resource consistency of "equivalent selector families," not
  ap_group's `types.Set`. This is a design decision made now, independent of
  wire-shape capture, and should not be revisited casually once implementation
  starts.
- **`client_macs` element type: `hwtypes.MACAddress` is an OPTION to validate
  against the capture, not established provider convention.** Verified
  directly against `unifi/firewall_policy_resource.go`:
  `firewallPolicyEndpointModel.ClientMACs`/`.NetworkIDs` (the *exact* selector
  pair this design is modeled on) are both plain
  `types.List{ElemType: types.StringType}` — firewall_policy does **not** use
  `hwtypes.MACAddress` for its `client_macs` list. `hwtypes.MACAddress` (the
  single-value custom type, not a list element type in current use anywhere
  in this codebase) is instead used for singular MAC attributes:
  `client_resource.go`'s `MAC`, `device_resource.go`'s `MAC`,
  `firewall_rule_resource.go`'s `SrcMac`, `power_supervisor_resource.go`'s
  `DeviceMAC` — one MAC per attribute, not a list. `unifi_ap_group.device_macs`
  uses `hwtypes.MACAddressType{}` as a **`types.Set` element type**, which is
  closer in shape to a `client_macs` list but is still a different resource
  family (device-membership, per §5.7, not a firewall/content-filter
  network+client selector pair). Given this, content filtering's
  `client_macs` has two real options, both compiled and testable today
  without a capture: (a) mirror `firewall_policy` exactly — plain
  `types.List{ElemType: types.StringType}`, no normalization, consistent with
  the resource this design says it's copying; or (b) adopt
  `hwtypes.MACAddressType{}` as the list element type (following
  `ap_group`'s precedent of using it inside a collection, adapted from `Set`
  to `List`) to get mixed-case/mixed-separator equality for free. **Default
  to option (a) for consistency with the stated firewall_policy precedent**
  unless the capture's synthetic-MAC edit tests (capture plan step (d)) show
  the controller itself normalizes case/separators in a way that would
  otherwise churn the plan — in which case (b) is justified and should be
  written up as a deviation with the evidence cited, not assumed up front.

### Capability precheck (C6, §6.1)

Content filtering is treated as a v2 resource under C6's v2-resource rule:
typed API errors are the source of truth, not a settings-snapshot key-absence
heuristic (that heuristic is C6's *settings* rule, for `unifi_setting`
sections; content filtering is not a setting).

- **NotFound** on Read/Delete → `Unmaterialized`/absent; standard
  `v2FinishRead` (`unifi/v2_resource.go`) handles this identically to every
  other v2 resource — `resp.State.RemoveResource` on NotFound.
- **Method/endpoint unsupported** (e.g. a controller build or product tier
  without the content-filtering feature at all) → `Unsupported`. Per C6
  behavior: configuring the resource against an `Unsupported` controller is a
  predictable error diagnostic, never a silent no-op; the resource is not
  listed for an unsupported controller (`Unsupported` + not configured →
  omitted, N/A for a resource with no ambient config to omit — this rule is
  more directly relevant to `unifi_setting` sections, but the *predictable
  error, never silent-null* half applies to any Create/Update attempt here).
- **401/403** → `Unauthorized`, fail (not skip) — same as every other C6
  consumer.
- **Non-mutating precheck (§6.1):** where the acceptance test suite needs to
  skip content-filtering coverage on a controller/demo image that lacks the
  feature, the precheck is a read-only capability probe (e.g. an empty List
  call, or a HEAD/GET against the collection endpoint) executed before any
  mutating test step — never a probe that itself creates/deletes a profile.
  Per parent spec §6.2: "Prechecks are non-mutating; where write capability is
  the thing under test, the mutation lives in the managed test lifecycle, not
  an out-of-band probe." The exact probe call is part of the wire-shape gap
  (it needs the same endpoint knowledge as everything else) but the *shape*
  of the precheck — read-only, pre-test, skip-with-reason on `Unsupported`,
  fail (not skip) on `Unauthorized`/transient — is decided now.

### Site identity + list resource (C3, §5.9, per PR-V's recorded rule) — CONDITIONAL on proof of per-profile IDs

**This entire section's per-item `<site>:<id>` identity model is conditional
on the capture proving that the controller actually exposes multiple
content-filtering profile objects, each with its own stable controller-issued
ID, per site.** The parent spec names `unifi_content_filtering` in its PR
list and identity table (§245, §399) as a collection-shaped, per-site
resource "the same shape as `unifi_firewall_policy`," but neither this
document nor the parent spec has direct evidence — no captured payload, no
UI walkthrough, no go-unifi struct — that the controller's content-filtering
feature is actually modeled as N independent profile objects with individual
IDs, as opposed to, for example, a **single per-site settings object** (the
same shape as `unifi_setting`'s sections, or BGP's per-site singleton) that
merely has a list-valued field or two inside it (e.g. one profile-like blob
containing `restricted_categories`/`safe_search`/`schedule`, with
`network_ids`/`client_macs` as the *only* multi-valued parts). Those are
structurally different resources: a singleton has no `list.ListResource`,
no per-item import identity, and no create/delete of individual items — only
update-in-place, matching BGP's and `unifi_setting`'s existing pattern in
this codebase. **The capture plan's step (b) (discovery/list requests) and
step (c) (creating a second, distinct synthetic profile) are designed
specifically to resolve this before any of the identity model below is
implemented.**

If the capture confirms multiple independently-addressable profile objects
per site, each with its own controller-assigned ID (e.g. the list/discovery
response is an array of `{id, ...}` objects, and POSTing a second profile
produces a second distinct ID coexisting with the first) — then the
following per-item identity model applies exactly as `unifi_firewall_policy`
already establishes it in this codebase:

- Identity follows the C3 table exactly as `unifi_content_filtering` is
  already named in the parent spec's identity table (§245): import accepts
  `<id>` or `<site>:<id>`; persisted `id` is the canonical `<site>:<id>`;
  `site` is derived from `id`; changing `site` is `RequiresReplace`.
- **Composite `id` depends on the same PR-V amendment PR-D identifies
  (PR-D design §8), not on `v2ImportState` as-is.** PR-V's current
  `v2ImportState` parses `<id>`/`<site>:<id>` but writes the **bare** `id`
  plus a separate `site` (`unifi/v2_resource.go:249`), so it cannot persist
  a canonical `<site>:<id>` `id` on its own. If the capture confirms the
  composite-identity model, E must consume the shared composite-identity
  import helper/mode added to PR-V by that amendment — exactly as PR-D will —
  rather than implementing resource-specific composite-ID parsing. (If the
  capture instead proves a per-site singleton, this whole identity model is
  dropped in favour of the `unifi_setting`/BGP singleton shape and no
  composite import applies.)
- `<id>` with no site prefix on import uses the provider default site and is
  provably equivalent to `<default-site>:<id>` (parent spec's shared
  `resolveSite` guarantee, exercised via PR-V's `resolveV2Site` wrapper).
  Equivalence must be tested under a provider configured with a
  **non-default** default site, per parent spec §5.5.
- **List resource: required IF collection-shaped.** PR-V's already-recorded
  §5.9 rule (see `unifi/v2_resource.go`'s package doc, lines 6–33) states
  every collection-shaped v2 resource in this package implements
  `list.ListResource`, and singleton resources (BGP, `unifi_setting`) do not.
  This rule is unconditional and not in question; what's conditional (per
  the proof requirement above) is *which side of that rule
  `unifi_content_filtering` falls on*. **If** the capture confirms
  collection-shape (many profiles per site, each with its own ID), it MUST
  implement `list.ListResource`: a `contentFilteringListConfigModel` (`site`
  + `filter` blocks, mirroring `firewallPolicyListConfigModel`/
  `portForwardListConfigModel`), a `List` method, and registration in
  `provider.go`'s `ListResources()`. **If** the capture instead shows a
  per-site singleton settings object, this resource takes BGP's/
  `unifi_setting`'s shape instead — no `list.ListResource`, no per-item
  identity, update-in-place only — and everything in this section describing
  per-item CRUD/import/list needs to be rewritten, not patched. This PR
  applies PR-V's rule directly once the shape is known; it does not
  re-derive or re-litigate the rule itself, and does not need to coordinate
  with PR-D (which applies the same already-recorded rule independently for
  NAT, where collection-shape is not in doubt).
- Uses PR-V's `v2Configure`, `v2Timeout`/`v2DefaultTimeout`,
  `v2SetIdentityAndState`, `v2FinishRead`, `v2IsNotFound` unconditionally —
  no hand-rolled equivalents, matching the "built ON PR-V" requirement.
  **Corrected helper-applicability claim** (an earlier draft of this spec
  overstated which PR-V helpers apply to which fields): per
  `unifi/v2_resource.go`'s actual signatures,
  `objectAsOptions[T any](ctx, obj types.Object) (T, diag.Diagnostics)`
  decodes a *single* `types.Object` into a struct, and
  `objectListAsOptions[T any](ctx, list types.List) ([]T, diag.Diagnostics)`
  decodes a `types.List` whose elements are themselves nested objects into
  `[]T`. Neither helper applies to a scalar list of strings — so **neither
  applies to `network_ids`/`client_macs`** (both are plain
  `types.List(String)`/`types.List(types.String)` scope selectors with no
  nested object structure to decode: converting them uses the framework's
  ordinary `list.ElementsAs(ctx, &out, false)` directly, the same call
  `objectListAsOptions` itself wraps, without the generic wrapper, since
  there is no `T` struct on the other side) **nor to a `safe_search` model if
  it turns out to be `types.List(types.String)`** of engine literals (a list
  of plain strings, not a list of nested objects). `objectAsOptions` DOES
  apply if `schedule` turns out to be a single nested `types.Object` (e.g.
  `{mode, start, end}`) — that is the one nested-object shape currently
  planned in this design. `objectListAsOptions` would apply only if a nested
  field turns out to be a *list of objects* (e.g. if capture shows `schedule`
  is actually multiple time-range blocks, per Open Decision #3, or if
  `safe_search` turns out to be a list of `{engine, enabled}` objects rather
  than a flat string list) — this is exactly the kind of shape question the
  capture must settle before the converter is written; do not assume either
  helper is needed for a field until its real cardinality is known.
  `unifi_content_filtering` must compile and behave identically whether or
  not PR-D (`unifi_nat_rule`) is merged, since both consume only PR-V's
  exported/package-private helpers and neither references the other's types.

## Non-goals / explicitly out of scope for this PR

- Any change to `unifi_setting`'s existing `ips` section
  (`AdvancedFilteringPreference`/`ContentFilteringBlockingPageEnabled`) — it
  is a different feature (IPS blocking-page UX), already implemented, and
  this PR does not touch it.
- Inventing go-unifi struct fields, JSON keys, or the v2 endpoint path. None
  appear in this spec's schema table as literal wire values — the table
  above is a Terraform-side attribute shape only.
- A go-unifi upstream PR to add the struct — that is a prerequisite
  unblocking step for implementation, tracked as a plan blocker, not part of
  this design document.

## Live-controller capture plan (turn-key — unblocks Tasks 5–9)

This is the exact, actionable procedure to run once against **one
non-production UniFi controller** to resolve every wire-shape unknown listed
above. It is written so it can be handed to whoever has controller access
without further design discussion. Output is a sanitized authenticated HAR
(browser devtools "Network" tab, "Preserve log," export as HAR) or an
equivalent raw request/response bundle (e.g. `mitmproxy` dump) covering the
steps below, in order:

**(a) Environment fingerprint.** Before touching the feature, record:
controller product/version/build string (from the UI's About/System panel),
the site id in use, and a screenshot or note of feature-availability UI state
(is "Content Filtering" present at all, and under what menu path — Ubiquiti
has moved this between "Traffic & Device Manager," "Internet Security," and
profile-based DPI screens across versions). This directly answers Open
Decision #5 (confirm no missed SDK endpoint) and anchors every other capture
to a known controller build.

**(b) Discovery/list capture.** Navigate to Content Filtering fresh (empty
profile list, if the controller has none yet). Capture **every** GET/list/
discovery request and its full response the UI fires on page load — this
establishes: the actual collection endpoint path; whether the response shape
is a bare array, a `{data: [...]}` envelope, or a single object
(singleton-vs-collection — the single biggest open question, see "Site
identity" above); pagination parameters if any; and any enum/catalog endpoint
the UI calls separately (e.g. a `GET .../categories` that returns the live
`restricted_categories` taxonomy, which would settle the open-set question
definitively instead of by analogy to `ips.enabled_categories`).

**(c) Create one fully-populated synthetic profile.** Using synthetic values
only (see SANITIZATION below), create ONE profile with **every
UI-exposable field populated**: a synthetic name; enabled/disabled toggle
exercised; multiple safe-search selections if the UI allows more than one
checkbox/toggle (directly answers Open Decision #2); at least one category
selection; a custom schedule/time-range configuration (not "always on" —
directly answers Open Decision #3); at least 2 networks selected; at least 2
locally-administered synthetic MAC addresses added as clients. Capture, for
this single create action: HTTP method, path (including query string),
full request JSON body, full response JSON body, HTTP status, AND the
follow-up GET/list request+response the UI issues to refresh its view after
the create succeeds (this follow-up read-back is often where the *actual*
stored/normalized shape is visible, as opposed to just an echo of the
request).

**(d) Edit one field at a time.** Starting from the profile created in (c),
make a **separate, isolated edit** for each of: name; enabled/disabled;
each individual safe-search value (toggle one at a time, not all at once);
categories (add one, remove one, as separate edits); schedule mode and
schedule ranges (separately); network scope; client scope. After each single-
field edit, capture the request and the normalized read-back (the GET/list
response reflecting the change). This isolates which fields the controller
echoes back verbatim vs. normalizes (e.g. MAC case/separator normalization,
directly relevant to the `client_macs` element-type option above) and
confirms the update method is PATCH-like (only the changed field appears in
the request) vs. full-replacement (the whole object is resent).

**(e) Scope-behavior cases (resolves the C1 null-vs-empty hypothesis
above).** Run and capture each of these as a distinct, separately-recorded
case, with a read-back after every one:

   1. `network_ids`/`client_macs` populated → send an explicit **empty
      list** for one of them (e.g. via the UI's "clear all" / deselect-all
      action) → observe: does the controller un-scope that selector, or does
      it reject/ignore an empty list?
   2. `network_ids`/`client_macs` populated → **omit the field entirely**
      from the update if the UI/API permits a partial update at all (this
      may not be reachable through the UI — if so, note that it could only
      be tested via a raw API replay, and record that limitation rather than
      skipping the case silently) → observe: is the prior value retained
      (PATCH semantics) or cleared (full-replacement semantics)?
   3. **Fresh profile, both selectors absent/empty from the start** → observe
      effective behavior: does the profile apply to all clients/networks by
      default, or none (inert until scoped)? This directly answers Open
      Decision #4.

**(f) Delete.** Delete the profile created above. Capture the DELETE
request/response, and the follow-up item-level GET and list-level GET —
record the exact status code and body shape for "item no longer exists"
(this is what `v2IsNotFound`/`v2FinishRead` need to match against; do not
assume it is a bare HTTP 404 until seen — some UniFi v2 endpoints return 200
with an empty/error-shaped body instead).

**(g) Error responses.** Where safely reproducible without affecting a real
site: capture the response for an unsupported method/endpoint (e.g. a
request variant the UI itself never sends, if one can be constructed safely)
and, only if safe to do so on this non-production controller, an
unauthorized request (expired/invalid session). These respectively inform
the C6 `Unsupported` classification and the non-mutating acceptance-test
precheck design (both already structurally decided above; this evidence
confirms which controller-observable signal maps to which C6 state, it does
not change the C6 decision itself).

**SANITIZATION rule (mandatory before this capture leaves the capturing
machine, matching the parent spec's §1 privacy-scrub discipline):** replace
host/site/profile/network IDs, and all MAC addresses, consistently with
synthetic equivalents (e.g. a fixed substitution map applied throughout the
whole capture, not ad hoc per-request) — **preserve the identifier
relationships across request and response** (the same real ID must map to
the same synthetic ID everywhere it appears, so cross-references between,
e.g., a profile's `network_ids` and the site's network list remain
internally consistent and useful for shape analysis). Strip all cookies,
bearer/session/CSRF tokens, and any user identity, address, or device-name
strings unrelated to the synthetic fixture data. Do not commit an
unsanitized capture to any branch, commit message, or issue at any point —
sanitize before it is saved anywhere version-controlled or shared.

## Riskiest assumptions to validate (capture must confirm or refute each)

These are the design's load-bearing guesses, ranked by how much of the
implementation collapses if the guess is wrong. Every one of them is checked
by a specific step in the capture plan above; none should be treated as
"probably fine" once the capture exists — check it explicitly against the
evidence before writing Task 5's converter.

1. **Per-site collection vs. singleton** — whether content filtering is N
   independently-addressable profile objects per site, or one per-site
   settings object. (Capture step (b).) This is the single biggest fork:
   it determines whether "Site identity + list resource" above applies at
   all, or whether the resource is a BGP/`unifi_setting`-shaped singleton
   instead.
2. **Each item has a stable controller ID suitable for `<site>:<id>`
   import** — even if (1) resolves to "collection," the per-item ID must be
   a real, stable, controller-issued identifier (not, e.g., a
   client-generated UUID the provider itself would have to invent, or an
   array index). (Capture steps (b), (c): does creating a second profile
   yield a second stable, distinct ID that survives a subsequent list call?)
3. **`safe_search` cardinality** — multi-select list vs. scalar/single-enum
   vs. object vs. bool-map (e.g. `{google: true, youtube: false}`) — four
   structurally different Terraform shapes, only one of which matches this
   draft's assumed `types.List(String)`. (Capture step (c): select more than
   one engine and see how it's represented in the read-back.)
4. **`schedule` cardinality** — one nested object (mode + start/end) vs. a
   reference to a separately-managed schedule resource vs. multiple
   time-range blocks in a list. (Capture step (c)/(d): configure a custom
   schedule with more than one distinct time window if the UI allows it.)
5. **Whether network and client selectors coexist and union, or are
   discriminator-based (C4)** — this draft assumes union (firewall_policy-
   style, no `matching_target`-equivalent), but no evidence beyond "no
   discriminator field is mentioned anywhere" has been checked against an
   actual payload. (Capture step (c): populate both selectors simultaneously
   and check for any additional mode/type field in the response.)
6. **Omitted vs. empty collections have distinct write semantics** — the
   C1 "stop-managing vs. clear" contract assumes the controller's update
   endpoint is PATCH-like; it may instead be full-replacement (silently
   clearing omitted fields) or require a read-overlay. (Capture step (e),
   cases 1–2.) Getting this wrong risks silently corrupting a user's other
   profile fields on apply — the highest-severity risk on this list.
7. **A list GET is a safe and sufficient capability probe** — the C6
   non-mutating precheck design assumes a read-only list/GET call reliably
   distinguishes `Unsupported` from `Unauthorized` from "supported, just
   empty." (Capture steps (b), (g): confirm the unsupported-feature response
   shape is distinguishable from an empty-but-supported list, and from a 401/
   403.)
8. **UI enum labels map directly to the stated uppercase wire literals** —
   this draft assumes the safe-search engine wire values are exactly
   `GOOGLE`/`YOUTUBE`/`BING` (named in the parent spec) and that schedule
   mode literals will be similarly-cased simple tokens; the actual JSON
   could use different casing, different tokens entirely, or numeric/enum-
   index encoding. (Capture step (c)/(d): read the literal JSON values, do
   not infer them from UI label text.)

## Open decisions to confirm before implementation

1. **Live-controller capture method:** see "Live-controller capture plan"
   above for the full turn-key procedure. Maintainer has a cloud UniFi
   controller becoming available per the parent spec's "Live validation"
   section — confirm whether content-filtering capture can piggyback on that
   effort's timeline.
2. **`safe_search` shape:** single enum attribute vs. a list of enabled
   engines (a site can plausibly enforce multiple engines' safe-search
   simultaneously) — cannot be determined without a real payload.
   Recommendation in this draft assumes a list of `OneOf` strings
   (`["GOOGLE", "BING"]`), analogous to `restricted_categories`'s list shape,
   but this is a guess pending capture, not a decision — see "Riskiest
   assumptions" #3 above. **No schema or model type may be implemented from
   this guess; it exists only to give the capture plan something concrete to
   confirm or refute.**
3. **`schedule` shape:** single mode + start/end vs. a list of time-range
   blocks (some controller schedule features, e.g. WLAN scheduling
   elsewhere in this provider, use a list-of-ranges shape) — same
   capture-blocked status; see "Riskiest assumptions" #4. **Same
   no-implementation-from-guess rule as `safe_search` above.**
4. **Whether an unscoped profile (`network_ids`/`client_macs` both
   omitted/empty) applies everywhere or nowhere** — needs live behavior
   observation, not inferable from any static source. See capture step (e),
   case 3.
5. **Confirm no existing v2 endpoint under a name this search missed** — this
   session's SDK search was thorough (multiple grep strategies: filename,
   struct name, JSON tag content, v2 path string) but was not a live
   controller diff; if a maintainer has independent knowledge of an endpoint
   name, it should be checked against `go-unifi` before assuming a bump/
   upstream contribution is required. See capture step (a).
6. **Per-profile controller ID existence** — see "Riskiest assumptions" #1–2
   and the "Site identity + list resource" section's CONDITIONAL framing
   above; this is not a refinement of the identity model, it is a
   precondition for the identity model existing in its current per-item form
   at all.
