# UniFi control plane transition roadmap

Date: 2026-08-03

Status: accepted. This document orders the move from the current repositories
to the control-plane architecture. It does not prescribe implementation tasks.

## Aim

A new controller profile or material API change should produce reproducible
evidence and a reviewable provider-change candidate. A maintainer decides
whether its semantics, lifecycle, compatibility, and support claim are
acceptable. The provider remains independently useful without `ubitofu`.

The shortest safe route is to establish trustworthy structural inputs, prove
one complete vertical slice, then expand by capability class. A provider-wide
rewrite, full endpoint inventory, and hardware matrix would delay that proof
without making its first result more trustworthy.

## Starting position

The work is not starting from zero.

- `go-unifi` v1.102.0 at commit `e255518385e0104eb838be56c2a491de158f3194`
  is the reconciled candidate line. It already contains the schema-generation,
  provenance, generated-API checking, controller-test, and current-behavior
  lineage. The eight-commit `provider-prereqs` branch is an obsolete side line
  to audit, not merge or cherry-pick. Its commit-by-commit disposition is in the
  [Milestone 0 baseline audit](2026-08-03-m0-baseline-audit.md).
  `specification.json` remains a bootstrap/golden artifact, but its inferred
  Terraform semantics have no policy authority. The next structural lock is
  `schemas/capture.lock.json`. No earlier JSON structural lock exists to preserve.
  `github.com/jamesbraid/go-unifi` is the canonical artifact origin during the
  transition. The community module path and provider `replace` remain recorded
  compatibility inputs.
- Provider v0.101.2 at commit
  `4e677062f23f232be9fda3e559279937a0f3d007` is the released external
  compatibility authority. Current development commit `26bcad84` differs only
  by a CI change that installs Terraform before documentation generation. The
  provider still consumes `go-unifi` v1.101.0 through its recorded `replace`.
  M0 does not update it to v1.102.0. The provider does not consume
  `specification.json` today. Compiler work first emits shadow candidates and
  comparisons and cannot silently register or reroute an existing resource.
- `ubitofu` already has manifest-centric capture, reconciliation, HCL surgery,
  coverage fixtures, and differential testing. Keep it downstream and optional.
  It consumes provider contracts in shadow mode before any authority moves from
  its legacy manifest.
- `unifi-containers` provides pinned, disposable controller/UOS targets,
  readiness, updates, and seeded test support. Use it as the first target
  substrate, not as the owner of discovery, catalog, or provider policy.
- `unifi-emu`, herder, and future HIL address device lifecycle and hardware
  evidence, not the first controller-only proof. Introduce them only when a
  support claim requires their evidence. Private firmware execution remains
  outside the core control-plane ownership boundary.

## Transition principles

1. Preserve the provider's public HCL, state, identity, and import grammars.
   New internal evidence or transport mechanisms never become a consumer
   migration by accident.
2. Make locked structural regeneration the first shared truth mechanism.
   Dynamic controller evidence complements it. It does not replace it.
3. Prove the controller-facts-to-provider-code compiler with one compact,
   already-supported resource before generalizing schemas, workflows, or code
   generation across the estate.
4. Run the old and new paths side by side wherever an existing behavior is
   being replaced. Promotion follows parity evidence, not confidence in a
   generator.
5. Scale support claims by capability and target profile, rather than treating
   "UniFi" as one uniform API surface.
6. Keep every new boundary file-based and independently releasable. There is
   no new long-running discovery service or required `ubitofu` dependency.

## Roadmap

### 0. Stabilize the truth base

#### Objective

Make the structural picture of a known controller release rebuildable,
reviewable, and attributable.

Start from v1.102.0/current canonical `go-unifi` main. Introduce
`schemas/capture.lock.json` as the sole immutable structural input and separate
networked artifact capture from ordinary generation. Confirm that the existing
generated models, client surface, `specification.json`, compatibility markers,
and normalized provenance rebuild twice, byte for byte, in clean
network-disabled Linux/amd64 environments.

This is a rebuildability step, not a branch convergence or `go-unifi` rewrite.
Nothing is copied from `provider-prereqs`. Existing handwritten behavior
remains usable. A future rebuild no longer depends on an unrecorded download or
whatever firmware happened to be current.

#### Gate

Two clean, locked, network-disabled reconstructions produce byte-identical
structural outputs. Existing compatibility checks explain any generated
surface difference. Unexplained API drift blocks the milestone. Every artifact
identifies the canonical repository, immutable commit, capture-lock digest, and
generator or workflow digest.

#### Not yet

Do not promise a full endpoint inventory, regenerate provider schemas, add a
second source lock, or change any released provider resource.

### 1. Prove the provider compiler in shadow

#### Objective

Prove that the existing generated structural facts can replace provider schema
and mapping boilerplate without changing the released Terraform contract.

Add the provider-owned policy overlay and deterministic spec compiler. For one
compact, already-supported resource, use the current `specification.json` as a
bootstrap structural input, resolve every field explicitly, and emit a
HashiCorp Provider Code Specification plus an impact report. Run a pinned
`tfplugingen-framework` for schemas and supported helpers. Keep the existing
resource lifecycle kernel, registration, imports, state handling, and CRUD.

The preferred proof vehicle is `unifi_dns_record`. Qualification covers its
custom duration type, defaults, timeouts, identity schema, list-resource support,
and state upgrader. A construct unsupported by `tfplugingen-framework` remains
an explicit provider-owned generated or handwritten seam. The resource is
disqualified only if exact schema equivalence would require a public behavior or
lifecycle-kernel change. Check generated output into the provider and make CI
regenerate and compare it. Generate public documentation from the exact built
provider, never the intermediate specification.

#### Gate

The same pinned inputs produce byte-identical generated output. The compiled
candidate has schema equivalence with the released resource, explicit mapping
coverage, and no change to HCL, imports, state, plans, or runtime behavior. A
missing field disposition or stale catalog reference fails compilation.
Unchanged inputs require no manual generated-file edits and automatically
reconfirm every declared lighthouse claim. Record elapsed time and every human
semantic decision as the first autonomy baseline.

#### Not yet

Do not generate CRUD, register discovered resources, migrate the lifecycle
kernel, or make the HashiCorp format a public contract.

### 2. Establish the observation-to-catalog seam

#### Objective

Turn limited, repeatable controller interactions into durable evidence that can
be joined to the structural baseline and compiler input.

Add the internal `go-unifi scout` capability beside the existing generator. A
named disposable target profile plus a declared read-only or disposable
workflow produces a sanitized, canonical observation bundle. The observed
catalog retains structural records, observations, conflicts, and admission
state as different facts. Establish that catalog, rather than the bootstrap
`specification.json`, as the durable controller-facts input to the provider
compiler.

Start with one pinned `unifi-containers` target. This proves the seam using
known, disposable state rather than trying to observe the entire UI or a user
controller. The product result is a repeatable receipt, not a large crawl.

#### Gate

Rerunning the same declared workflow against the same seeded target has the
same canonical result after permitted redaction and normalization. Invalid or
incomplete evidence fails closed. The lighthouse compiler can replace its
bootstrap input with the catalog without changing its resolved Provider Code
Specification or built schema. An extraction-rules change preserves catalog IDs
or supplies the reviewed migration map required by the architecture.

#### Not yet

Do not start browser-wide crawling, production-controller inspection, automatic
API admission, generated provider resources, or a separate scout repository.

### 3. Prove one admitted lifecycle lighthouse

#### Objective

Show that admitted API evidence, generated plumbing, and handwritten lifecycle
policy can produce a complete candidate without granting automation authority
it has not earned.

Feed the lighthouse's admitted operation and provider policy through the
compiler, then run its handwritten lifecycle kernel against the declared target
profile. The campaign rebuilds the locked structural source, replays the
observation workflow, selects the relevant provider scenarios, and emits a
profile attestation: unchanged, additive, divergence, or invalid evidence.
It names coverage, attempts, failures, affected operations, and provider impact.

If the lighthouse replaces an existing provider backend path, run legacy and
candidate adapters in parity. Preserve state and import behavior. For an
official local Integration read path, keep credentials, capability discovery,
and missing-capability behavior scoped to the new operation. Do not reroute
existing resources. No write path receives cross-transport fallback.

#### Gate

The same provider schema, state, imports, plans, and controller outcome are
demonstrated for the lighthouse's required lifecycle scenarios, or the
candidate is explicitly rejected. A deliberate catalog or policy change
produces a bounded, intelligible impact report. A campaign result can block a
support claim without blocking unrelated stable provider capabilities.

#### Not yet

Do not start provider-wide generation, broad official-API write adoption,
runtime transport guessing, or a release based on a successful one-off manual
test.

### 4. Bring `ubitofu` into the loop without coupling it to runtime

#### Objective

Let capture and reconciliation benefit from provider knowledge while preserving
both tools' independent operation.

Publish an exact-binary-bound provider contract for the lighthouse and let
`ubitofu` consume it beside its current manifest. Its first role is
differential shadowing: compare enumeration, import identity, sensitive
handling, generated HCL, plan behavior, and coverage. The legacy manifest
remains authoritative until the two paths meet the exact per-resource
differential gate defined in Decision 7, with no unexplained mismatch.

This stage verifies that semi-autonomous provider creation helps the
downstream authoring workflow. It does not turn `ubitofu` into a required
provider component or allow unsigned metadata to merge configuration.

#### Gate

The contract identifies one locked provider binary and both systems give a
diagnosable result for their declared scope. Mismatch is visible and safe.
Ordinary provider commands work with no `ubitofu` installed.

### 5. Scale by evidence-backed capability class

#### Objective

Convert the lighthouse into a repeatable maintenance model, not a one-off
architecture demonstration.

Expand in deliberately different classes: read-only official operations,
compact private managed resources, complex private resources, then device or
topology capabilities. Each new class earns its own catalog admission,
management policy, target profile, lifecycle evidence, and rollback story.
Existing resources migrate only when the new path has demonstrated public
contract and controller parity. Otherwise they remain supported on the legacy
path.

Before this stage begins, `unifi_port_forward` must pass a second schema-only
shadow compile. It exercises nested single and list attributes already present
in the generated specification. The first two admitted profile campaigns must
also produce lead-time and human-decision baselines, and maintainers must set
the next improvement targets.

Compiler-driven candidate construction and campaign execution can now become
the normal intake path for a new controller version. Automation gathers
evidence, compiles the change candidate, classifies impact, and opens the
maintainer's decision. It never expands support or releases by itself.

#### Gate

Adding a controller profile or API delta produces a reproducible candidate and
an attestation with a clear support decision. Every admitted operation has an
explicit coverage state. Unknown is not silently treated as supported.

### 6. Add device and hardware promotion only where necessary

#### Objective

Make strong support claims honestly, without making hardware a universal
delivery blocker.

Use `unifi-emu`/herder for device-lifecycle claims and the HIL environment for
claims that cannot be substantiated by a controller target: adoption, inform,
reboot, upgrade, gateway topology, and hardware-qualified firewall behavior.
The same catalog, scenario receipt, and campaign vocabulary applies. Only the
target substrate and required evidence differ.

#### Gate

HIL is reproducibly resettable, exclusively leased, sanitized, and able to
distinguish product behavior from fixture, cleanup, runtime, and evidence
failure. It promotes only the claims it exercises.

## Work that proceeds in parallel

The roadmap is sequential at its trust boundaries, but not serial across every
repository.

- Provider maintenance continues independently. Ordinary fixes and releases do
  not wait for the architecture work.
- `go-unifi` may add the capture lock and validate reproducibility on v1.102.0
  now. It must not merge or cherry-pick `provider-prereqs`.
- `unifi-containers` may prepare the pinned Network/UOS target, synthetic
  seed, and declared workflow now.
- Observation/catalog contracts may be designed now. Admitting a record waits
  for the locked structural base.
- The provider compiler can establish its policy format, resolved-spec output,
  generated-output ownership, and schema-equivalence fixture now. The structural
  catalog can replace the bootstrap input after its seam is established.
- `ubitofu` can design read-only contract consumption and differential fixtures
  now. Exact provider-contract shape waits for the lighthouse.
- After the lifecycle lighthouse, automated campaign work and `ubitofu` shadow
  integration may proceed in parallel. Authority does not move until both the
  campaign and exact differential gates pass.
- Device/HIL can establish synthetic targets, reset, and lease mechanics now.
  They do not gate provider delivery until a hardware claim is selected.

## Release posture during transition

- Continue to release existing provider functionality under its current
  compatibility commitments.
- Treat new catalog, scout, compiler, generated, and campaign outputs as
  experimental until a specific capability passes its gate.
- Make additive, clearly isolated official Integration reads possible after the
  lighthouse evidence exists. Keep local private transport primary for current
  managed behavior.
- Use corrective releases for a forced vendor transport migration. Never hide
  a failed write behind runtime fallback.
- Retain last-admitted catalogs, receipts, and provider contracts so an invalid
  new controller observation is diagnosable and disposable rather than a
  destructive update.

## Explicit non-goals for the transition

- A full inventory of every controller/UI endpoint before value is delivered.
- A new service or repository for scout, catalogs, or campaign coordination.
- Replacing the provider with `ubitofu`, or requiring `ubitofu` for provider
  use.
- Treating local official Integration API coverage as a complete replacement
  for private controller behavior.
- Making private firmware execution, device emulation, or HIL a prerequisite
  for controller-only resource support.
- Exposing the controller's inconsistent internal surface in HCL or Terraform
  state.

## Arrival criteria

The architecture is operational, rather than merely documented, when a pinned
new controller profile can move through this path:

```text
locked structural source + declared disposable workflow
    -> sanitized catalog evidence
    -> admitted operation + explicit provider policy
    -> resolved Provider Code Specification + impact report
    -> generated Framework plumbing + lifecycle kernel
    -> reviewable provider-change candidate
    -> targeted compatibility attestation
    -> human admission, correction, or rejection
```

At that point, manual work has shifted from rediscovering controller surfaces
and hand-translating boilerplate to reviewing the facts Terraform cannot safely
infer: ownership, defaults, lifecycle, identity, replacement, and support
scope. The unchanged lighthouse requires no manual generated-file edits and
reconfirms all declared claims automatically. The recorded lead time and human
decision count make later improvement measurable. That is the intended
semi-autonomous provider-creation boundary.
