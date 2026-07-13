# PR-D · `unifi_nat` resource — Design

Date: 2026-07-13
Status: draft — revised per independent codex DESIGN review (verdict
NEEDS-WORK on the prior revision); this PR **stays a draft** for
hand-review, not implementation, this cycle.
Parent: `docs/superpowers/specs/2026-07-11-settings-remediation-design.md`
(§4.2, §4.4, §5.2, §5.6, §5.9, §6.1, §390 PR-D entry)
Depends on: PR-V (`unifi/v2_resource.go` — merged in this worktree), **plus
one new PR-V amendment this revision identifies as required before D's
implementation** (composite-identity import helper, §8).

## Review disposition (this revision)

An independent design review flagged eight issues in the prior draft:
wrong resource name, an unnecessary empty discriminator block, a
composite-identity/import contradiction, a missing post-mutation
reconciliation requirement, a wrong claim about which decode helper NAT's
filter-group field uses, and some over/under-claiming in the
uncertain-fields and testing sections. All are addressed below. This is a
docs-only revision — no Go code exists for this PR yet, so every fix is a
spec/plan correction, not a code change.

## 1. Scope

A new v2 (`terraform-plugin-framework`) resource, `unifi_nat`, managing
single NAT rules (DNAT/SNAT/MASQUERADE) on a UniFi gateway. One resource
instance = one `unifi.Nat` object. Docs-only PR: this file plus the SDD plan.
No Go code changes.

**Resource name: `unifi_nat` — settled, not a decision pending
confirmation.** The SDK object is `Nat` (`unifi.Nat`), and this provider's
naming convention keys off the object noun, not a universal `_rule` suffix:
`unifi_port_forward` (object `PortForward`), `unifi_firewall_policy` (object
`FirewallPolicy`), `unifi_network` (object `Network`) — none of these carry
a `_rule`/`_policy`-doubling suffix beyond the object's own name. `Nat` is
the object; `unifi_nat` is the resource. There is no naming decision left to
confirm here.

**Stale prose elsewhere is out of scope for this PR's edits.** The parent
design (`2026-07-11-settings-remediation-design.md:244`, its C3 identity
table) and PR-V's own package doc/comments
(`unifi/v2_resource.go:20`, `:239`) still say `unifi_nat_rule` in prose.
Those are pre-existing documents this PR does not own and this task does
not authorize editing (D's brief is D's own two files). **At integration
time** (whenever a PR-V-amending change lands, e.g. the composite-import
helper this revision requires — see §8 — or whenever D itself is
implemented), whoever touches those files should correct `unifi_nat_rule` →
`unifi_nat` in the same commit as a drive-by, so the naming discrepancy
doesn't live in the merged tree indefinitely. Flagged here so it isn't lost,
not fixed here.

## 2. Wire model (go-unifi, frozen at v1.33.43 / effectively 9.5.21 semantics)

Source: `github.com/ubiquiti-community/go-unifi/unifi/nat.generated.go`,
`nat.go` (module `github.com/ubiquiti-community/go-unifi@v1.33.43-...`).

```go
type Nat struct {
    ID     string `json:"_id,omitempty"`
    SiteID string `json:"site_id,omitempty"`

    Hidden   bool   `json:"attr_hidden,omitempty"`
    HiddenID string `json:"attr_hidden_id,omitempty"`
    NoDelete bool   `json:"attr_no_delete,omitempty"`
    NoEdit   bool   `json:"attr_no_edit,omitempty"`

    Description           string                `json:"description,omitempty"`
    DestinationFilter     *NatDestinationFilter `json:"destination_filter,omitempty"`
    Enabled               bool                  `json:"enabled"`
    Exclude               bool                  `json:"exclude"`
    IPAddress             string                `json:"ip_address,omitempty"`
    InInterface           string                `json:"in_interface,omitempty"`
    IsPredefined          bool                  `json:"is_predefined"`
    Logging               bool                  `json:"logging"`
    OutInterface          string                `json:"out_interface,omitempty"`
    Port                  *int64                `json:"port,omitempty"`
    PppoeUseBaseInterface bool                  `json:"pppoe_use_base_interface"`
    Protocol              string                `json:"protocol,omitempty"` // all|tcp|udp|tcp_udp
    RuleIndex             *int64                `json:"rule_index,omitempty"`
    SettingPreference     string                `json:"setting_preference,omitempty"` // auto|manual
    SourceFilter          *NatSourceFilter      `json:"source_filter,omitempty"`
    Type                  string                `json:"type,omitempty"`       // DNAT|SNAT|MASQUERADE
    Version               string                `json:"ip_version,omitempty"` // IPV4|IPV6
}

type NatDestinationFilter struct {
    Address          string   `json:"address,omitempty"`
    FilterType       string   `json:"filter_type,omitempty"` // NONE|ADDRESS_AND_PORT|FIREWALL_GROUPS|NETWORK_CONF
    FirewallGroupIDs []string `json:"firewall_group_ids,omitempty"`
    InvertAddress    bool     `json:"invert_address"`
    InvertPort       bool     `json:"invert_port"`
    NetworkConfID    string   `json:"network_conf_id,omitempty"`
    Port             *int64   `json:"port,omitempty"`
}

// NatSourceFilter: identical shape to NatDestinationFilter.
```

Client methods (`unifi/nat.go`), all on `v2/api/site/%s/nat[/%s]`:
`ListNat`, `GetNat` (list-then-find; NotFound when absent — **note:** the SDK
`getNat` returns `NotFoundError` when the list is *empty*, not specifically
when the id is absent from a non-empty list — both cases fall through to the
same `NotFoundError{}` at the end of the loop, so this is fine, but Read must
not assume a non-empty list implies the id will be found), `DeleteNat`,
`CreateNat`, `UpdateNat` (PUT to `.../nat/<d.ID>` — `d.ID` must be set before
calling Update, same convention as every other v2 resource in this package).

## 3. Ownership-flag / predefined semantics (§5.6)

Four flags carry meaning, distinct from each other — do not collapse them:

| Field | Meaning |
|---|---|
| `IsPredefined` (`is_predefined`) | Rule shipped by the controller (e.g. the default masquerade-on-WAN rules), not user-created. |
| `NoEdit` (`attr_no_edit`) | Controller rejects PUT against this rule. |
| `NoDelete` (`attr_no_delete`) | Controller rejects DELETE against this rule. |
| `Hidden` (`attr_hidden`) / `HiddenID` | UI-hidden bookkeeping; not itself a mutation constraint. `HiddenID` groups a hidden rule with its visible counterpart — round-trip only, never user-set. |

`IsPredefined` and `NoEdit`/`NoDelete` are **observed independently** — the
provider does not infer one from the other. A controller could in principle
ship a predefined rule that is edit-permitted (SettingPreference `manual`
predefined rules are plausible per the enum) or a user rule the controller
has locked (`NoEdit` set post-hoc). The go-unifi comment gives no guarantee
either way; treat NoEdit/NoDelete as the operative constraint for what
Update/Delete may attempt, and `IsPredefined` as documentation/UX signal
(surfaced as a read-only attribute) for why a rule might carry those flags.

### RUD behavior for flagged rules

Every rule the controller returns from `ListNat`/`GetNat` is manageable
*for import and Read* regardless of flags — `unifi_nat` is a full CRUD
resource, not read-only, because most rules are editable and a read-only
resource would refuse to manage the common case. Flag-gating happens per
operation, matching the capability-precheck pattern (§6.1) rather than a
resource-level mode switch:

- **Read**: always works. `is_predefined`, `attr_no_edit` (exposed as
  `no_edit`), `attr_no_delete` (exposed as `no_delete`) are surfaced as
  `Computed` attributes so config/import can see them.
- **Create**: unaffected — a newly created rule cannot be predefined
  (`is_predefined` is a controller-assigned read-back field, never sent on
  the request body; Create never sends it).
- **Update**: if the rule's last-read `NoEdit` is `true`, Update returns a
  **plan-time-adjacent, apply-time error diagnostic** ("NAT rule <id> is
  marked non-editable by the controller (`attr_no_edit`); it cannot be
  updated through this provider") *before* calling `UpdateNat` — a
  non-mutating precheck against the resource's own last-known state,
  consistent with C6's "prechecks are non-mutating" rule (§6.1). This is
  cheaper and more predictable than sending the PUT and parsing a
  controller error, and matches "predictable Read/Update/Delete" from
  §390's PR-D description. The precheck reads the *state* NoEdit value
  (last Read), not a fresh probe — no independent fetch, consistent with
  PR-A's "no section fetches independently" spirit extended to v2
  resources.
- **Delete**: same precheck against `NoDelete`, same error shape, before
  calling `DeleteNat`.
- **Import**: unrestricted — importing a predefined/no-edit rule is
  legitimate (observing it), only *mutating* it afterward is blocked. This
  mirrors "predefined-NAT import and attempted mutation" in the parent
  spec's testing-strategy list (§8) — both must be covered, distinctly.

This is deliberately **not** "read-only if predefined" — that would be
simpler but wrong: it conflates `IsPredefined` (why the rule exists) with
`NoEdit`/`NoDelete` (whether it can be mutated), and the SDK exposes them as
independent booleans. Gating on the two real constraint flags, not the
provenance flag, is the more accurate and narrower behavior.

**Uncertain / needs live-controller confirmation:** whether a `NoEdit=true`
rule, if PUT anyway, fails with a clean 4xx or is silently accepted/ignored
by the controller. The precheck makes this moot for the golden path, but the
acceptance test suite (§7 below) should note this is unverified against a
real controller — the demo docker controller may not enforce it either.
Flagged as needing live-controller confirmation (consolidated list: §6a).

## 4. Discriminator contract (C4 / §4.2, §4.4)

### 4.1 Rule-type discriminator (`type`: DNAT | SNAT | MASQUERADE)

Per C4, "shapes per discriminator value are modeled explicitly ... not a
flat bag." `unifi_nat`'s schema therefore does **not** expose one bag of
optional fields for all three types. Instead:

- `type` (`Required`, `stringvalidator.OneOf("DNAT", "SNAT", "MASQUERADE")`)
  is the top-level discriminator.
- **Two** mutually-exclusive nested blocks, one per non-trivial type, each
  `Optional` + `SingleNestedAttribute`:
  - `dnat` — `ip_address` (destination translation target), `port`,
    `destination_filter` (block, §4.2 below). Required and populated when
    `type = "DNAT"`.
  - `snat` — `ip_address` (source translation target), `port`,
    `source_filter` (block, §4.2 below). Required and populated when
    `type = "SNAT"`.
  - **There is no `masquerade` block.** MASQUERADE is a complete
    discriminator value on its own (`type = "MASQUERADE"`) with no
    additional per-type nested shape: go-unifi's `Nat` struct has no
    MASQUERADE-specific fields — its only relevant fields
    (`out_interface`, `protocol`, `ip_version`,
    `pppoe_use_base_interface`, `setting_preference`) are all shared,
    top-level, flat fields already present regardless of `type` (§4.3
    below). An empty `masquerade {}` marker block was considered and
    **rejected**: a block that can never carry a field is not a
    discriminator shape, it's a decorative no-op that adds a fourth
    conversion path (serialize/deserialize an always-empty object) for zero
    behavioral benefit. `type = "MASQUERADE"` needs no companion block at
    all; the top-level fields are the whole shape.
- A config-time validator (resource-level, via
  `resource.ResourceWithValidateConfig` or a schema-level object validator)
  rejects: (a) `type = "DNAT"` with a populated `snat` block present, and
  vice versa; (b) `type = "MASQUERADE"` with either `dnat` or `snat`
  populated; (c) the active type's own block being **required and absent**
  for DNAT/SNAT — `type = "DNAT"` with no `dnat {}` block at all is a
  config error (DNAT/SNAT are meaningless without their translation-target
  block; there is nothing "optional-with-defaults" about an absent `dnat`
  block when `type = "DNAT"`, unlike the now-removed MASQUERADE case).
- A plan modifier clears the inactive block (`snat` when `type = "DNAT"`,
  `dnat` when `type = "SNAT"`, both when `type = "MASQUERADE"`) to null
  *before* validation runs, so a `type` transition (e.g. `SNAT` → `DNAT`)
  does not error on stale children left over from the prior apply — this is
  the "stale prior-state children cleared by a plan modifier when the
  discriminator changes, before validation" rule from C4, applied to `type`
  the same way the parent spec applies it to NAT `filter_type` and
  firewall's `matching_target_type`.
- `modelToNat` (outbound conversion) serializes only the active type's
  block into the flat `unifi.Nat` wire fields (`IPAddress`, `Port`,
  `DestinationFilter`/`SourceFilter`) plus the shared top-level fields
  (§4.3) — never both a `DestinationFilter` and a `SourceFilter` populated
  at once, matching "outbound normalization serializes only the active
  discriminator's children."
- `natToModel` (inbound conversion) reads `nat.Type` and populates only the
  matching nested block (for DNAT/SNAT); for MASQUERADE, both `dnat` and
  `snat` are set to `types.ObjectNull(...)` and there is no third block to
  populate. If the controller returns a rule whose `Type` is unrecognized
  (future value), Read still succeeds (surfaces `type` as whatever string
  was returned; leaves both `dnat`/`snat` null) — this is the "unknown
  discriminator value defers child validation to apply" case: it's a read,
  so there's nothing to validate, but it means a resource authored against
  config can never legitimately end up with an unrecognized `type` since
  the schema's `OneOf` bars it at plan time; this path is reachable only
  via import or drift from an unmodeled controller-side rule type.

### 4.2 Filter discriminator (`filter_type` inside `destination_filter` /
`source_filter`)

`NatDestinationFilter`/`NatSourceFilter` share one shape:
`filter_type` ∈ {`NONE`, `ADDRESS_AND_PORT`, `FIREWALL_GROUPS`,
`NETWORK_CONF`}, with fields `address`/`port`/`invert_address`/
`invert_port` (owned by `ADDRESS_AND_PORT`), `firewall_group_ids` (owned by
`FIREWALL_GROUPS`), `network_conf_id` (owned by `NETWORK_CONF`). `NONE` owns
none of them.

Same C4 mechanism as §4.1, applied one level deeper:

- `filter_type` (`Required` within the filter block,
  `OneOf("NONE", "ADDRESS_AND_PORT", "FIREWALL_GROUPS", "NETWORK_CONF")`).
- A config validator rejects a child field configured under a `filter_type`
  that does not own it (e.g. `network_conf_id` set while
  `filter_type = "ADDRESS_AND_PORT"`) — plan-time error, not deferred to the
  controller.
- A plan modifier clears the filter block's non-owned fields to null/empty
  when `filter_type` changes (e.g. `FIREWALL_GROUPS` → `NETWORK_CONF` drops
  a stale `firewall_group_ids`), before validation — this is precisely the
  "NAT stale selectors" defect named in the parent spec's traceability
  table (§4.2 row) and its "not just MASQUERADE-only" acceptance-coverage
  requirement (§390) exists specifically so this path gets exercised by a
  real DNAT/SNAT test, not just the trivially-filterless MASQUERADE case.
- Outbound: only the owned fields for the active `filter_type` are sent
  (others omitted/zeroed) on `NatDestinationFilter`/`NatSourceFilter`.
  `invert_address`/`invert_port` are plain bools with no `omitempty`
  exemption issue since the wire type already lacks `omitempty` on them
  (`json:"invert_address"` — always sent) — this matches the struct as
  generated, so the provider must always send both regardless of
  `filter_type`, defaulting `false` when not user-configured (no
  ambiguity/null case for these two).
- Inbound: normalize stale values out of state — if the controller (or a
  pre-provider-managed rule) reports `filter_type = "NETWORK_CONF"` but
  also a leftover non-empty `firewall_group_ids` (SDK doesn't clear other
  fields itself; they're `omitempty` so an inactive field *should* come
  back empty in practice, but this is not guaranteed by the wire contract),
  the read path nulls the non-owned fields rather than trusting the raw
  bytes. This is the "imported/controller stale values are normalized out
  of state on read when inactive; the discriminator is authoritative" rule.
- Filter block presence: `destination_filter`/`source_filter` are
  `Optional + Computed` single-nested attributes on the `dnat`/`snat`
  blocks respectively (absent config → controller default, likely `NONE`;
  **needs live-controller confirmation** — go-unifi does not document the
  server-side default when the field is omitted entirely on create;
  consolidated list: §6a).

### 4.3 Shared top-level (non-discriminated) fields

`protocol`, `ip_version`, `pppoe_use_base_interface`, and
`setting_preference` are modeled as **flat, top-level schema attributes**,
not duplicated inside `dnat`/`snat`. This is settled, not a pending
decision:

- go-unifi's `Nat` struct declares `Protocol`, `Version` (`ip_version`),
  `PppoeUseBaseInterface`, and `SettingPreference` exactly once, at the
  struct's top level — there is no per-rule-type variant of any of these
  fields on the wire. Modeling them per-block in the Terraform schema would
  require picking one of three (`dnat.protocol`/`snat.protocol`/nothing for
  MASQUERADE) as *the* wire value on outbound conversion, i.e. inventing a
  precedence rule the API itself has no concept of. That is strictly more
  conversion complexity for a UX symmetry that doesn't correspond to
  anything the controller does. NAT's flat placement follows one field,
  one location, matching the wire struct directly.
- Concretely, this means: `protocol` is a single top-level `OneOf(all, tcp,
  udp, tcp_udp)` attribute that applies regardless of `type`; same for
  `ip_version` (`OneOf(IPV4, IPV6)`), `pppoe_use_base_interface` (bool),
  and `setting_preference` (`OneOf(auto, manual)`). `modelToNat` reads all
  four directly from the top-level model fields with no
  discriminator-dependent branching.

## 5. Full schema sketch

```
unifi_nat {
  id           = computed, UseStateForUnknown           // canonical "<site>:<id>"
  site         = optional+computed, RequiresReplace, UseStateForUnknown  // C3, derived from id
  description  = optional+computed, default ""
  type         = required, OneOf(DNAT, SNAT, MASQUERADE)  // §4.1 discriminator
  enabled      = optional+computed, default true
  logging      = optional+computed, default false
  exclude      = optional+computed, default false          // needs live confirm: semantics (§6a)
  protocol     = optional+computed, OneOf(all, tcp, udp, tcp_udp), default "all"      // top-level, shared (§4.3)
  ip_version   = optional+computed, OneOf(IPV4, IPV6), default "IPV4"                 // top-level, shared (§4.3)
  in_interface  = optional  // used by DNAT (ingress side); needs live confirm (§6a)
  out_interface = optional  // used by SNAT/MASQUERADE (egress side); needs live confirm (§6a)
  rule_index    = computed-ONLY, Int64, UseStateForUnknown
                 // controller-assigned ordering. The SDK serializes rule_index
                 // but exposes no reorder operation on NAT, and there is no
                 // evidence the controller accepts a client-supplied value on
                 // write — Computed-only, never Optional, until a reorder
                 // capability is proven to exist (§6a item 5). Do not promise
                 // an ordering feature the provider cannot demonstrate.
  setting_preference       = optional+computed, OneOf(auto, manual), default "auto"   // top-level, shared (§4.3)
  pppoe_use_base_interface = optional+computed, default false                          // top-level, shared (§4.3)

  dnat {                        // SingleNestedAttribute, Optional (required-when-active — §4.1)
    ip_address         = optional  // translation target
    port               = optional, string (port-or-range, mirrors firewall_policy's `port` string fix)
    destination_filter {        // SingleNestedAttribute, optional+computed
      filter_type        = required, OneOf(NONE, ADDRESS_AND_PORT, FIREWALL_GROUPS, NETWORK_CONF)
      address            = optional
      port               = optional, string
      invert_address     = optional+computed, default false
      invert_port        = optional+computed, default false
      firewall_group_ids = optional, set(string)   // types.Set — see §9
      network_conf_id    = optional
    }
  }

  snat {                        // SingleNestedAttribute, Optional (required-when-active — §4.1)
    ip_address    = optional
    port          = optional, string
    source_filter {             // same shape as destination_filter
      ...
    }
  }

  // No masquerade block. type = "MASQUERADE" uses only the top-level shared
  // fields above (out_interface, protocol, ip_version,
  // pppoe_use_base_interface, setting_preference) — see §4.1.

  is_predefined = computed, Bool          // §5.6, read-only signal
  no_edit       = computed, Bool          // §5.6, read-only signal (attr_no_edit)
  no_delete     = computed, Bool          // §5.6, read-only signal (attr_no_delete)

  timeouts { create, read, update, delete }
}
```

Notes on the sketch:

- `port` fields are modeled as **strings**, not `int64`, deliberately
  mirroring the `firewall_policy_resource.go` v0→v1 state-upgrade lesson
  (`unifi/firewall_policy_resource.go:764-772`, `portToStringValue`): the
  wire type is `*int64` with a documented `[1-9][0-9]{0,4}` regex, single
  port only (no evidence of comma/range support like firewall's string
  port, so a plain numeric string with regex validation is the honest
  model — **do not** copy firewall's comma/range grammar wholesale; NAT's
  wire type is a genuine single optional int64, so validate with a numeric
  string pattern, not firewall's range grammar). `nil`/absent → Terraform
  null; a real value → string.
- `RuleIndex` is exposed **read-only (`Computed`-only)**, following the
  `firewall_policy` `index` precedent
  (`unifi/firewall_policy_resource.go:316-327`) — but stronger than "assumed
  by analogy": the SDK serializes the field, but nothing in `nat.go`
  exposes a reorder endpoint, and there is no evidence (doc comment, second
  endpoint, or otherwise) that the controller accepts a client-supplied
  `rule_index` on Create/Update. Absent that proof, `Computed`-only is the
  only defensible schema shape — promising `Optional + Computed` ordering
  control without evidence it works is a feature claim this PR cannot back
  up. If live-controller testing later proves client-supplied ordering is
  accepted and honored, that is a schema-widening change for a future PR,
  not this one.
- `Exclude` (`json:"exclude"`, no `omitempty`) has no go-unifi doc comment
  explaining its semantics. Modeled as `optional+computed` bool, default
  `false`, with a schema description flagged **uncertain — needs
  live-controller confirmation** (plausibly "exclude this traffic from
  NAT" as a rule-level toggle, but unverified).

## 6. Uncertain wire fields — split into two lists

The prior revision's uncertain-fields list mixed genuine open questions with
one item this revision can now close from the SDK alone (MASQUERADE's field
set). Splitting these matters: §6a blocks nothing about writing or landing
this draft; §6b (in §11) is the acceptance-scope gate for a live-controller
pass, kept separate so neither list is diluted.

### §6a. Needs live-controller confirmation (does not block this draft)

go-unifi is frozen at 9.5.21-era codegen; none of these can be resolved by
reading the SDK alone. None of them block writing the schema — they are
documented as "assumed, needs live confirmation" in the resource's markdown
descriptions per field, consistent with how `firewall_policy_resource.go`
documents `matching_target_type`'s controller-derived behavior inline.

1. **`exclude` semantics** — undocumented in go-unifi; needs a live
   controller with an `exclude`-toggled rule to confirm meaning and UI
   label.
2. **Server-side default for an omitted `filter_type`/absent filter block
   on create** — assumed `NONE`, unverified.
3. **Whether `NoEdit=true` rules reject a PUT with a clean error** or are
   silently accepted/ignored — the precheck (§3) avoids sending the PUT in
   the golden path, but the *fallback* behavior (what happens if the
   precheck is bypassed, e.g. a stale local state where NoEdit flipped
   remotely between Read and Update) is unverified. Low risk since Update
   always reads fresh plan+state, but the precheck's state is only as
   fresh as the last Read.
4. **`in_interface` vs `out_interface` applicability per type** — the
   struct has both fields at top level with no doc comment restricting
   which rule types use which; the schema sketch's assumption ("DNAT uses
   `in_interface`, SNAT/MASQUERADE use `out_interface`") is inferred from
   NAT semantics generally (DNAT rewrites destination on ingress, SNAT/
   MASQUERADE rewrite source on egress) but not confirmed against actual
   controller-generated rules of each type.
5. **Whether `rule_index` supports client-controlled ordering at all** —
   the schema in this revision is `Computed`-only precisely because this is
   unconfirmed (§5). Do not change `rule_index` to `Optional + Computed`
   until a live controller demonstrates it accepts and honors a
   client-supplied value; this item exists to gate that future change, not
   to flag ambiguity in the current (Computed-only) schema, which is
   already the conservative, defensible choice.
6. **`is_predefined` + `setting_preference` interaction** — whether
   `setting_preference = "manual"` is how a user "adopts" a predefined
   rule to make it editable (i.e. whether `NoEdit` is *derived* from
   `SettingPreference` client-side by the controller, making the two
   flags correlated in practice even though nothing in the SDK asserts
   it) is unverified. If confirmed correlated, the §5.6 precheck logic is
   unaffected (it still gates on the authoritative `NoEdit` bit) but the
   schema description should say so for user clarity.

**Settled, not a live-controller blocker:** the MASQUERADE field set is
fully determined by the SDK for this draft's schema — `Nat` has no
MASQUERADE-specific fields beyond the shared top-level ones (§4.3), so
there is nothing MASQUERADE-shaped left to confirm against a live
controller for the purpose of this schema. (A live controller *might*
expose a MASQUERADE-only UI toggle this SDK snapshot doesn't model at all
— that is an optional SDK-completeness audit item, not a blocker: it would
mean go-unifi itself is incomplete, which is out of scope for this PR to
discover or fix. See §11's separate live-validation list, which does not
repeat this item as a blocker.)

## 7. Capability precheck (C6 / §6.1)

`unifi_nat` is a v2 resource, so C6's "v2 resources: typed API errors"
branch applies (not the settings-snapshot branch):

- **NotFound** on Read/Delete → `Unmaterialized`/absent, handled by
  `v2FinishRead` (Read) and the existing "ignore NotFound on Delete"
  pattern (`firewall_policy_resource.go` Delete: only non-NotFound errors
  raise a diagnostic).
- **Method/endpoint unsupported** (e.g. a controller/firmware without the
  `v2/api/site/%s/nat` endpoint at all — plausible on older
  non-gateway-capable controllers) → `Unsupported`. Surfaces as an error
  diagnostic on Create (never silent-null, per C6's "configured +
  Unsupported/Unauthorized → predictable error diagnostic"); on
  Read/ListResource, absence of the endpoint reads the same as an empty
  collection (no distinguishing error shape is available from `ListNat`
  beyond a generic transport error, so this is treated as a **transient/
  Unknown** case, not silently mapped to `Unsupported`, per C6's "Unknown
  ... a non-configured section is ignored" — but `unifi_nat`'s List has no
  "configured" concept the way settings sections do, so in practice this
  means: a `ListNat` error surfaces as a diagnostic on the list resource's
  stream, it is not swallowed).
- **401/403** → `Unauthorized`, same error-diagnostic treatment.
- **The `NoEdit`/`NoDelete` precheck (§3) is the resource-specific
  non-mutating capability check** this PR adds beyond the generic v2
  typed-error mapping: it is "non-mutating" in the sense required by
  §6.1 ("prechecks are non-mutating; where write capability is the thing
  under test, the mutation lives in the managed test lifecycle, not an
  out-of-band probe") because it inspects already-fetched state rather
  than issuing a separate probe request.

No new capability-state machinery is introduced by this PR; it consumes the
typed-error mapping pattern already established by every other v2 resource
(NotFoundError type assertion via `v2IsNotFound`), plus the one
resource-specific precheck above.

**Test-scope honesty on the precheck (per review):** a pure-predicate unit
test of the precheck function (§ Task 7 in the plan) proves the *predicate*
is correct — given `noEdit=true`, it returns blocking diagnostics; given
`noEdit=false`, it doesn't. It does **not**, by itself, prove that
Update/Delete's actual method bodies honor the precheck's return value by
skipping the `UpdateNat`/`DeleteNat` call — that requires either (a) a real
injectable seam (e.g. the client is an interface, and the test asserts the
interface's Update/Delete method is never invoked when the precheck fires)
or (b) an acceptance/live test that imports a `NoEdit=true` rule and
confirms no mutation reaches the controller. §11 names which of these two
this PR actually has available, rather than letting the unit test's
predicate-correctness stand in for request-non-invocation proof it doesn't
provide.

## 8. Site identity (C3, via PR-V) — composite-identity import, PR-V amendment required

Follows the table row already recorded in the parent design (§237-245):

| | |
|---|---|
| Import ID accepted | `<id>` or `<site>:<id>` |
| Persisted `id` | canonical `<site>:<id>` |
| `site` | derived from `id` |
| Change `site` | `RequiresReplace` |

**This contract cannot be satisfied by calling `v2ImportState` as-is, and
the prior revision of this spec was wrong to say it could.** Reading
`v2ImportState`'s actual body (`unifi/v2_resource.go:249-262`):

```go
func v2ImportState(
    ctx context.Context,
    req resource.ImportStateRequest,
    resp *resource.ImportStateResponse,
    providerDefault string,
) {
    site, id, err := parseSiteID(req.ID, providerDefault)
    if err != nil {
        resp.Diagnostics.AddError("Invalid Import ID", err.Error())
        return
    }
    resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("site"), site)...)
    resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
```

`v2ImportState` parses `<site>:<id>` (or bare `<id>`) and writes the **bare**
`id` to the `id` attribute, with `site` set as an independent sibling
attribute. That is exactly the `firewall_policy`/`port_forward` shape (bare
id + sibling site) — the shape this spec's C3 table says `unifi_nat`
*diverges from* in favor of a composite `id = "<site>:<id>"` with `site`
*derived from* `id`. Both cannot be true simultaneously: either `id` holds
the bare id (what `v2ImportState` actually writes today) or `id` holds the
composite string (what C3 mandates for NAT) — not both, and the function as
written only produces the former.

**Resolution, at the design level, for this draft:** this spec does **not**
special-case ID parsing inside `unifi/nat_resource.go` to work around the
gap (that would mean NAT rolling its own import-parsing logic parallel to
`v2ImportState`, defeating the point of the shared helper existing at all,
and it would leave PR-E with the identical unmet need — content-filtering
carries the same composite-id requirement per the parent spec's C3 table).
Instead:

- **PR-V requires an amendment** before `unifi_nat` (or PR-E's
  content-filtering resource) can be implemented: either (a) a second,
  composite-identity-flavored import helper (e.g. `v2ImportStateComposite`)
  that writes the *composite* string to `id` and leaves `site` to be
  derived from it on every subsequent Read/Update/Delete rather than stored
  independently, or (b) an explicit mode flag/parameter on `v2ImportState`
  itself selecting between the two id-shapes. Either shape is acceptable;
  this spec does not mandate which, since that is PR-V's implementation
  choice to make with its own tests, not something D should pre-decide by
  writing NAT-local code around it.
- **This is a blocking dependency for D's implementation, not for this
  draft.** This PR remains docs-only and a draft; the amendment is
  something a future PR-V change (or a follow-up to PR-V) must deliver
  before Task 8 of the implementation plan can be written as currently
  scoped. The plan (Task 0) now records this explicitly as a precondition
  check, not an assumption.
- **Shared with PR-E.** The same composite-identity need applies to
  content-filtering (per the parent spec's C3 table entry for that
  resource too), so whichever helper/mode PR-V adds should be written once
  and consumed by both D and E, not duplicated. Flagging this here so
  whoever implements the PR-V amendment does it generically rather than
  NAT-specifically.

Once that helper/mode exists, `unifi_nat` consumes it exactly as it would
consume any other PR-V helper — no NAT-local ID-parsing logic:

- `ImportState`: the PR-V composite-identity import helper (name TBD by
  PR-V's amendment), parameterized with `r.client.Site` the same way
  `v2ImportState` is today.
- Create/Read/Update/Delete: `resolveV2Site` still applies for the
  *configured* site value (Create, where there is no persisted `id` yet);
  Read/Update/Delete recover `site` from the persisted composite `id` via
  `parseSiteID` (`unifi/site.go`) — the same parser `v2ImportState` already
  uses internally, just applied to the stored `id` rather than the raw
  import string.
- Timeouts: `v2Timeout(ctx, model.Timeouts, v2TimeoutCreate|Read|Update|Delete)`.
- NotFound: `v2FinishRead(ctx, resp, err, "Error Reading NAT Rule")` in
  Read; direct `v2IsNotFound(err)` check in Delete (matching
  `firewall_policy`'s Delete — only non-NotFound errors raise a
  diagnostic).
- Identity+state tail: `v2SetIdentityAndState[natModel](ctx, resp.Identity, resp.State, model.ID, &model)`
  in Create/Read/Update, replacing the hand-written
  `SetAttribute`-then-`State.Set` pairs.
- Conversion: `objectAsOptions[natDnatModel](ctx, model.Dnat)` /
  `objectAsOptions[natSnatModel](ctx, model.Snat)` for the two discriminated
  blocks, and `objectAsOptions[natFilterModel](ctx, dnatModel.DestinationFilter)`
  one level deeper — this is the first real consumer of `objectAsOptions`
  outside `v2_resource.go`'s own tests, resolving the "`objectAsOptions`-in-NAT
  defect" the parent spec's PR-V section names as one of the reasons PR-V
  was split out (§379-388). See §9 for why `firewall_group_ids` does **not**
  use `objectListAsOptions`.

**Persisted-id shape divergence from `firewall_policy`/`port_forward`:**
those two resources persist the *bare* object id as `id` and keep `site`
as a separate sibling attribute (their `ImportState` splits `site:id` and
sets both attributes independently — see
`firewall_policy_resource.go:628-640`). C3's table instead specifies NAT
and content-filtering persist the **composite** `<site>:<id>` as `id`
itself, with `site` *derived from* `id` rather than stored independently.
This is a genuine shape difference from the existing reference resources
named in the task brief, not an oversight — `unifi_nat`'s `id` attribute
documentation and its `IdentitySchema` (`id` `RequiredForImport`) must
say "composite `<site>:<id>`," and Read/Update/Delete parse `state.ID`
back into site+bare-id (via `parseSiteID`, applied to the persisted `id`
rather than the raw import string) instead of reading `state.Site` as an
independent field.

## 9. Set-vs-list collection choices (§5.7)

"Order-meaningful → list; set-membership → set. Equivalent selector
families use the same type across resources."

- `firewall_group_ids` (inside `destination_filter`/`source_filter`):
  **set of strings** (`types.Set`, `SetAttribute`, element type
  `types.StringType`). Membership in a firewall-group reference list is
  not order-meaningful, and this is the same selector family as
  `firewall_group_resource.go`'s `members`
  (`schema.SetAttribute{ElementType: types.StringType}`,
  `unifi/firewall_group_resource.go:132-135`) and conceptually the same
  kind of "these firewall groups apply" reference set used elsewhere in the
  provider. Using `Set` here keeps NAT consistent with that existing family
  rather than introducing a third representation.
- **Decode helper correction (per review):** `firewall_group_ids` decodes
  via `types.Set`'s own `ElementsAs(ctx, &out, false)` into `[]string`
  directly (a flat string set has no nested object shape to decode through
  `objectAsOptions`/`objectListAsOptions` at all) — it is **not** a
  consumer of `objectListAsOptions[T]`. The prior revision of this spec
  wrongly claimed `firewall_group_ids` was decoded via
  `objectListAsOptions`; that helper
  (`unifi/v2_resource.go:300-315`, `list.ElementsAs(ctx, &out, false)` into
  `[]T`) is built for a `types.List` of **nested objects** — e.g. a
  `ListNestedAttribute` whose elements are themselves structured (multiple
  named fields per element), decoded into `[]someStructModel`. NAT
  introduces no such shape: nothing in this schema is a list of nested
  objects. `firewall_group_ids` is a flat `Set` of bare strings, which
  needs no `objectListAsOptions`-style per-element struct decode at all —
  a direct `types.Set.ElementsAs` (or the schema-level equivalent) into
  `[]string` is sufficient and is what NAT actually uses. Remove the
  `objectListAsOptions` claim from this design; it does not apply unless a
  real `ListNestedAttribute` (elements with more than one field) is
  introduced somewhere in this schema, which it currently is not.
- No other NAT field is a genuine collection at the top level — `unifi_nat`
  models one rule per resource (not a rule list embedded in one resource),
  so `ListResource` (§10 below) is what supplies the "many rules" view,
  not an in-schema list/set of rules.

## 10. ListResource (§5.9)

**Yes.** PR-V's recorded rule (`unifi/v2_resource.go:6-33`): every audited
*collection-shaped* resource in this package implements
`list.ListResource`; only true per-site singletons (BGP, `unifi_setting`)
don't. NAT rules are collection-shaped (many rules per site, each with its
own id, and the SDK exposes `ListNat` returning `[]Nat`) exactly like
`firewall_policy`/`port_forward`/`firewall_group`, so `unifi_nat` MUST
implement it. This PR applies the already-recorded rule directly — no new
audit, no coordination with PR-E.

Shape, following `firewallPolicyResource`'s implementation
(`firewall_policy_resource.go:1071-1221`) as the template:

- `NewNatListResource() list.ListResource` returning the same
  `*natResource` struct (both `resource.Resource` and `list.ListResource`
  satisfied by one type, per the existing pattern).
- `ListResourceConfigSchema`: `site` (`Optional` string) + `filter`
  (`ListNestedBlock` of `name`/`value` string pairs), matching
  `firewallPolicyListConfigModel`/`firewallPolicyListFilterModel` shape
  exactly (rename to `natListConfigModel`/`natListFilterModel`).
- `List`: calls `ListNat(ctx, site)`, applies post-filters (candidate
  filter names: `type`, `enabled`, `description` — mirroring
  `firewall_policy`'s `name`/`action`/`enabled` filter set, adapted to
  NAT's own salient fields), builds each result via a shared
  `natToModel`-based conversion (reusing the same conversion function Read
  uses, not a parallel hand-written flatten), sets identity `id` to the
  composite `<site>:<id>` (consistent with §8's persisted-id shape — the
  list resource's identity must match the managed resource's identity
  exactly, or `terraform-plugin-framework`'s "convert list result to
  config" workflow round-trips inconsistently).
- Registration: add `NewNatListResource` to `ListResources()` in
  `unifi/provider.go` (alongside `NewFirewallPolicyListResource` etc.), and
  `NewNatResource` to `Resources()`.
- Enum fields surfaced by `List`'s conversion (`type`, `protocol`,
  `ip_version`, `setting_preference`, and each filter's `filter_type`) use
  the same `OneOf` validators as the managed resource's schema — see §4/§6
  for the closed-enum list; `firewall_group_ids` is a `Set` in the listed
  model too, matching the managed resource (§9), not a list.

## 11. Real DNAT/SNAT acceptance coverage (not MASQUERADE-only)

§390 explicitly calls out that acceptance tests must exercise real
DNAT/SNAT rules, not just MASQUERADE (which is nearly fieldless and would
under-exercise the discriminator/filter logic in §4). Concretely, the SDD
plan's acceptance-test task must include, at minimum:

- A DNAT rule with `destination_filter.filter_type = "ADDRESS_AND_PORT"`
  (exercises `ip_address`/`port`/`invert_*`).
- A DNAT rule with `destination_filter.filter_type = "FIREWALL_GROUPS"`
  (exercises the `firewall_group_ids` set, likely needs a companion
  `unifi_firewall_group` in the test fixture, following
  `TestAccPortForward_sourceLimitingFirewallGroup`'s pattern of provisioning
  a firewall group resource inline in the test config).
- A SNAT rule with `source_filter.filter_type = "NETWORK_CONF"` (exercises
  `network_conf_id`, likely needs a companion `unifi_network` in the
  fixture).
- A `type` transition test (DNAT → SNAT or similar) asserting stale
  discriminator children are cleared (§4.1's plan-modifier behavior) — this
  is the direct regression test for "NAT stale selectors" (§4.2 in the
  traceability table).
- A `filter_type` transition test within one rule type (e.g.
  `ADDRESS_AND_PORT` → `FIREWALL_GROUPS`) — same reasoning one level
  deeper.
- A predefined/`no_edit` rule test: import a controller-seeded predefined
  rule (if the demo docker controller ships one — needs verification
  during plan execution; if it doesn't, this becomes a live-validation-only
  item per the parent spec's "Live validation" section rather than an
  automated TF_ACC test) and assert Update/Delete against it fail with the
  precheck's diagnostic. Per §7's test-scope-honesty note: assert this via
  whichever of (a) or (b) is actually available (a real injectable client
  seam proving `UpdateNat`/`DeleteNat` is never called, or an
  acceptance-level assertion that the controller-side rule is unchanged
  after the attempted apply) — do not claim the precheck's unit test alone
  proves the request was never sent.
- A MASQUERADE rule, retained as the simple/happy-path smoke test, not the
  only test.
- **Post-mutation reconciliation test (new, per review — see §12):**
  Create and Update each assert that the state written after the operation
  reflects a fresh `GetNat` read keyed on the response's id, not a
  transformation of the POST/PUT response body alone. Concretely: a test
  double or acceptance assertion that would catch a regression to "trust
  the echo body" — e.g. asserting Create/Update issue a `GetNat` call (via
  an injectable seam if the acceptance harness supports one) or, at
  minimum, an acceptance step that mutates server-observable derived state
  between the PUT and would only be caught by a real re-read.

All gated behind `TF_ACC=1` per the existing `TestMain`/demo-controller
pattern (`unifi/provider_test.go`), no live-UDM dependency, per the parent
spec's "Live validation is a separate, manual step" policy (§483-493) — NAT
is explicitly named there as one of the resources the demo controller
cannot fully exercise for *live* validation, but automated acceptance tests
against the demo controller are still the PR's own gate.

### §6b / Must-resolve-with-a-live-controller list (acceptance-scope gate, kept separate from §6a)

This is the authoritative "cannot be closed by SDK reading alone, and
implementation should not proceed past the draft stage on these specific
points without a live pass" list, consolidated in one place so it isn't
scattered across §3/§4/§6:

1. **Exclude semantics** — undocumented; needs a live controller with an
   `exclude`-toggled rule.
2. **Omitted-filter/block default behavior** — whether an absent
   `destination_filter`/`source_filter` on create defaults to `NONE`
   server-side (assumed, unverified).
3. **`NoEdit=true` PUT response when state is stale** — clean 4xx vs.
   silent accept/ignore, for the precheck-bypassed fallback path.
4. **Interface applicability/ownership by NAT type** — whether
   `in_interface` is genuinely DNAT-only and `out_interface` genuinely
   SNAT/MASQUERADE-only, or whether the controller accepts/uses both more
   flexibly per type.
5. **Whether client-controlled `rule_index` ordering is supported at
   all** — must be proven before ever exposing a writable `rule_index`;
   until proven, `rule_index` stays `Computed`-only (§5).
6. **`is_predefined`/`setting_preference` relationship** — whether they are
   correlated in controller behavior beyond what the SDK asserts.

**Explicitly not on this list:** the MASQUERADE field set (§6a) — that is
settled from the SDK for this draft's schema and is, at most, an optional
SDK-completeness audit item (whether go-unifi itself is missing a
MASQUERADE-only field the real controller UI exposes), not a live-controller
blocker for what this PR ships.

## 12. Post-mutation reconciliation (§5.2, new in this revision)

**This is a must-fix, not a nice-to-have.** The parent spec's §5.2/C2.5
principle — "post-mutation reconcile... No trusting of PUT response
bodies" — was written primarily for `unifi_setting`'s snapshot engine, but
it states a general principle, and the prior revision of this plan did not
apply it to NAT's Create/Update at all; it only specified the
identity+state *write* tail (`v2SetIdentityAndState`), silently assuming
the model fed into that tail could come from the POST/PUT response.

**Neither of this provider's two existing v2 reference resources actually
does this today** — `firewallPolicyResource.Create`
(`unifi/firewall_policy_resource.go:454-466`) calls
`firewallPolicyToModel(ctx, created, &plan)` where `created` is
`CreateFirewallPolicy`'s **response body**, not a subsequent `GetFirewallPolicy`
read; `portForwardResource.Create`
(`unifi/port_forward_resource.go:369-378`) does the same with
`CreatePortForward`'s response. Both templates trust the echo body. This
spec deliberately does **not** copy that part of either template for NAT:

- **Create**: after `CreateNat` returns an id (whether or not the response
  body is fully trusted for other fields), `unifi_nat`'s Create calls
  `GetNat(ctx, site, created.ID)` and builds state from *that* read via
  `natToModel`, not from `created` directly. If `GetNat` fails
  immediately after a successful `CreateNat` (unlikely but possible —
  eventual consistency, a transient error), this is a genuine error
  diagnostic on Create (the rule may exist server-side with an unconfirmed
  shape; do not silently fall back to the POST echo body, since that is
  exactly the trust `unifi_setting`'s §5.2 principle disallows) — the user
  can retry/import.
- **Update**: same pattern — after `UpdateNat` succeeds, Update calls
  `GetNat(ctx, site, id)` and builds state from that read, not from
  `UpdateNat`'s response body.
- **Read** already does this by construction (it only ever reads via
  `GetNat`), and **Delete** has no post-mutation state to reconcile (state
  is removed).
- **Rationale specific to NAT, beyond general §5.2 hygiene:** NAT has
  controller-assigned fields with no documented request-vs-response
  contract (`rule_index`, `is_predefined`, the `attr_*` flags) — trusting a
  POST/PUT echo body for these is exactly the kind of assumption §6a/§6b
  flag as unverified elsewhere in this spec. A fresh `GetNat` after mutate
  removes the need to additionally ask "does the create/update response
  body definitely include a correct `rule_index`/`attr_no_edit` etc." as
  its own open question — it becomes moot, because state always comes from
  the same Read path regardless of which operation preceded it.
- **This must be both specified and tested** (per review): the
  implementation plan's task covering Create/Update must include an
  explicit reconcile-read step (not just the state-write tail), and its
  test must assert the reconcile happens — e.g. a test double where the
  `CreateNat`/`UpdateNat` response body deliberately differs from what a
  subsequent `GetNat` would return, asserting the state written matches
  the `GetNat` shape, not the mutate response shape. A test that only
  checks "state was written" without differentiating the two sources would
  not catch a regression to the echo-body pattern and does not satisfy
  this requirement.

## 13. Privacy-safe fixtures

Per the parent design's privacy-scrub policy (§407-418): no real ObjectIDs,
IPs, or hostnames in fixtures/docs/examples. Use RFC 5737/3849 documentation
ranges (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`) for any
`ip_address`/`address` example values, synthetic firewall-group/network
names, and no `/Users/...` paths in any commit message this PR produces.

## Decisions resolved in this revision (no longer open)

1. ~~Resource type name~~ — **`unifi_nat`**, settled (§1). Not
   `unifi_nat_rule`; the provider's object-noun convention decides this
   unambiguously, no maintainer input needed.
2. ~~Whether an active discriminator's own block may be entirely absent~~ —
   **resolved differently per type** (§4.1): DNAT/SNAT require their block
   present; MASQUERADE has no block at all (removed, not "optional and
   empty").
3. ~~`protocol`/`ip_version`/`pppoe_use_base_interface`/`setting_preference`
   placement~~ — **top-level, flat**, settled (§4.3). Not a UX choice
   pending confirmation; it's the only placement consistent with the wire
   struct having exactly one instance of each field.
4. ~~Whether the `masquerade` block is worth modeling~~ — **no block at
   all** (§4.1). Superseded by the decision above; there is no block to
   evaluate "worth" for.
5. ~~Confirm the composite-id-with-derived-site identity shape~~ — the
   *shape* is confirmed as C3-mandated and intentional (§8), but
   **implementing it requires a PR-V amendment** that does not exist yet
   (§8) — this is now a tracked implementation blocker (Task 0 of the
   plan), not an open design question.
6. ~~`rule_index`: `Computed`-only vs `Optional + Computed`~~ — **`Computed`-only**,
   settled for this draft (§5): no evidence of a reorder capability exists,
   so the conservative schema is the only defensible one; widening it is a
   future change gated on live-controller proof (§6a item 5 / §6b item 5).

No decisions remain open for *this draft's* schema shape. The one
remaining blocker before implementation can start is the PR-V composite-
identity import amendment (§8), which is a dependency on a different PR,
not an open question within this one.
