# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### 🐛 Bug Fixes

- **Six attributes the controller owns were being overwritten by a value the provider invented.**
  This is one defect with six instances, and they are grouped because the shape matters more than
  the list. Each attribute carried a static default in the schema. Terraform fills a default in
  *before* the provider is consulted, so a configuration that never mentioned the attribute still
  planned the default — and the write path, seeing a value that was always known, sent it. A setting
  the controller held was replaced by one nobody asked for, on every apply.

  | resource | attribute | controller held | provider wrote |
  | --- | --- | --- | --- |
  | `unifi_wan` | `type` | `static` | `dhcp` |
  | `unifi_wlan` | `minrate_setting_preference` | `manual` | `auto` |
  | `unifi_network` | `lte_lan` | `false` | `true` |
  | `unifi_network` | `ipv6_interface_type` | a real interface type | `none` |
  | `unifi_network` | `dhcp_v6_server.dns_auto` | `true` | `false` |
  | `unifi_radius_profile` | `use_usg_auth_server` | `true` | `false` |

  Each was reproduced against a 10.4.57 controller with one fixture and only the provider binary
  differing: import, plan with the attribute omitted, apply, then read the controller back. The
  clearest is `use_usg_auth_server` — the controller held it on, the plan showed `true -> false`, the
  apply exited 0, and the controller then held it off. An apply that mentioned nothing turned a
  setting off.

  In every case the default is dropped and `UseStateForUnknown` added, so omitting the attribute
  keeps whatever the controller holds. Each value is one a practitioner could legitimately choose,
  which is what made the fault invisible: `none` is in `ipv6_interface_type`'s own list of accepted
  values, so "the controller was never asked" was indistinguishable from "turn IPv6 off".

  If you added an explicit value to work around one of these, it can be dropped. Upgrading plans no
  changes against existing state.

- **`unifi_network`: a `dhcp_v6_server` block could not be applied unless the IPv6 attributes were
  stated explicitly.** The apply failed naming `dhcp_v6_server.enabled`, `.start` and `.stop`, which
  reads like three faults in one block. It was two faults, neither of them in the block.

  The first is the default above. `ipv6_interface_type` defaulting to `none` switched IPv6 off and
  `enabled` went down with it — established by an A/B against the same controller with the same
  binary: with the attribute omitted the apply exits 1 and the controller's record loses
  `dhcpdv6_enabled` entirely; with `ipv6_interface_type = "static"` stated, the same apply exits 0
  and every field survives.

  The second only became visible once the first was fixed and IPv6 stayed on. `ipv6_static_subnet`,
  `dhcp_v6_server.start` and `.stop` are assigned by the controller but were `Optional` and not
  `Computed`, so a configuration omitting them planned null while the read brought the controller's
  value back, and the apply aborted — `.dhcp_v6_server.start: was null, but now
  cty.StringVal("fd41:9::2")`. That is the same fault as `ap_group_ids` below, on a different
  resource. All three are now `Optional + Computed`.

  **All three carry a measured abort naming the attribute** — the failure was observed for each,
  not inferred from the other two. That is worth stating because `network_id` below is the one
  attribute fixed in this release where it was *not*: its case argues from the read and update
  paths, is labelled inferred, and records the measured fact that no run names it as the subject of
  an abort. `ap_group_ids` has its own observed failure, as these three do.

  Two things follow that are easy to get backwards. Fixing a defect is how the second one was found,
  not a regression it introduced — it had been hidden behind the larger failure the whole time. And
  `dhcp_server`'s own `start`/`stop` pair is untouched and was never affected; the two pairs share
  attribute names under different parents, and only the `dhcp_v6_server` pair moved.

- **`unifi_wlan`: a configuration that omits `ap_group_ids` could not complete an apply at all.**
  The controller always returns an AP group, the read path copied it into state, and Terraform
  rejected the result against a null plan with `Provider produced inconsistent result after apply:
  .ap_group_ids: was null, but now cty.SetVal([...])`. `ap_group_ids` and `network_id` were
  `Optional` and not `Computed`, which tells Terraform the practitioner owns the value and the
  provider must leave it null. That is wrong for a value the controller assigns. Both are now
  `Optional + Computed` with `UseStateForUnknown`. A configuration that sets either is unaffected.

  The two attributes do not have equal evidence and the ledger entry says so: the aborting apply was
  observed for `ap_group_ids`, while `network_id` is argued from the read and update paths and
  labelled inferred.

- **`unifi_wlan`: changing `minrate_setting_preference` to `auto` failed the apply.** Handing the
  minimum data rates to the controller makes it recompute them, but both rate attributes promised
  Terraform the stored value would survive, so the apply died with `.minimum_data_rate_5g_kbps: was
  cty.NumberIntVal(0), but now cty.NumberIntVal(6000)` — with the preference flip as the only change
  in the plan. The rates now stay at their stored value except when that sibling is changing, and
  are left unknown then so the controller may supply them. Planning them unknown unconditionally
  would have shown a spurious "(known after apply)" on every plan.

- **`unifi_wlan`: a data rate the controller does not report is no longer recorded as `0`.** The
  controller omits `minrate_n{a,g}_data_rate_kbps` when they are unset, and the read path turned
  that absence into zero. Zero is a rate a practitioner can legitimately request — it is in both
  attributes' accepted values — so storing it for "the controller said nothing" recorded a value the
  controller never gave, indistinguishable from one it did.

### 📋 Known Issues

- **`unifi_vpn_server` still drops the third and fourth DNS servers.** A VPN server configured with
  four DNS servers writes only two. The cause is in the `go-unifi` SDK rather than in this provider:
  its marshaller emits two of the four slots. The SDK fix is written and pushed but **not tagged**,
  so this provider cannot consume it yet, and no amount of provider-side change fixes it. It is
  listed here rather than left out because this release fixes other things and a note that mentions
  only what was fixed reads as a clean bill of health.

### 📖 Documentation

- **`unifi_network`'s `lte_lan` description no longer promises a default it does not have.** The
  released text said "Defaults to `true`", and the fix above removed that default, so the prose
  asserted something false about the behaviour shipping beside it. It now says the value is read
  back from the controller and explains what the old default did wrong.

  Correcting it required building something first. The schema baseline gate treats the released
  provider's schema as authoritative for every field of every attribute and had no way to accept a
  change that was intended — its own failure message named a file to record one in, but that file
  is a state-migration policy the test never reads, and its type has no field that can carry a
  description. A deliberate change had nowhere to be declared. There is now a declaration that names
  the surface, the attribute, the field and the exact old and new values, states why the change
  cannot break an existing configuration, and labels each supporting claim measured or inferred.
  Both values must match, so a later drift to a third value fails again rather than living inside a
  permanent exemption.

### 🔧 Maintenance

- **The release gate over the migration manifest was missing six classes of defect that another
  check already caught.** Two functions with the same name in different packages validated the same
  artifact — one when it is generated, one when the release is qualified — and neither was a
  superset of the other. Six things the first rejects passed the second: an identity migration
  carrying an attribute mapping, a state move, an import transform or a schema version change,
  entries out of order, and a shift in the per-kind surface counts. The second is the only guard
  over the committed file, so those six were unheld at exactly the point a hand edit or a stale
  regeneration would land. The shared name is why it stayed invisible from either side.

  One now calls the other and keeps its release-specific rules on top. Two deliberately stay where
  they are: the generator legitimately validates manifests that are not identity migrations, while
  the release asserts that all 67 surfaces migrate by identity with snapshot recovery. Those are
  claims about this release rather than about manifests in general.

  **The per-kind count is the case worth reading twice.** The release gate checks that the manifest
  has 67 entries, which a shift of one managed resource into one data source satisfies exactly.
  Three further checks compare the receipts against each other, and each fires in turn as they are
  moved one at a time — until all four move together, which is what a regeneration produces, and
  then every one of them falls silent. Only the first of those three involves the manifest at all;
  the others compare two of the remaining receipts. **A check that compares two records cannot see
  them drift as a pair, and a coherent regeneration satisfies every pairwise comparison in the set.**
  Only an absolute count catches that, and this gate had none.

  Wiring the validator in failed the happy path immediately, because the test fixture was unsorted
  where the real manifest is sorted. The fixture modelled a manifest the generator cannot emit, so
  every test built on it had been running against a shape that could never arrive. That is the
  second fixture found this way in this release; both were found by pointing a real check at
  something that had only ever had to satisfy its consumer.

---

## [v0.102.0] - 2026-08-16

### 🐛 Bug Fixes

- **Nested configuration blocks could not be written at all.** The provider declared a distinct Go
  type for every nested block in its schema — `dhcp_server` on `unifi_network`, `wireguard` on both
  VPN resources, `source` and `destination` on `unifi_traffic_route`, and 49 others — and then never
  produced a value of any of them. Every one of those blocks was populated at runtime as a plain
  object. Terraform checks the value against the type the schema declares, finds a plain object
  where a specific type was promised, and rejects the apply with a value conversion error. Any
  configuration setting one of the affected blocks failed on every apply. 52 bindings across 15
  packages were affected, on `unifi_wan` (12), `unifi_network` (7), `unifi_vpn_server` (5),
  `unifi_traffic_route` (5), `unifi_wlan` (3), `unifi_vpn_client` (3), `unifi_radius_profile` (2),
  `unifi_firewall_policy` (2), `unifi_bgp`, `unifi_client` and `unifi_power_supervisor` (1 each),
  plus four data sources.

  The bindings are removed. The 35 custom types that carry real validation — MAC addresses,
  durations, IP addresses and prefixes — are deliberately kept; those are the ones where the custom
  type *is* the check, and dropping one would silently accept any string.

  The schema Terraform serves is unchanged, and that is structural rather than a spot check. All 51
  of the generated types embedded the framework's own object type and not one of them overrode the
  method that decides the wire representation, so each was already indistinguishable on the wire
  from the plain object that replaces it. (51 types for 52 bindings: `unifi_wan` binds the same
  `Options` type at two places.) That is also why no comparison against the previous release could
  ever have shown the fault — there was nothing in the served schema to differ. It was found by
  running both providers against a real controller and diffing the results: 54 tests that pass on
  v0.101.2 failed here, and after the fix the whole suite passes with the released side unchanged
  as a control.

- **`unifi_network`: fix `setting_preference` planning itself back to `auto`.** The attribute
  shipped with `Default: "auto"`. A default is applied before the controller is consulted, so a
  network the controller holds as `manual` planned a change back to `auto` on every run the moment
  the attribute was absent from the configuration — and never settled, because applying it did not
  change what the controller returned. Attaching a network to a `unifi_firewall_zone` is what makes
  the controller hold `manual`, so any zoned network written without an explicit
  `setting_preference` had a permanent diff. It is now `Optional + Computed` with no default and
  `UseStateForUnknown`, matching `purpose` on the same resource, so leaving it out keeps whatever
  the controller holds. Upgrading plans no changes against existing state, and an explicit
  `setting_preference` added to work around the diff can be dropped.

- **`unifi_network`: fix creating a `third_party_gateway` network without any DHCP or IGMP option
  set.** The vlan-only branch of the post-write read copied the planned `setting_preference` through
  unchanged, which was safe only while the schema default guaranteed it was already known. With the
  default gone it is unknown on create, and Terraform rejects the result with "Provider returned
  invalid result object after apply". It now resolves from the controller's answer, which for a
  vlan-only network is absent — the same treatment `multicast_dns` and the IPv6 attributes in that
  branch already had.

- **`unifi_device`: keep the adoption result and the configured name after adopting a device.**
  Immediately after a device is adopted the controller can still report the previous adoption state
  and the previous name, because it applies both asynchronously. The provider read that stale
  response back over the result of its own successful adoption, so a device that had just been
  adopted was recorded as not adopted, and a name set in the configuration was replaced by the
  controller's old one. The next plan then showed a difference that applying could not settle.
  Create now keeps the adoption result and the configured name, alongside the port overrides and
  plan-only flags it already preserved, and a later refresh picks up whatever the controller settles
  on.

### 🔧 Maintenance

- **A new test compares the schema the provider serves against the code that fills it in.** This
  class of fault was invisible to every check the project had. All of them compare this provider
  against the previous release, and on the wire the two schemas are identical — so the comparison
  was blind to it by construction, not by oversight.

  The new test compares the two halves of the same provider instead. For every object-valued
  attribute and block in every registered resource and data source, at any nesting depth, it rejects
  a nested object bound to a type the provider cannot produce a value of, requires a runtime model
  whose fields match the members exactly, and rejects any model field typed against a generated
  type.

  It is checked by retrodiction rather than by argument: run against the commit before the fix, it
  reports all 52 bindings — compared as a set against the known list, none missed and none invented.
  That run names `unifi_firewall_policy`'s `source` and `destination`, which is the case the test
  exists for, because the controller suite could not reach them at all.

  Its limits, because a check that hides them is worth less than one that states them. Nine
  attributes resolve to more than one candidate model — several unrelated blocks legitimately
  declare the same members, such as the three that declare `enabled` and `servers` — and for those
  nine the test cannot tell which model is wrong. They are listed by name in the test and compared
  as a set, so a tenth cannot appear unnoticed and a resolved one cannot linger.

- **`unifi_firewall_policy` and `unifi_site_to_site_vpn` have acceptance tests for the first time.**
  Neither was exercised by anything that talks to a controller; `unifi_firewall_policy` in
  particular has sixteen uses on the author's own network and had no managed acceptance test at all.

- **Most of this release's diff is invisible on purpose.** 42 files under `unifi/` changed as
  surfaces moved to generated schemas, and the schema the provider serves still matches v0.101.2
  exactly — checked against the released baseline rather than assumed. There is nothing to announce
  about the conversion itself, which is the point of doing it that way.

---

## [v0.101.2] - 2026-08-02

### 🐛 Bug Fixes

- **`unifi_port_profile`: make `tagged_networkconf_ids` an exact tagged-VLAN set.** Port profiles store Custom tagged VLANs as the inverse `excluded_networkconf_ids` list, but the provider accepted an include-list it could neither send nor read. Applying one dropped the requested set, could put the profile into the opposite controller configuration, and then failed with `Provider produced inconsistent result after apply`. The provider now lists the site's VLAN networks, writes the complement, and reconstructs the actual tagged set on refresh and import. An empty include-list maps to the UI's Block All mode. `tagged_vlan_mgmt = "auto"` maps to Allow All. The raw exclusion list remains available for existing configurations, but it cannot be configured together with the exact include-list. A VLAN created in the same apply is discovered on the following refresh, and that next apply adds it to the exclusions.

- **`unifi_port_profile`: keep forwarding mode consistent with tagged-VLAN mode.** The controller stores Allow All with `forward = "all"`, Block All with `"native"`, and Custom with `"customize"`. The provider now derives that pairing when `forward` is omitted and rejects conflicting explicit combinations instead of accepting a plan the controller will normalize after apply.

### ✨ Features

- **The `unifi_port_profile` data source now reports the stored VLAN mode, actual tagged-network set, and raw exclusion set.** `tagged_networkconf_ids` is derived from the site network inventory instead of always returning null. `tagged_vlan_mgmt` and `excluded_networkconf_ids` expose the controller representation when it matters.

---


## [v0.101.1] - 2026-08-02

### 🐛 Bug Fixes

- **`unifi_wlan`: fix `roaming_assistant_na_enabled` and `roaming_assistant_6e_enabled` planning themselves off.** Both shipped in v0.101.0 with `Default: false`. A default is applied before the controller is consulted, so any WLAN that already had roaming assistance enabled planned a change turning it off the moment the attribute was absent from the configuration — which is every configuration written before v0.101.0. Both are now `Optional + Computed` with no default and `UseStateForUnknown`, so leaving them out keeps whatever the controller holds. Upgrading from v0.101.0 removes the spurious diff. No configuration change is needed, and anyone who added an explicit `= true` to work around it can drop it again.

### 🔧 Maintenance

- **A schema test now pins every `Optional + Computed` attribute that also carries a `Default`.** That combination is what caused the bug above and #323 before it: `Computed` says the controller may own the value, and a `Default` overrides it. The inventory lives in `unifi/testdata/optional_computed_defaults.txt` (166 attributes), and a new one fails the build until it is added deliberately. The list is a record of what still needs checking against a live controller, not a set of approved patterns.

---


## [v0.101.0] - 2026-08-01

### ⚠️ Breaking Changes

- **`unifi_device`: `radio_table.assisted_roaming_enabled` and `radio_table.assisted_roaming_rssi` are removed.** UniFi Network 10.x dropped the per-radio assisted roaming setting — the controller no longer stores or returns either field, so the attributes could only report a value the provider had made up. The equivalent control moved to the WLAN and is exposed in this release as `unifi_wlan.roaming_assistant_na_enabled` / `roaming_assistant_na_rssi` and the `_6e_` pair. Existing state is migrated by a schema upgrader (v1 → v2), so no manual state edit is needed, but a configuration that sets either attribute now fails to plan and has to be updated.

### ✨ Features

- **`unifi_setting`: enable the site's RADIUS server with `radius.enabled`.** A `unifi_vpn_server` that authenticates against the controller's own accounts rather than an external RADIUS server points at the built-in `Default` profile, and the controller holds those accounts in its own RADIUS server. Create one while that server is off and it answers `api.err.RadiusServerNotEnabled`. The UI enables it inline, prompting for the pre-shared key as you create the VPN server. The provider's `radius` block exposed the ports, secret and accounting toggle but not the switch itself, so a configuration could not reach the same state. A VPN server pointing at an external RADIUS profile is unaffected and needs nothing extra.

    ```hcl
    resource "unifi_setting" "radius" {
      radius = {
        enabled = true
        secret  = "..."
      }
    }

    resource "unifi_vpn_server" "ovpn" {
      depends_on       = [unifi_setting.radius]
      radiusprofile_id = data.unifi_radius_profile.default.id
      # ...
    }
    ```
- **`unifi_wlan`: manage the roaming assistant.** Four new `Optional + Computed` attributes — `roaming_assistant_na_enabled` / `roaming_assistant_na_rssi` for 5GHz, `roaming_assistant_6e_enabled` / `roaming_assistant_6e_rssi` for 6GHz. The assistant disconnects a client whose signal drops below the threshold so it reassociates with a closer AP. The two RSSI ranges are not the same: 5GHz accepts `-80` to `-60`, 6GHz accepts `-90` to `-70`. These replace the per-radio `unifi_device` attributes removed above.

### 🐛 Bug Fixes

- **`unifi_network`: `dhcp_guarding` now takes effect on corporate and guest networks.** Two separate faults had to be fixed. The network encoder sent `dhcpguard_enabled` without the `dhcpd_ip_1..3` trusted-server slots the controller requires alongside it, so the controller rejected creates and updates with `api.err.MissingIPAddress` — including an unmodified round trip, which left an already-guarded network unmanageable once created. Underneath that, the provider sent `setting_preference = "auto"` (the attribute's default), and on `auto` the controller manages the advanced block itself and stores `false` for `dhcpguard_enabled` however it was sent. That write returns success, so the setting silently never applied. The trusted-server slots and the paired `dhcpd_mac_1..3` are now sent, and `setting_preference` switches to `manual` on its own when the plan needs it (below).
- **`unifi_network`: settings the controller only honors under `setting_preference = "manual"` now switch it automatically.** On `auto` the controller discards `dhcpguard_enabled`, `igmp_snooping` and the `dhcpd` DNS, NTP and time-offset toggles, storing `false` whatever the payload said, and re-enables its built-in DHCP server, which turns `dhcp_relay` off. All of it failed silently, so a configured DHCP DNS server was stored and never handed out. `setting_preference` already switched to `manual` for `dhcp_relay`. It now also switches when the plan enables `igmp_snooping`, `dhcp_guarding`, or any of the three `dhcp_server` toggles. Only a `true` triggers it, and an explicitly configured `setting_preference` is still left alone. **This shows up as a one-time plan diff** (`setting_preference` `"auto"` → `"manual"`) for an existing network that enables any of those and does not set `setting_preference` itself. Set `setting_preference = "auto"` explicitly to keep the old behaviour, at the cost of those settings continuing to have no effect.
- **`unifi_network`: WAN and vlan-only networks stop losing fields on a round trip.** WAN dropped `setting_preference` and `ipv6_setting_preference`, vlan-only dropped `mdns_enabled`. Reading one of these networks and writing it back discarded the stored value, so for vlan-only, disabling mDNS and then saving any other change turned it back on.
- **`unifi_network`: `enabled = false` now works on a vlan-only network.** The encoder previously forced `enabled=true` for that purpose and ignored the field, so a disabled vlan-only network could not be created and a read-modify-write silently re-enabled one.
- **`unifi_network`: corporate and guest networks no longer pin `dhcpd_leasetime`, `gateway_type` and `networkgroup`.** The encoder substituted `86400`, `"default"` and `"LAN"` when these were left unset. The controller supplies `networkgroup` itself and stores nothing for the other two, so a network now follows whatever default applies rather than the encoder's choice.
- **`unifi_ap_group`: fix `device_macs` written as `AA-BB-CC-DD-EE-FF` failing the apply.** The controller returns MACs lower-case and colon-separated, and the create and update paths overwrote the configured value with that form, so an apply of a config written any other way ended in `Provider produced inconsistent result after apply`. A refresh rewrote it the same way. The attribute's element type does compare MACs semantically, but a set identifies its members by their string value, so that never reached the set. Create, update and refresh now keep the representation already in state when the controller returns the same addresses, and take the controller's when the membership actually differs. Rewriting an applied `aa:bb:…` as `AA-BB-…` also plans empty: Terraform never consults semantic equality while building a plan, so `device_macs` became `Optional + Computed` and a plan modifier holds the stored value when the configured addresses match.
- **`unifi_vpn_server`: fix creating an OpenVPN server failing with `api.err.InvalidPayload` (400).** `openvpn.encryption_cipher` accepted and defaulted to `AES_256_GCM`, which the controller does not take: it answers `api.err.InvalidValue` naming the pattern `AES_256_CBC|BF_CBC`, and the SDK has declared that same pair all along. Every OpenVPN server that did not override the default therefore failed to create. The default is now `AES_256_CBC` and `AES_256_GCM` is no longer offered — a configuration that sets it explicitly now fails validation with the accepted values rather than a 400 from the controller. The create also stopped sending the controller-generated `x_ca_crt`, `x_ca_key`, `x_dh_key` and `x_server_crt` as empty strings, which is wrong independently of the cipher.

### 🔧 Maintenance

- **go-unifi updated to v1.101.0**, which is where the network encoder fixes above come from. Two settings objects moved on the controller as part of UniFi Network 10.x: geo IP filtering left the `usg` setting for a separate `usg_geo` object, and IPS suppression left `ips` for `ips_suppression`. The Terraform schema is unchanged — `usg.geo_ip_filtering_*` and `ips.suppression_alerts` / `suppression_whitelist` stay exactly where they were, and no state migration is required — but the provider now reads and writes those attributes through the new objects. Two consequences: a controller that does not expose them reports an explicit error when the attributes are configured (it previously wrote them to an endpoint that quietly ignored them), and the first plan after a controller upgrade may re-apply an existing geo IP filtering config once. Geo IP filtering attributes left unset in Terraform are no longer written at all, so a configuration set in the controller UI survives.

---


## [v0.55.0] - 2026-07-10

### ✨ Features

- **`unifi_ap_group`: manage AP group membership.** Full CRUD, complementing the existing read-only data source. Which APs belong to a group was fixed in the controller UI: the data source could read a group, but nothing could create or edit one, so `unifi_wlan.ap_group_ids` could only reference groups built by hand. The resource writes membership through the v2 `apgroups` API. `device_macs` reuses the `unifi_client` MAC type, so `AA-BB-…` and `aa:bb:…` read back equal rather than churning the plan on every refresh. Import takes the group ID, or `site:id` for a non-default site (#359, go-unifi#52).

### 🐛 Bug Fixes

- **`unifi_firewall_policy`: fix creating a policy that matches an IP group failing with `api.err.EmptyFirewallDestinationIps` (400).** A `source`/`destination` referencing an address group via `ip_group_id` (#316) must be sent with `matching_target_type = "OBJECT"`, but the #293 derivation back-filled an empty type as `SPECIFIC` for any non-ANY match — and on create the type is never controller-assigned, so every create with `ip_group_id` was rejected and only literal `ips` worked. A group reference now derives `OBJECT`, also overriding a stale `""`/`"ANY"`/`"SPECIFIC"` carried in state so switching an existing policy from literal `ips` to a group reference works on update too; a controller-assigned `OBJECT`/`LIST` is still preserved (#365, #316, #293)
- **`unifi_device`: carry `switch_vlan_enabled`, `radio_table[].vwire_enabled`, and `mesh_sta_vap_enabled` in the update PUT.** The update PUT was assembled from a minimal `Device` that dropped several configured fields, so the controller never received them and every apply that set one failed with `inconsistent result after apply` (`was cty.True, but now cty.False`). Fixed for: `switch_vlan_enabled` (the "Port VLAN" toggle, e.g. an AP with a built-in switch, where the toggle is what makes VLAN tagging take effect on the built-in ports); `radio_table[].vwire_enabled` (the "Mesh Parent" toggle — the whole `radio_table` was omitted from the minimal PUT, dropping every radio sub-field); and `mesh_sta_vap_enabled` (the "Mesh Connect" toggle, newly added to the `unifi_device` schema as an `Optional + Computed` bool). All are now carried in the PUT body when configured. `omitempty` (at every level, including `radio_table`) keeps a `false`/empty off the wire, so it never disturbs the controller default. Verified against a real controller (#363)
- **`unifi_device` / `unifi_setting`: stop controller-managed lists churning to "known after apply" on unrelated edits.** Several `Optional + Computed` lists were replanned as `(known after apply)` whenever any other field on the same resource changed — a spurious diff (the same class as #338). They now use `UseStateForUnknown`, keeping their prior value unless explicitly changed: `unifi_device` `radio_table` and `outlet_overrides`, and `unifi_setting` `contents` (syslog facilities), `server_names` (DoH), `enabled_categories` / `enabled_networks` (IPS), and `network_ids` (IGMP snooping).
- **`unifi_ap_group`: allow empty membership and stop empty groups reading back as `null`.** `device_macs` was `Required` with a `SizeAtLeast(1)` validator, and the read mapped an empty member list to `SetNull` — so a group the controller legitimately allows to have zero members (the API returns 201 for an empty membership) could not be authored, and importing one surfaced as an empty-vs-`null` inconsistency. `device_macs` now accepts an empty set and reads empty back as an empty set. The built-in default "All APs" group (which the controller marks read-only) is documented as non-editable through the resource.

---


## [v0.54.1] - 2026-07-05

### 🐛 Bug Fixes

- **`unifi_radius_profile`: make `auth_server` / `acct_server` `ip` optional so the default profile can be imported.** The controller-managed default RADIUS profile (created when a gateway RADIUS/VPN service is enabled, with `use_usg_auth_server = true`) returns a server entry without an IP. `ip` was `Required`, so re-declaring an imported profile failed with `The argument "ip" is required`, and an empty IP read back as `""` instead of null. `ip` is now `Optional` and an absent IP maps to null, so the default profile round-trips cleanly (#356)

---


## [v0.54.0] - 2026-07-02

### ✨ Features

- **`unifi_network`: expose the network `purpose` (`corporate`, `guest`, `vlan-only`).** A new `Optional + Computed` attribute. The provider previously hard-coded `corporate` (or `vlan-only` for a `third_party_gateway`), so a `guest` network could not be authored and a controller-assigned guest purpose was silently fought on every apply. `purpose` is now sent when configured and read back from the controller. **Note:** on Zone-Based-Firewall controllers the purpose is coupled to the firewall zone — a `guest` network only keeps `purpose = "guest"` while it belongs to the guest/Hotspot zone (assign it there via `unifi_firewall_zone`); placed in a non-guest zone the controller rewrites it back to `corporate`. `third_party_gateway = true` still forces `vlan-only` for backward compatibility (#276)
- **`unifi_firewall_policy`: make `connection_state_type` / `connection_states` author-settable.** Both attributes were `Computed`-only, so setting them returned `Invalid Configuration for Read-Only Attribute` — you could not author a policy scoped to a specific connection state. They are now `Optional + Computed`: leave them unset and the controller manages them as before, or set `connection_state_type = "CUSTOM"` with `connection_states = ["NEW", …]` (or `RESPOND_ONLY`) to author, for example, a `NEW`-only logging/deny policy that coexists with stateful returns in a zone-based firewall. Values are validated (`ALL`/`RESPOND_ONLY`/`CUSTOM`; states `NEW`/`ESTABLISHED`/`RELATED`/`INVALID`) and still round-trip on update (#351)
- **`unifi_wan`: expose `networkgroup` (`WAN`, `WAN2`, …).** A new computed-by-default attribute identifying which WAN group an interface belongs to. The provider previously hard-coded `wan_networkgroup`/`attr_hidden_id` to `WAN`, so updating a **secondary** uplink (`WAN2`) collided with the primary and the controller rejected the PUT (`api.err.WanConfigurationForNetworkGroupAlreadyExists`). The group is now read from the controller and preserved in the update payload (`UseStateForUnknown`, so an imported `WAN2` needs no explicit config), making multi-WAN setups manageable (#334)

### 🐛 Bug Fixes

- **`unifi_network`: fix `inconsistent result after apply` on `multicast_dns` for non-vlan-only networks.** Some controllers (notably UniFi OS gateways) ignore the per-network `mdns_enabled` flag and always store `false`, so a configured `true` conflicted with the post-apply read. The corporate-network read path now preserves the configured value (the vlan-only path already did), falling back to the controller's value only when it was left unset (#282)
- **`unifi_wan`: fix `inconsistent result after apply` on `wan_dslite_remote_host_auto`.** The controller can force this field back to `true` server-side, so the post-apply read conflicted with a user-configured `false`. The create/update paths now re-assert the configured DS-Lite values on the post-apply state (the update path applied its write-preserve before the API round-trip, so the controller value won); the next refresh still reconciles with the controller (#281)
- **`unifi_firewall_policy`: stop `source`/`destination` match lists churning to "known after apply" on unrelated edits.** Changing any other field (e.g. `index` or `protocol`) replanned `network_ids`, `client_macs`, `ips` and `web_domains` as `(known after apply)` — showing a spurious diff and risking the controller recomputing them. These Computed attributes now use `UseStateForUnknown`, so they keep their prior value unless explicitly changed (#338)
- **`unifi_device`: fix LED updates failing with `inconsistent result after apply`.** The update PUT body was assembled as a minimal device that dropped the LED override fields (`led_override`, `led_override_color`, `led_override_color_brightness`), so the controller kept the old values and the post-apply read conflicted with the plan. They are now included in the PUT, and — because the controller applies LED changes to APs asynchronously — the update path also re-asserts the planned LED values on the post-apply state, leaving the next refresh to reconcile with the controller (#337)
- **`unifi_device`: fix `mgmt_network_id` (Network Override) never persisting.** The update PUT was assembled from a minimal `Device` that dropped a configured `mgmt_network_id`, so the controller never received it: every apply that set it failed with `inconsistent result after apply`, and the per-device management VLAN could not be set through the provider. The field is now carried in the PUT body when configured. `omitempty` keeps a null value off the wire, so it never reintroduces the #177 zero-value rejection. The tag-upstream-first connectivity requirement documented in #330 still applies (#329)
- **`unifi_wan`: fix `inconsistent result after apply` on `dns` address fields (`primary`, `secondary`, `ipv6_primary`, `ipv6_secondary`).** When no DNS server is configured the controller persists and returns an empty string `""`, but these Optional fields plan as `null`, so the post-apply read conflicted with the plan (e.g. after import with IPv6 DNS preference `auto`). The read now normalizes `""` (and a nil pointer) to `null`, so unset addresses stay null and a real address still round-trips (#333)
- **`unifi_firewall_policy`: make `index` read-only to stop `inconsistent result after apply` and a perpetual diff.** Pinning `index` failed: the controller ignores a client-supplied value and always appends the policy at the end of its source/destination zone-pair, so the post-apply read (e.g. `10010` → `10020`) conflicted with the plan and then looped forever. Verified against a real UniFi OS 10.x controller — the supported integration API rejects `index` as input and exposes no reorder operation, so policy ordering cannot be managed through the provider. `index` is now `Computed` (controller-assigned) and the provider no longer sends it; reorder policies in the UniFi UI if needed (#348)

---


## [v0.53.0] - 2026-06-24

### ✨ Features

- **`unifi_firewall_policy`: match an IP group on `source`/`destination`.** A new `ip_group_id` attribute references a `unifi_firewall_group` of type address-group (used with `matching_target = "IP"` and `matching_target_type = "OBJECT"`), alongside the existing `port_group_id`. Backed by a go-unifi change adding the `ip_group_id` field to the firewall-policy source/destination structs (#316)
- **`unifi_dns_record`: support `NS` records.** `record_type` now accepts `NS`, enabling Forward Domain entries (delegating a domain to another name server). Schema, validator, docs and an example were updated (#318, #319)

### 🐛 Bug Fixes

- **`unifi_firewall_policy`: fix `inconsistent result after apply` on `source`/`destination` `matching_target_type` when updating a policy (e.g. changing `action`).** This field is firmware-derived: the controller (and the provider's own derivation for #293) may set it to a concrete value during the update PUT (e.g. `""` → `"SPECIFIC"` for a non-ANY match), which the planned value cannot anticipate when the prior state still carries an empty type. The update path now re-asserts the planned value on the post-apply state, leaving the next refresh to reconcile it with the controller (#324)
- **`unifi_wlan`: fix `inconsistent result after apply` on controller-managed fields.** `minimum_data_rate_2g_kbps`/`minimum_data_rate_5g_kbps` defaulted to `0`, but the controller assigns its own value in `auto` mode (e.g. `1000`/`6000`); they are now `Computed` (via `UseStateForUnknown`) instead of statically defaulted. `radius_profile_id` and `bc_filter_list` were `Optional`-only yet the controller populates them on its own, so they too became `Optional + Computed`. When these are left unset, the controller's value is now accepted instead of conflicting with a `0`/`null` plan (#323)

### 📚 Documentation

- **`unifi_device`: document the `mgmt_network_id` tag-upstream-first requirement.** Setting the Network Override tags the device's management onto the target VLAN; if that VLAN is not tagged on the device's upstream port the device drops off and the apply fails with an inconsistent-result error. The description now spells out the two-step apply (tag the uplink first, then set `mgmt_network_id`) (#329, #330)

---


## [v0.52.4] - 2026-06-17

### 🐛 Bug Fixes

- **`unifi_firewall_zone`: create no longer fails with "Unrecognized field default_zone" (400) on UniFi Network 10.4.x.** The server-computed `default_zone` was always serialized into the create request; it is now omitted (modeled as `*bool` in go-unifi) and only read back as a computed attribute (#310)

### 📚 Documentation

- **`unifi_network`: clarify that `subnet` sets the gateway IP.** A custom gateway is already supported — the host portion of `subnet` is the gateway (e.g. `10.0.10.254/24` → gateway `.254`); it need not be the first usable address (#308, #309)

---


## [v0.52.3] - 2026-06-17

### 🐛 Bug Fixes

- Fix operation timeouts for the list resources, and add acceptance tests for them

---


## [v0.52.2] - 2026-06-16

### 🐛 Bug Fixes

- **`unifi_firewall_policy`: set `matching_target_type` for specific matches.** Switching a `source`/`destination` from `matching_target = "ANY"` to a specific target (e.g. `"IP"`) left `matching_target_type` empty, so the update was rejected with `api.err.MissingFirewallPolicySourceMatchingTargetType (400)`. The provider now sends `SPECIFIC` for a non-ANY match (preserving a controller-assigned `OBJECT`/`LIST`) (#293)

---

## [v0.52.1] - 2026-06-16

### 🐛 Bug Fixes

- **`unifi_setting`: stop serializing `0` for unset numeric fields.** An unset Optional+Computed integer was sent as `0`, which the controller rejects (e.g. syslog `netconsole_port: 0` → `400 api.err.InvalidPayload`). The provider now omits `syslog.port`/`syslog.netconsole_port`, `lcm.brightness`/`lcm.idle_timeout`, and `ips` alert `gid`/`id` when unset (#303)

---

## [v0.52.0] - 2026-06-16

### ✨ Features

- **`unifi_setting` `mgmt` block — full management settings** (#274): `advanced_feature_enabled`, `auto_upgrade_hour`, `debug_tools_enabled`, `direct_connect_enabled`, `unifi_idp_enabled`, `wifiman_enabled`, `ssh_username`, `ssh_password` (sensitive), `ssh_auth_password_enabled`. Configured fields are overlaid onto the controller's current settings, so unset fields are preserved.
- **`unifi_setting` `ips` block — signature alert suppression** (#275): new `suppression_alerts` list (`category`, `gid`, `id`, `signature`, `type`) with a nested `tracking` list (`direction`, `mode`, `value`).

---

## [v0.51.0] - 2026-06-16

### ✨ Features

- **`unifi_client`: new read-only `last_ip` attribute** — the most recent IP the controller has seen for the client (#287)
- **`unifi_setting`: new `auto_speedtest` block** — periodic internet speed test (`enabled`, `cron_expr`) (#272)
- **`unifi_setting`: six more setting categories** (#273):
  - `dpi` — Deep Packet Inspection (`enabled`, `fingerprinting_enabled`)
  - `lcm` — device display (`enabled`, `brightness`, `idle_timeout`, `sync`, `touch_event`)
  - `network_optimization` — automated network optimization (`enabled`)
  - `ntp` — time servers (`ntp_server_1..4`, `setting_preference`)
  - `syslog` — remote rsyslog (`enabled`, `ip`, `port`, `contents`, `log_all_contents`, `debug`, `this_controller`/`this_controller_encrypted_only`, `netconsole_*`)
  - `country` — regulatory `code`

---

## [v0.50.0] - 2026-06-16

### ⚠️ Breaking Changes

- **`unifi_firewall_policy` `source.port`/`destination.port` are now strings** (were numbers). Update configs from `port = 161` to `port = "161"`. Existing state is migrated automatically by a schema upgrader, so no manual action is required. This is what fixes #288 below and adds comma-separated port lists (#286).

### ✨ Features

- **List resources for 19 more managed resources** (5 → 24 listable), enabling `terraform query` / config-driven import workflows: `radius_user`, `dns_record`, `dynamic_dns`, `radius_profile`, `firewall_group`, `port_forward`, `static_route`, `traffic_route`, `wan`, `vpn_client`, `vpn_server`, `wireguard_peer`, `device`, `client_qos_rate`, `site`, `power_supervisor`, `firewall_rule`, `network`, `port_profile` (#277, #279)
- **Per-resource operation timeouts** — resources and data sources now accept a standardized `timeouts` block (create/read/update/delete) (#285)
- **`unifi_firewall_policy` ports accept a comma-separated list** (e.g. `"80,443"`) and round-trip correctly on import (#286)

### 🐛 Bug Fixes

- **`unifi_firewall_policy`: a portless source/destination no longer freezes the gateway firewall.** A policy with `port_matching_type = ANY` was serialized with `port = "0"`, which current UniFi OS rejects (valid ports are 1–65535) — silently dropping the *entire* firewall ruleset while `apply` reported success. Portless endpoints now omit the port field entirely (#288)
- **`unifi_wlan`: `enhanced_iot = true` no longer fails with "provider produced inconsistent result after apply".** When enhanced IoT is enabled the controller forces `iapp_enabled`, `wpa3_support`, `wpa3_transition`, `pmf_mode` and `dtim_ng`; the provider now pins those fields to the controller's values so apply and subsequent plans stay consistent (#283)

### 🔧 Maintenance

- CI: gate `golangci-lint` on newly-introduced issues only, so a `latest`-tracking linter no longer blocks every PR on pre-existing findings, and clear the existing findings in the test suite (#294)
- CI: workflow cleanup, coverage reporting, and stricter dependency linting (#278, #285)
- Build(deps): bump `golangci/golangci-lint-action` 8 → 9.2.1 (#291) and `codecov/codecov-action` 5 → 7 (#289)

---

## [v0.49.0] - 2026-06-12

### ✨ Features

- **New `unifi_power_supervisor` resource — UniFi Device Supervisor** (UniFi Network 10.2+). Watch a device's heartbeat and have the controller automatically power-cycle its upstream PoE source after a silence threshold. Reference the supervised device by `device_mac`; set the `heartbeat_interval` / `silence_threshold` / `power_off_duration` timings (seconds). The controller resolves the upstream PoE port automatically (`power_sources` is computed). Full CRUD + import by `id`, `site:id`, or the device's MAC. Backed by a new go-unifi v2 client; live-validated on UniFi Network 10.4.57. Note: the supervised device must be powered by a controller-manageable PoE port — a non-PoE uplink is rejected with `PORT_NOT_POE_CAPABLE` (#244)

### 🐛 Bug Fixes

- **Surface the controller's actual error message on v2 API failures.** Errors from the v2 API (firewall policy/zone, wireguard peer, power supervisor) previously showed only a bare `(400 Bad Request)` because the SDK parsed only the v1 error shape. The underlying go-unifi SDK now reads the v2 error body too, so failures include the controller's reason and code (e.g. `api.err.PurePoeRequiresUplinkException: … PORT_NOT_POE_CAPABLE`)

---

## [v0.48.0] - 2026-06-12

### ✨ Features

- **`unifi_firewall_policy`: allow `protocol = "icmp"` / `"icmpv6"`.** The protocol validator only accepted `all`/`tcp`/`udp`/`tcp_udp`, so zone-based firewall ICMP policies could not be planned even though the controller (UniFi Network 10.4.57) accepts and returns them. The firmware-managed `icmp_typename` / `icmp_v6_typename` fields are already round-tripped, so the validator was the only blocker. Note: the controller rejects `create_allow_respond = true` for ICMP policies (`FirewallPolicyCreateRespondTrafficPolicyNotAllowed`) — keep it `false` and add an explicit reverse policy for the reply (#259)

### 🐛 Bug Fixes

- **`unifi_device`: stop a single `port_override` from wiping every other port.** The UniFi `PUT /rest/device/<id>` treats `port_overrides` as a full-replace array, and the provider sent only the declared subset — so declaring one port silently dropped all other ports' overrides back to the default VLAN (a port carrying e.g. an NVR on a CCTV VLAN would lose connectivity). The provider now merges the declared `port_override` blocks (by `index`) onto the device's current overrides before the PUT, making `port_override` **partial management**: manage only the ports you declare, leave the rest untouched. Removing a block stops managing that port but does not reset it (#266)

---

## [v0.47.2] - 2026-06-12

### 🐛 Bug Fixes

- **`unifi_site`: fix provider panic when importing/reading with an unmatched identifier.** Importing a site by an identifier that is neither a 24-hex controller id nor a known site name (e.g. the UUID shown in the UI / Integration API) crashed the provider with a nil-pointer dereference. The read paths now return cleanly on not-found, and `siteToModel` guards against a nil site. Import docs clarify the supported forms (24-hex `_id` or `name=<site-name>`) (#261)
- **`unifi_wan`: fix spurious plan diff after import.** Two read quirks made an imported WAN unable to reach `No changes` without an apply: `vlan.id` was read as null (so it always wanted `+ id = 0`) and is now mapped to the schema default `0`; and `provider_capabilities` (the detected line rate) became `Optional + Computed` with `UseStateForUnknown`, so omitting it from config no longer tries to clear it (#262)

---

## [v0.47.1] - 2026-06-11

### 🔒 Security

- **Stop leaking secrets in error messages.** A failed create/update embedded the raw request payload in the error — including `x_wireguard_private_key`, `x_passphrase`, and `x_ipsec_pre_shared_key` in cleartext — exposing them in terminal scrollback and CI logs. The underlying go-unifi SDK now redacts sensitive fields from payloads in error messages (#256)

### 🐛 Bug Fixes

- **`unifi_vpn_server`: generate the WireGuard `private_key` when unset.** The controller does not generate one (it rejects creation with `api.err.WireguardMissingPrivateKey`) despite the schema marking the field optional/computed. The provider now generates a valid key at create time, and the subnet docs note that the **gateway** form (`10.x.0.1/24`) is required, not the network address (#255)

- **`unifi_network`: fix `inconsistent result after apply` / perpetual diffs on the IPv6 RA/PD attributes.** Networks that carry controller-set RA/PD values (`ipv6_ra`, `ipv6_ra_priority`, `ipv6_ra_preferred_lifetime`, `ipv6_ra_valid_lifetime`, `ipv6_pd_start`, `ipv6_pd_stop`, `ipv6_pd_auto_prefixid_enabled`) — common even on v4-only networks — drifted forever (e.g. `ipv6_ra: true -> false`, `ipv6_pd_start: "::2" -> null`) and could fail apply. These are now `Optional + Computed` with `UseStateForUnknown`, and unset values are no longer serialized as `""`/`0`, so controller-normalized values are preserved instead of clobbered. Extends the v0.47.0 fix to `unifi_network` (#253)
- **`unifi_network`: fix create failing with `api.err.InvalidPayload` when `ipv6_client_address_assignment` is unset.** The attribute (added in v0.45.0) is `Optional + Computed`, so on create it was serialized as an empty string `""`, which the controller rejects — breaking network creation unless the field was pinned to a value. It is now omitted from the payload when unset (#252)
- **`unifi_wan`: allow `type_v6 = "slaac"`.** The validator only accepted `dhcpv6`/`static`/`disabled`, but the controller also supports `slaac` — and **requires** it when the IPv6 delegation type is `single_network` (`api.err.SingleNetworkMustBeSLAAC` otherwise). This blocked enabling IPv6 on the WAN for ISPs that deliver it by Router Advertisement (e.g. Free/Freebox in bridge mode). Validated live on UniFi Network 10.4.57 (#250)

---

## [v0.47.0] - 2026-06-11

### ✨ Features

- **`unifi_firewall_policy`: match traffic by domain/FQDN.** A new `web_domains` attribute on `source` and `destination` (used with `matching_target = "WEB"`) lets a policy filter on hostnames. Backed by a go-unifi change that adds the `web_domains` field and the `WEB` matching target to the firewall-policy schema (#242)

### 🐛 Bug Fixes

- **`unifi_firewall_policy`: actually send/read `network_ids` and `client_macs`.** These match fields were exposed in the schema but never wired to the API — the provider dropped them on write and forced them to `null` on read. They now round-trip like `ips` (#242)
- **`unifi_device`: fix `Provider produced inconsistent result after apply` that broke every device update.** Write-only attributes never returned by the controller (`forget_on_destroy`, `allow_adoption`) are no longer clobbered to `null` by prior state (notably after an import), and the LED attributes (`led_override`, `led_override_color`, `led_override_color_brightness`) now preserve their configured value when the controller does not echo them back. All five gained `UseStateForUnknown` plan modifiers (#243)
- **`unifi_port_profile`: fix `inconsistent result after apply` on `stp_port_mode` and `excluded_networkconf_ids`.** `stp_port_mode` is now actually round-tripped to/from the controller (it was forced to `null` and never sent), and both attributes became `Optional + Computed` with `UseStateForUnknown` so controller-computed values no longer conflict with the plan (#245)
- **`unifi_wlan`: fix `inconsistent result after apply` on `dtim_ng`/`dtim_na`/`dtim_6e` and `iapp_enabled`.** The DTIM fields became `Optional + Computed` so controller defaults (e.g. `1`/`3`/`3`) are accepted when unset, and `iapp_enabled` dropped its static `false` default (the controller may return `true`) in favor of `Optional + Computed` + `UseStateForUnknown` (#245)

---

## [v0.46.0] - 2026-06-11

### ✨ Features

- **New `unifi_site_to_site_vpn` resource** — manage a UniFi manual site-to-site IPsec VPN (`purpose = site-vpn`, `vpn_type = ipsec-vpn`). Exposes the tunnel essentials (`peer_ip`, `interface`, `key_exchange`, `remote_subnets`, `pre_shared_key`) plus the full `profile = customized` IKE/ESP tuning surface (encryption, hash, DH groups, lifetimes, PFS, dynamic routing, route distance). The pre-shared key supports a write-only variant (`pre_shared_key_wo`, Terraform 1.11+). Backed by a go-unifi fix that completes the previously-stubbed site-VPN marshaler. Validated live on UniFi Network 10.4.57 (#78, #239)

### 🧹 Maintenance

- Added a regression unit test for the `unifi_device` `port_override` refresh crash fixed in v0.45.1, and removed a duplicate initialization left by merging the parallel fix (#236, #240)

---

## [v0.45.1] - 2026-06-11

### 🐛 Bug Fixes

- `unifi_device`: fix a refresh/plan crash (`Value Conversion Error … types.ListType[!!! MISSING TYPE !!!]` on `tagged_networkconf_ids`) that hit any device with `port_override` blocks. The override read path now initializes the list to a typed null. Note: `tagged_networkconf_ids` is not yet round-tripped (it reads as null) pending the field being added to the go-unifi SDK (#235, #237)

---

## [v0.45.0] - 2026-06-10

### ✨ Features

- **`unifi_network.ipv6_client_address_assignment`** — new optional+computed attribute to declaratively pin how clients on a network obtain an IPv6 address: `slaac` (SLAAC only), `dhcpv6` (DHCPv6 only), or `slaac-dhcpv6` (both). UI: Networks → IPv6 → Client Address Assignment. Backed by a go-unifi fix that emits the field in the corporate/guest marshalers (it was decode-only before). Validated on a live UniFi Network 10.4.57 controller (#232, #233)

### 🐛 Bug Fixes

- **Login rate-limit resilience** — username/password auth no longer fails with `Unable to Create HTTP Client` when several back-to-back operations (`init → import → plan → plan → apply`) exhaust the controller's `POST /api/auth/login` rate-limit. The SDK now surfaces HTTP 429 and retries login with a dedicated budget that honors `Retry-After`. API-key auth is unaffected (it skips login) (#231)

---

## [v0.44.0] - 2026-06-10

### ✨ Features

- **Site-level IGMP snooping** — manage the `igmp_snooping` site setting (`enabled` + `network_ids`) through the `unifi_setting` resource. On UniFi Network 10.3.x+ the effective IGMP snooping toggle moved from the per-network object to this site setting; advanced querier/flood options configured in the UI are preserved across updates (#164)

### 🐛 Bug Fixes

- `unifi_firewall_policy`: round-trip `connection_states` so a policy whose `connection_state_type` is `CUSTOM` updates successfully — the provider previously sent an empty state list and the firmware rejected it with HTTP 400 (#227)

---

## [v0.43.1] - 2026-06-10

### ✨ Features

- `unifi_radius_user`: derive the assigned VLAN from `network_id`, so MAC-based authentication (MAB) hands out the correct VLAN without hand-setting the tunnel attributes (#226)
- `unifi_radius_user`: support moving a deprecated `unifi_account` in place via a `moved` block — no more destroy/recreate or hand-edited state when migrating, since both are backed by the same RADIUS account (#222, #224)

### 🐛 Bug Fixes

- `unifi_firewall_policy`: round-trip the firmware-required fields (`connection_state_type`, `icmp_typename`, `icmp_v6_typename`, and the source/destination `matching_target_type`) so a zone-based UPDATE no longer fails with HTTP 400 on UniFi OS 5.1.x / Network 10.x (#220, #221, #223)
- `unifi_device`: write `op_mode` for non-default ports so SFP+ link aggregation (LAG) actually forms, while still skipping it on gateways (UDM) that reject `op_mode` on a PUT (#213, #225)

---

## [v0.43.0] - 2026-06-09

### ✨ Features

- **New `unifi_wireguard_peer` resource** — manage WireGuard VPN peers (the "clients" of a WireGuard server network), with full CRUD and import (#194)
- **New `unifi_firewall_zone` resource** — create and manage zone-based firewall zones (UniFi OS 8.x+) and their network membership, alongside the existing data source (#214, #218)
- **IPv6 network configuration** on `unifi_network` — static IPv6 subnet, Router Advertisement (`ipv6_ra*`), Prefix Delegation (`ipv6_pd_*`) and a DHCPv6 server block (#158)
- **WLAN private pre-shared keys (PPSK)** — per-key passphrases each optionally bound to a network/VLAN (#47, #212)
- **WLAN write-only passphrase** `passphrase_wo` (Terraform 1.11+) so the secret is used at apply time but never persisted to state (#201)

### 🐛 Bug Fixes

- `unifi_device`: read `radio_table` `channel`/`tx_power` returned as numbers by UniFi 10.x controllers — previously broke device read/import with an unmarshal error (#112)
- `unifi_device`: stop resetting `state`/`adopted` in the update payload, fixing writes on UDM / Dream Machine gateways (#177)
- `unifi_network`: keep `dhcp_relay` enabled by pinning a manual `setting_preference` (#208)
- `unifi_network`: stop forcing `multicast_dns = true` at create, which caused an "inconsistent result after apply" on UniFi OS gateways (#209)
- `unifi_network`: make `subnet` optional for vlan-only networks (#124)
- `unifi_network`: tolerate string-encoded boolean flags such as `dhcpd_enabled` from some controllers (#65)
- `unifi_network`: send `vlan_enabled` so create/update with a VLAN no longer fails with `api.err.VlanUsed` (#76, #85)
- `unifi_port_forward`: stop perpetual drift when the `source_limiting` block is omitted (#187)
- `unifi_firewall_policy`: support SPECIFIC port matching via a `port` attribute (#207)
- `unifi_wlan`: stop `mac_filter` drift, populate `wlangroup_id`, and stabilize `minimum_data_rate` (#200, #203)
- `unifi_dns_record`: make `record_type` required (#197)
- `unifi_port_profile`: expose forward/native/tagged VLANs in the data source schema (#196)
- `unifi_radius_user`: allow `tunnel_type` 13 (VLAN) (#193)
- `unifi_client`: zero-diff import/create for `blocked`/groups/`qos_rate` (#174)
- `unifi_client_info`: don't fail with 404 on controllers where the active-clients endpoint is unavailable (#121)
- structured logging via a dedicated subsystem (#168)

### 🔧 Build & CI

- run `gosec` on dependabot PRs and on `go.mod`/`go.sum` changes so dependency bumps can satisfy the code-scanning gate (#204, #205)
- dependency updates: testcontainers/compose, terraform-plugin-testing, grouped go modules, and GitHub Actions (#206, #166)

### 📄 Documentation

- clarify what `lte_lan` does (#202)
- document that `ipv6_pd_start`/`ipv6_pd_stop` are required for prefix-delegation networks (#215)

---

## [v0.41.20] - 2026-03-08

### 💥 Breaking Changes

#### `unifi_network` Resource Replaced

The `unifi_network` resource has been replaced with the modernized `unifi_virtual_network` implementation, which is now renamed to `unifi_network`.

**What changed:**

* The old `unifi_network` resource (flat attribute schema with `purpose`, `vlan_id`, `dhcp_start`, `dhcp_stop`, etc.) has been removed
* The `unifi_virtual_network` resource has been renamed to `unifi_network`
* The new `unifi_network` resource uses nested attributes (`dhcp_server`, `dhcp_relay`, `dhcp_guarding`) instead of flat prefixed fields
* The `unifi_network` data source is unchanged

**Migration guide:**

* Replace `purpose = "corporate"` — the new resource defaults to corporate purpose
* Replace `vlan_id` with `vlan`
* Replace `subnet` value format — now uses `cidrtypes.IPv4Prefix` (e.g., `"192.168.1.1/24"`)
* Replace flat DHCP fields (`dhcp_start`, `dhcp_stop`, `dhcp_enabled`) with nested `dhcp_server` block
* Replace `purpose = "vlan-only"` with `third_party_gateway = true`
* Remove `purpose`, `network_group`, and `vlan_enabled` attributes (no longer needed)
* WAN-specific attributes are no longer part of this resource — use `unifi_wan` instead

**Example migration:**

```hcl
# Before (old unifi_network)
resource "unifi_network" "vlan" {
  name         = "my-vlan"
  purpose      = "corporate"
  subnet       = "10.0.0.1/24"
  vlan_id      = 10
  dhcp_start   = "10.0.0.6"
  dhcp_stop    = "10.0.0.254"
  dhcp_enabled = true
}

# After (new unifi_network)
resource "unifi_network" "vlan" {
  name   = "my-vlan"
  subnet = "10.0.0.1/24"
  vlan   = 10

  dhcp_server = {
    enabled = true
    start   = "10.0.0.6"
    stop    = "10.0.0.254"
  }
}
```

**Other changes:**

* **Removed**: Old `unifi_network` resource and tests
* **Updated**: Examples for `unifi_client`, `unifi_port_profile`, and `unifi_wlan` to use new schema
* **Updated**: Documentation regenerated with new schema and examples

---

## [v0.41.19] - 2026-03-07

### 🔧 Improvements

#### Client Resource Enhancements

This release adds bulk import capability to the `unifi_client` resource, building on the expanded client list support introduced in v0.41.18.

**Changes**

* **New Example**: Added bulk import example (`examples/resources/unifi_client/bulk-import.tf`)
  * Demonstrates how to manage multiple client devices using a tfquery data file
* **New Example**: Added bulk import tfquery configuration (`examples/resources/unifi_client/bulk-import.tfquery.hcl`)
* **Improved**: Enhanced `unifi_client` resource with additional attributes and fixes
* **Docs**: Updated client list resource and port action documentation

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.18...v0.41.19>

---

## [v0.41.18] - 2026-03-07

### 🚀 New Features

#### New Data Sources

This release introduces two new list-style data sources for querying UniFi network clients and network member groups.

##### `unifi_client_list` (List Data Source)

A new list data source that provides a rich, queryable view of all UniFi network clients.

* Query and filter clients by various attributes
* Supports bulk operations and data-driven configurations
* Includes comprehensive tests

##### `unifi_network_members_group_list` (Data Source)

A new data source for listing network member groups.

**Other Changes**

* **Improved**: Enhanced `unifi_client` resource with additional attributes (158 additions)
* **Updated**: go-unifi dependency version bump
* **Fixed**: Minor fixes to `unifi_virtual_network_resource` and `unifi_vpn_client_resource`
* **Added**: New data source examples for `unifi_client_list` and `unifi_network_members_group_list`

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.17...v0.41.18>

---

## [v0.41.17] - 2026-02-26

### 🐛 Bug Fixes

#### Dynamic DNS Identity Field Fix

* **Fixed**: `bug: Fix identity in dynamic dns` — corrected the identity field in the Dynamic DNS resource that was broken since v0.41.13

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.16...v0.41.17>

---

## [v0.41.16] - 2026-02-26

### 🐛 Bug Fixes

#### UniFi Client Fix

* **Fixed**: Additional fixes to the `unifi_client` resource following the v0.41.15 update

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.15...v0.41.16>

---

## [v0.41.15] - 2026-02-26

### 🐛 Bug Fixes

#### UniFi Client Update

* **Fixed**: Updated `unifi_client` resource to resolve issues introduced in v0.41.13

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.14...v0.41.15>

---

## [v0.41.14] - 2026-02-26

### 🐛 Bug Fixes

#### Network Data Source Fix

* **Fixed**: `bug: Fix Network Data Source` — resolved a regression in the `unifi_network` data source introduced in v0.41.13

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.13...v0.41.14>

---

## [v0.41.13] - 2026-02-22

### 🔧 Maintenance

#### go-unifi Dependency Update and Provider Refactor

This release updates the go-unifi client library and significantly refactors the provider configuration code.

**Changes**

* **Updated**: go-unifi dependency version bump
* **Refactored**: Significant cleanup of `provider.go` (removed 92 lines of legacy code, -81 net lines)
* **Updated**: Provider tests updated to reflect new provider configuration
* **Fixed**: Minor fixes to `setting_resource.go`

> ⚠️ **Warning**: This release introduced regressions that were fixed in v0.41.14–v0.41.17:
>
> * **Network Data Source** had issues (fixed in v0.41.14)
> * **UniFi Client** had issues (fixed in v0.41.15–v0.41.16)
> * **Dynamic DNS** identity field was broken (fixed in v0.41.17)
>
> **Upgrade recommendation**: If upgrading from v0.41.12, skip directly to v0.41.17 or later.

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.12...v0.41.13>

---

## [v0.41.12] - 2026-01-25

### 🐛 Bug Fixes & 📄 Documentation

#### Client Data Source Fix and Documentation Update

* **Fixed**: `bug: Fix client data source` — resolved field mapping issues in the `unifi_client` data source
* **Fixed**: `Fix pointer` — corrected a nil pointer issue
* **Docs**: Updated generated documentation

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.11...v0.41.12>

---

## [v0.41.11] - 2026-01-25

### 🐛 Bug Fixes

#### DNS Port Fix

* **Fixed**: `bug: Fix DNS port` — corrected the port used for DNS queries in the provider

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.10...v0.41.11>

---

## [v0.41.10] - 2026-01-22

### 🐛 Bug Fixes

#### go-unifi Version Fix

* **Fixed**: `bug: Fix go-unifi version` — pinned the correct go-unifi dependency version to resolve compatibility issues introduced in v0.41.9

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.9...v0.41.10>

---

## [v0.41.9] - 2026-01-22

### 🚀 New Features & 🔧 Improvements

#### New WireGuard VPN Client Resource, WAN/WLAN Refactoring, and Expanded Tests

This release adds the `unifi_vpn_client` resource for WireGuard VPN configuration, refactors the WAN and WLAN resources for better code quality, and significantly expands test coverage.

**New Features**

* **NEW**: `unifi_vpn_client` resource (`unifi/vpn_client_resource.go`, 667 lines)
  * WireGuard VPN client configuration support
  * Dual configuration modes:
    * **File mode**: Upload a complete WireGuard configuration file
    * **Manual mode**: Configure peer settings directly (public key, endpoint, allowed IPs)
  * DNS servers support (1–2 entries required in manual mode)
  * Auto-mode detection based on nested configuration structure
  * Preshared key support for enhanced security
  * Sensitive field handling for private keys and configuration content
  * Flexible import formats: `id`, `name=<name>`, `site:id`, `site:name=<name>`
  * Complete CRUD operations with comprehensive error handling

**Improvements**

* **WAN Resource Refactoring**: Migrated to pointer-based API calls, simplified null value handling, reduced code verbosity (net -22 lines)
* **WLAN Resource Refactoring**: Converted to pointer-based API patterns, cleaner enabled state checks (net -16 lines)

**Testing**

* **New**: VPN client acceptance tests (file mode, manual mode with DNS, preshared key)
* **New**: Virtual network acceptance tests (basic VLAN, DHCP server, guest network)

**Files Changed**

* `unifi/vpn_client_resource.go` (NEW, 667 lines)
* `unifi/vpn_client_resource_test.go` (NEW, 211 lines)
* `unifi/virtual_network_resource_test.go` (NEW, 185 lines)
* `unifi/wan_resource.go` (+242/-264)
* `unifi/wlan_resource.go` (+11/-27)

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.8...v0.41.9>

---

## [v0.41.8] - 2026-01-16

### 🔧 Dependency Updates

#### Security and Dependency Bumps

* **Updated**: `github/codeql-action` from 3.29.0 to 4.31.10 (major version bump via Dependabot)
* **Updated**: `github.com/containerd/containerd/v2` from 2.1.4 to 2.1.5 (security patch, indirect dependency)

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.7...v0.41.8>

---

## [v0.41.7] - 2026-01-16

### 🔧 Improvements

#### CodeQL Security Scanning and Query/Actions Fixes

* **Added**: CodeQL analysis workflow configuration for automated security scanning
* **Fixed**: `feat: Fix query and actions` — resolved issues with list resource queries and action handling
* **Fixed**: `chore: Fix formatting and generation` — corrected code formatting and regenerated provider documentation

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.6...v0.41.7>

---

## [v0.41.6] - 2026-01-16

### 🚀 New Features

#### Client Info Data Source

* **Added**: `feat: Added Client Info` — new `unifi_client_info` data source for retrieving detailed information about a specific network client by MAC address or hostname

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.5...v0.41.6>

---

## [v0.41.5] - 2026-01-15

### 🐛 Bug Fixes & Build Improvements

#### Client Info Data Source Fix and GoReleaser Update

This release fixes the `unifi_client_info` data source and updates the release pipeline for proper Terraform Registry integration.

**Changes**

* **Fixed**: `unifi_client_info` data source field mapping and model alignment
* **Updated**: GoReleaser configuration with Terraform Registry support
* **Added**: `terraform-registry-manifest.json` for proper Terraform Registry integration
  * This enables correct discovery by the Terraform Registry

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.4...v0.41.5>

---

## [v0.41.4] - 2026-01-15

### 🚀 New Features

#### Terraform Plugin Framework Migration (Stable Release) and Client Info Data Sources

This is the stable release of the Terraform Plugin Framework migration, incorporating all the work from the beta and RC pre-releases.

**Changes since v0.41.3**

* **Migrated**: Full provider migration from Terraform Plugin SDK v2 to Terraform Plugin Framework via the MUX adapter — allows both old SDK resources and new Framework resources to coexist
* **Added**: `unifi_client_info` data source (single-client lookup by MAC/hostname)
* **Added**: `unifi_client_info_list` data source (bulk client info queries)
* **Breaking**: `unifi_user` renamed to `unifi_client`; `unifi_user_group` renamed to `unifi_client_group`
* **Added**: `unifi_wan` resource for full WAN interface configuration
* **Improved**: `unifi_wlan` resource with major schema and behavior improvements
* **Added**: Structured logging via `unifi/logger.go`
* **Fixed**: GoReleaser configuration and Terraform Registry manifest

## What's Changed

* Pivot to Plugin Framework via the MUX Framework by @appkins in <https://github.com/ubiquiti-community/terraform-provider-unifi/pull/17>
* feat: Migrate to Terraform plugin framework by @appkins in <https://github.com/ubiquiti-community/terraform-provider-unifi/pull/50>

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.3...v0.41.4>

---

## [v0.41.4-rc3] - 2026-01-06

### ⚠️ BREAKING CHANGES

#### Renamed `unifi_user` → `unifi_client` and `unifi_user_group` → `unifi_client_group`

This release candidate introduces a **breaking rename** of the user-related resources and data sources to better reflect their purpose in UniFi terminology.

**Breaking Changes**

| Old Name | New Name |
|----------|----------|
| `unifi_user` (resource) | `unifi_client` (resource) |
| `unifi_user_group` (resource) | `unifi_client_group` (resource) |
| `unifi_user` (data source) | `unifi_client` (data source) |
| `unifi_user_group` (data source) | `unifi_client_group` (data source) |

> **Migration**: Replace all references to `unifi_user` with `unifi_client` and `unifi_user_group` with `unifi_client_group` in your Terraform configurations.

**Other Changes**

* **Added**: WAN resource (`unifi_wan`) documentation and import examples
* **Updated**: go-unifi dependency

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.4-rc2...v0.41.4-rc3>

---

## [v0.41.4-rc2] - 2026-01-06

### 🚀 New Features & Bug Fixes

#### New WAN Resource, WLAN Improvements, and Acceptance Test Fixes

This release candidate adds the `unifi_wan` resource, significantly improves `unifi_wlan`, and fixes the acceptance test suite for the new plugin framework.

**New Features**

* **NEW**: `unifi_wan` resource (`unifi/wan_resource.go`, ~1129 lines)
  * Full WAN interface configuration management
  * Import support
  * Comprehensive documentation
* **Improved**: `unifi_wlan` resource with major enhancements (319 additions)
* **Added**: Structured logging (`unifi/logger.go`)
* **Improved**: `unifi_network` resource with bug fixes and schema improvements

**Bug Fixes**

* Fixed acceptance tests to work with the new plugin framework
* Updated `unifi_site` resource with framework compatibility fixes
* Updated Dependabot configuration for automated dependency management

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.4-rc1...v0.41.4-rc2>

---

## [v0.41.4-rc1] - 2025-12-31

### 🚀 Release Candidate: Terraform Plugin Framework Migration

This release candidate marks the first RC of the full migration from Terraform Plugin SDK v2 to the Terraform Plugin Framework, delivered via the MUX adapter so old and new resource implementations can coexist.

**Changes**

* **Migrated**: Provider core pivoted to Terraform Plugin Framework via the MUX (protocol multiplexer) adapter
* **Maintained**: Full backward compatibility with all existing resources during the migration period

## What's Changed

* Pivot to Plugin Framework via the MUX Framework by @appkins in <https://github.com/ubiquiti-community/terraform-provider-unifi/pull/17>
* feat: Migrate to Terraform plugin framework by @appkins in <https://github.com/ubiquiti-community/terraform-provider-unifi/pull/50>

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.3...v0.41.4-rc1>

---

## [v0.41.4-beta2] - 2025-11-18

### 🔧 Improvements

#### Optional Provider Configuration

This beta release makes provider configuration fields optional, allowing more flexible authentication configuration via environment variables.

**Changes**

* **Improved**: Provider configuration fields are now optional (previously required)
  * All fields can now be configured via environment variables (`UNIFI_API_URL`, `UNIFI_USERNAME`, `UNIFI_PASSWORD`, `UNIFI_API_KEY`, etc.)
  * This enables cleaner CI/CD configurations without hardcoded provider blocks
* **Added**: Expanded `unifi_device` resource documentation with full attribute reference

> **Note**: This is a beta release for the Terraform Plugin Framework migration. See v0.41.4-beta1 for the full feature list.

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.4-beta1...v0.41.4-beta2>

---

## [v0.41.4-beta1] - 2025-11-18

### 🧪 Beta: Terraform Plugin Framework Migration

Initial beta release of the Terraform Plugin Framework migration. This beta introduces the new plugin framework architecture while maintaining compatibility with all existing resources.

**Changes**

* **Migrated**: Provider core to Terraform Plugin Framework via the MUX adapter
* **Refactored**: Multiple resources updated to use the new plugin framework patterns

## What's Changed

* Pivot to Plugin Framework via the MUX Framework by @appkins in <https://github.com/ubiquiti-community/terraform-provider-unifi/pull/17>
* Plugin-framework-migration by @appkins in <https://github.com/ubiquiti-community/terraform-provider-unifi/pull/19>

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.3...v0.41.4-beta1>

---

## [v0.41.3] - 2025-06-19

### 🚀 New Features

#### API Key Authentication Support and Code Quality Improvements

This release introduces **API Key authentication** as an alternative to username/password authentication, providing enhanced security and convenience for automated deployments. It also includes extensive code quality improvements across the provider.

**New Features**

* **New `api_key` provider configuration**: Authenticate using API keys instead of username/password
* **Environment variable support**: Use `UNIFI_API_KEY` environment variable for CI/CD pipelines
* **Automatic fallback**: When API key is provided, username/password fields are ignored
* **Validation**: API keys are validated to ensure they meet minimum length requirements (32+ characters)

```terraform
provider "unifi" {
  api_key = var.api_key    # or use UNIFI_API_KEY env var
  api_url = var.api_url
  site    = "default"
}
```

**Code Quality Improvements**

* **Fixed 60+ golangci-lint issues** across data sources and resources
* **Enhanced type safety**: All type assertions now include proper error checking to prevent runtime panics
* **Improved error handling**: Return values from `d.Set()` calls are now properly handled
* **Parameter validation**: Function parameters validated with appropriate error messages

**Migration from Username/Password**

Existing configurations using username/password will continue to work unchanged. This release is **fully backward compatible**.

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.2...v0.41.3>

---

## [v0.41.2] - 2024-07-31

### 🔧 Build & Release Fixes

#### GoReleaser and Workflow Updates

* **Updated**: GoReleaser configuration to fix release artifact generation
* **Updated**: Release workflow permissions and configuration
* **Fixed**: Version bump and cleanup of release tooling

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/compare/v0.41.1...v0.41.2>

---

## [v0.41.1] - 2024-07-31

### 🚀 Initial Release of Community Fork

#### DNS Record Resource, WireGuard, and Provider Modernization

This is the initial release of the `ubiquiti-community/terraform-provider-unifi` fork, establishing the project foundation with new resources, updated tooling, and a clean dependency structure.

**New Features**

* **Added**: `unifi_dns_record` resource for managing DNS records in UniFi controllers
* **Added**: WireGuard VPN configuration support
* **Updated**: DNS record resource with improved field handling

**Infrastructure**

* **Updated**: Go module versions and dependency versions
* **Removed**: Vendored dependencies in favor of Go modules
* **Added**: Dependabot configuration for automated dependency management
* **Updated**: Release workflow permissions
* **Added**: Provider documentation

**Full Changelog**: <https://github.com/ubiquiti-community/terraform-provider-unifi/commits/v0.41.1>
