# UniFi control-plane research and compatibility annex

Date: 2026-08-03

Status: supporting evidence. This document records research, experiments,
comparison notes, and proposed admission gates. It is not the product or
architecture source of truth.

Read these documents first:

- [Product brief](2026-08-03-unifi-control-plane-product-brief.md)
- [Architecture](2026-08-03-unifi-control-plane-architecture.md)
- [Architecture decisions](2026-08-03-unifi-control-plane-decisions.md)
- [Implementation program](2026-08-03-unifi-control-plane-implementation-program.md)

The material below preserves the basis for the decisions in those documents.
It may contain historical observations and implementation alternatives that do
not define the current product boundary.

In particular, the shared field ledger, configure-time `PlaneBinding`, and
immediate public `go-unifi/v2` proposal below were rejected during the final
adversarial review. The decisions record replaces them with source-owned
catalog/profile contracts, per-request `ExecutionBinding`, and a narrow
provider-facing `control` package.

The final architecture also replaces the generic construction proposal below
with a provider-owned spec compiler. `go-unifi/specification.json` is a bootstrap
artifact, not the durable inter-repository contract. The authoritative
architecture and transition roadmap define that correction.

A second Claude Fable review returned `ACCEPT_WITH_CHANGES` with no blocking
findings. The authoritative documents now incorporate its confirmed corrections:
frozen legacy Connector attributes, exact admission and `ubitofu` differential
gates, canonical transition-artifact provenance, autonomy measures, catalog-ID
migration, lighthouse qualification, normative evidence verdicts, scoped
`ExecutionBinding` rollout, and upgrade-chain profiles. The review's request to
resolve the public Go module identity before catalog work was narrowed. Catalog
artifacts bind to the canonical source repository, commit, and digests now. A
public module-major or organization migration remains a separate compatibility
decision.

## Research synthesis

Keep the private controller API plane as the Terraform provider's system of
record. Add Ubiquiti's published Network Integration API as a local,
capability-gated implementation behind the same domain services. Cloud
Connector support is optional and excluded from the first provider and
`ubitofu` design. Do not replace the existing SDK or provider with an
Integration-API-only implementation.

Build one versioned control contract across the projects:

```text
firmware and controller evidence
        |
        v
go-unifi structural catalog and admitted operations
        |
        v
provider policy -> provider spec compiler -> generated Framework plumbing
        |
        +--------------------+
        |                    |
        v                    v
Terraform provider       ubitofu capture, reconcile, and plan gate
        |                    |
        +--------- exact saved plan ---------+
                             |
                         external apply
```

`go-unifi` owns API facts and controller behavior. The provider owns Terraform
behavior. `ubitofu` owns reconciliation policy. None should infer the other
two from Go `omitempty`, a raw OpenAPI document, or a controller response.

### Decisions versus admission gates

The following are design decisions, not outstanding research questions. The
remaining unknowns are deliberately bounded admission gates, each with a
specific experiment and a fail-closed outcome.

| Topic | Decision | Remaining admission gate |
| --- | --- | --- |
| API planes | Private is the record plane. Local Integration is a hidden per-operation implementation | An operation needs capability, identity, patch, and lifecycle evidence before it may use Integration |
| Cloud Connector | Excluded from provider v2 routing and `ubitofu`. Retain legacy configuration only as compatibility | Separate product proposal if remote transport is ever wanted |
| Terraform public surface | Preserve resource names, state, imports, and intent. No plane choice in HCL or state | Resource-specific state/import parity before migration |
| SDK public surface | Freeze legacy v1. Put new normalized operations behind a small v2 facade | Per-operation adapter parity against v1 |
| ZBF migration | Disposable scenario setup only, never provider or generated-HCL behavior | Scenario must prove the required migration state after every target update |
| Firewall zones | Private remains authoritative. A hidden adapter is possible | Cross-plane update/delete, restart/upgrade, provider lifecycle, and HIL vectors |
| Firewall policies | Private-only | Full custom lifecycle plus controller-supplied policy identity and semantic bridge |
| Evidence | Containers and emulator are routine evidence. HIL promotes release support | Required scenario results and safe redaction/cleanup receipt |

Use a separate sensitive `integration_api_key` credential for the local
Integration API. It is not a backend selector and is not required for an
all-private provider configuration. An external resolver can be added later as
an alternative credential source without changing the provider's routing or
Terraform resource surface.

## Existing system

| Project | Responsibility | Excludes |
| --- | --- | --- |
| `unifi-ghost` | Reproducible firmware-rootfs and appliance evidence with strict extraction provenance | Provider generation and reconciliation |
| `unifi-containers` | Pinned Network and UniFi OS Server controller targets. `seeded` supports configuration tests. `sim` supports device demonstrations | Device-protocol fidelity or production hosting |
| `unifi-emu` | Inform/adoption, device profiles, firmware upgrade/reboot, and topology against a real controller | A substitute for controller-configuration behavior |
| `go-unifi` | HTTP dialects, models, request encoding, discovery, structural generation, and controller conformance | Terraform lifecycle and HCL merge policy |
| Provider | Framework schemas, state migration, imports, read-modify-write policy, and controller quirks | Discovering arbitrary configuration or editing HCL |
| `ubitofu` | Read-only discovery, HCL generation, three-way reconciliation, coverage, and plan safety decisions | Controller writes, apply, import, state mutation, CI, or Git publication |

The testing projects are complementary. `unifi-emu` has proven adoption and
lifecycle against a real controller. `unifi-containers` supplies quick,
pinned controller targets. `unifi-ghost` keeps firmware-derived evidence
reproducible without committing proprietary artifacts. Hardware in the loop
extends this ladder. It must not be the only routine test environment.

### `ubitofu` is the operational control loop

The architecture-review worktree has the right product boundary. It observes
committed HCL, OpenTofu state, and live controller state. It merges independent
changes, reports same-value conflicts, stays read-only, and binds a safety
decision to an exact saved plan applied outside `ubitofu`. Its staged HCL
validation is transactional and proved on macOS. Linux network enforcement is
still an open production gate.

This is not merely an import tool. It prevents a normal Terraform apply from
overwriting a legitimate controller UI or mobile-app change. It is therefore a
first-class consumer of the provider contract.

It is an optional downstream consumer, not a provider runtime dependency. A
normal Terraform or OpenTofu user installs the provider, configures credentials,
and plans/applies exactly as they do today. The provider neither invokes,
downloads, nor requires `ubitofu`, its contract-set sidecar, a reconciliation
receipt, or a `ubitofu` credential. `ubitofu` improves an operator's capture
and change-control workflow. It never becomes a prerequisite for provider
CRUD, import, refresh, acceptance tests, or support.

### Current duplication

`ubitofu` has a handwritten Python manifest of resource types, endpoints,
discriminators, import-ID rules, and UI-owned lifecycle exceptions. It audits
the manifest against `tofu providers schema -json`, which detects lag but
cannot prevent it. The schema cannot replace the manifest because it does not
contain controller routes, operation semantics, presence rules, lifecycle,
normalization, sensitive HCL representation, or reconciliation keys.

The objective is to remove independent ownership of this knowledge, not remove
it from the system.

## Versioned control contract

Publish immutable JSON artifacts with canonical serialization and SHA-256
digests. Each release pins dependency artifacts by digest. Do not depend on a
sibling checkout or a module-cache version.

### Observed API catalog: `go-unifi`

The observed catalog records controller facts:

- controller family and supported version range.
- artifact provenance, extractor version, capture date, and content digest.
- collections, singleton and item paths, operations, and response envelopes.
- field names, types, validators, sensitivity, and explicit presence rules.
- capability probes with an evidence level.
- object identity and stable nested collection keys.
- behavior vectors for create, update, remove, clear-to-empty, clear-to-zero,
  and controller-default preservation.

The SDK needs a `Model -> Intent -> Patch` boundary. A response model describes
what the controller returned. An intent says what the caller owns. A patch
represents set, clear, or omit. Reflection merging and `omitempty` cannot
express that difference and are not safe write semantics.

Extraction creates structural candidates. A behavior test or a reviewed
exception establishes that the candidate can be safely managed.

### Provider compiler and management contract: Terraform provider

The provider consumes a released catalog/admitted-operation digest plus a
hand-owned Terraform overlay. Its compiler resolves those inputs and a released
compatibility baseline into an internal HashiCorp Provider Code Specification,
generated Framework plumbing, and an impact report. It releases
`provider-management-contract.json` with the provider binary. Each resource
declares:

- provider type and resource version.
- catalog resource and capability predicate.
- import identity grammar and projection from live object.
- Terraform ownership for every attribute: required, optional, computed,
  sensitive, write-only, replacement, and default behavior.
- controller/UI lifecycle and allowed operations.
- API-to-state-to-HCL transforms, normalization, and stable nested keys.
- lossy or unsupported cases, reconciliation units, and conflict rules.
- import and state-upgrade compatibility metadata.

The exact built Framework schema remains runtime authority. CI must prove the
released management contract agrees with schema output from both pinned
Terraform and OpenTofu CLIs for that binary. A digest mismatch is an error.

Hand-owned policy remains essential. Network defaults, WLAN feature gates,
settings read-modify-write behavior, imports, state upgrades, and plan
modifiers cannot be generated from extracted fields.

### Reconciliation policy: `ubitofu`

`ubitofu` consumes the provider-management contract plus the exact schema
snapshot. It stops independently maintaining resource semantics. A temporary
direct controller reader is acceptable during migration, but it must map
records through the released contract.

Every receipt must bind the observed-catalog digest, provider release and
management-contract digest, provider-schema digest, controller capability
digest, and redacted live/HCL/state snapshot digests. `check` additionally
binds the exact saved-plan digest and identity. The external deployment system
must apply that exact plan.

### Concrete repository boundaries

The rearchitecture is additive and staged. It does not begin by moving every
existing model or resource into a new directory.

`go-unifi` gains four internally separate concerns:

- a capture pipeline that turns firmware/controller/OpenAPI inputs into pinned
  structural snapshots with provenance and a reviewed diff.
- generated private and Integration DTO packages that never serve directly as
  write intents.
- a small public domain layer with normalized read models, `Intent`, `Patch`,
  typed operation errors, source diagnostics, and capability resolution.
- test-only controller scenarios and behavior vectors that promote a structural
  field or operation from candidate to safe.

Existing private clients remain the default adapter until a domain operation is
admitted. The Integration generator lives behind the adapter boundary. It does
not replace the existing controller generator, and `go generate` is never
allowed to overwrite hand-owned overlays or behavior vectors.

The provider keeps its Framework resources and public schemas. It adds a policy
overlay and compiler, not reflection over Framework internals. The overlay
supplies only provider-owned facts: attribute ownership, import grammar,
state-upgrade declarations, lifecycle restrictions, capture eligibility, and
HCL/API transforms. The compiler produces an internal resolved Provider Code
Specification, generated schema/helpers, and an impact report. CI compares the
exact built schema with the released baseline and management contract. A
mismatch is a release failure. Resource lifecycle kernels remain the authority
for complex behavior. The contract records their externally meaningful result
rather than attempting to serialize arbitrary Go code.

### Public SDK and module transition

The current `ApiClient`, public private models, methods, and module path form a
v1 compatibility surface. Freeze that surface. Do not turn a Framework
resource directly from its embedded `*ApiClient` into a different client or a
second public model universe. Instead, introduce a narrow `control` facade
whose operation interfaces are owned by the provider adapter. A resource moves
independently from its v1 adapter to one complete facade operation, with no
mixed-plane implementation inside a single lifecycle operation.

The proposed canonical new module is `github.com/jamesbraid/go-unifi/v2`. Its
first public package is the stable facade, not the generated DTOs. The current
`github.com/ubiquiti-community/go-unifi` module remains frozen for the legacy
adapter, so v1 consumers do not face a surprise path or behavior change. The
contract set records the v2 module identity, version, immutable commit, and
artifact digest. The provider must consume ordinary pinned releases. Its
current community-module-to-James-Braid `replace` directive is a baseline
bridge only. It is not a supported dependency shape, contract input, or
consumer installation requirement. Remove it only after the provider has
established parity against the frozen v1 adapter.

`ubitofu` replaces its handwritten manifest gradually with the released
provider-management contract. Its planner, tree-sitter editing, staged
validation, transactional commit, three-way merge, and saved-plan binding stay
in place. A resource may be marked `generic_capture` only when its contract
contains a complete declarative projection. A complex resource is marked
`provider_projection_required` or `unsupported_capture` until a reviewed,
portable projection exists. This is fail-closed and prevents a contract format
from becoming a second, weaker provider implementation.

The three artifacts have distinct compatibility rules:

| Artifact | Producer | Consumer | Breaking change response |
| --- | --- | --- | --- |
| Observed API catalog | `go-unifi` | provider compiler | block provider upgrade or add an adapter/state migration |
| Resolved Provider Code Specification | provider compiler | pinned Framework generator | fail generation and never publish to consumers |
| provider-management contract | provider | `ubitofu` | block reconcile until the pinned compatible contract is present |
| provider schema snapshot | built provider | provider CI and `ubitofu` | block release when it disagrees with management contract |

All use canonical JSON, a format major, a producer version, input provenance,
and SHA-256 digest. Contract compatibility is assessed at resource and field
level: additive observed fields are review items. Changed identity, omission,
default, replacement, sensitive, or lifecycle semantics require an explicit
compatibility decision. A digest is a receipt binding, not an access-control
mechanism.

### Field-decision ledger and operation binding

Generated fields are structural candidates, not policy. `go-unifi` and the
provider share a reviewed, versioned field-decision ledger keyed by canonical
operation and field path. Every entry declares its structural source,
normalization owner, presence mode, sensitivity, provider ownership, write
admission, and behavior-vector IDs. It starts as `candidate` and can be
promoted only to `readable`, `writable`, or `preserve_only` by review plus
evidence. A generated tag, `specification.json`, or an OpenAPI `required` bit
cannot promote a field on its own.

Source selection is a configure-time, immutable `PlaneBinding` for each
resource version and operation. It binds the contract digest, controller and
site identity, credential class, capability-evidence IDs, selected plane, and
expiry. A write never falls back between planes. Its read-after-write uses the
same binding. A cross-plane read is allowed only when an admitted one-to-one
controller-supplied identity bridge exists and records both raw identities in a
redacted receipt. An unauthorized or unavailable selected plane is a
diagnostic, not a reason to silently choose another implementation.

Capabilities have explicit evidence levels:

`advertised`, `authenticated`, `authorized`, `state_enabled`,
`behavior_verified`, and `hil_verified`. Cache keys include controller UUID,
Network version, site, gateway topology, credential class, and relevant state
revision or probe time. A ZBF migration invalidates the site capability set.
Unknown or expired evidence fails closed for a promoted operation. Probes are
read-only and have a documented configure/refresh cadence.

Every write operation also records idempotency, asynchronous-convergence rule,
retryable error categories, deadline, and postcondition. Reads can retry
transport failures within budget. A timed-out non-idempotent create must resolve
through an identity-safe lookup or return `outcome_unknown`. It must never be
blindly replayed. SDK errors carry a typed category, HTTP status, vendor code,
`Retry-After`, plane, operation ID, and a redacted request fingerprint.

### Credentials and contract-set release

Keep the existing provider `api_key` semantics unchanged for private-plane
compatibility. Before any official-backed provider operation is released, add
an optional sensitive `integration_api_key` credential. It is a credential,
not a backend selector. Do not
reinterpret existing API keys, reuse classic cookies, or select the official
plane merely because a key happens to work in a fixture. The owner-seeded UOS
key's private-read parity is useful evidence, but private-write parity must be
proven independently.

Release a checksummed, signed `contract-set.json` sidecar rather than relying
on arbitrary files adjacent to an unpacked provider plugin. It binds the SDK
module/version and commit, API-contract digest, provider source address/version
and platform, provider binary SHA-256, management-contract digest, schema
snapshot digest, scenario-corpus digest, and supported consumer feature range.
Consumers reject unknown required features even when the format major matches.
Retain a contract set for every supported provider release so historical state
does not require a sibling checkout.

Release CI verifies the actual distributable: install the candidate provider as
OpenTofu will and verify its schema against the sidecar. A separate downstream
compatibility job runs one pinned `ubitofu check` receipt against that installed
binary. It proves consumer compatibility but is not a provider runtime
dependency. Repository generation and controller/HIL evidence are inputs to
these gates, not replacements for them.

### `ubitofu` contract consumption

`ubitofu` receives an explicit contract-set location and expected digest. It
verifies the provider source address, version, platform, binary checksum, and
lock-file digest before accepting the sidecar. It rejects multiple matching
provider schemas or an ambiguous plugin identity. It must not discover a
contract by scanning the plugin cache or selecting the first installed provider
that has a familiar resource name.

The management contract has a closed capture-mode enum:

- `declarative_capture` contains a bounded read-only query, envelope, filter,
  identity descriptor, and canonical HCL projection, all backed by vectors.
- `provider_plan_projection` provides safe enumeration/import metadata and
  delegates state-shaped projection to an isolated non-mutating OpenTofu plan.
- `provider_projection_required` reports a precise coverage result.
- `unsupported_capture` is never emitted as generic HCL.

An identity is a versioned tuple: scope, controller-native stable keys,
plane-specific identifiers, equality relation, Terraform import-ID parser and
renderer, and migration edges. An import string is a transport format, not the
identity model. Missing or ambiguous bridges are unsupported capture, never
name, MAC, or structural matching.

The schema-equivalence check compares a defined static projection: provider
identity, resource/data-source type, attribute and block path, cty type and
nesting, required/optional/computed/sensitive/write-only/replacement flags, and
static defaults. Plan modifiers, validators, state upgrades, imports,
read-modify-write behavior, and omission semantics require named contract
declarations plus behavior vectors. A schema match is not a runtime-equivalence
claim.

Coverage has one owner. `go-unifi` declares the version- and
capability-specific observable universe. The provider contract assigns every
item `managed`, `accepted` with reason, `gated` with evidence, or `unmanaged`.
`ubitofu` evaluates and reports those assignments. An absent disposition is an
error or coverage gap, never an implicit skip.

Reconciliation permissions are field-unit specific. A unit declares its read
set, write set, ownership, stable nested keys or order semantics, default and
omission rule, and allowed merge operation. Only an explicit `auto_merge` unit
may edit HCL automatically. ZBF is an atomic topology unit, not separately
reconciled zones, policies, and networks. Secret fields carry an output policy
only: `variable_required`, `omit_and_ignore`, `never_observable`, or
`unsupported_capture`. Vault locations and variable names remain operator
policy. Unknown secret-shaped data produces a redacted attention result.

Shadow migration runs the legacy manifest and contract engine on the same
sanitized controller, HCL, state, schema, and plan snapshots. It compares
targets/import IDs, gap and skip classifications, generated HCL, secret paths,
reconciliation decisions, exit status, and receipt inputs. The manifest remains
authoritative per resource until its differential corpus passes. A rollback
selects that legacy adapter for the same pinned contract set. No resource may
silently combine manifest and contract decisions.

## Official Network Integration API

The Integration API is a useful, versioned and documented application API. Its
current public documentation covers sites, adopted devices, clients, networks,
WiFi broadcasts, firewall zones and policies, ACL rules, DNS policies, traffic
matching lists, and supporting resources. The normal path is local controller
access. The remote Connector is a separate optional transport with its own
100-request-per-minute per-console limit, 25-second upstream timeout, 10 MB
response limit, and UniFi OS firmware requirement. It is not a required API
plane and must never be selected implicitly.

The compatibility baseline for this review is Network 10.4.57, which is the
current controller test target. The checked 10.4.57 Integration schema has the
same relevant Network, WiFi, firewall-zone, and firewall-policy paths and
schemas as 10.5.67. Later versions are forward-drift evidence only. No 10.4
compatibility conclusion may be inferred from an older document set.

It does not replace the current controller API surface. The published surface
does not cover the provider's full settings aggregation and much of WAN, VPN,
port-profile, device configuration, routing, RADIUS, and legacy configuration.
Its object model and identities are not automatically compatible with existing
Terraform state or private-plane models.

### Benefits

- Published operations and schemas reduce extraction risk where it applies.
- It provides a better stability and support story for new resources.
- OpenAPI is a strong input for generated DTOs, validators, negative tests,
  and contract-diff review.
- It independently checks private-plane extraction. A disagreement is drift
  evidence rather than a reason to silently choose one side.

### Costs

- Published endpoints can still lag the UI or change behavior by version.
- An official-only provider would regress coverage and require state/import
  migrations before it could replace the existing provider.
- Optional Connector use changes credentials, failure modes, rate limits, and
  network trust. It must remain a separately selected SDK transport.
- Generic OpenAPI CRUD repeats the presence, defaulting, and read-modify-write
  failures that a Terraform provider must avoid.
- Two planes double test cost unless they project into one normalized domain
  model and provider-management contract.

### An elegant split

The split belongs below the SDK's public domain API, not in every caller. Do
not expose parallel `Internal()` and `Official()` SDKs and make the provider,
`ubitofu`, and users choose between them.

```text
provider and ubitofu
          |
          v
  go-unifi domain service
  Networks, WiFi, Firewall, Devices
          |
          v
 resource operation contract
          |
     +----+----+
     |         |
     v         v
private API  local Integration API
```

Each domain service accepts and returns normalized models plus `Intent` and
`Patch` types. Each operation has one contract-selected implementation. The
service may use the local Integration API only when its capability predicate
and behavior tests say that exact operation is safe. Otherwise it uses the
private implementation. The returned value includes a diagnostic source tag,
but callers do not branch on it.

Generated Integration OpenAPI DTOs, private controller models, and HTTP
transports remain internal adapters. Normalization is hand-owned at their
boundary. The public SDK, provider code, and `ubitofu` see one resource model,
one identity, and one patch vocabulary. A source switch becomes a reviewed
provider-contract change with compatibility tests, not a runtime preference.

Any future Cloud Connector transport belongs below the local Integration
adapter, with separate rate and failure policy. The new domain-routing path and
`ubitofu` reject it until there is a deliberate product need and an end-to-end
safety design.

The current provider already exposes `cloud_connector` and `hardware_id`.
Those public configuration fields are compatibility surface, not permission to
route new domain operations remotely. Preserve their existing behavior during
the rearchitecture, freeze them out of all new Integration and management
contract paths, and make `ubitofu` reject remote-controller operation. Remove
or deprecate them only through a separately reviewed user migration. Never
silently reinterpret a local API-key configuration as Connector traffic.

### Promotion policy

Add the local Integration adapter with separate generated models and source
provenance. Promote a resource only after published CRUD and list semantics,
clear/presence behavior, stable import identity, a state migration or opt-in
new resource, and container plus hardware evidence all exist. Begin with one
data source or small resource. Do not reroute existing resources merely because
a matching endpoint exists.

## Reviewed projects

No reviewed project supersedes this stack.

| Project | Useful reference | Reason it is not a replacement |
| --- | --- | --- |
| `filipowm/go-unifi` | Separate internal and official generated clients, deterministic snapshots, customization, capability gate | Its private plane is deliberately frozen and its generic write behavior does not establish Terraform patch ownership |
| `filipowm/terraform-provider-unifi` | Controller-version matrix, Framework plan checks, feature validation | A generic CRUD/merge base cannot encode each resource's lifecycle policy |
| BadgerOps provider | OpenAPI provenance, generated-client boundary, translation, upstream-change alerting | Official-API scope is narrower. JSON transcoding loses patch ownership |
| Terrifi | HIL and import workflow experience | Provider-local API workarounds belong upstream as SDK conformance cases |
| Resnick, Akerl, older forks | Historical behavior and regression cases | No contract, generation, or reconciliation architecture to adopt |
| `beezly/unifi-apis`, `ubiquiti-community/unifi-api` | Independent schema inventory and extraction history | Advisory evidence only. Not canonical operation or behavior truth |

No reviewed UniFi project has the combination required here: reproducible
private-surface reconstruction, profile-scoped behavior evidence, stable
Terraform lifecycle, and downstream HCL reconciliation. They remain useful
evidence and regression sources, but none supersedes this stack.

### Mature provider-generation precedents

| Reference | Adopt | Do not copy blindly |
| --- | --- | --- |
| [HashiCorp code generation](https://developer.hashicorp.com/terraform/plugin/code-generation/design) | Intermediate specification plus explicit mappings and overrides | Treat opaque UniFi structure as complete OpenAPI |
| [HashiCorp Framework generator](https://developer.hashicorp.com/terraform/plugin/code-generation/framework-generator) | Pinned, replaceable schema and nested-helper backend with checked-in output | Depend on a technical-preview format as a public contract or expect generated CRUD |
| [Magic Modules](https://googlecloudplatform.github.io/magic-modules/) | Declarative product facts plus handwritten overrides, tests, generated change review, and breaking-change checks | Its scale, bespoke DSL, or centralized multi-provider release machinery |
| [PAN-OS codegen](https://github.com/PaloAltoNetworks/pan-os-codegen) | Feed SDK and provider from one normalized API spec while retaining custom code and device tests | Assume UniFi has PAN-OS XML's stability |
| [Cloudflare provider generation](https://blog.cloudflare.com/automatically-generating-cloudflares-terraform-provider/) | Use API specifications for breadth while treating defaults, variable responses, and computed semantics as provider concerns | Infer Terraform ownership from request/response shape |
| [OpenTofu provider compatibility](https://opentofu.org/faq/) | Build one provider binary and test both CLIs | Fork schemas or generation solely for OpenTofu |

The consistent precedent is hybrid generation: normalize structural facts,
apply an explicit provider-owned policy layer, generate mechanical Framework
plumbing, and retain handwritten lifecycle code. The UniFi-specific addition is
the evidence catalog needed because the vendor surface is unpublished,
incomplete, and profile-dependent.

## Evidence ladder

| Tier | Runs | Coverage |
| --- | --- | --- |
| Contract/unit | Every change | Generator determinism, compatibility, patch vectors, transforms, provider-contract/schema equivalence, and `ubitofu` merge fixtures |
| Container | Pull request and nightly | Classic and UniFi OS behavior. Use `seeded` for configuration and `sim` when device presentation matters |
| Emulator | Pull request and nightly | Inform, adoption, topology, port/radio profiles, upgrade/reboot, and provider acceptance requiring adopted devices |
| Hardware in the loop | Scheduled and release candidate | Real controller/device behavior, firmware regression, and unsupported-edge discovery |

HIL needs immutable inventory, isolated sites, reset/recovery procedures,
receipts, redacted raw captures, and quarantine for destructive or flaky cases.
It should execute the same declarative scenarios used by SDK, provider, and
`ubitofu` tests. Do not create a lab-only shell-script test suite.

### Scenario executor and evidence admission

The shared corpus is `scenario/v1`, not a convention around test names. A
scenario declares target profile, exclusive lease scope, input artifacts,
lifecycle actions, assertions, cleanup order, and supported runtime profiles.
The executor attests the resolved Docker engine/API, host OS and architecture,
UOS runtime contract, reaper mode, image manifest digests, firmware/profile
digests, and runner commit. A mutable image tag is acceptable for developer
convenience but cannot produce promotable evidence.

Redaction is a release gate. Every controller, UOS, emulator, herder, plan, and
diagnostic artifact first passes one central structural redactor and secret
scanner. Only the redacted result may leave the runner or appear in CI logs.
Unredacted inputs are mode 0600, ephemeral, and deleted after redaction. A
redaction or scan failure is `evidence_unsafe`, invalidates the scenario, and
blocks capability promotion. This protects against the known UOS key and
device-management-configuration log paths.

Runtime profile is explicit. Linux CI uses `required_reaper`. Colima may use
`explicit_verified_cleanup` only when the executor labels every owned object,
snapshots the pre-run inventory, deletes child devices then controller then
network, and fails if final inspection finds a labeled survivor. Herder's own
terminal cleanup event is necessary but does not prove controller/network
cleanup. A skipped required scenario is failure. A local unavailable scenario
is `not_run` and cannot be evidence.

Each scenario declares `required_when`. Contract vectors run on every change.
Private seeded-controller vectors are required pull-request checks. An admitted
UOS/UXGENT/ZBF vector becomes required for changes to its adapter or contract
and remains a nightly discovery target otherwise. HIL is scheduled and
release-candidate required. ZBF leases one disposable controller volume/site
per run and rejects every external target before mutation. HIL maps that lease
to one isolated physical site.

Controller compatibility is a persisted-volume `N -> N+1` vector: capture
private and official identities, provider import/refresh, and `ubitofu`
receipt before and after restart/upgrade. Record reported UOS and embedded
Network versions, not merely image tags. Do not automate unsupported in-place
downgrade. Recovery restores a pre-upgrade snapshot into a fresh compatible
target and is reported separately.

Every receipt ends in exactly one verdict:
`passed`, `product_regression`, `controller_divergence`,
`scenario_precondition_unsatisfied`, `fixture_invalid`,
`runtime_incompatible`, `infrastructure_unavailable`, `cleanup_failed`,
`evidence_unsafe`, or expiring `quarantined_inconclusive`. It includes phase,
retry policy, input digests, and owning component. Only `passed` promotes a
capability.

## Current migration evidence

The earlier two-line migration description is obsolete. `go-unifi` v1.102.0
at `e255518385e0104eb838be56c2a491de158f3194` is now the reconciled candidate
line and contains both the schema-generation/provenance lineage and the current
controller-test and behavior work. The provider remains on v1.101.0 during M0.
That dependency is a compatibility input, not the branch to extend.

The eight commits on `provider-prereqs` form an obsolete side line. They are
audited individually in the
[Milestone 0 baseline audit](2026-08-03-m0-baseline-audit.md), but none is
merged or cherry-picked in M0. One patch is already integrated, one feature is
superseded by generated v1.102.0 output, several tests or feature ideas remain
useful but deferred, and direct edits to the old custom/generated layout
conflict with current generated ownership. Structural rebuildability therefore
starts from v1.102.0 and adds the capture-lock subsystem directly.

The provider already demonstrates why a policy overlay is required. Its Network
resource forces a controller policy choice in `ModifyPlan`. WLAN and WAN code
preserves or reasserts controller-version-specific values. Settings update only
their configured sections and require read-modify-write handling. The generic
reflection merge helper treats zero values and empty collections as absent, so
it must not be used for API patches.

`ubitofu` also records a useful hard boundary: a generated import is not proof
that a resource can round-trip. Its seeded-controller scenarios found import
reads that dropped real network values, forcing an unsafe no-op update, and a
null-to-empty-string result that caused Terraform consistency failure. Those
are provider/SDK conformance cases. They must become shared contract vectors
before a resource is marked safe for capture and reconcile.

The current provider module path also replaces the community module with the
James Braid `go-unifi` release. Resolve this provenance and canonical module
identity as part of the v2 contract migration. Do not make tools depend on the
replace directive or the local source layout.

## Live 10.4.57 Integration feasibility spike

The local Integration API is present in the standalone 10.4.57 Network image
at `/integration/v1`. It rejects an unauthenticated request, an invalid
`X-API-Key`, and a valid classic-controller cookie with `403 api.forbidden`.
Classic `POST /api/login` therefore cannot authenticate an Integration API
request. Local access still requires an Integration API key. Cloud Connector
is not involved.

The standalone seeded Network image is suitable for fast private-API tests,
but it does not provide a hermetic Integration key or a supported key-minting
path. Do not work around that by baking a fixed secret into an image, falling
back to a cookie, or mutating the controller database.

The owner-seeded UniFi OS Server image is the appropriate Integration fixture.
It creates a private per-volume key at `/unifi/api-key`. Its health contract
already verifies that key against
`/proxy/network/integration/v1/sites`. Tests read the key without logging it
and send it as `X-API-Key`. The fixture needs the existing UOS systemd runtime
contract and a longer startup budget. This is still local controller traffic.

Use two explicit test flavors:

- `network-seeded`: fast classic/private API behavior, one empty default site,
  cookie authentication, and disposable test data.
- `uos-seeded`: local Integration API behavior, owner-seeded API key, proxied
  Network path, and disposable private volume.

An owner-seeded UOS run with an adopted UXGENT now demonstrates a concrete
candidate identity relation. The private 24-character hexadecimal site,
default-network, and UXGENT documents each expose an `external_id` equal to
the corresponding official Integration UUID. This is evidence for a
provider-private, controller-supplied bridge. It is not a public Terraform
identifier or a general name-matching rule. The older classic simulation still
only establishes that `network.site_id` equals the legacy site ID.

The bridge remains a hypothesis until it survives controller restart,
controller/Network upgrade, import plus refresh, deletion and recreation, and
HIL verification. The next required proof creates or identifies the same zone
through both planes, records legacy and official site/network/zone identities,
then verifies all mappings and a Terraform refresh remain stable. Failure or
ambiguity means no transparent official-backed resource migration.

The initial UOS gateway run did not make firewall zones testable because it
did not invoke the controller's ZBF migration. Before that migration, the
official zone route returns `api.firewall.zone-based-firewall-not-configured`
even with an adopted UXGENT and valid official network UUIDs.

The disposable private-API bootstrap already exists in the merged
`go-unifi` ZBF work. `POST /v2/api/site/{site}/firewall/migrate` runs the
`ZONE_BASED_FIREWALL` site-feature migration. In the measured 10.4.57
controller it returns 204 with an empty body, then creates the default
Internal, External, Gateway, Vpn, Hotspot, and Dmz zones. It is idempotent,
safe to read before or after, and a custom zone can then be created with 201
and updated with 200. On a seeded UOS controller with a herded UXGENT, the
migration also creates ten predefined firewall policies. The no-gateway
fixture produces the zones but no policies.

This replaces the older, accidental one-zone seed: an unmigrated private
zone POST returned `CouldNotFindHotspotFirewallZone` after writing one object,
but was order-sensitive and could not update or delete. Preserve it only as
historical controller behavior, never as a test bootstrap.

The remaining ZBF admission gate is therefore specific and bounded: run the
supported migration in owner-seeded UOS, then prove official local Integration
zone and policy operations, private-to-official identities and references,
provider import/refresh, and controller restart stability. HIL is required
before transparent existing-resource migration, but not to build the disposable
scenario corpus.

The first UOS gateway-target attempt stopped before adoption because it used an
older `unifi-emu` worktree whose generated model registry predates `UXGENT`.
That is a branch-selection problem, not missing device evidence: the newer
emulator worktree already has the generated `UXGENT`/Gateway Enterprise profile
and a live classic adoption test, with corresponding `unifi-ghost` firmware
evidence. Do not synthesize or substitute a different gateway model. Reuse that
profile in the disposable owner-seeded UOS harness and make the UOS UXGENT
adoption plus Integration probe a first-class scenario.

Evidence capture has a separate security requirement: the current emulator can
log raw `mgmt_cfg`, including an adopted-device authentication key, during UOS
adoption. Redact controller credentials, API keys, `mgmt_cfg`, and device
authkeys before writing any evidence, attaching failures, or streaming CI
logs. This must be a harness invariant, not a reviewer convention.

The UOS runtime itself is available under its existing systemd contract. On
this Colima host, Testcontainers needs the explicit Colima Docker socket and
Ryuk disabled because it cannot mount that macOS-host socket into the VM.
The release herder currently rejects that explicit-cleanup mode, so a UOS plus
UXGENT run cannot claim adoption on this host until the test harness has a
reviewed `required reaper` versus `explicit verified cleanup` contract. The
latter must enumerate every container, network and image it creates, remove
them in a deterministic order, and fail the run if final inspection finds a
leak. This is a harness portability gate, not a product behavior change.

## Disposable ZBF scenario contract

ZBF migration is a controller-wide site transformation. It is test setup only:
the provider, `ubitofu`, and generated HCL must never invoke
`/firewall/migrate`. The scenario rejects an externally supplied controller
such as `UNIFI_TEST_URL`. It runs only in a harness-owned disposable site.

`zbf-policy-lifecycle/v1` has two target profiles:

- gateway-less Network 10.4.57 for the synchronous migration and zone
  invariants.
- owner-seeded UOS plus exactly one herded UXGENT for policy and local
  Integration evidence. The fixture may poll rather than assume immediate
  completion because a gateway with SSL inspection can defer migration.

Its private-plane setup is fixed and evidence-backed:

1. migrate the disposable site and require all six default `zone_key` values.
   A 204 is insufficient without the collection read-back.
2. assert that the Internal zone initially includes the discovered LAN ID.
   default zones are references, never Terraform-managed objects.
3. repeat migration and require no duplicate default zones.
4. create two test-only networks and two custom zones with disjoint membership.
   read back their IDs and memberships.
5. create a minimal, live-proven non-predefined policy from one custom zone to
   the other, read it back, update one author-owned field, and read it back.
6. create a second policy in the same zone pair to observe append semantics,
   then delete policies before zones and verify absence.

The generated SDK type is not proof that step five works. The controller has
already shown that migration with UXGENT creates ten *predefined* policies, but
custom policy create, update, delete, and minimum valid payload are still
admission tests. The scenario must explicitly separate these verdicts:

- `zbf-enabled-zone-crud` — migration and custom-zone lifecycle pass.
- `zbf-enabled-policy-lifecycle` — a custom policy also passes full lifecycle.
- `zbf-async-pending` — migration accepted but its observed state misses the
  deadline.
- `zbf-noop-migration` — 204 but default zones are absent.
- `zbf-unsupported` — a prerequisite or migration operation is rejected.

Policy order is not Terraform-owned. The controller appends rules within a
source/destination zone pair and exposes no reorder operation. The scenario
may assert that observed append behavior is stable, but `index` remains
computed and reconciliation must not promise declarative precedence. The
provider-management contract must group a topology as
`zbf_topology:{site}` and a policy unit as
`zbf_policy_pair:{site}:{source_zone}:{destination_zone}`. It captures custom
zones only, excludes default and predefined objects, and reports unsupported
policy forms as coverage gaps.

Each run records controller, UOS, emulator and profile digests, migration and
CRUD status sequences, redacted response shapes, IDs and counts, polling, and
ordered-cleanup results. It never records UOS keys, cookies, or device
management credentials. The container teardown is containment, not evidence of
correct policy deletion.

### Official-plane admission after ZBF migration

The 10.4.57 Integration contract specifies paged zone and policy list/get
operations, zone and policy create/update/delete, and a separate policy-ordering
operation per source/destination zone pair. The official API has no ZBF
migration operation, so every official ZBF scenario begins with the private,
test-only migration above.

This does not make either existing Terraform resource an official-backend
candidate yet. Official zones expose a UUID, name, network UUIDs, and metadata.
The current resource must also preserve the private state/import ID and its
computed `zone_key` and `default_zone`. A zone adapter is admissible only if it
can obtain those compatibility fields from a private read without changing the
resource's external behavior.

The basic zone bridge has now passed on a disposable owner-seeded UOS target
after private migration. Each of the six default private zones had
`external_id == official.id`. The Internal private network ID likewise bridged
through its `external_id` to the official `networkIds` UUID. A custom zone
created privately was visible officially as the same UUID with
`metadata.origin=USER_DEFINED`. A custom zone created officially was visible
privately with a distinct legacy `_id` and matching `external_id`. Official
defaults reported `SYSTEM_DEFINED`, with configurability aligned to the
private `attr_no_edit` behavior. The same local API key could read both the
official and proxied-private zone collections, but private mutation with that
credential remains unproven.

This satisfies only the first zone-adapter admission vector. Remaining zone
gates are cross-plane update and custom-only delete, controller restart and
upgrade stability, provider create/update/import/refresh/destroy with unchanged
legacy IDs, and HIL confirmation. Until those pass, `unifi_firewall_zone`
continues to use the private implementation.

Policies are more constrained. Official `ALLOW` return traffic, protocol and
connection-state filters, typed traffic filters, matching lists, and ordering
do not losslessly match the legacy source/destination representation. Legacy
IP and port groups would need independently proven matching-list bridges. No
controller-supplied private-to-official policy identity has yet been observed.
name or field matching is prohibited. `unifi_firewall_policy` therefore stays
private-only even if a zone bridge is admitted. Official ordering also does not
change the existing computed-only `index`. Any ordering feature would be a new,
additive resource or operation with its own lifecycle design.

The next zone vector proves cross-plane update and custom-only delete, excludes
system zones from management, restarts the controller, and repeats the bridge
and provider import/refresh with the unchanged legacy ID. A separate policy
vector must create a minimal non-predefined policy through each plane and
discover a deterministic, controller-supplied identity before policy migration
can even be considered.

## Historical delivery proposal, superseded

The transition roadmap supersedes the sequencing below. This section is kept
only to preserve the investigation trail. It still refers to the rejected field
ledger, early public-v2 proposal, and pre-compiler construction model.

The delivery order is deliberately dependency-first. No workstream consumes an
unreleased sibling checkout, and no later phase authorizes a public Terraform
change on the strength of an earlier structural artifact alone.

| Phase | Primary owner | Deliverable | Exit gate |
| --- | --- | --- | --- |
| 0. Baseline | all repositories | Immutable controller/firmware/provider/schema/manifest comparison receipt | Clean differential corpus for current private behavior |
| 1. Facts and facade | `go-unifi` | Provenance capture, API contract, field ledger, `control` v2 operations, behavior vectors | v1 adapter parity for one read-only operation. No provider behavior change |
| 2. Evidence platform | `unifi-containers`, `unifi-emu`, `unifi-ghost` | `scenario/v1` executor, pinned targets, redaction, lease and cleanup receipts | One redacted seeded-controller run is promotable on Linux CI |
| 3. Provider shadow | provider | Registry, schema projection, management contract, contract-set release verifier | Exact built binary agrees with contract. Existing resource tests/imports remain unchanged |
| 4. Reconciler shadow | `ubitofu` | Explicit contract resolver and side-by-side manifest evaluator | Corpus parity per resource. Legacy adapter remains rollback path |
| 5. Plane admission | `go-unifi` and provider | Additive adopted-device read/list, then any separately admitted hidden adapter | Operation-specific identity, patch, lifecycle, and credential vectors pass |
| 6. Release support | all repositories and HIL | Upgrade/recovery vectors and isolated HIL receipts | Release candidate passes required container, emulator, and HIL policy |

0. **Freeze baselines.** Pin the provider-used `go-unifi` behavior line, the
   10.4.57 controller/UOS images, UXGENT profile and firmware evidence, current
   provider schema, and current `ubitofu` manifest behavior. Produce a
   comparison receipt before porting anything from the schema-fetcher line.

1. **Build facts without changing provider behavior.** Port extraction,
   provenance, sensitivity review, snapshots, and deterministic regeneration as
   a separate `go-unifi` subsystem. Emit API-contract v1, typed operation
   errors, capability records, and `Model -> Intent -> Patch` vectors. Exit
   only when every current private resource is classified as observed, proven,
   gated, or unsupported.

2. **Add provider policy metadata in shadow mode.** Create the reviewable
   resource registry and emit provider-management-contract v1 from the exact
   built provider. Compare it with `tofu providers schema -json`, current
   imports, state-upgrade tests, and existing resource behavior. It must report
   every divergence before it becomes a release gate.

3. **Bind `ubitofu` in shadow mode.** Load the released provider contract beside
   its existing manifest and require identical enumeration identities, capture
   eligibility, secret handling, coverage classifications, and reconciliation
   decisions for the supported subset. Then make the contract authoritative and
   reduce the manifest to a temporary compatibility adapter. Preserve its
   read-only, transactional boundary throughout.

4. **Promote one shared scenario corpus.** Move the disposable ZBF migration,
   controller seeding, herder adoption, redaction, and behavior vectors into a
   scenario format used by SDK conformance tests, provider acceptance tests,
   and `ubitofu` fixtures. Exit only when a scenario failure produces one
   redacted, digest-bound receipt rather than divergent project-specific logs.

5. **Run official-plane pilots.** Start with an additive read/list capability
   such as adopted-device inventory. It has no existing Terraform state to
   preserve. Do not start with Network, WLAN, zones, or policies. A future
   hidden zone adapter is conditional on the ZBF bridge admission vector.
   policy remains private until it has its own identity and semantic proof.

6. **Introduce HIL as a release gate.** Re-run admitted scenario cases against
   isolated hardware sites with reset/recovery receipts. HIL promotes support
   claims and guards controller upgrades. It does not replace container and
   emulator feedback or authorize new schema fields by observation alone.

No phase is complete because its code exists. It completes only when its
receipt and compatibility gate are published in the pinned contract set. A
failed or inconclusive later phase never rolls back a consumer's state shape.
It keeps the prior admitted provider adapter and marks the new operation gated.

## Guardrails

- API schema changes never automatically become provider schema changes.
- Generated DTOs never become write payloads without presence tests.
- Provider releases require a contract compatibility report.
- `ubitofu` fails closed on an unknown provider contract.
- A plan-gate approval applies only the plan whose digest was checked.
- Hardware observations never silently change generated code or support claims.
- Official API use never introduces cloud transit without explicit selection.

## Sources checked

- [Ubiquiti official API overview](https://help.ui.com/hc/en-us/articles/30076656117655-Getting-Started-with-the-Official-UniFi-API)
- [Network 10.4.57 API documentation](https://developer.ui.com/network/v10.4.57/ai-gettingstarted.md)
- [HashiCorp provider code-generation design](https://developer.hashicorp.com/terraform/plugin/code-generation/design)
- [HashiCorp Framework generator](https://developer.hashicorp.com/terraform/plugin/code-generation/framework-generator)
- [HashiCorp OpenAPI generator](https://developer.hashicorp.com/terraform/plugin/code-generation/openapi-generator)
- [Magic Modules](https://googlecloudplatform.github.io/magic-modules/)
- [PAN-OS codegen](https://github.com/PaloAltoNetworks/pan-os-codegen)
- [Cloudflare provider generation](https://blog.cloudflare.com/automatically-generating-cloudflares-terraform-provider/)
- [OpenTofu provider compatibility](https://opentofu.org/faq/)
- Local worktrees for `go-unifi`, `terraform-provider-unifi`, `ubitofu`,
  `unifi-ghost`, `unifi-emu`, and `unifi-containers`
- `filipowm/go-unifi`, `filipowm/terraform-provider-unifi`, BadgerOps, Terrifi,
  Resnick, Akerl, `beezly/unifi-apis`, and `ubiquiti-community/unifi-api`
