# Design: feature-complete UniFi setting sections + missing resources

Date: 2026-07-10
Status: approved

## Goal

Close the configuration gap found by live enumeration of a UDM against this
provider: the controller exposes 42 `rest/setting` sections, the provider
handles 13. Add the missing site-scoped sections to the consolidated
`unifi_setting` resource, plus three non-setting gaps: NAT rules, content
filtering, and APP/DPI matching in `unifi_firewall_policy`.

Out of scope: `super_*` console-level settings; controller-generated or
read-only sections (`connectivity`, `element_adopt`, `peer_to_peer`,
`openvpn`, `ugw`, `super_cloudaccess`); `rest/scheduletask` (managed by the
controller alongside `auto_speedtest`).

## User-facing surface

- `unifi_setting` remains the **only** settings resource (embedded model).
  No per-section `unifi_setting_*` resources.
- New nested sections (Tier 1 = live config on the reference UDM today,
  Tier 2 = parity):
  - Tier 1: `global_switch`, `global_nat`, `global_network`, `mdns`,
    `teleport`, `magic_site_to_site_vpn`, `traffic_flow`, `ether_lighting`,
    `locale`, `ipsec`
  - Tier 2: `snmp`, `netflow`, `usg_geo`, `ssl_inspection`, `guest_access`,
    `provider_capabilities`, `radio_ai`, `dashboard`
- **Name alignment rule:** where filipowm/terraform-provider-unifi has an
  equivalent `unifi_setting_<x>` resource (`global_switch`, `teleport`,
  `magic_site_to_site_vpn`, `locale`, `ssl_inspection`, `ether_lighting`,
  `guest_access`), our nested attribute names match their field names
  exactly; we may expose a superset of fields. Sections filipowm lacks
  follow go-unifi/controller field naming. The existing 13 sections keep
  their current names — no breaking changes.
- New resources: `unifi_nat_rule`, `unifi_content_filtering`.
- `unifi_firewall_policy`: `matching_target` gains `APP` and `APP_CATEGORY`;
  source/destination endpoint objects gain `app_ids` and `app_category_ids`
  lists, consistent with the existing discriminator shape.

## Internal architecture

- **Incremental section-handler refactor** (no dedicated refactor PR): as
  each PR adds sections, they land as per-section files
  `unifi/setting_section_<name>.go` (model struct, attrTypes map, schema
  fragment, converters, tests) implementing a small handler interface
  (`key / attrTypes / schema / apply / read`). `setting_resource.go`
  Create/Update/Read iterate a registry. Existing sections migrate
  opportunistically when a PR touches them.
- Create and Update share one apply path: settings are PUT-only singletons.
- Shared read-modify-write helper (generic over the go-unifi setting type):
  GET current object → overlay only non-null configured fields → full-object
  PUT → re-read for state. A non-404 GET failure **aborts without PUTting**
  so a transient error never clobbers unmanaged fields (filipowm's
  `decideBase*` pattern); 404 falls back to a zero-value base.
- Sensitive fields: `magic_site_to_site_vpn` key material is computed +
  `Sensitive`, never required in config; all `guest_access` portal/payment
  credentials `Sensitive` (mirror filipowm's list); `snmp` community strings
  and v3 credentials `Sensitive`.
- `radio_ai` is co-managed by the controller via `setting_preference`:
  attributes use `UseStateForUnknown` and docs carry an explicit churn
  warning.

## go-unifi prerequisite

`ubiquiti-community/go-unifi` (local clone: `/Users/jamesb/projects/go-unifi`)
needs one PR covering:

- setting structs for `global_network`, `ipsec`, `usg_geo`,
  `provider_capabilities` (fields from the captured live payloads);
- exported NAT CRUD — `nat.generated.go` has the full client but lowercase
  (`listNat`, `createNat`, …);
- a content-filtering client for `v2/api/site/<site>/content-filtering`
  (absent entirely);
- `APP`/`APP_CATEGORY` in the firewall-policy `matching_target` enums plus
  the `app_ids`/`app_category_ids` fields.

The provider bumps `go.mod` after merge (a local `replace` directive is fine
during development, dropped before release); gated sections/resources ride in
whichever themed provider PR is open when the bump lands, or a small trailing
PR.

Captured live payloads (this session, read-only, scratchpad — contain
secrets, never copy values into committed docs): `udm-settings.json` (all 42
sections), `udm-content-filtering.json`, `udm-firewall-policies.json`.

## New resource shapes

- `unifi_nat_rule` (v2 API, list-only reads → list-then-filter): `type`
  (MASQUERADE/SNAT/DNAT), interface/WAN, source/destination matchers, ip
  address, `enabled`, `description`, `index`. Shape validated against the
  live masquerade rule captured during enumeration.
- `unifi_content_filtering`: name, target networks, blocked
  categories/domains — shape from the two live policies.
- Firewall APP matching: extends the existing `matching_target`
  discriminator; the live "block shield DNS" APP policy becomes importable.

## Testing

- Unit tests per section: model→setting overlay, setting→model round-trip,
  and merge semantics proving unset fields preserve remote values.
- Acceptance tests run **only** against the docker-compose demo controller;
  sections the demo controller doesn't support get probe-once skip-guards
  (`t.Skip` with reason). The live UDM is never mutated by tests.
- Per-PR manual validation: `tofu plan` against the live UDM expecting
  either no-op or exactly the drift the PR intends to capture.

## PR sequence

Each PR is independently shippable: code + unit tests + acceptance tests +
docs + CHANGELOG entry.

**Review gate: PRs are prepared as local branches only. Nothing is pushed or
posted publicly (provider repo or go-unifi) until James reviews it.**

0. **go-unifi structs** (prereq; parallel with PRs 1–2)
1. **Switching & NAT globals:** `global_switch`, `global_nat`, `locale` —
   introduces the handler pattern + shared RMW helper
2. **Connectivity services:** `mdns`, `teleport`, `magic_site_to_site_vpn`,
   `traffic_flow`, `ether_lighting`
3. **Monitoring & security parity:** `snmp`, `netflow`, `ssl_inspection`
   (+ `global_network`, `usg_geo`, `ipsec` if the go-unifi bump has landed)
4. **Portal & long tail:** `guest_access`, `radio_ai`, `dashboard`
   (+ `provider_capabilities`)
5. **New resources:** firewall_policy APP matching, `unifi_nat_rule`,
   `unifi_content_filtering`

Anything still gated on go-unifi after PR 4 gets a small trailing PR rather
than blocking the train.

**Release flow:** all work is implemented and merged on James's fork first
(the existing `registry.terraform.io/jamesbraid/unifi` temporary-fork
pipeline), built, and live-tested against the UDM via `~/ansible/infra/unifi`
before any upstream PRs to ubiquiti-community are opened. The merge/PR
strategy (stacking, collapsing, ordering) is decided after implementation,
looking at the real diffs.

## Prior-art notes (research summary)

- **filipowm** (de-facto community fork, active): 25 per-section
  `unifi_setting_*` singletons over a generic base; covers 7 of our targets;
  no NAT (their open issue #104) or content filtering; DPI matching lives in
  `unifi_firewall_zone_policy`'s `web{}` block. Patterns adopted:
  abort-on-GET-failure RMW overlay, read-after-write for settings,
  Create==Update, sensitive-field list for guest_access.
- **terrifi**: plugin-framework from scratch, zero settings coverage;
  adopted ideas: `TODO(go-unifi):` workaround tagging, two-tier
  docker/hardware acceptance harness concept.
- Upstream paultyng (archived) originated the per-section convention
  (`setting_mgmt`/`radius`/`usg`); this fork deliberately consolidated, and
  stays consolidated with aligned names instead.
