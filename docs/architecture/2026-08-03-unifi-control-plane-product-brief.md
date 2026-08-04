# UniFi Terraform control plane product brief

Date: 2026-08-03

Status: accepted product contract. This is the authoritative statement of what
the provider and its supporting projects are for.

## The problem

UniFi controller configuration is not a stable, single API product. Its private
controller surface changes with firmware and Network releases. The published
local Integration API is useful but incomplete and differs in identity and
write semantics. Controller UI, mobile applications, and Terraform can all
change the controller's persistent configuration.

Terraform users need a provider that makes these implementation details
irrelevant. They need stable resource names, state, imports, and predictable
plans across controller changes. Provider maintainers need a way to learn about
new API surfaces without automatically exposing or managing them. Operators
who adopt existing controllers need safe capture and drift review.

Today, a controller release can force bespoke investigation and a risky deploy
to learn whether an existing operation still works. That does not scale with the
surface area or produce a repeatable support decision. The product must turn a
candidate controller profile into a reproducible provider-change candidate and
compatibility result before a support claim is made.

## Product

The product is a local-first Terraform provider for UniFi controller
configuration, backed by a semi-autonomous provider-creation and compatibility
system. It presents a stable Terraform interface while internally using proven
controller operations. The controller's persistent configuration is the
observed truth. Terraform configuration and state express desired and
last-applied provider truth. The provider preserves their compatibility as the
controller changes.

`go-unifi` supplies controller facts, normalized operations, and compatibility
evidence. It can rebuild its generated structural client from locked controller
artifacts and record profile-scoped API behavior through declared observations.
The provider repository combines those facts with provider-owned policy in a
provider spec compiler. The compiler emits a resolved HashiCorp Provider Code
Specification, impact report, and replaceable generated Framework plumbing.
Handwritten lifecycle kernels retain the Terraform decisions that cannot be
inferred safely. `ubitofu` is an optional, read-only operator tool for capture,
three-way reconciliation, and plan review. It is not required to install or use
the provider.

## Users and jobs

| User | Job | Product promise |
| --- | --- | --- |
| Terraform user | Manage a controller from HCL | New and migrated surfaces hide API planes and generated models, while frozen legacy routing fields retain compatibility only |
| Existing-controller operator | Bring selected live configuration under management safely | Imports and generated HCL are checked against provider semantics before an apply is proposed |
| Provider maintainer | Add or preserve a resource through controller churn | Controller facts and explicit provider policy compile into a tested candidate, leaving only resource semantics for review |
| Release operator | Support a controller version with confidence | Each support claim names its tested profile, coverage, and replayable evidence |

## Product promises

- New resources and migrated operations never add an API-plane selector to
  Terraform configuration or state. Existing `cloud_connector` and
  `hardware_id` attributes remain frozen legacy compatibility surface.
- Existing resources retain their primary compatibility transport until a
  replacement path has proved import, state, identity, lifecycle, and upgrade
  compatibility.
- If Ubiquiti withdraws a load-bearing private operation, that is a forced
  compatibility migration: a corrective provider release may move only the
  affected operation through an evidence-backed, state-preserving transition.
  It is never an unannounced runtime fallback.
- A discovered field is not automatically a Terraform attribute or writable
  API field.
- A controller/API change is a reviewed compatibility event. It does not
  silently change schemas, defaults, patches, or state.
- Every supported operation has a claim-specific controller profile and
  reproducible compatibility evidence. An uncovered or divergent claim is
  reported as such, never silently treated as support.
- A discovered, evidenced API operation can produce a buildable provider-change
  candidate: a resolved provider code specification, generated schema and
  mappings, tests, documentation inputs, and compatibility impact. It is not
  registered, released, or made writable without reviewed provider policy and
  semantic admission.
- The provider works without `ubitofu`. Normal provider use requires only the
  provider binary, controller access, and the credentials needed by the chosen
  resource operations.
- Sensitive controller and device material never enters generated HCL, public
  receipts, or normal test logs.
- A `go-unifi` structural rebuild and an observed controller workflow are
  reproducible from their locked inputs. Neither alone becomes provider behavior.

## Deliberate boundaries

The private local controller API is the primary compatibility transport. The
local published Integration API may power an internally selected operation when
it has evidence of equivalent identity and lifecycle behavior. The selection
is invisible to Terraform users. A write never silently falls back from one
transport to another.

On a newly observed controller profile, released private-plane reads and writes
continue with a compatibility diagnostic unless a confirmed behavior mismatch
is detected. Newly admitted or expanded capabilities fail closed. This gives
existing users predictable continuity without treating an unknown profile as
proven support.

Existing `api_key` behavior remains private-plane compatibility behavior. An
optional sensitive `integration_api_key` permits only admitted local
Integration operations. It is a credential, not a backend selector.

Cloud Connector is outside this product. Existing legacy configuration remains
compatible through the existing `cloud_connector` and `hardware_id` attributes,
but new provider operations and `ubitofu` do not add Connector routing. Those
attributes cannot select the Integration API or gain new semantics.

## Non-goals

- Generate complete provider resources or lifecycle behavior directly from
  controller schemas or OpenAPI.
- Promise management of every object visible in a controller.
- Expose generated DTOs, endpoint paths, transport-specific bridge identities,
  or API-plane choice as Terraform configuration.
- Turn `ubitofu` into an apply engine, a required provider dependency, or a
  replacement for Terraform/OpenTofu.
- Infer safe updates from a successful read, matching object name, or matching
  JSON shape.
- Treat a live controller as an unrestricted integration-test target.

## Maintenance model

Automation discovers candidate profiles, rebuilds API knowledge, compiles a
provider-change candidate, and runs the required compatibility campaigns. The
provider compiler resolves every discovered field through an explicit policy:
managed, computed, preserve-only, or omitted. Unresolved or dangling policy is
a build failure. A pinned Framework generator may emit schemas and mechanical
helpers from that resolved specification. It does not own Terraform semantics.
Automation then replays evidence against the appropriate controller, emulator,
or hardware profiles and reports coverage, divergence, and impact on existing
promises. It never promotes semantics or releases a provider. A human admits
the resource-specific policy: managed fields, defaults, replacement, write
behavior, imports, state evolution, and lifecycle restrictions.

The resolved Provider Code Specification is an internal provider build artifact,
not the inter-repository API contract and not a public compatibility surface.
The current `go-unifi/specification.json` is retained as a migration input while
the structural catalog and provider policy are separated. It cannot decide the
final Terraform schema because it contains mechanical Terraform guesses.

This keeps routine API churn cheap without turning newly observed fields into
accidental provider behavior. It also means support is claimed per controller
profile and operation, rather than as a vague version-wide promise.

## What success looks like

A new controller version produces a compatibility campaign attestation: which
existing claims were replayed, which operations are unchanged, which are newly
observed, which diverged, and which remain uncovered. A provider release remains
usable by ordinary Terraform users even if they never install `ubitofu`. An
operator can use `ubitofu` to review a captured change and apply the exact
Terraform/OpenTofu plan through their normal deployment workflow.

Every campaign records four outcome measures: elapsed time from a locked source
to an attested candidate, human semantic decisions, manual edits to generated
output, and the fraction of existing declared claims reconfirmed without human
intervention. The first compiler lighthouse must regenerate unchanged inputs
with no manual generated-file edits and automatically reconfirm every declared
lighthouse claim. The first two admitted profile campaigns establish the
lead-time and human-decision baselines. Stage 5 cannot begin until maintainers
set improvement targets from those measurements.

The system learns only from locked artifacts and declared disposable-target
campaigns. A user's live controller is never the place where support is
discovered or validated.

The first official-plane feature is additive and read-only. No existing
resource is silently rerouted. ZBF zones and policies remain on their legacy
implementation until their independent admission criteria pass.
