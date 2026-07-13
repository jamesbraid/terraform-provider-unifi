# PR-B4: `guest_access` Settings Section — Design

Date: 2026-07-13
Status: draft — pending maintainer review (rev. 2, post-codex review)
Depends on: PR-A (`settings-expansion-v2`, foundation as simplified by
`docs/superpowers/plans/2026-07-12-settings-simplification-prA.md`, tip
`0ff93708`)
Parent design: `docs/superpowers/specs/2026-07-11-settings-remediation-design.md`
§367 ("PR-B4 guest portal: `guest_access` alone — large schema; payment/auth
`WriteOnlySecret` surface; no SDK dependency after PR-0").

## Revision note (rev. 2)

An independent codex review of rev. 1 returned NEEDS-WORK on five points, all
addressed in this revision:

1. **Secret leaf schema flags corrected.** Rev. 1 followed the design's
   general C1 "`WriteOnlySecret` = Optional+Sensitive, no Computed" table
   literally. That table is stale for a nested-object secret leaf: the actual
   shipped precedent, `radius.secret` (`unifi/setting_section_radius.go`), is
   **Optional+Computed+Sensitive**. See Key Decision 2a below for why
   Computed is framework-required here, not optional.
2. **Four operationally-load-bearing fields were mis-classified as
   preserved**: `auth_url`, `ec_enabled`, `custom_ip`, and `redirect_https`
   (distinct wire key from the already-modeled `redirect_to_https`) are now
   promoted to modeled. See Key Decision 1.
3. **Preserved-field arithmetic corrected and reconciled everywhere**: the
   real split is 31 `PortalCustomized*` fields (including the parent
   toggle) + 14 other fields = 45 preserved in rev. 1; after promoting the 4
   fields in point 2, preserved is now 31 + 10 = 41, modeled is 56 (18
   secrets unchanged), for 56 + 41 = 97. Every count in this document uses
   this final split.
4. **`allSectionAttrsNull`/`allSectionsNullModel` wiring was missing from the
   plan.** The plan now has explicit task steps for it.
5. **Secret test matrix under-specified.** The plan now has an exhaustive
   table-driven matrix over all 18 secret leaves.

## Goal

Add the `guest_access` settings section to `unifi_setting`, following the
PR-A section contract (`settingSection` interface: `key`, `attrName`,
`schemaAttribute`, `decode`, `overlay`, `carryBestEffort`, `isConfigured`) and
the class-free codec (`decode*`/`overlay*`, `decodeObject(List)`/
`overlayObject(List)`) that all 13 existing sections already use. No engine
change beyond one section-local helper (Key Decision 2b). No go-unifi change.

## Source of truth

`github.com/ubiquiti-community/go-unifi@v1.33.43-.../unifi/settings/guest_access.generated.go`
— `GuestAccess` struct, 97 JSON-tagged fields plus `BaseSetting`. Read-only
reference; not imported by the provider (the section, like all 13 existing
ones, decodes/overlays the raw `map[string]any` snapshot directly — the typed
struct exists in go-unifi only for the codegen/SDK's own use).

## KEY DECISION 1 — Scope: model the operational core, preserve the portal template

**97 total fields. This spec models 56 (18 of them secrets) and preserves 41
by RMW.** These counts were produced mechanically (script-diffed against
every `json:"..."` tag in `guest_access.generated.go`, not hand-tallied) —
the plan's Task 1 acceptance check re-derives the same split the same way and
must match exactly.

This is the central YAGNI call for B4 and mirrors the parent design's own
framing ("large schema... Do NOT model the entire portal-template surface").
The line is drawn by **who plausibly sets the field through Terraform, and
whether the field is operationally load-bearing for a feature this section
already exposes** — not by how easy the field would be to model. Most of the
41 preserved fields are trivial-to-model scalars excluded because they
compose a portal *authoring* surface (HTML/CSS-adjacent: colors, fonts,
background image, logo, welcome/success/ToS copy, language list) that in
practice is authored once through the controller's own guest-portal editor
UI — a WYSIWYG surface — not hand-typed into HCL. Modeling it doesn't serve a
real Terraform workflow, and it is exactly the kind of speculative "complete
API coverage" that `settings-engine-simplification` already rejected for the
engine itself (see `MEMORY.md`: "v2 engine judged over-engineered;
simplify"). If a real user need for a specific preserved field shows up, it
is a small additive PR — the RMW default means preserved fields are never at
risk of being clobbered in the meantime.

### Rev. 2 correction: four fields promoted from preserved to modeled

Rev. 1 preserved four fields that are not portal-authoring cosmetics — they
are operational configuration this section itself needs in order for
already-modeled features to be actually usable or secure:

- **`auth_url`** (`AuthUrl` / `auth_url`) — without this, `auth = "custom"`
  (already one of the modeled `auth` field's four `OneOf` values) is
  advertised as a supported mode but has no way to configure where the
  custom auth endpoint actually lives. Leaving `auth_url` preserved would
  ship a mode selector that silently can't be pointed anywhere new through
  Terraform.
- **`ec_enabled`** (`EcEnabled` / `ec_enabled`) — a real operational toggle
  (Elliptic-Curve — the controller's TLS/crypto-mode flag for the guest
  portal), not a cosmetic.
- **`custom_ip`** (`CustomIP` / `custom_ip`) — pairs with `portal_hostname`
  (already modeled) as the alternate way to pin the portal's advertised
  address; a portal-networking knob, not styling.
- **`redirect_https`** (`RedirectHttps` / `redirect_https`) — **a genuinely
  distinct wire key from the already-modeled `redirect_to_https`**
  (`RedirectToHttps` / `redirect_to_https`). go-unifi models both as separate
  fields on `GuestAccess`; rev. 1 modeled only `redirect_to_https` and left
  `redirect_https` preserved, which silently dropped a real security-relevant
  redirect-scheme knob. Both are now modeled as independent boolean leaves —
  no assumption is made about how the controller reconciles the two if a
  user sets them inconsistently; that reconciliation, if any, is the
  controller's concern, matching this PR's "provider validates only what a
  single field's own value-space allows" stance (Key Decision 3).

None of these four are secrets; all four use the existing
`decodeBool`/`overlayBool` or `decodeString`/`overlayString` primitives with
no new codec work.

**Modeled (56 fields, 18 secrets) — the operational surface:** auth mode
selection and its endpoint (`auth`, `auth_url`), portal enable/network
behavior (including `custom_ip`, `ec_enabled`), guest-session expiry,
subnet/DNS restriction, both redirect-scheme flags (`redirect_to_https` and
`redirect_https`), shared-password mode, voucher enable, RADIUS-backed auth,
the three OAuth SSO providers' connection settings (enable/id/secret — not
their cosmetic scope/domain sub-flags), and the full payment-gateway
credential surface for all 6 gateways (explicitly in scope per the parent
design's "payment/auth secret surface" framing, and it's exactly where the
real secrets live).

**Preserved (41 fields) — the portal template/styling surface, plus a small
residual of genuinely cosmetic/sub-feature knobs:** all 31 `PortalCustomized*`
fields (the parent `PortalCustomized` toggle plus 30 sub-fields: colors,
fonts, logo, background, welcome/success/ToS text and their `*_enabled`
toggles, language list, box opacity/radius, button styling, Unsplash
attribution), plus 10 more: `TemplateEngine`, `VoucherCustomized`, the OAuth
cosmetic sub-flags (`FacebookScopeEmail`, `FacebookWifiBlockHttps`,
`FacebookWifiGwID`/`GwName`/`GwSecret`, `GoogleScopeEmail`, `GoogleDomain`),
and `WechatShopID`. All 41 round-trip via the standard
`snap.dataCopy("guest_access")` RMW base on write; **the modeled fields are
the only ones that ever appear in Terraform state** — preserved fields are
never decoded into the model and never appear in plan/state, matching how
`mgmt`'s `alert_enabled`/`boot_sound`/etc. and `radius`'s
`configure_whole_network`/`tunneled_reply` are handled today.

`FacebookWifiGwSecret` (wire `x_facebook_wifi_gw_secret`) is itself a secret
field but is in the **preserved** set: it belongs to the Facebook-WiFi
gateway-binding sub-feature (paired with `FacebookWifiGwID`/`GwName`, both
also preserved), not the primary Facebook-login OAuth flow
(`FacebookAppID`/`FacebookAppSecret`, both modeled). Since it is never
decoded into state, RMW preserves whatever value the controller already
holds for it verbatim on every write — it is never at risk of being cleared,
it is simply not user-configurable through this PR. This is flagged
explicitly because it is the one preserved field that is also a credential;
see Open Decisions for the promote-if-needed escape hatch. This is also the
"19th credential-like field" referenced in the secret-count discussion below
— it is deliberately NOT one of the 18 modeled `Sensitive` attributes.

## Full field table

Legend: **M** = modeled (in schema); **P** = preserved (RMW only, not in
schema); **S** = secret (`WriteOnlySecret` class, `Sensitive` schema flag,
**and `Computed`** — see Key Decision 2a for why Computed is included here).
Counts verified by script against every `json:"..."` tag in
`guest_access.generated.go`: **56 M (18 S) + 41 P = 97.**

### Modeled (56 fields)

| Go field | Wire key | S | Notes |
|----------|----------|---|-------|
| `Auth` | `auth` | | OneOf `none/hotspot/facebook_wifi/custom` |
| `AuthUrl` | `auth_url` | | **rev. 2: promoted from preserved.** Required to make `auth = "custom"` actually configurable; no validator (free-form URL, controller-side interpretation) |
| `PortalEnabled` | `portal_enabled` | | |
| `PortalUseHostname` | `portal_use_hostname` | | |
| `PortalHostname` | `portal_hostname` | | regex `^[a-zA-Z0-9.-]+$\|^$` |
| `CustomIP` | `custom_ip` | | **rev. 2: promoted from preserved.** Alternate portal-address pinning to `portal_hostname`; go-unifi regex constrains to dotted-quad-or-empty |
| `EcEnabled` | `ec_enabled` | | **rev. 2: promoted from preserved.** Operational TLS/crypto-mode toggle, not cosmetic |
| `Expire` | `expire` | | `[\d]+` or `custom`; kept string per go-unifi tag |
| `ExpireNumber` | `expire_number` | | 1–1,000,000 |
| `ExpireUnit` | `expire_unit` | | OneOf `1,60,1440` (minutes/hours/days) |
| `RedirectEnabled` | `redirect_enabled` | | |
| `RedirectUrl` | `redirect_url` | | |
| `RedirectToHttps` | `redirect_to_https` | | |
| `RedirectHttps` | `redirect_https` | | **rev. 2: promoted from preserved.** Distinct wire key from `redirect_to_https` — go-unifi models both independently; see Key Decision 1's rev. 2 note |
| `AllowedSubnet` | `allowed_subnet_` | | trailing `_` in wire key (go-unifi tag, not a typo) |
| `RestrictedSubnet` | `restricted_subnet_` | | trailing `_` |
| `RestrictedDNSEnabled` | `restricted_dns_enabled` | | |
| `RestrictedDNSServers` | `restricted_dns_servers` | | `types.List` of IPv4 strings |
| `PasswordEnabled` | `password_enabled` | | shared/hotspot password mode toggle |
| `Password` | `x_password` | **S** | shared portal password |
| `VoucherEnabled` | `voucher_enabled` | | |
| `RADIUSEnabled` | `radius_enabled` | | |
| `RADIUSProfileID` | `radiusprofile_id` | | |
| `RADIUSAuthType` | `radius_auth_type` | | OneOf `chap/mschapv2` |
| `RADIUSDisconnectEnabled` | `radius_disconnect_enabled` | | |
| `RADIUSDisconnectPort` | `radius_disconnect_port` | | 1–65535 |
| `FacebookEnabled` | `facebook_enabled` | | |
| `FacebookAppID` | `facebook_app_id` | | |
| `FacebookAppSecret` | `x_facebook_app_secret` | **S** | |
| `GoogleEnabled` | `google_enabled` | | |
| `GoogleClientID` | `google_client_id` | | |
| `GoogleClientSecret` | `x_google_client_secret` | **S** | |
| `WechatEnabled` | `wechat_enabled` | | |
| `WechatAppID` | `wechat_app_id` | | |
| `WechatAppSecret` | `x_wechat_app_secret` | **S** | |
| `WechatSecretKey` | `x_wechat_secret_key` | **S** | distinct from `WechatAppSecret` per go-unifi — see Open Decisions #3 |
| `PaymentEnabled` | `payment_enabled` | | |
| `Gateway` | `gateway` | | OneOf `paypal/stripe/authorize/quickpay/merchantwarrior/ippay` |
| `PaypalUsername` | `x_paypal_username` | **S** | |
| `PaypalPassword` | `x_paypal_password` | **S** | |
| `PaypalSignature` | `x_paypal_signature` | **S** | |
| `PaypalUseSandbox` | `paypal_use_sandbox` | | |
| `StripeApiKey` | `x_stripe_api_key` | **S** | |
| `AuthorizeLoginid` | `x_authorize_loginid` | **S** | |
| `AuthorizeTransactionkey` | `x_authorize_transactionkey` | **S** | |
| `AuthorizeUseSandbox` | `authorize_use_sandbox` | | |
| `QuickpayMerchantid` | `x_quickpay_merchantid` | **S** | |
| `QuickpayApikey` | `x_quickpay_apikey` | **S** | |
| `QuickpayAgreementid` | `x_quickpay_agreementid` | **S** | |
| `QuickpayTestmode` | `quickpay_testmode` | | |
| `MerchantwarriorMerchantuuid` | `x_merchantwarrior_merchantuuid` | **S** | |
| `MerchantwarriorApikey` | `x_merchantwarrior_apikey` | **S** | |
| `MerchantwarriorApipassphrase` | `x_merchantwarrior_apipassphrase` | **S** | |
| `MerchantwarriorUseSandbox` | `merchantwarrior_use_sandbox` | | |
| `IPpayTerminalid` | `x_ippay_terminalid` | **S** | |
| `IPpayUseSandbox` | `ippay_use_sandbox` | | |

18 secret leaves, itemized by family (matches Key Decision 2a's count
exactly): `Password` (1: shared portal password) + `FacebookAppSecret` (1) +
`GoogleClientSecret` (1) + `WechatAppSecret` + `WechatSecretKey` (2: two
genuinely distinct controller keys, see Open Decisions #3) + 13
payment-gateway credential leaves (`PaypalUsername`, `PaypalPassword`,
`PaypalSignature`, `StripeApiKey`, `AuthorizeLoginid`,
`AuthorizeTransactionkey`, `QuickpayMerchantid`, `QuickpayApikey`,
`QuickpayAgreementid`, `MerchantwarriorMerchantuuid`, `MerchantwarriorApikey`,
`MerchantwarriorApipassphrase`, `IPpayTerminalid`) = 1+1+1+2+13 = **18**.

A **19th** credential-like field, `FacebookWifiGwSecret` (wire
`x_facebook_wifi_gw_secret`), exists on the struct but is deliberately
**preserved, not modeled** — see Key Decision 1's discussion above. Do not
confuse the "19 `x_`-prefixed fields on the struct" fact with the "18
modeled secrets" count; they differ by exactly this one field.

### Preserved (41 fields, RMW-only, never decoded into state)

31 `PortalCustomized*` fields (the parent toggle plus 30 sub-fields):
`PortalCustomized`, `PortalCustomizedAuthenticationText`,
`PortalCustomizedBgColor`, `PortalCustomizedBgImageEnabled`,
`PortalCustomizedBgImageFilename`, `PortalCustomizedBgImageTile`,
`PortalCustomizedBgType`, `PortalCustomizedBoxColor`,
`PortalCustomizedBoxLinkColor`, `PortalCustomizedBoxOpacity`,
`PortalCustomizedBoxRADIUS`, `PortalCustomizedBoxTextColor`,
`PortalCustomizedButtonColor`, `PortalCustomizedButtonText`,
`PortalCustomizedButtonTextColor`, `PortalCustomizedLanguages`,
`PortalCustomizedLinkColor`, `PortalCustomizedLogoEnabled`,
`PortalCustomizedLogoFilename`, `PortalCustomizedLogoPosition`,
`PortalCustomizedLogoSize`, `PortalCustomizedSuccessText`,
`PortalCustomizedTextColor`, `PortalCustomizedTitle`, `PortalCustomizedTos`,
`PortalCustomizedTosEnabled`, `PortalCustomizedUnsplashAuthorName`,
`PortalCustomizedUnsplashAuthorUsername`, `PortalCustomizedWelcomeText`,
`PortalCustomizedWelcomeTextEnabled`, `PortalCustomizedWelcomeTextPosition`.

That is 31 names total (script-verified: `grep -c '^PortalCustomized'` over
the struct's field names, excluding the `UnmarshalJSON` alias-struct
duplicates), not 25 — rev. 1's prose undercounted this group; the 31 figure
is now load-bearing (used directly in the 31 + 10 = 41 arithmetic below) and
is the number Task 1's mechanical diff must reproduce.

Remaining 10 preserved fields (rev. 2: down from rev. 1's 14, after
promoting `AuthUrl`, `EcEnabled`, `CustomIP`, `RedirectHttps` to modeled):
`TemplateEngine`, `VoucherCustomized`, `FacebookScopeEmail`,
`FacebookWifiBlockHttps`, `FacebookWifiGwID`, `FacebookWifiGwName`,
`FacebookWifiGwSecret`, `GoogleScopeEmail`, `GoogleDomain`, `WechatShopID`.

31 + 10 = 41, matching the top-line count. `FacebookWifiGwSecret` is the one
secret-shaped field in this preserved list (see Key Decision 1's discussion).

### Field count reconciliation (authoritative — verify against source in Task 1)

The plan's first task step MUST mechanically diff the modeled-field wire-key
list against every `json:"..."` tag in `guest_access.generated.go` (e.g. a
throwaway `go run`/script over the struct's field tags, or a table-driven Go
test asserting set equality) and assert:

- every one of the 97 fields is classified exactly once (modeled-non-secret,
  modeled-secret, or preserved) — no field silently falls through both sets
  or neither;
- the modeled set is exactly the 56 Go field names in the "Modeled" table
  above, 18 of which are tagged **S**;
- the preserved set is exactly the complementary 41 field names (the two
  "Preserved" lists above, verified to total 41 by the script, not by the
  prose parenthetical counts);
- `BaseSetting`'s embedded fields (`Key`/etc.) are out of scope entirely —
  they are handled generically by every section via `settings.RawSetting{
  BaseSetting: settings.BaseSetting{Key: s.key()}, ... }`, not per-field.

If the mechanical diff produces a different split than 56/18/41, the plan
task fixes this spec's tables, not the code — the tables are the spec's
claim, the struct is ground truth.

## KEY DECISION 2a — Secret schema flags: Optional + Computed + Sensitive (overrides rev. 1's stale C1 reading)

**This overrides the parent design's general C1 "`WriteOnlySecret` =
Optional+Sensitive, no Computed" table for this section.** All 18 secret
leaves in `guest_access` are `Optional + Computed + Sensitive`, matching the
**actually-shipped** flat-secret precedent: `radius.secret`
(`unifi/setting_section_radius.go`, schema block ~lines 92-104):

```go
"secret": schema.StringAttribute{
    MarkdownDescription: "RADIUS shared secret.",
    Optional:            true,
    Computed:            true,
    Sensitive:           true,
    Validators: []validator.String{ ... },
},
```

Rationale, spelled out because it is easy to get backwards from the general
C1 table alone:

- All 18 secret leaves are children of `guest_access`, a **`SingleNestedAttribute`
  that is itself `Optional + Computed`** (Key Decision "Schema shape" below).
  A child attribute's Computed-ness interacts with the parent object's plan
  behavior: when the parent is Computed and the practitioner's config leaves
  a child null, the framework needs a source of truth for what that child's
  *planned* value is.
- `decode()` for every secret leaf **always sets it from prior state**
  (`secret := priorModel.Secret`-shaped, never read from the wire) —
  identical to `radius.secret`. That means the provider itself is the one
  computing this attribute's value when config doesn't supply it, which is
  precisely the framework's definition of `Computed`.
- **Without `Computed`, a config-null secret with a non-null prior state
  value produces a "provider produced inconsistent result after apply"
  error**: the framework expects an `Optional`-only (non-Computed) attribute
  that is null in config to plan as null, but this section's own `decode`
  logic will set it to the prior (non-null) value post-apply — a direct
  contradiction the framework detects and errors on. `Computed` is the
  signal that tells the framework "the provider, not just the config, may
  supply this value," which is exactly what's happening.
- `mgmt.ssh_password` (`unifi/setting_section_mgmt.go`, ~lines 132-137) is
  `Optional + Sensitive`, **without** `Computed` — a **pre-existing
  variance** in the codebase, not a second precedent to follow. It differs
  from `radius.secret` in exactly this one flag and nothing else; both
  sections share the identical decode-always-prior / overlay-delete-on-unset
  behavior. **Do not model `guest_access`'s 18 secrets after `mgmt`'s
  ssh_password.** `radius.secret` is the correctly-flagged precedent because
  it is Computed; `mgmt.ssh_password`'s omission is not explained anywhere
  in that section's own doc comments and is not re-litigated by this PR — it
  is simply not copied.

Applied uniformly: **all 18 modeled secret leaves in `guest_access` are
`Optional + Computed + Sensitive`.**

## KEY DECISION 2b — Secret handling: a section-local multi-leaf carry helper, not a new shared engine function

PR-A's existing pattern (`radius.secret`, `mgmt.ssh_password`) is **one**
`WriteOnlySecret` leaf per section, handled by:
- `decode`: unconditionally `secret := priorModel.Secret` (never read from the
  API — the controller returns a mask, never the real value);
- `overlay`: delete the wire key from `base` when config is null/unknown,
  write it verbatim (including explicit empty) when configured;
- `carryBestEffort`: `carrySecretObject(plan.X, dst.X, "secret")` — a helper
  that rebuilds the plan object but substitutes prior's value for exactly
  **one named leaf** when that leaf is null/unknown in plan.

`guest_access` has 18 such leaves in one section. **Rev. 2 decision: add a
new function `carryGuestAccessSecrets`, local to
`unifi/setting_section_guest_access.go`, that loops the existing
`carrySecretObject` once per leaf — NOT a new shared
`carrySecretObjectMulti` in `setting_engine.go`.** This reverses rev. 1's
recommendation (option (b), a shared generalized helper) in favor of rev.
1's own documented fallback (option (a)), specifically to minimize
shared-PR-A-file churn: `setting_engine.go` is a file every Bn tranche reads
and potentially depends on, and the parent design's "each tranche...
introduces no dependency on another Bn" rule is best honored by touching it
not at all when a section-local alternative is equally correct. This is a
policy change from rev. 1's Key Decision 2, not a technical correction — the
generalized-helper approach was not "wrong," just not the minimal-footprint
choice, and the codex review's NEEDS-WORK flagged the churn as worth
avoiding.

`carryGuestAccessSecrets` has this shape:

```go
// carryGuestAccessSecrets threads plan.GuestAccess through carrySecretObject
// once per secret leaf in guestAccessSecretLeaves, accumulating the rebuilt
// object across iterations while always reading the leaf's prior value from
// the ORIGINAL prior object — never from the intermediate "out" produced by
// an earlier iteration. See the loop body comment for why this distinction
// is load-bearing, not stylistic.
func carryGuestAccessSecrets(plan, prior types.Object, secretLeaves []string) (types.Object, diag.Diagnostics) {
    var diags diag.Diagnostics

    if plan.IsNull() || plan.IsUnknown() {
        return prior, diags
    }

    out := plan
    for _, leaf := range secretLeaves {
        var d diag.Diagnostics
        // CRITICAL: arg1 is the accumulating "out" (this leaf must be
        // carried on top of every previous leaf's result), but arg2 is
        // ALWAYS the original "prior" parameter, never "out" itself.
        // carrySecretObject reads prior.Attributes()[secretLeaf] to decide
        // what to substitute when plan's leaf is null/unknown. If "out" were
        // passed as arg2 instead of the original "prior", then by the time
        // a LATER leaf in this loop is processed, "out" would already have
        // had EARLIER leaves overwritten — but critically, "out" started as
        // a copy of "plan", so passing out-as-prior would make every
        // not-yet-processed leaf see PLAN's value as if it were "prior",
        // silently losing the real original prior value for any leaf whose
        // plan value is null/unknown and which hasn't been processed yet in
        // this loop. Threading the untouched "prior" parameter as arg2 on
        // every iteration is what keeps each leaf's substitution correct
        // regardless of loop order.
        out, d = carrySecretObject(out, prior, leaf)
        diags.Append(d...)
    }
    return out, diags
}
```

with `guestAccessSecretLeaves` a package-level `[]string` of the 18 tfsdk
names, defined in `unifi/setting_section_guest_access.go` alongside the
section:

```go
func (guestAccessSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
    obj, diags := carryGuestAccessSecrets(plan.GuestAccess, dst.GuestAccess, guestAccessSecretLeaves)
    dst.GuestAccess = obj
    return diags
}
```

`setting_engine.go` is untouched by this PR. `carrySecretObject` itself is
unmodified and is called by `guest_access`, `radius`, and `mgmt` alike —
`guest_access` simply calls it 18 times in a loop instead of once inline.

**decode/overlay for all 18 leaves follow the identical radius/mgmt
inline pattern** — no new mechanism there, just 18 repetitions of:

```go
// decode: never read from API, always carry prior
paypalPassword := priorModel.PaypalPassword
...
// overlay: delete-on-unset, verbatim-on-set (including explicit empty)
if !m.PaypalPassword.IsNull() && !m.PaypalPassword.IsUnknown() {
    base["x_paypal_password"] = m.PaypalPassword.ValueString()
} else {
    delete(base, "x_paypal_password")
}
```

None of guest_access's secrets are `echoed` (the design's C1 escape hatch for
a field known to echo its real value back) — the controller masks all of
them like every other UniFi settings secret observed to date. No `echoed`
annotation is used in this PR.

None of the 18 secrets are nested inside an object/list — all are top-level
scalars on `GuestAccess` itself (unlike, hypothetically, a per-gateway nested
object). So the nested-secret-leaf question the task brief raises ("mirror
the nested-secret-leaf pattern... unless a secret is a top-level leaf where
native write-only fits") resolves the same way B3/SNMP resolves it: these are
top-level leaves, so the existing top-level `WriteOnlySecret` pattern
(`Optional + Computed + Sensitive` per Key Decision 2a, decode-preserves-prior
/ overlay-delete-on-mask) is used directly — no nested-object wrapping is
introduced for them. "Native write-only" (`WriteOnlyAttribute`/`Ephemeral`
resource support in terraform-plugin-framework) is explicitly NOT adopted
here, matching the PR-A simplification plan's constraint ("No `*_wo`
attributes... Secrets are frozen" at the pattern level) — introducing a
different secret idiom for one section would fragment the codec's uniform
handling for no benefit, and is out of scope for a docs-only, engine-frozen
tranche.

## KEY DECISION 3 — Validation (design §7)

| Field | Validator | Rationale |
|---|---|---|
| `auth` | `stringvalidator.OneOf("none", "hotspot", "facebook_wifi", "custom")` | go-unifi comment enumerates the value set |
| `expire_unit` | `int64validator.OneOf(1, 60, 1440)` | minutes/hours/days multiplier, per go-unifi comment |
| `expire_number` | `int64validator.Between(1, 1000000)` | go-unifi regex `^[1-9][0-9]{0,5}\|1000000$` |
| `portal_hostname` | `stringvalidator.RegexMatches(^[a-zA-Z0-9.-]+$\|^$)` | go-unifi field comment; empty allowed (unset) |
| `custom_ip` | `stringvalidator.RegexMatches` dotted-quad-or-empty | go-unifi regex `^(...)\.){3}(...)$\|^$`; rev. 2 promoted field |
| `restricted_dns_servers[*]` | `stringvalidator.RegexMatches` per-element, dotted-quad pattern, via `listvalidator.ValueStringsAre(...)` | go-unifi regex constrains to dotted-quad; the codebase's `iptypes.IPv4Address` custom type (used in `client_resource.go`/`client_data_source.go`) is a different resource family's convention, not used by any existing `unifi_setting` section — stay consistent with the settings sections' plain-string + regex style rather than introducing a new per-element custom type for one list. `listvalidator.ValueStringsAre` is an established codebase pattern for per-element string-list validation (see `unifi/firewall_policy_resource.go` and `unifi/site_to_site_vpn_resource.go`, both of which combine it with a `stringvalidator`/custom validator) — no existing `unifi_setting` section validates a string list per-element yet, so this is the first one, following the pattern from those two non-setting resources rather than inventing a new composition idiom |
| `radius_auth_type` | `stringvalidator.OneOf("chap", "mschapv2")` | go-unifi comment |
| `radius_disconnect_port` | `int64validator.Between(1, 65535)` | go-unifi regex is a decimal-port pattern; same bound already used for `radius.auth_port`/`acct_port` |
| `gateway` | `stringvalidator.OneOf("paypal", "stripe", "authorize", "quickpay", "merchantwarrior", "ippay")` | go-unifi comment enumerates all 6 supported gateways |
| `expire` | none (free-form `[\d]+|custom` string per go-unifi; not worth a regex validator — `expire_number`/`expire_unit` carry the actual bound) | avoid over-constraining a string the controller itself treats loosely |
| `auth_url` | none (free-form URL/host string; go-unifi has no regex comment for this field) | rev. 2 promoted field; the controller is the source of truth for a valid custom-auth endpoint |

No cross-field `OneOf`/conditional-requirement validator (e.g. "gateway
requires payment_enabled=true" or "auth=facebook_wifi requires
facebook_enabled=true" or "auth=custom requires auth_url set") is added in
this PR — the parent design's C4 discriminator framework was deleted in PR-A
Task 4 as YAGNI ("`setting_discriminator.go`... zero production callers...
reintroduce... with the first NAT/firewall consumer"); guest_access does not
reintroduce it. The controller is the source of truth for cross-field
consistency; the provider validates only what a single field's own
value-space allows, consistent with every other migrated section.

## Schema shape

One `SingleNestedAttribute` (`guest_access`) on `unifi_setting`, `Optional +
Computed`, no `UseStateForUnknown` (matching `radius`'s parent-level shape,
not `mgmt`'s — `guest_access` has no nested list requiring churn
suppression). 56 children: 38 non-secret scalars/lists + 18 `Optional +
Computed + Sensitive` `StringAttribute`s (`WriteOnlySecret` class per Key
Decision 2a).

## Privacy-safe synthetic fixtures

No real payment credentials, RADIUS secrets, OAuth client IDs/secrets, or
hostnames appear in any test or example. Fixtures use:

- Portal hostname: `guest.example.internal`
- Redirect URL: `https://welcome.example.com/`
- Custom auth URL (rev. 2 field): `https://auth.example.internal/guest`
- Custom portal IP (rev. 2 field): `192.0.2.10` (TEST-NET-1, RFC 5737)
- Allowed/restricted subnet: `10.20.30.0/24` (RFC 5737/1918-safe, distinct
  from any subnet already used elsewhere in the test suite — cross-check
  against `192.168.2.1/24` (teleport, already flagged as private in the
  parent design) before reuse)
- Restricted DNS servers: `192.0.2.1`, `198.51.100.1` (TEST-NET-1/2, RFC 5737)
- RADIUS profile ID: `"radius-profile-example"` (a placeholder, not a
  controller-shaped ObjectID — go-unifi's `radiusprofile_id` is a free string
  reference; a fake 24-hex-char ObjectID would look real and risks
  copy-paste into a real config, so a human-readable placeholder is safer)
- Facebook/Google/WeChat app IDs: `example-app-id-123`
- All 18 secret values in tests: short descriptive placeholders in the style
  already used by `radius`/`mgmt` secret tests (e.g. `"test-radius-secret"`,
  `"real-secret"`, `"new-secret"` — see
  `unifi/setting_section_radius_test.go`), never a plausible-looking real
  key/token format
- Payment gateway sandbox usernames: `sandbox-user`, never a real merchant ID

No fixture anywhere uses a real hostname, a public IP, or the word "public"
as a placeholder value — RFC 5737 (`192.0.2.0/24`, `198.51.100.0/24`,
`203.0.113.0/24`) and RFC 1918/2606 (`10.0.0.0/8`, `.example`/`.internal`)
ranges only.

## Open decisions to confirm before implementation

1. **~~`carrySecretObjectMulti` vs. 18 chained single-leaf calls~~ — RESOLVED
   in rev. 2: section-local `carryGuestAccessSecrets` looping the existing
   `carrySecretObject`, no `setting_engine.go` change** (Key Decision 2b).
2. **The exact modeled/preserved line** (Key Decision 1) — rev. 2 already
   promotes the four operationally-necessary fields identified by the codex
   review (`auth_url`, `ec_enabled`, `custom_ip`, `redirect_https`). Confirm
   no *further* known real-world Terraform use case needs a specific
   remaining preserved field (e.g. `portal_customized_tos`/
   `portal_customized_tos_enabled` for a compliance-driven ToS-text-as-code
   workflow — the most plausible remaining preserved-field objection). If
   ToS text/enable is a real requirement, promote just those 2 fields into
   "modeled" (cheap, no ripple).
3. **`WechatAppSecret` vs. `WechatSecretKey`** — go-unifi models these as two
   distinct secrets; confirm this isn't SDK-level duplication/drift before
   shipping two separate `Sensitive` attributes (a quick live-controller or
   go-unifi-changelog check, not blocking if unavailable — worst case both
   are modeled and one is simply always null in practice).
