# Milestone 0 baseline and side-line audit

Date: 2026-08-03

Status: accepted M0a evidence. This record fixes the repository starting point
for the architecture documents. It is an audit, not authorization to move code
between branches or repositories.

## Provider baseline

| Fact | Accepted value | Evidence |
| --- | --- | --- |
| Canonical provider address | `registry.terraform.io/ubiquiti-community/unifi` | Current registry namespace and repository module ownership |
| Released compatibility authority | v0.101.2, commit `4e677062f23f232be9fda3e559279937a0f3d007` | Annotated release tag `v0.101.2` |
| Development baseline | `26bcad84d786a4e9dd9bd3654179ae3f3c8c9059` | `git diff v0.101.2..26bcad84` changes only `.github/workflows/ci.yaml` to install Terraform for documentation generation |
| Effective SDK dependency | module path `github.com/ubiquiti-community/go-unifi`, replaced by `github.com/jamesbraid/go-unifi v1.101.0` | Provider `go.mod`. v1.101.0 resolves to `45cec1b4052871f1b0c0eaa27421f4305a004597` |

M0 treats the published v0.101.2 archive as the external compatibility
authority and `26bcad84` as the source-development baseline. It does not infer
runtime compatibility from the one intervening CI-only commit. Archive,
binary, toolchain, and schema hashes are produced by M0c, not guessed in this
documentation gate.

The legacy provider address `registry.terraform.io/paultyng/unifi` is not an
alias promise. Moving existing state to the canonical address requires an
explicit, separately tested `terraform state replace-provider` operation.

M0c measured one CLI capability difference. Terraform 1.15.8 returns the
provider's action schema and 25 list-resource schemas; OpenTofu 1.12.1 omits
those two categories. Their provider, resource, data-source, and identity
projections are otherwise byte-identical after removing only the CLI envelope.
The baseline preserves both full projections and treats the missing categories
as an explicit OpenTofu compatibility fact, not as provider schema drift.

## `go-unifi` baseline

`go-unifi` v1.102.0 resolves to
`e255518385e0104eb838be56c2a491de158f3194`. It is the reconciled candidate line
for M0b and already contains the schema-generation, extraction provenance,
generated-API compatibility, controller-test, drift-probe, and current
controller-behavior lineage. The provider does not consume it during M0.

The existing `schemas/VERSION`, `schemas/SOURCE`, and `schemas/ARTIFACT` files
are independent text markers in v1.102.0. They are not the structural lock
described by this architecture. M0b replaces their input authority with
`schemas/capture.lock.json` and makes the text files generated compatibility
projections. `specification.json` remains bootstrap/golden evidence and has no
Terraform policy authority.

## `provider-prereqs` disposition

The branch forks from `596843add3cb3da75df14e65ec6866af76504b31` and contains
exactly eight commits. A comparison with v1.102.0 reports only `894aafc3` as
patch-equivalent (`git cherry` marks it `-`). The other seven are not accepted
merely because their patches still apply or their ideas remain useful.

| Commit | Subject | Disposition | Evidence and future treatment |
| --- | --- | --- | --- |
| `894aafc3ccdaf7ad01f02c0364bbe161a7609b04` | `test: use neutral fixtures in AP group client tests` | Already integrated | Patch-equivalent content is in v1.102.0. No transplant is needed. |
| `a6b7e16dcac1edfb8eaf747ac2cfc66abf558fe6` | `feat(settings): add global_network, ipsec, and provider_capabilities settings` | Still useful but deferred | v1.102.0 does not contain these setting types. They are possible future structural/behavior candidates, but M0 may not add SDK or provider surface. Re-observe them against a locked target and admit them through current ownership rules. |
| `92d3517d2edac4bed4a05ded27249fb09e318eac` | `feat(settings): add usg_geo setting with nested ip_filtering` | Superseded | v1.102.0 owns `UsgGeo` in `unifi/settings/usg_geo.generated.go`, including the same nested object plus generated enum and pattern constraints. The handwritten side-line version must not replace it. |
| `882cfb968dade39954d6aa3688bfaef3629c41a2` | `test(nat): pin exported NAT CRUD surface with client tests` | Still useful but deferred | Its list/get client tests cover existing behavior, but v1.102.0 has no `unifi/nat_test.go`. Porting tests is separate SDK work after the hermetic baseline. M0 does not move code from this branch. |
| `f63c46f60e6559367b7e247188a3421feb1a6fa2` | `test(nat): pin ListNat/CreateNat/UpdateNat/DeleteNat` | Still useful but deferred | It extends the preceding NAT test work with create/update/delete and not-found behavior. Preserve the intent for a later test-first change on canonical main, not as a dependent cherry-pick. |
| `79e363585631569394b36c8ce50a8416c77d616a` | `feat(content-filtering): add v2 content-filtering client` | Conflicts with generated ownership model | It adds a custom schema under the side line's `cmd/fields/custom` layout and commits generated client/model output. v1.102.0 uses `overrides/resources`, collision/orphan guards, and measured drift ownership. Re-capture and generate this capability on canonical main if it is later admitted. |
| `de14f7a7e77b831158cc573ff7b632b94f0bc79b` | `test(content-filtering): cover list/update/delete wrappers` | Still useful but deferred | The wrapper scenarios are useful only after a content-filtering operation is captured and admitted on canonical main. This commit depends on the rejected ownership path in `79e36358` and cannot be moved independently in M0. |
| `f61c9240ad48206d22ace66bf53668062478ac45` | `feat(firewall-policy): add APP/APP_CATEGORY matching (app_ids, app_category_ids)` | Conflicts with generated ownership model | It edits `FirewallPolicy.json` in the obsolete custom layout and directly changes generated Go. v1.102.0 does not currently expose those fields. Their possible usefulness requires a locked structural/behavior finding and regeneration through current ownership rules. |

The branch is therefore neither a prerequisite nor a merge source. Its useful
test cases and feature hypotheses remain provenance for later, independently
reviewed work.

## `provider-prereqs` re-verification (2026-08-09)

Re-checked against `forgejo/main` at `9e18ee5` — v1.102.0 plus the twenty
M0b/M2/M4 control-plane commits — rather than against v1.102.0, which is what
the original audit used. The twenty change none of the per-commit conclusions.

In short: the branch is not a merge source and never becomes one, but it is not
valueless. Four of its eight commits describe capability that still does not
exist on the canonical line.

### What the branch is

Eight commits, seven written on 2026-07-10 and `894aafc3` on 07-06, forked from
`596843add3cb3da75df14e65ec6866af76504b31`. That fork point sits on the
*upstream* line: the branch is eight ahead of and seven behind `upstream/main`
and shares none of the regeneration history that produced the canonical line.
It is a pre-regeneration capability spike — 1180 insertions across nineteen
files adding hand-written settings types, a v2 content-filtering client, NAT
client tests, and firewall-policy APP matching.

### Why it is not a merge source

Ownership, not defects. The branch writes capability by hand into
`cmd/fields/custom/*.json` and commits hand-edited generated Go. The canonical
line derives its models from controller definitions extracted at build time,
layered with reviewed inputs under `overrides/`: `resources/*.json` for the
internal v2 endpoints the controller does not describe, and `fields.toml` for
REST paths and per-field overrides. `cmd/fields/custom/` does not exist on
`forgejo/main` at all, so the branch's schema files sit at a path the generator
never reads and its edits to generated Go are discarded by the next generation
run. That part of the roadmap's judgment stands: do not merge or cherry-pick
this branch.

The roadmap goes further and calls it "an obsolete side line". That is harsher
than the audit it cites. The disposition table above records "still useful but
deferred" against four commits and tells a later reader to "preserve the intent"
of one of them, and this re-verification confirms the capability those four
describe is still missing. The branch is a dead vehicle, not a dead idea.

### Per-commit state

`git cherry forgejo/main f61c924` marks only `894aafc3` patch-equivalent (`-`);
the other seven remain `+`. Presence checked with `git ls-tree -r --name-only
forgejo/main`.

| Commit | State on `forgejo/main` |
| --- | --- |
| `894aafc3` AP-group neutral fixtures | Integrated. Patch-equivalent. |
| `92d3517d` usg_geo | Superseded by generation. `unifi/settings/usg_geo.generated.go` carries the same nested object plus generated enum and pattern constraints the hand-written version lacks. |
| `882cfb96`, `f63c46f6` NAT tests | Subject present, tests absent. `unifi/nat.go` exports ListNat, GetNat, CreateNat, UpdateNat and DeleteNat; there is no `unifi/nat_test.go`. |
| `a6b7e16d` global_network, ipsec, provider_capabilities | Absent. None of the three exists under `unifi/settings/`. |
| `79e36358`, `de14f7a7` content filtering | Absent. No `ContentFiltering` type or client. The only match is `Ips.content_filtering_blocking_page_enabled`, an unrelated IPS blocking-page toggle. |
| `f61c9240` firewall APP matching | Absent. `overrides/resources/FirewallPolicy.json` declares no `app_ids` or `app_category_ids`, and its `matching_target` omits `APP` and `APP_CATEGORY`. |

### Still missing, and what each would cost

The deferral recorded above was conditioned on the hermetic baseline. That
baseline now exists: the M0b two-run rebuild gate passed at `180b24f`, both runs
producing `61358fb6…` and matching that commit's `schemas/GENERATED_SHA256`.
These four therefore move from deferred to backlog. In ascending cost:

1. Firewall APP matching (`f61c9240`). Add `app_ids` and `app_category_ids` to
   the source and destination blocks of `overrides/resources/FirewallPolicy.json`
   and extend `matching_target` with `APP|APP_CATEGORY`, then regenerate. The
   old and new files use the same format, wire field name to validation string,
   so this is a JSON port of roughly sixteen lines. Discard the branch's
   hand-edited generated Go.
2. NAT client tests (`882cfb96`, `f63c46f6`). The subject already exists
   untested. Re-author test-first against canonical main. The branch's fixtures
   target the older client shape and are provenance, not a patch.
3. Content filtering (`79e36358`, `de14f7a7`). Add
   `overrides/resources/ContentFiltering.json` and a path entry in
   `overrides/fields.toml` — compare `[FirewallPolicy] path = "firewall-policies"`
   — then regenerate and port the wrapper tests.
4. `global_network`, `ipsec`, `provider_capabilities` (`a6b7e16d`). Each carries
   a single scalar hand-observed from one live controller, and the branch's own
   comments record that the 9.5.21 field spec does not define them. Observe
   against a locked target and admit through the catalog before adding them.

The AssistedRoaming precedent does not apply to items 1 and 3, which is the
first objection this will attract. Those fields disappeared because they lived
in *generated* code and 10.4.57 stopped declaring them. `FirewallPolicy` and
`ContentFiltering` are internal v2 endpoints the controller's schema files never
described, and `overrides/resources/*.json` is the sanctioned durable input for
that case: regeneration re-emits from it rather than deleting it.

### Working-copy trap

`/Users/jamesb/projects/go-unifi` has been sitting checked out on
`provider-prereqs`. The branch has no `schemas/`, no capture lock, no
`internal/campaign/` and no `internal/controllertest/`, so a survey run against
that checkout concludes the control-plane work does not exist. This has already
caused one wrong-tree assessment. Work from `forgejo/main`. The branch is
published at `origin/provider-prereqs`, so the local ref can be deleted without
losing anything.

## M0 sequencing and non-effects

M0 lands as five checkpoints:

1. This provider documentation and divergence audit.
2. The v1.102.0-based `go-unifi` capture lock and hermetic rebuild.
3. Provider baseline and schema tooling.
4. Digest-pinned targets and DNS-record qualification.
5. A cross-repository evidence checkpoint that confirms every hash and receipt.

This first checkpoint changes documentation only. It does not change generated
models, provider schemas, provider or SDK dependencies, runtime code, resource
registration, HCL, Terraform state, import grammar, or API routing. Initial
future contract consumption is operator-pinned by local checksum. Signed
publication and signing-key distribution, rotation, and revocation are later
release work.
