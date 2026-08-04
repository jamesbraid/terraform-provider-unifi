# UniFi Terraform control plane architecture

Date: 2026-08-03

Status: accepted architecture. This is the authoritative technical design for
the product brief. Research and test history live in the compatibility annex.

## Architectural outcome

The system has four planes. API knowledge establishes what a controller profile
appears to expose. The provider compiler combines admitted controller facts with
explicit Terraform policy to make a buildable provider-change candidate.
Compatibility campaigns test that candidate and all existing promises across
the required controller, device, and hardware profiles. The Terraform product
exposes reviewed operations behind a stable public contract.

```text
locked controller artifact ────┐
                               ├─ API knowledge plane
disposable target ── scout ────┘       |
                                       v
          structural catalog and admitted operations
                                       |
                                       v
             provider compiler + policy overlay
                                       |
                                       v
              resolved Provider Code Specification
                     /                         \
                    v                           v
       pinned Framework generator     lifecycle kernels
                    \                           /
                     v                         v
                  buildable provider candidate
                                       |
                                       v
              compatibility campaign plane
       profile matrix, replay, impact, attestation
                                       |
                                       v
               Terraform product compatibility boundary
                           |                         \
                           v                          v
                normal Terraform use       optional ubitofu
                plan and apply             capture, reconcile, plan review
```

The provider is a complete standalone Terraform provider. `ubitofu` is a
separate downstream consumer of released provider metadata. It never sits in
the provider request path.

## Components and ownership

| Component | Owns | Must not own |
| --- | --- | --- |
| `go-unifi` | Structural regeneration, `scout`, raw client/models, API catalog, domain operations, and evidence | Terraform/HCL policy, final provider specification, or target provisioning |
| Provider spec compiler | Catalog plus policy compilation into a resolved provider specification and impact report | Discovery, policy inference, runtime CRUD, admission, or release |
| Pinned Framework generator | Replaceable generation of Framework schemas and mechanical helpers from the resolved specification | Provider policy, resource registration, CRUD orchestration, imports, state upgrades, or public compatibility |
| Compatibility campaign protocol | Profile selection, scenario replay, claim-specific evidence requirements, impact calculation, and redacted profile attestation | API semantics, provider schema, target implementation, or release promotion |
| Provider | Policy overlay, lifecycle kernels, state upgrades, import behavior, plans, read-modify-write policy, public compatibility, and a `ManagementProfile` beside each resource | API-surface discovery or generic HCL capture |
| `ubitofu` | Read-only discovery, capture, three-way reconciliation, and plan safety receipts | Controller writes, provider runtime, Terraform state mutation, or apply |

`unifi-containers`, `unifi-emu`/herder, and HIL are target substrates. They
supply a named disposable profile to a scenario or `go-unifi scout` invocation.
They do not own source provenance, the observed catalog, generated code, or
Terraform policy. Private firmware execution stays outside this architecture.
It may supply real-firmware evidence to those test systems without becoming a
control-plane component.

## API knowledge pipeline

The `go-unifi` repository contains one API-knowledge pipeline. Its structural
generator and `scout` command are separate commands with a file contract, not
separate products. `scout` has one job: given a named disposable target and
declared observation workflow, emit a sanitized, reproducible observation
bundle. It does not generate Go, decide provider policy, provision a target, or
touch a user controller.

```text
capture lock -> structural extraction -> structural source record
target profile + scout workflow -> observation bundle
structural source record + observation bundle -> observed API catalog
catalog structure -> generated raw models/client
catalog evidence + review -> admitted normalized operations
```

The observed catalog has three non-interchangeable layers:

| Layer | Claim | Admission status |
| --- | --- | --- |
| Structural source | A locked controller artifact contains a field, validator, candidate collection, or metadata item | Candidate only |
| Observed evidence | A named controller profile accepted, returned, persisted, or rejected a declared request/workflow | Profile-scoped evidence only |
| Admitted operation | A stable `go-unifi` operation has passed its Decision 7 vectors | Provider may consider it |

No layer overwrites another. Conflicting static and runtime evidence is retained
as a conflict, scoped to its source/profile, and blocks promotion until reviewed.
The catalog reports coverage as `seen`, `shape_known`, `behavior_probed`,
`admitted`, or `uncovered`. It never treats inventory breadth as provider
support.

## Provider compiler plane

The provider repository owns a deterministic compiler between controller facts
and Framework code. This is the durable reuse boundary. `go-unifi` describes
what the controller exposes and what operations have been admitted. The
provider describes what Terraform means. Neither repository duplicates the
other's authority.

```text
pinned structural catalog + admitted operations
                       +
provider policy overlay + released compatibility baseline
                       |
                       v
              provider spec compiler
                       |
          +------------+-------------+
          v                          v
resolved Provider Code       coverage and impact
Specification               report
          |
          v
pinned tfplugingen-framework + provider-owned helper generation
          |
          v
generated schemas/codecs/fixtures + handwritten lifecycle kernels
```

The policy overlay resolves every structural field as `managed`, `computed`,
`preserve_only`, or `omitted`. Managed fields must also declare type mapping,
presence, sensitivity, defaults, replacement, validation, and patch intent as
applicable. Existing resources additionally name the released schema, state,
and import baseline they preserve. A missing catalog reference, unclassified
field in a selected operation, stale override, or unsupported mapping fails the
compile. No resource or data source is registered merely because an endpoint or
model exists.

The compiler emits the HashiCorp Provider Code Specification as an internal,
versioned build artifact. A pinned `tfplugingen-framework` consumes that artifact
to generate only the Framework schemas and nested helpers it supports. A small
provider-owned generator may emit explicit codecs, mapping coverage, golden
fixtures, and documentation inputs from the same resolved decisions. Generated
output is checked in, reproducible, and compared in CI. The HashiCorp generator
is a replaceable backend, not a public contract or runtime dependency.

Generated code may own Framework attributes and nested schemas, plan/state model
structures where the selected generator supports them safely, explicit
raw/domain/Terraform codecs, validators, mapping coverage, and golden fixtures.
It may not own resource registration, lifecycle orchestration, import grammar,
state upgrades, plan modifiers, defaults, replacement, read-modify-write and
concurrency policy, capability branches, asynchronous convergence, or
controller-specific recovery. Those remain in a small handwritten lifecycle
kernel per resource. There is no reflective or generic CRUD engine.

The existing `go-unifi/specification.json` is a bootstrap artifact for this
transition. It usefully proves deterministic extraction and the HashiCorp code
generation toolchain, but it mechanically guesses Terraform optional/computed
semantics. The provider compiler may ingest it while the structural catalog is
established, provided every guess is overridden or confirmed by provider policy.
It is not the long-term inter-repository contract and is never sufficient to
register or ship a resource.

For an existing resource, the first compiler result is a shadow candidate. CI
builds that exact provider and compares `providers schema -json` against the
released baseline, then runs mapping, import, state-upgrade, and targeted
acceptance evidence. Public documentation is generated from the exact built
provider with `tfplugindocs`, not from controller structure or the intermediate
Provider Code Specification.

The result is a reviewable provider-change candidate. It remains outside the
product contract until its policy and compatibility campaign pass.

## Terraform and OpenTofu baseline

One provider binary serves Terraform and OpenTofu. The provider compiler does
not emit separate product variants. CI pins the code-generation tool versions,
rebuilds checked-in output, builds the provider once, and runs the appropriate
schema and acceptance matrix with both CLIs. Any known CLI divergence is tracked
as a compatibility fact, not hidden in a second generator. The built provider
schema is the runtime witness for both products.

## Compatibility campaign plane

A compatibility campaign is the repeatable control loop for a candidate
controller profile. It is a CI/release protocol implemented by the existing
projects and shared receipts, not a new service or repository. It answers one
question: which existing claims continue to hold for this profile?

```text
candidate controller profile
          |
          v
rebuild static structure and replay declared observations
          |
          v
select claim-specific controller, emulator, or HIL scenarios
          |
          v
compare against admitted operations and released provider contracts
          |
          v
profile attestation: unchanged | additive | divergence | invalid evidence
```

A logical target profile identifies controller and UniFi OS version, form
factor, seeded configuration, relevant device capabilities, upgrade history,
and any required hardware qualification. Profiles distinguish fresh seeded
state, a persisted single-hop upgrade, and a long-lived multi-hop upgrade chain.
A substrate is merely how that profile is supplied. Containers, UOS targets,
emulators, and HIL can satisfy different profiles without changing the campaign,
catalog, or provider contract. A support claim that lacks its required upgrade
history is `uncovered`, not inferred from a fresh target.

Campaign inputs are the locked structural source, observation workflows,
admitted-operation vectors, compiled provider candidate, lifecycle scenarios,
and current release contracts. Its output is a redacted profile
attestation that names coverage, attempts, failures, and every affected admitted
operation. It also records source-to-attestation elapsed time, human semantic
decisions, manual generated-file edits, and automatically reconfirmed claims.
It does not infer coverage from a passing happy path.

Automation discovers candidate profiles, runs the complete required campaign,
classifies the result, and opens a review candidate. It cannot change an
admitted operation, provider schema, support claim, or release by itself.
Humans decide promotion from the attestation and compatibility impact.

## Runtime shape

Provider configuration creates endpoint and transport clients, credential
handles, and a capability resolver. A resource calls a narrow domain operation
rather than generated controller models directly. At the start of each
`Read`, `Create`, `Update`, or `Delete`, it resolves one proven implementation
for that request's known scope and keeps it fixed through retries, polling, and
read-after-write.

Terraform provides no transaction boundary spanning independent resource
lifecycle requests. The provider therefore guarantees binding stability inside
one lifecycle request, not an atomic multi-resource apply. It rechecks the
controller fingerprint before each write. A change fails that lifecycle safely
and asks for refresh or re-plan. It never uses a changed fingerprint to select
a different adapter mid-request. Fingerprint preflight applies only to newly
admitted or explicitly migrated operations. An estate-wide legacy rollout is a
separate compatibility change.

```text
Terraform/OpenTofu
      |
      v
provider resource and lifecycle policy
      |
      v
provider-owned operation interface
      |
      v
go-unifi control facade
      |                       \
      v                        v
private local API       local Integration API
```

The controller's persistent configuration is observed truth. The private local
API is the primary compatibility transport. The Integration API is an internal
adapter, used only for an admitted operation. Cloud Connector is not a route for
new or migrated operations. Its frozen legacy behavior remains a compatibility
exception.

## Public compatibility boundary

Terraform consumers see provider configuration, resources, data sources,
imports, state, diagnostics, and documentation. New and migrated surfaces do
not expose the selected API plane, generated DTOs, endpoint paths, bridge
identities, or transport fallback. Existing resource-native IDs, scopes, import
grammar, and the frozen `cloud_connector` and `hardware_id` attributes remain
public compatibility surface. Those legacy attributes keep their released
behavior but cannot gain new routing semantics.

The existing `go-unifi` v1 public client interface and observable provider
semantics are frozen as the legacy adapter. Evidence-backed fixes for a changed
controller remain permitted when they preserve that contract and pass the
legacy behavior vectors. A narrow provider-facing `control` package introduces normalized
operations without promising a replacement general-purpose SDK. Its compatibility
promise is limited to supported provider releases. A future `/v2` module is a
separate product decision, taken only after repository ownership and external
Go-consumer requirements are clear. A resource migrates one operation at a time
behind provider-owned interfaces. No lifecycle operation mixes adapters or
changes plane after it starts.

Private-plane withdrawal is a first-class compatibility event, not ordinary
discovery drift. A corrective release may use a forced-migration path only when
the operation's private transport is demonstrably unavailable and the proposed
replacement has target-profile behavior vectors, a valid identity bridge,
state/import parity, rollback evidence, and an explicitly bounded supported
profile. The first lifecycle lighthouse rehearses this transition before a
forced migration is needed. No runtime flag or environment override exposes a
transport choice to an operator.

`api_key` retains existing private-plane semantics. `integration_api_key` is
optional and sensitive. It enables admitted local Integration operations but
does not select a backend. Every local operation requires `api_url`. Provider
configuration accepts either viable credential class. An operation requires its
specific class when it executes. An Integration operation rejects
`cloud_connector = true`. A missing Integration credential produces an
actionable diagnostic. It never changes the operation's binding. Both keys are
registered with the provider logger and evidence redactor before use.
An adapter migration either covers every released credential and transport
configuration in its declared scope or leaves the uncovered configuration on
its proven legacy binding.

## Operation contract

Generated controller/OpenAPI fields are structural candidates. They are not
write instructions. Ownership is deliberately singular:

- `go-unifi` owns an observed API catalog. It joins locked structural source and
  profile-scoped `scout` evidence without erasing either. It records provenance,
  structural fields, raw-to-normalized mapping, API presence rules, identity
  evidence, supported transports, conflicts, coverage, and behavior vectors. It
  may mark an API field patch-capable. It never marks a Terraform field managed,
  sensitive, or replace-on-change.
- The provider owns a typed policy overlay and `ManagementProfile` co-located
  with each resource's lifecycle kernel. They declare catalog references,
  exposure and ownership, type mapping, capture policy, reconciliation coverage,
  defaults, replacement, sensitivity, imports, and policy facts the Framework
  schema cannot express. They reference stable catalog operation and field IDs
  without duplicating controller facts.
- The provider compiler composes those authorities into a resolved Provider
  Code Specification, generated Framework plumbing, and a candidate management
  contract. The exact built Framework schema is the runtime witness for HCL
  type, required/optional/computed, defaults, replacement, and `Sensitive`.
  Behavior vectors and lifecycle kernels remain the authority for runtime
  behavior. The build fails when the authored policy and resulting schema
  disagree.

Every catalog operation uses `Model -> Intent -> Patch`: a response describes
what the controller returned. Intent declares API-level caller ownership. A
patch explicitly sets, clears, or omits fields. Operation metadata declares
identity, idempotency, retry budget, asynchronous convergence, postcondition,
typed error classification, and a concurrency strategy. A writable operation
must classify itself as `atomic_patch`, `revision_checked`, `bounded_rmw`, or
`last_writer_wins_legacy`. The first two have controller-enforced conflict
semantics. `bounded_rmw` preserves unowned fields and verifies its
postcondition, while retaining a documented race window. `last_writer_wins_legacy`
is permitted only for grandfathered behavior and explicitly makes no stronger
concurrency claim. New operation promotion requires a class stronger than
`last_writer_wins_legacy` unless a compatibility exception is reviewed.

A policy or profile reference to a missing, removed, or reclassified catalog
operation or field is a build failure. So is a selected catalog field without a
provider disposition. A `breaking_suspect` report enumerates every affected
profile before a compatibility decision can be made.

Catalog IDs are assigned semantic identities, not derived from extractor order,
raw endpoint paths, or mutable display names. An operation or field keeps its ID
when extraction rules or raw vendor names change. Removed IDs are tombstoned and
never reused. Any intentional ID change emits a reviewed migration map with old
ID, new ID, reason, and affected scope. The map is a `breaking_suspect` artifact
that the provider compiler consumes before resolving policy references. An
extraction-rules bump without either stable IDs or a complete migration map is
`capture_invalid`.

Existing Ubiquiti sensitivity metadata is an API privacy and redaction input.
The catalog records it as a `secret_candidate`. It cannot directly generate a
Terraform `Sensitive` flag. The built provider schema must explicitly mark a
managed field sensitive where appropriate. The profile records a reviewed
sensitive, non-sensitive, or omitted capture disposition. CI rejects any other
disposition. Removing `Sensitive` from a released managed field is a
`breaking_suspect` compatibility event.

An `ExecutionBinding` resolves an operation, known scope, controller
fingerprint, credential class, and cached capability evidence at the beginning
of a lifecycle request. It remains immutable for that request only. A
read-only preflight may run during refresh or apply, but never changes schema or
plan semantics. Writes do not fall back between planes. Read-after-write uses
the same binding.
A cross-plane read needs an explicit one-to-one controller-supplied or
provider-witnessed identity bridge. Name, MAC, or structure matching does not
qualify.

## Capability admission

Capabilities progress through evidence levels:

`advertised` -> `authenticated` -> `authorized` -> `state_enabled` ->
`behavior_verified` -> `hil_verified`.

An unknown or expired capability fails closed for a newly admitted Integration
operation or a capability-gated expansion. A released private-plane operation
continues within its explicitly claimed compatibility envelope and reports an
unknown controller profile as a warning, rather than silently selecting a new
adapter. Reads and writes retain their established legacy behavior. A
confirmed behavior mismatch still fails the lifecycle request. This warning is
not a support claim and is captured in the scenario receipt when evidence is
collected.
Capability cache keys include controller identity/version, site, topology,
credential class, and relevant state revision. A site-wide ZBF migration
invalidates that site's capability cache.

An API field or endpoint becomes provider-managed only when its catalog entry,
operation vectors, resource `ManagementProfile`, and release evidence agree.

Evidence is proportional to risk. A read-only data source needs structural,
identity, and read-equivalence evidence. A compact private managed operation
adds patch and lifecycle vectors. Device, topology, upgrade, and hardware claims
add emulator or HIL evidence. The capability ladder expresses evidence quality,
not a mandatory six-step tax for every contributor change. Each tier has a
maintainer-facing paved path with pinned fixtures and one command to reproduce
its required receipt.

## Build, provider, and reconciliation contracts

The compiler produces internal build artifacts before the provider is built:

| Artifact | Producer | Consumer | Purpose |
| --- | --- | --- | --- |
| Structural/observed API catalog | `go-unifi` | Provider compiler and maintainers | Controller facts, provenance, behavior coverage, and admitted operation references |
| Provider policy overlay | Provider maintainers | Provider compiler | Explicit Terraform disposition and compatibility policy |
| Resolved Provider Code Specification | Provider compiler | Pinned Framework generator and CI | Internal, replaceable Framework schema input |
| Compiler impact report | Provider compiler | Maintainers and compatibility campaign | Coverage, unresolved decisions, generated diff, and affected released promises |

These artifacts are not loaded by the provider at runtime and are not consumed
by `ubitofu`. The Provider Code Specification is deliberately internal so a
change in HashiCorp's technical-preview format or generator can be contained
inside the provider repository.

The provider publishes immutable canonical JSON artifacts with each release:

| Artifact | Producer | Consumer | Purpose |
| --- | --- | --- | --- |
| Provider-management contract | provider | `ubitofu` | Provider-owned import, projection, Terraform ownership, lifecycle, coverage, and reconciliation rules |
| Provider schema snapshot | Exact built provider | Provider CI and `ubitofu` | Static Terraform schema compatibility check |
| Contract set | Release pipeline | `ubitofu` and release tooling | Binds artifact digests to provider source, version, platform, binary, and scenario corpus |

`go-unifi` owns the JSON schema and compatibility fixtures for the observed API
catalog and scenario receipt. The provider owns the JSON schema and fixtures
for the management contract and contract set. `ubitofu` implements consumers of
those formats and does not fork their definitions. The projects share JSON
fixtures and redacted receipts, not a cross-language test executor. Each keeps
its native test harness and reports the common receipt format.

The Framework schema and resource code remain the provider runtime authority.
The management contract derives every fact available from the built schema and
records profile-only policy plus behavior evidence. The
provider does not load a contract set at runtime. `ubitofu` receives an
explicit contract-set location and verifies it against the selected binary and
lock file before using it. The set records the emitting Terraform/OpenTofu CLI
product, exact version, and schema JSON format. Verification uses that pinned
toolchain.

The contract set is a release sidecar. It binds the provider source address,
version, platform, binary checksum, schema digest, management-contract digest,
observed catalog digest, and scenario-corpus digest. Initial consumption accepts
only an operator-provided local sidecar path plus expected checksum, so the
operator supplies the trust decision. Publishing it in a signed release
checksum manifest is later release work and cannot become authoritative until
signing-key trust, rotation, and revocation have a separate design. A digest
alone is not a publisher identity.

`ubitofu` migrates from its handwritten manifest in shadow mode. Per resource,
it compares the legacy and contract paths over identical sanitized inputs. The
legacy path remains authoritative until the exact differential gate in Decision
7 passes for that resource. Contract adoption then retires that resource's
legacy path. Its
capture is default-deny: it may emit only fields explicitly marked capturable
by the provider management contract. Unprofiled fields and unresolved secret
candidates remain redacted. An unsigned local sidecar is read/plan-only. It
cannot authorize `auto_merge`. Automatic HCL edits wait for a separately
designed trusted sidecar verification path.

## Identity and Terraform state

Identity is a versioned tuple: scope, native stable keys, plane identifiers,
equality relation, Terraform import grammar, and migration edges. An import ID
is only a serialization of that identity.

Provider-private state may retain opaque adapter affinity and compatibility
generation when needed to keep an existing object on its proven implementation.
It is neither a schema attribute nor import grammar, is not configured in HCL,
and is never interpreted by `ubitofu` as user intent. A state upgrade must
preserve or safely initialize it before an adapter migration can ship.

A valid cross-plane bridge is either controller-supplied one-to-one evidence or
provider-witnessed dual identity recorded while the controller acknowledges the
same create or migration transaction. A later name, MAC, or structural match is
never a bridge. Witnessed identity permits a state-preserving migration. It does
not permit a second object to be created merely to manufacture a bridge.

No resource may move to a new API implementation merely because it can list or
read an object. It must preserve state and import behavior through create,
update, refresh, delete, restart, upgrade, and bridge failure vectors. Existing
resources retain their legacy adapter until those vectors pass.

ZBF migration is scenario setup only. Provider code, `ubitofu`, and generated
HCL never invoke it. Zones may gain a hidden adapter only after their identity
and lifecycle vectors pass. Policies remain private until a controller-supplied
policy bridge and lossless semantic mapping exist.

## Automated change intake

Controller drift must be cheap to discover and impossible to promote by
accident. Scheduled discovery may query mutable vendor sources, but it only
creates a review candidate. Its proposed immutable structural-source lock is
`schemas/capture.lock.json`. The lock records:

- Lock format version, controller product/build identity, Network version,
  applicable UOS version, and capture timestamp.
- Original source location, media type, byte size, and SHA-256.
- An optional restricted content-store locator.
- Extraction-rule and generator-input digests.
- Extracted structural and sensitivity snapshot digests.

Proprietary controller bytes remain outside Git in a restricted,
content-addressed store. Public artifacts contain only the lock, permitted
generated interoperability output, digests, and provenance. The existing
`schemas/VERSION`, `schemas/SOURCE`, and `schemas/ARTIFACT` files become
generated compatibility projections of this lock and cannot be edited as
independent inputs.

Capture canonicalization is a versioned contract. It declares allowed
volatile-field removal, ordering, redaction, and normalization rules. Fixtures
with equivalent captures that differ only in those permitted forms must
canonicalize identically. All other differences remain evidence for review.

Acquisition and generation are separate entry points. Capture discovers or
accepts a controller artifact, verifies it, stores it by digest, and proposes a
new lock. Ordinary `cmd/fields` generation is a pure lock consumer with no
latest-version behavior and no authority to update the lock. `go generate
./...` rebuilds only from the exact retained bytes with networking disabled.
Missing, mismatched, or unavailable retained content fails closed. Generation
never substitutes a newer controller artifact.

During migration, `go-unifi/specification.json` may be regenerated from the
same lock and used as a compiler bootstrap input. Its mechanically derived
Terraform annotations have no authority. The durable output of the knowledge
plane is the catalog and admitted operations. The durable provider input is the
compiler's explicit policy overlay. Once the structural catalog covers the
same facts, the bootstrap specification is retired rather than promoted into a
cross-repository contract.

`scout` runs only declared workflows against a named disposable profile. Its
receipt records target fingerprint, workflow digest, request/response shapes,
normalization, redaction result, and cleanup result. It does not crawl or infer
semantics from arbitrary traffic. Private extraction, published OpenAPI, and
`scout` observation are separate origins in the catalog.

The canonical diff classifier has five outcomes:

| Outcome | Meaning | Automation result |
| --- | --- | --- |
| `unchanged` | Identical canonical structure | No candidate change |
| `additive_candidate` | New structural information | Review candidate only |
| `breaking_suspect` | Removed, renamed, type, cardinality, validator, default, or presence change | Block promotion pending compatibility decision |
| `capture_invalid` | Unknown, malformed, or unverifiable input | Publish no generated output |
| `generator_defect` | Nondeterministic generated output from the same lock | Fail the generator |

Discovery can open a candidate pull request. It cannot auto-merge, tag, publish
a release, alter a provider schema, or promote an API operation. A tag is a
release input, never an effect of a newly observed controller version.

## Evidence and release policy

Scenarios are portable, pinned, and exclusive. Each declares its controller
profile, input artifacts, lifecycle, assertions, cleanup, runtime profile, and
`required_when` policy. Every result emits a redacted receipt that binds image,
firmware, controller, runner, contract, and scenario digests.

Every receipt ends in exactly one verdict: `passed`, `product_regression`,
`controller_divergence`, `scenario_precondition_unsatisfied`, `fixture_invalid`,
`runtime_incompatible`, `infrastructure_unavailable`, `cleanup_failed`,
`evidence_unsafe`, or expiring `quarantined_inconclusive`. Only `passed` promotes
a capability. `not_run` is a local execution status and never evidence.

Only redacted artifacts may leave a runner. Redaction or secret-scan failure is
`evidence_unsafe` and cannot promote support. Linux CI uses a required reaper.
Other runtimes may use explicit verified cleanup only when all owned resources
are labelled, inventoried, removed in order, and inspected for leaks.

Provider release verification rebuilds with a pinned builder, installs the
actual distributable, and compares its schema with released metadata. A
separate compatibility job runs `ubitofu` against that exact binary. Required
evidence is operation/profile-specific: a pure controller operation does not
wait for HIL, while adoption, device lifecycle, topology, and hardware upgrade
claims do. A controller-only operation uses a `scout` receipt from the declared
target profile. This validates the consumer pair without making `ubitofu` a
provider dependency.

Hardware in the loop promotes release support and upgrade confidence. It does
not replace container/emulator feedback or turn observation into automatic
schema management.

## Invariants

- No new or migrated surface exposes an API plane, DTO, or endpoint path in a
  Terraform attribute, HCL, import grammar, documentation, or emitted state.
  Frozen `cloud_connector` and `hardware_id` behavior is the only legacy
  exception.
- No generated field becomes a provider attribute or writable patch by default.
- No resource is registered because it appears in a generated specification.
- No unresolved structural field or stale provider override produces generated
  provider code.
- No write silently retries across planes.
- No unknown identity bridge becomes a name-based match.
- No raw secret-bearing evidence is published.
- No `ubitofu` workflow gains controller-write or apply authority.
- No controller change silently changes a released provider contract.
- No static artifact, UI observation, or generated model alone becomes an
  admitted operation or Terraform behavior.
- No provider-wide atomicity is claimed across Terraform's independent resource
  lifecycle requests. A fingerprint change before a write fails that lifecycle
  safely and requires a refresh or re-plan. Retries and read-after-write stay
  within its immutable binding.
