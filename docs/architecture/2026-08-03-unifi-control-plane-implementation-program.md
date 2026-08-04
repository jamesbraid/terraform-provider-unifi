# UniFi Terraform control plane implementation program

Date: 2026-08-03

Status: accepted. This program implements the product brief and architecture
without changing every resource or making `ubitofu` a provider dependency.

The [transition roadmap](2026-08-03-unifi-control-plane-transition-roadmap.md)
governs trust-boundary sequencing. This document supplies the detailed work
breakdown. Milestones 4 and 5 may proceed in parallel after Milestone 3, but no
`ubitofu` authority moves until both gates pass.

## Delivery rules

- A milestone changes one compatibility boundary at a time.
- Controller discovery, provider runtime, and `ubitofu` remain independently
  releasable.
- A later milestone cannot change an existing resource's state shape as a
  shortcut for migration.
- Each milestone has a rollback that preserves ordinary provider use.
- ZBF, device adoption, and HIL are evidence programs. They are not prerequisites
  for a controller-only lighthouse.

## Milestone 0: converge the structural and compatibility baselines

Owners: `go-unifi` and provider. `unifi-containers` supplies the first pinned
target.

Milestone 0 has three independently reviewable gates. They land in order and do
not change provider runtime behavior, dependencies, HCL, state, import grammar,
or routing.

### M0a: architecture and historical baseline

Accept this document set as the design baseline and ground it in the current
repositories. Provider v0.101.2 is the released external compatibility
authority. Commit `26bcad84` differs only by its documentation-generation CI
fix. The provider continues to consume `go-unifi` v1.101.0. `go-unifi`
v1.102.0/current canonical main is the reconciled candidate line and already
contains the schema-generation and controller-testing lineage.

Audit every commit on the obsolete eight-commit `provider-prereqs` side line.
Classify it as already integrated, superseded, conflicting with current
generated ownership, or still useful but deferred. Do not merge, cherry-pick,
or copy code from that branch during M0. The
[baseline audit](2026-08-03-m0-baseline-audit.md) is the evidence record.

Use `registry.terraform.io/ubiquiti-community/unifi` as the canonical provider
address. Treat a legacy `paultyng/unifi` state address as a separate, explicit
`terraform state replace-provider` migration, not an implicit compatibility
promise. Initial future contract consumption uses an operator-pinned local
checksum. Signed publication and signing-key trust, rotation, and revocation
remain later release work.

Gate: all architecture documents are tracked and internally consistent, every
side-line commit has an evidence-backed disposition, and no generated model,
provider schema, dependency, state, or runtime code changes.

### M0b: structurally rebuildable `go-unifi`

Start from `go-unifi` v1.102.0/current canonical main. Introduce
`schemas/capture.lock.json` as the sole structural-source lock. It records:

- Format version and controller product/build identity.
- Original source location, media type, byte size, and SHA-256.
- Optional restricted content-store locator.
- Extraction-rule and generator-input digests.
- Extracted structural and sensitivity snapshot digests.
- Network version, applicable UOS version, and capture timestamp.

Keep proprietary controller bytes outside Git in a restricted,
content-addressed store. Public repository artifacts contain only the lock,
permitted generated interoperability output, digests, and provenance. Missing
or mismatched retained bytes fail closed.

Separate networked acquisition from ordinary generation. A capture entry point
discovers or accepts an artifact, verifies it, stores it by digest, and proposes
a new lock. `cmd/fields` becomes a pure lock consumer. `go generate ./...`
never asks for latest, downloads replacement bytes, or updates the lock.
`schemas/VERSION`, `schemas/SOURCE`, and `schemas/ARTIFACT` become generated
compatibility projections of the JSON lock. `specification.json` remains a
bootstrap/golden artifact without Terraform policy authority.

Build in a digest-pinned Linux/amd64 OCI image with Go 1.26.5, the complete
content-addressed Go dependency cache, extraction utilities and their digests,
and the exact generator toolchain. Run two clean rebuilds with networking
disabled and fresh worktrees/caches. Generated Go, `specification.json`, marker
files, and normalized provenance must be byte-identical.

Required checks are `go test ./...`, `go generate ./...` followed by a clean
worktree check, the existing Network and UOS integration matrix against locked
targets, and intentional missing-artifact, digest-mismatch, corrupt-lock, and
generator-nondeterminism failures. Any unexplained generated API difference
blocks M0 rather than becoming incidental drift.

### M0c: provider and controller compatibility freeze

Build an internal canonical baseline manifest from the published v0.101.2
archive and development commit `26bcad84`. Record:

- Provider source, version, commit, platform, archive SHA-256, and binary
  SHA-256.
- Effective `go-unifi` module path, replacement, version, and commit.
- Go 1.25.8, exact build flags and environment, module digests, documentation
  tool, and observed GoReleaser version.
- Terraform 1.15.8 and OpenTofu 1.12.1 binary checksums.

Install the same provider binary through development overrides and obtain raw
`providers schema -json` output from both CLIs. Archive the raw output for
diagnosis. Canonicalize the complete provider-exposed projection, including
resources, data sources, list resources, functions, actions, and ephemeral
resources where returned. Strip only CLI envelope metadata. Record a canonical
schema digest and per-resource digests, and require the Terraform and OpenTofu
projections to match.

Run two clean, network-disabled Linux/amd64 provider builds and require
byte-identical binaries and unchanged generated documentation. HEAD schema must
match the v0.101.2 released schema. Do not update the provider to `go-unifi`
v1.102.0 in M0. That upgrade is a later candidate requiring provider parity
evidence.

Create target receipts for the UniFi Network 10.4.57 simulation/seeded image
and UniFi OS Server 5.1.21 with Network 10.4.57. Each receipt names the OCI
index digest, selected platform-manifest digest and architecture,
seed/configuration, Compose/runtime, readiness and cleanup digests, and
controller/test-runner fingerprints. Tags remain development conveniences.
Promotion evidence uses Network on amd64 and UOS on its supported arm64 systemd
runtime, always by immutable manifest.

Drive current v0.101.2 `unifi_dns_record` through ordinary CLI fixtures against
fresh locked targets. Cover schema, identity, list-resource, custom duration,
defaults, validators, timeouts, replacement modifiers, create, update, refresh,
import, no-op plan, delete, omitted versus configured optional fields, the v0
integer-TTL to v1 duration state upgrade, persisted-UOS restart and no-op
refresh, cleanup, and evidence redaction. Run an isolated lifecycle with each
CLI and require equivalent state and plan outcomes. Failure rejects the DNS
record lighthouse and returns the milestone for architecture review. It cannot
silently select a weaker resource.

For `unifi_port_forward`, capture only the current canonical schema, nested
object/list shapes, mapping tests, import behavior, and existing acceptance
result. It is a shadow baseline, not a generation, migration, lifecycle, or
admission claim.

Exit gate: the locked `go-unifi` reconstruction succeeds twice without
networking. Provider builds and documentation are deterministic. Both CLIs
expose the accepted canonical schema. DNS record passes its complete
qualification. Every artifact identifies its canonical repository, immutable
commit, toolchain, input lock, and output digest.

Landing order: (1) provider documentation and divergence audit, (2)
`go-unifi` capture lock and hermetic rebuild, (3) provider baseline/schema
tooling, (4) digest-pinned target and DNS qualification, and (5) a
cross-repository evidence checkpoint confirming all hashes and receipts.

M0 excludes the provider compiler, `tfplugingen-framework` adoption, a v2 or
`control` facade, Integration API credentials, operation adapters, `ubitofu`
contracts, ZBF, emulator/herder requirements, HIL, and any merge or cherry-pick
from `provider-prereqs`.

Rollback: remove the experimental baselines and receipts. No released schema,
state, dependency, routing, or API behavior has changed.

## Milestone 1: provider compiler and schema-equivalence lighthouse

Owner: provider.

Define the provider policy overlay and compiler input/output contract. Use the
bootstrap `specification.json` for the qualified existing resource, but require
an explicit `managed`, `computed`, `preserve_only`, or `omitted` disposition for
every selected field. Compile it with the released compatibility baseline into
a resolved HashiCorp Provider Code Specification and impact report. Run a pinned
`tfplugingen-framework`, check generated schema/helpers into the provider, and
retain the existing lifecycle kernel.

Add provider-owned generation only for explicit codecs, mapping coverage,
fixtures, and documentation inputs that the standard generator does not
produce. Public docs continue to come from the exact built provider. Build the
same provider once and test its schema with both Terraform and OpenTofu.
When the pinned Framework generator cannot express a released construct, keep
that construct in an explicit provider-owned generated or handwritten seam. Do
not change public behavior to fit the generator.

Exit gate: regeneration is byte-identical and all catalog and policy references
resolve. The built lighthouse schema matches the released baseline, mapping
coverage is complete, and import, state, plan, and runtime behavior are unchanged.
The compiler rejects an unclassified field and a stale override. Unchanged
inputs require no manual generated-file edits and automatically reconfirm every
declared lighthouse claim. Record elapsed time and human semantic decisions.

Rollback: restore the handwritten schema for the lighthouse. Its lifecycle
kernel and public contract never moved.

## Milestone 2: observation catalog and compiler cutover

Owners: `go-unifi` and `unifi-containers`.

Define the versioned observed-catalog and scenario-receipt formats. Add the
internal `go-unifi scout` command: a named target profile and declared read-only
or disposable workflow produce a sanitized, profile-scoped observation bundle.
It must not provision a target, inspect a user controller, generate provider
code, or promote an operation. Preserve structural-source records, observed
records, conflicts, and coverage states separately.

Make the catalog reproduce the controller facts used by the lighthouse. Replace
the provider compiler's bootstrap `specification.json` input with the pinned
catalog and admitted operation digest. Reclassify generated sensitivity hints as
catalog `secret_candidate` inputs. Only provider policy can make an attribute
Terraform-sensitive or capturable.

Define semantic catalog IDs, tombstones, and the reviewed old-to-new migration
map required for intentional ID changes. An extraction-rules bump without stable
IDs or a complete map fails before publication.

Exit gate: identical declared workflows on the seeded target canonicalize
identically after permitted redaction. Malformed or unsafe evidence fails
closed. Replacing the bootstrap input with the catalog does not change the
resolved Provider Code Specification, generated output, or built schema. Every
catalog ID is stable or covered by a reviewed migration map.

Rollback: retain the bootstrap compiler fixture and last admitted catalog. The
released provider is unchanged.

## Milestone 3: one managed-operation migration

Owners: `go-unifi`, provider, and `unifi-containers`.

Implement a private normalized operation for the qualified resource with
explicit `Model -> Intent -> Patch` presence behavior. Add a provider-local
narrow backend interface and migrate only that resource. The provider compiler
continues to own its generated schema and mappings. The handwritten lifecycle
kernel owns orchestration. Preserve its Terraform name, schema, state, and
import grammar. This milestone does not admit the official transport for writes.

Exit gate: legacy and new adapters agree for create, update, refresh, delete,
import, no-op update, state upgrade, controller restart, and supported target
upgrade. Their Terraform state and controller outcomes match. The provider
contract and installed-binary schema agree. State written by either adapter
also round-trips through the other with an
identical plan before release. If the resource has a valid identity bridge,
rehearse its forced-migration transition on the pinned target.

Rollback: a corrective provider release restores the legacy adapter while
preserving the same state shape. No request falls back after a failed write.

## Milestone 4: automated compatibility campaigns

Owners: `go-unifi`, provider, and `unifi-containers`.

Use `schemas/capture.lock.json` as the immutable input to deterministic extraction.
Separate mutable latest-version discovery from locked rebuilding.
Run declared `scout` workflows against each admitted disposable controller
profile. Select provider scenarios from the claims affected by the candidate
profile. Produce one redacted profile attestation with canonical
structural and observation diffs, claim coverage, affected admitted operations,
provider-contract impact, elapsed time, human decisions, manual generated-file
edits, and automatically reconfirmed claims. Generate candidate changes only.
Pin builder and controller inputs by digest. Record and classify every attempt.
Include fresh seeded, persisted single-hop, and long-lived multi-hop profile
classes. Unsupported upgrade histories remain explicitly uncovered.

Exit gate: a deliberate fixture mutation produces the expected fail-closed
classification. Equivalent captures canonicalize identically. An unchanged
lock and identical observation workflow produce byte-identical output. A new
vendor version cannot alter provider behavior, support claim, or release without
review.

Rollback: retain the last admitted catalog and provider release. Discovery
candidates are disposable.

## Milestone 5: provider and `ubitofu` contract lighthouse

Owners: provider and `ubitofu`.

For the qualified managed resource, emit a management contract from its policy,
lifecycle evidence, and exact built provider schema. Publish an unsigned
development sidecar bound to that binary and catalog digest, its exact
Terraform/OpenTofu schema toolchain, and a local sidecar checksum. Add explicit
contract resolution to `ubitofu` and run it beside the legacy manifest over
sanitized fixtures. `ubitofu` consumes the released management contract and
built schema, never the structural catalog or Provider Code Specification.

Start with `provider_projection_required` unless direct declarative capture
proves exact import/HCL/plan parity. The legacy manifest remains authoritative.

Exit gate: the contract identifies one installed and locked provider binary.
The versioned corpus covers absent/defaulted, configured, imported, live-drifted,
sensitive, and unsupported cases. Legacy and contract paths agree exactly on
enumeration, import identity, capture eligibility, redaction, generated HCL,
plan outcome, coverage, and receipt inputs. Every mismatch is reported.
Normal provider commands work without `ubitofu`. Binary, sidecar, lock, or
schema-toolchain mismatch hard-fails contract mode with both identities named.

Rollback: `ubitofu` selects its legacy manifest adapter. The provider remains
independently usable.

## Milestone 6: expand by capability class

Expand only when a resource meets its own evidence and compatibility gate:

1. Read-only official data sources.
2. Compact private managed resources.
3. Complex private resources through `provider_projection_required`.
4. Device/adoption operations with `unifi-emu` and herder.
5. ZBF zone adapter after remaining lifecycle, restart, upgrade, and HIL gates.
6. Firewall policy only after a controller-supplied identity bridge and lossless
   semantic mapping exist.

Before expanding beyond the first resource, compile `unifi_port_forward` in
schema-only shadow mode and prove exact nested-attribute equivalence. Use the
first two admitted profile campaigns to set the next lead-time and
human-decision targets.

Every resource carries a catalog scope, management profile, scenario profile,
state/import evidence, and rollback adapter. Uncovered objects are explicitly
out of scope with a reason. Universal inventory is not a prerequisite.

## Milestone 7: HIL and support promotion

Owners: `unifi-emu`, `unifi-containers`, and the HIL environment.

HIL promotes only hardware-dependent claims: adoption, inform, device reboot,
firmware/device upgrade, gateway topology, and hardware-qualified ZBF support.
Pure controller CRUD relies on pinned container/UOS evidence unless the release
claims hardware support. Automated reset/recovery, exclusive lease, redaction,
and receipt retention are prerequisites for a HIL promotion.

Exit gate: the claimed capability/profile has passed its required evidence.
Failure taxonomy distinguishes product behavior from fixture, runtime, cleanup,
and evidence failures. HIL has not promoted an unreviewed field or operation.

## Continuous gates

| Change | Required evidence |
| --- | --- |
| Catalog or structural generator | Locked deterministic rebuild, `scout` receipt where behavior changes, canonical diff classification, secret scan, format validation |
| Provider compiler or policy | Pinned regeneration, complete field disposition, impact report, exact-binary schema comparison, mapping coverage |
| Provider resource/profile | Unit/patch vectors, exact-binary schema comparison under Terraform and OpenTofu, targeted controller scenario |
| Adapter or device-profile change | Required emulator/UOS scenario with all attempts retained in the receipt |
| Provider release | Pinned rebuild, signed contract set, installed-binary schema from the pinned CLI, required capability/profile receipts |
| `ubitofu` compatibility | Sidecar verification and exact agreement across the complete per-resource differential corpus |

The provider remains releasable for unchanged capabilities when unrelated HIL
or device evidence is unavailable. It must not claim unsupported profiles.

## Repository allocation

| Repository | First responsibility | Later responsibility |
| --- | --- | --- |
| `go-unifi` | Locked structural generator, bootstrap-spec migration support, internal `scout`, observed catalog, `control` operations, patch vectors, and scenario receipts | Controller-change intake and capability expansion |
| Provider | Policy overlay, provider spec compiler, resolved Provider Code Specification, generated Framework plumbing, lifecycle kernels, and installed-binary schema checks | Resource migrations, emitted management contracts, and signed release sidecar |
| `ubitofu` | Explicit local contract-set resolver and manifest shadow comparison | Per-resource contract adoption after parity |
| `unifi-containers` | Pinned standalone and seeded UOS profiles for lighthouses | Target profiles for admitted controller capabilities |
| `unifi-emu` and herder | No initial dependency | Device/adoption lifecycle evidence |
| HIL | No initial dependency | Hardware-specific capability promotion |
