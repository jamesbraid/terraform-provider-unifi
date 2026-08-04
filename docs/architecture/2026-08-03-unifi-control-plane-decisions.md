# UniFi Terraform control plane decisions

Date: 2026-08-03

Status: accepted decisions. This record resolves the alternatives that would
otherwise create duplicate policy, public API churn, or a big-bang rewrite.

## External architecture review disposition

An independent Claude Fable review on 2026-08-03 challenged the architecture's
ability to survive vendor-forced change. This record adopts its recommendations
for forced transport migration, explicit legacy concurrency classes,
provider-witnessed identity, schema-derived contract facts, measurable contract
adoption exit criteria, proportional evidence tiers, and defined unknown-profile
behavior.

The review's proposed runtime transport override and capture-by-default policy
are deliberately rejected. An override would expose a backend choice and make
reproduction depend on ambient process state. Capture remains default-deny so
new controller fields cannot enter HCL before provider policy exists. The
legacy capture path remains authoritative until a resource passes the exact
per-resource differential corpus in Decision 7.

A second independent Claude Fable review on 2026-08-03 accepted the architecture
with no blocking findings. This revision adopts its corrections for frozen
legacy routing attributes, normative admission and parity gates, artifact
provenance, measurable autonomy, catalog-ID evolution, lighthouse
qualification, evidence verdicts, binding rollout, and upgrade-chain profiles.
It does not adopt a public module rename as a prerequisite for catalog work.

## Decision 1: rebuildable API knowledge before provider semantics

`go-unifi` owns API dialects, locked structural regeneration, observed catalog,
normalized operations, and behavior evidence. Its internal `scout` command
emits sanitized observation bundles from declared workflows against named
disposable profiles. It is not a separate service or provider runtime
dependency.

The provider owns Terraform semantics and consumes admitted `go-unifi`
operations only. `ubitofu` is an optional downstream consumer of provider
metadata. This keeps controller churn below the Terraform compatibility boundary
without turning the provider into a second HTTP client or `ubitofu` into a
provider runtime dependency.

The provider spec compiler consumes a pinned structural catalog, admitted
operations, provider policy overlays, and the released compatibility baseline.
It eliminates repeated model, schema, documentation, and test scaffolding
without creating a generic CRUD engine. It cannot infer managed fields,
defaults, replacement, destructive behavior, or state migration, and it never
registers or releases a candidate automatically.

Compatibility campaigns are the second plane. They are a shared CI/release
protocol, not a new project. A campaign selects the required logical target
profiles, replays structural and behavioral evidence, calculates impact on
admitted operations and provider contracts, and emits a redacted attestation.
Automation may run and classify campaigns. It cannot promote a claim or release
without review.

## Decision 2: one writable owner per fact

There is no shared field ledger.

| Fact | Owner | Consumer |
| --- | --- | --- |
| Locked structural source, profile-scoped observations, API presence, raw-to-normalized mapping, API patch behavior, identity evidence, and coverage | `go-unifi` observed API catalog | Provider compiler |
| Terraform field disposition, schema policy, import/state/default/replacement/sensitivity, and compatibility baseline | Provider policy overlay and lifecycle kernel | Provider compiler and runtime |
| Resolved Provider Code Specification and construction impact | Provider spec compiler | Pinned Framework generator, maintainer, and campaign |
| Terraform runtime schema and lifecycle behavior | Exact built provider and resource code | Released management contract and `ubitofu` |
| Catalog links, capture/reconciliation policy, and behavior coverage | Provider `ManagementProfile` beside each resource | Released management contract and `ubitofu` |
| Terraform static schema | Exact built provider binary | Provider CI and `ubitofu` verification |
| Reconciliation decision over HCL, state, and live controller | `ubitofu` | Operator receipt |

The catalog has structural-source, observed-evidence, and admitted-operation
layers. They retain provenance and conflicts rather than overwriting each other.
The provider policy references admitted catalog IDs. The compiler resolves the
catalog and policy into generated Framework plumbing. The build verifies that
result against the installed-binary schema and behavior vectors before emitting
a release contract. Neither source duplicates another's policy. A dangling,
unclassified, or reclassified reference fails the build, and a breaking catalog
diff names every affected profile.

Ubiquiti sensitivity metadata is a privacy/redaction input in the catalog. The
provider schema alone maps a managed field to Terraform `Sensitive`. The profile
records an explicit sensitive, reviewed non-sensitive, or omitted capture
disposition. Removing `Sensitive` from a released field is a breaking
compatibility event.

## Decision 3: Provider Code Specification is a provider build artifact

The long-term inter-repository contract from `go-unifi` is its structural and
observed API catalog plus admitted operation identities. It is not a HashiCorp
Provider Code Specification. The provider repository owns a deterministic
compiler that combines those controller facts with an explicit policy overlay
and a released compatibility baseline.

The compiler emits a resolved Provider Code Specification and an impact report.
A pinned `tfplugingen-framework` generates the Framework schemas and nested
helpers it supports. Provider-owned generation may add explicit codecs, mapping
coverage, fixtures, and documentation inputs. Generated files are checked in
and regenerated in CI. Handwritten lifecycle kernels own resource registration,
CRUD orchestration, import grammar, state upgrades, defaults, replacement,
read-modify-write, convergence, and controller quirks. There is no reflective
or generated generic CRUD layer.

The current `go-unifi/specification.json` remains a useful bootstrap and golden
artifact while the compiler boundary is established. Its mechanically inferred
Terraform semantics are untrusted until the provider overlay confirms or
overrides them. It is retired as an inter-repository input when the structural
catalog reaches parity. The HashiCorp format and generator are replaceable
implementation details, not public provider or `ubitofu` contracts.

The exact built provider schema remains the runtime witness. Existing resources
enter generation only through shadow schema equivalence and lifecycle evidence.
No discovered model or endpoint registers a Terraform resource automatically.
One provider binary is built and tested with both Terraform and OpenTofu. There
is no separate OpenTofu generator.

## Decision 4: hidden per-operation transport admission

The controller's persistent configuration is observed truth. The private local
API is the primary compatibility transport. The local Integration API is an
internal alternative for a specific admitted operation. Cloud Connector is not
part of new routing.

Transport is never a Terraform choice. An API adapter can change only behind an
unchanged resource name, schema, state, import grammar, and lifecycle contract.
Writes have no cross-transport fallback. Cross-transport reads require a
controller-supplied one-to-one identity bridge or provider-witnessed dual
identity from the same controller-acknowledged transaction.

Private-plane withdrawal is a forced-migration event. A corrective provider
release may transition only the affected operation after target-profile
behavior, identity, state/import, and rollback vectors pass. This is a released
compatibility change, never runtime fallback or an operator-selectable route.

## Decision 5: bind execution, not provider configuration

Provider configuration creates clients, credentials, and a capability resolver.
Each lifecycle request resolves an immutable `ExecutionBinding` for the known
scope, controller fingerprint, operation, credential class, and capability
evidence. The binding lasts through that request's retries, convergence, and
read-after-write only.

This machinery applies first to newly admitted or explicitly migrated
operations. Adding fingerprint preflight to an untouched legacy resource is a
separate compatibility change with its own campaign and release gate.

This preserves deterministic writes while allowing a later refresh to notice a
controller upgrade or changed capability. Terraform cannot provide a
provider-wide transaction over all resources, so a fingerprint change fails the
affected lifecycle safely rather than pretending an apply is atomic. Runtime
preflight cannot modify schema or plan semantics.

## Decision 6: preserve existing public compatibility

Existing Terraform resources, resource-native IDs, scope, import grammar,
state, and `api_key` behavior are compatibility surface. Transport-specific
UUIDs, bridge IDs, endpoint paths, DTOs, and routing are internal.

The existing `cloud_connector` and `hardware_id` provider attributes are frozen
legacy compatibility surface. They may preserve their released behavior but
cannot select the Integration API, acquire new routing semantics, or become a
pattern for new provider configuration. New and migrated operations expose no
API-plane selector.

`integration_api_key` is a separate optional sensitive credential for admitted
local Integration operations. Every local operation needs `api_url`. An
Integration operation requires its key at execution and rejects Connector
routing. A key that happens to work against multiple endpoints does not choose
a transport. A hidden adapter migration must prove every released transport and
credential configuration in its declared scope. Connector-scoped configurations
remain on their legacy binding unless the migration campaign covers them.

The existing public `go-unifi` client remains the legacy adapter. A small
provider-facing `control` package is introduced before any general `/v2` SDK
claim. A module-major change is deferred until there is an explicit external
Go-consumer contract.

Released legacy private-plane operations retain their historical read/write
behavior on a newly observed controller profile with a compatibility warning.
New Integration or capability-expanded operations fail closed. A confirmed
behavior mismatch fails the lifecycle. This warning is an explicit degraded
support posture, not a silent new transport selection.

## Decision 7: automate rebuilding and observation, not promotion

Scheduled discovery proposes an update to `schemas/capture.lock.json`, the
immutable structural-source lock, and creates a candidate change. Capture and
ordinary generation are separate entry points: only capture may propose a new
lock, while `cmd/fields` and `go generate ./...` consume the exact retained
artifact named by the current lock without networking. Normal generation is
deterministic and uses only locked inputs. `go-unifi scout` separately runs declared workflows
against named disposable profiles and emits profile-scoped receipts. The catalog
joins those origins without treating either as universal API truth. Admitted
operations then produce a provider-compiler candidate before the campaign
tests it. Structural diffs classify as `unchanged`, `additive_candidate`,
`breaking_suspect`, `capture_invalid`, or `generator_defect`.

Discovery cannot auto-merge, tag, publish, change provider schema, or promote
an operation. `scout` cannot provision a target, inspect a user controller, or
promote a result. Construction cannot register or release a resource. Promotion
needs behavior vectors, a reviewed provider policy, and a passing compatibility
campaign. This is the minimum automation that lowers maintenance without
converting API drift into accidental Terraform behavior.

Evidence is tiered by risk, not collected ceremonially. The architecture defines
a reproducible paved path for read-only, compact managed, and hardware-dependent
classes. Higher tiers add only the evidence their claim requires.

### Normative admission and adoption gates

Every required vector must pass on every claimed profile. There is no percentage
allowance. A missing vector is `uncovered`. A divergent vector blocks admission
for that operation and profile.

- A read-only operation requires a locked structural source, an authenticated
  and authorized observation, stable native identity, list/get envelope and
  absence behavior, typed denied/missing errors, deterministic normalization,
  and redaction evidence.
- A compact managed operation adds explicit `Model -> Intent -> Patch` vectors
  for set, clear, omit, default preservation, and no-op update. It also requires
  create, read, update, delete, import, controller restart, supported profile
  upgrade, concurrency classification, and verified cleanup.
- A complex or read-modify-write operation adds preservation of every unowned
  field, race and postcondition behavior, state upgrade, and legacy-versus-new
  adapter parity.
- A transport migration adds a controller-supplied or provider-witnessed
  identity bridge, cross-adapter state/import round trips, bridge-failure
  behavior, and every released credential and transport configuration in the
  migration scope.
- A device or hardware claim adds the emulator or HIL lifecycle vectors named by
  that claim. Hardware evidence cannot compensate for a missing controller
  vector.

`ubitofu` authority moves one resource at a time. Its versioned differential
corpus includes absent/defaulted, explicitly configured, imported, live-drifted,
sensitive, and unsupported or ambiguous cases. Contract and legacy paths must
agree exactly on enumeration, import identity, capture eligibility, redaction,
generated HCL, plan outcome, coverage, and receipt inputs. Any unexplained
difference keeps the legacy manifest authoritative.

## Decision 8: release metadata is an exact downstream contract

The provider emits a management contract from co-located resource profiles and
verifies it against the schema from the exact installed binary. A contract-set
sidecar binds the provider archive, source address, platform, schema, management
contract, observed catalog, and scenario corpus.

The initial `ubitofu` integration accepts an operator-pinned local sidecar path
and checksum, matched to an exactly locked provider binary and pinned
Terraform/OpenTofu schema toolchain. It hard-fails on version, checksum, or
toolchain mismatch. The provider itself does not read the sidecar at runtime.
Checksum-pinned sidecars are read/plan-only: they cannot authorize automatic
HCL edits. Signed publication, remote discovery, `auto_merge`, and signing-key
trust wait for a separate key distribution, rotation, and revocation design.

## Alternatives rejected

| Alternative | Rejection rationale |
| --- | --- |
| Provider owns raw controller API behavior forever | Duplicates transport, model, identity, and drift work across resources |
| Generated private/OpenAPI models become provider schema and CRUD | Cannot establish omission, defaults, import/state migration, or destructive lifecycle behavior |
| `go-unifi/specification.json` becomes the durable provider contract | It mixes controller structure with mechanical Terraform guesses and assigns policy to the wrong repository |
| Provider consumes the structural catalog directly at runtime | Couples provider behavior to controller churn and bypasses reviewed schema policy |
| HashiCorp's technical-preview code specification becomes a public contract | Makes an upstream build format part of provider and `ubitofu` compatibility |
| Generate full CRUD and lifecycle from the specification | Imports, state evolution, concurrency, convergence, and controller quirks require resource-specific code and evidence |
| A custom monolithic UniFi generator replaces standard tooling | Needlessly owns Framework schema emission that HashiCorp already provides and increases migration cost |
| One mega-contract drives SDK, provider, and `ubitofu` | Reimplements provider behavior in a brittle DSL and couples independent releases |
| Generic CRUD resource engine | Hides resource-specific read-modify-write, ordering, imports, and server-owned fields in an opaque framework |
| Separate official-API provider | Leaks transport choice and creates competing resource/state/import universes |
| Private API only | Gives up documented operations and proven identity bridges where they reduce maintenance |

## Precedent and standards basis

This design composes established provider patterns instead of inventing a new
Terraform runtime model:

- HashiCorp's [code-generation design](https://developer.hashicorp.com/terraform/plugin/code-generation/design)
  separates API sources, a versioned Provider Code Specification, and SDK
  generators. Its [OpenAPI generator](https://developer.hashicorp.com/terraform/plugin/code-generation/openapi-generator)
  adds operation mappings, overrides, and ignores before producing that
  specification. We place the equivalent policy compiler in the provider
  repository because UniFi's structural source is incomplete and opaque.
- HashiCorp's [Framework generator](https://developer.hashicorp.com/terraform/plugin/code-generation/framework-generator)
  currently generates schemas and nested helpers and is a technical preview.
  We adopt that narrow replaceable backend, not a promise that it generates
  lifecycle behavior.
- The [Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework)
  remains the provider implementation baseline. HashiCorp's guidance for
  [state upgrades](https://developer.hashicorp.com/terraform/plugin/framework/resources/state-upgrade),
  [provider versioning](https://developer.hashicorp.com/terraform/plugin/best-practices/versioning),
  and [acceptance tests](https://developer.hashicorp.com/terraform/plugin/testing/acceptance-tests)
  governs lifecycle kernels and release gates even when schemas are generated.
- Google Cloud's [Magic Modules](https://googlecloudplatform.github.io/magic-modules/)
  and Palo Alto Networks' [PAN-OS codegen](https://github.com/PaloAltoNetworks/pan-os-codegen)
  show the durable pattern: normalized API specifications plus handwritten
  overrides and tests generate SDK/provider plumbing, while review and real
  acceptance evidence control release.
- Cloudflare's account of [generating its Terraform provider](https://blog.cloudflare.com/automatically-generating-cloudflares-terraform-provider/)
  records the same hard edge around defaults, variable response shapes, and
  computed-field semantics. Those are provider policy, not safe OpenAPI
  inferences.
- OpenTofu is compatible with existing Terraform providers, as its
  [FAQ](https://opentofu.org/faq/) and [provider documentation](https://opentofu.org/docs/language/providers/)
  describe. We therefore test one provider with both CLIs rather than fork the
  generation path.

## Release identity

The canonical source address for new provider contract sets is
`registry.terraform.io/ubiquiti-community/unifi`. The current Terraform Registry
publishes [that provider](https://registry.terraform.io/providers/ubiquiti-community/unifi)
from this repository. The README's `paultyng/unifi` links are legacy
documentation and must not become an implicit state migration.
Any support or migration for that legacy source is a separate, explicit
`terraform state replace-provider` workflow with its own compatibility test.

Initial contract consumption uses an operator-pinned local sidecar checksum.
This avoids inventing a signing-key trust model during the architecture rollout.

During the transition, `https://github.com/jamesbraid/go-unifi` is the canonical
source repository for `schemas/capture.lock.json`, observed catalogs, scenario receipts,
and admitted-operation artifacts. Every published artifact identifies that
repository, an immutable commit, the source-lock digest, and the generator or
workflow digest. The current Go module path and provider `replace` directive are
recorded compatibility inputs, not artifact identity. A public module-major or
organization migration remains a separate decision and does not block catalog
construction. No first catalog may be published until these provenance fields
are present and verified.
