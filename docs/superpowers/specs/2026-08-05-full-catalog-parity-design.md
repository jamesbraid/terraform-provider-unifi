# Full-Catalog Provider Parity Design

> Private design record. Exclude this file and its commit from public-export
> branches. Public artifacts derived from this work must pass the separate
> confidentiality gate.

## Status and purpose

The private control-plane lighthouse proves deterministic reconstruction,
provider compilation, DNS-record adapter parity, locked-controller lifecycle
coverage, and exact downstream contract consumption. It does not prove that
the new architecture covers the released provider catalog.

The released schema contains 28 managed resources, 13 data sources, 25 list
resources, and one action. Only `unifi_dns_record` has an admitted normalized
operation and exact downstream contract. `unifi_port_forward` has a
schema-only shadow. Every other surface remains on its released implementation.

Full-catalog parity moves the complete released surface through the same
evidence chain while preserving controller intent and object ownership. Public
HCL, state shape, and import grammar remain compatible when that does not
distort the new architecture. Intentional breaks are collected into one
catalog-wide migration release with deterministic per-surface recipes. Fleet
configuration sets the order and scenario depth. It never narrows the catalog
target.

## Goals

- Account for every released resource, data source, list resource, and action
  in one machine-checked parity ledger.
- Preserve the v0.101.2 contract where it is cheap and structurally sound.
  Prefer a simpler uniform provider model when compatibility would require
  permanent special cases.
- Prove schema, mapping, lifecycle, controller, state, import, documentation,
  and downstream parity at the granularity of one catalog surface.
- Publish every intentional Terraform contract change in one migration
  manifest with a forward recipe, destructive-risk classification, and tested
  recovery path.
- Use sanitized fleet-derived shapes to prioritize work and strengthen
  scenarios without retaining private values, state, or controller output.
- Keep the previous adapter compiled but unreachable for one release so a
  corrective binary remains possible. The migration bundle, not adapter
  retention alone, owns state-shape recovery.
- Block public export until the whole catalog and the exact candidate binary
  pass the release gate.

## Non-goals

- A single bulk generator pass that treats schema similarity as behavioral
  parity.
- Runtime fallback between released and candidate adapters.
- Production-controller mutation as a qualification mechanism.
- New resources, new provider configuration, new transports, or broader
  hardware claims while parity is incomplete.
- Publishing private CI topology, repository identities, fleet values, raw
  state, raw controller responses, or credentials.
- Preserving historical Terraform shape when it adds permanent compiler,
  adapter, or lifecycle exceptions without preserving controller capability.

## Catalog scope

The parity ledger is derived from the canonical v0.101.2 provider projection.
Each released semantic surface must have a candidate owner even when the
migration release consolidates or reshapes its Terraform representation:

| Surface | Count | Current architectural admission |
| --- | ---: | --- |
| Managed resources | 28 | DNS record only |
| Data sources | 13 | None |
| List resources | 25 | DNS list schema only |
| Actions | 1 | None |

The real fleet currently references 17 managed resource types and one data
source type. Those types move earlier and receive scenarios reflecting their
actual configuration shapes. The other catalog entries receive the same
admission requirements before the release gate can pass.

## Admission ledger

Each surface has one ledger entry and advances through these states:

1. `baseline`: released schema, implementation, tests, imports, and state
   versions are identified.
2. `cataloged`: structural facts, observations, target scope, stable semantic
   identifiers, and conflicts are recorded.
3. `policy_complete`: every field has an explicit Terraform disposition and
   every released lifecycle seam has an owner.
4. `generated_shadow`: deterministic generated schema and mappings reproduce
   the released projection or its manifest-declared candidate form without
   registering a new runtime path.
5. `adapter_parity`: released and candidate implementations agree on the
   applicable unit, HTTP-boundary, state, import, and controller scenarios
   after declared migration transforms.
6. `admitted`: a receipt binds the passing result to exact inputs, toolchains,
   provider binary, targets, and scenario corpus.
7. `contract_parity`: the exact-binary management contract agrees with the
   downstream legacy manifest after declared migration transforms.
8. `release_ready`: whole-provider checks and required fleet or hardware
   evidence pass for this surface.

Only `release_ready` satisfies the final catalog gate. `unknown`,
`shadow_only`, `uncovered`, `divergent`, `inconclusive`, `invalid`, and
`legacy_authoritative` remain blocking states.

A catalog entry may use a compatible terminal implementation when generation
or adapter migration provides no behavior or ownership benefit. That decision
still requires complete baseline, schema, state, import, controller, and
downstream evidence. The ledger records the implementation as terminal and
release-ready rather than silently treating an untouched path as migrated.

## Compatibility budget and migration contract

The canonical provider address remains
`registry.terraform.io/ubiquiti-community/unifi`. Existing controller objects,
their stable semantic identities, and their ownership must survive the
migration. An upgrade must not silently plan replacement, abandon an object,
or transfer ownership to a different resource address.

Compatibility decisions follow this order:

1. Preserve the released HCL, resource address, state shape, and import grammar
   when the new architecture can support them without a permanent exception.
2. Use a Terraform state upgrader for lossless internal schema changes.
3. Emit a generated HCL and state-address recipe when a resource or attribute
   needs restructuring.
4. Use an import bridge when the old state cannot be upgraded losslessly but
   the controller object has a stable identity.
5. Reject an unrepresentable legacy case with a hard diagnostic before any
   write. The migration manifest must explain the missing controller semantic
   and the operator choice it requires.

All intentional breaks ship in one catalog-wide migration release. The
provider repository owns a machine-readable per-surface migration manifest and
an operator-facing migration bundle. The bundle reports the required native
Terraform or OpenTofu commands and HCL edits. It does not write local or remote
state directly. Operators take a state snapshot before upgrade. Terraform and
OpenTofu remain the only state writers.

Each manifest entry records the old and new resource type, address and identity
rules, attribute mapping, state versions, upgrader or import transform,
configuration rewrite, destructive risk, forward assertions, and recovery
procedure. An unchanged surface records an explicit identity transform. The
release gate tests the whole manifest against synthetic v0.101.2 workspaces and
sanitized fleet-derived configuration shapes.

This target is semantic parity: the candidate must manage the same controller
intent and capabilities unless a declared break proves that the controller no
longer exposes an equivalent operation. Byte-for-byte Terraform shape is not a
goal.

## Ownership and data flow

`go-unifi` owns locked controller facts. It records structural definitions,
sanitized observations, semantic identifiers, normalized operations, patch
presence, target profiles, and scenario receipts. It does not assign Terraform
semantics or promote support.

The provider owns Terraform policy. Its overlay assigns each field a managed,
computed, preserve-only, omitted, sensitive, defaulted, replacing, or
state-upgraded disposition. The provider compiler resolves catalog facts and
policy into schema, mapping, identity, impact, and generated plumbing. The
handwritten lifecycle kernel retains orchestration and Terraform-specific
behavior.

The differential runner drives the released and candidate adapters with the
same fixtures and controller scenarios. Admission binds that result to the
exact catalog, policy, generator, provider source and binary, CLI toolchains,
target manifests, and scenario corpus.

The provider emits an exact-binary management contract only after admission.
The private downstream reconciler consumes that contract and the installed
provider schema. It never consumes the structural catalog or intermediate
provider specification, and it cannot promote a provider surface.

The release checkpoint collects the catalog ledger and per-surface receipts.
Construction, observation, compilation, and downstream comparison may produce
candidates. None can change a released support claim without the release gate.

## Adapter and rollback model

Once a migrated surface is admitted, the candidate adapter is its only
reachable runtime implementation. There is no environment variable, feature
flag, request-level selector, or failure fallback to the released adapter.

The previous adapter remains compiled but unreachable for one provider
release. A rollback requires a reviewed corrective build that restores its
registration. The release bundle takes a pre-upgrade state snapshot and
includes a tested recovery recipe for each changed surface. Lossless changes
provide a reverse transform. Other changes restore the previous configuration
and state snapshot or use the declared import bridge. A corrective binary is
not claimed to understand an incompatible candidate state shape unless a test
proves that path.

The following provider release may remove an inactive adapter only when the
soak period closed without rollback and the retained corrective build remains
reproducible from its locked source and toolchain.

## Migration waves

### Wave 0: catalog-wide parity machinery

- Replace DNS-specific status with a ledger derived from the complete installed
  provider projection.
- Generalize catalog, policy, impact, contract, and evidence formats across
  resources, data sources, list resources, and actions.
- Make the compiler reject missing ledger entries, missing dispositions,
  dangling semantic identifiers, stale overrides, and unmanifested outputs.
- Add released-versus-candidate differential runners for each surface class.
- Add the migration-manifest schema, completeness checks, and dry-run report
  for the full released catalog.
- Reconcile tracked development receipts with the retained exact CI evidence.

Wave 0 changes no additional runtime behavior.

### Wave 1: shared read surfaces

Bring all 13 data sources and 25 list resources through schema, identity,
pagination, filtering, sensitivity, empty-result, and result-shape parity.
Read surfaces expose generic compiler and mapping defects before more writes
move. Fleet-used firewall-zone lookup receives the first deep scenario set.

### Wave 2: fleet foundations

Migrate site, setting, network, WAN, DNS record, dynamic DNS, firewall zone,
and firewall group. These objects supply identities or configuration consumed
by later fleet resources. DNS keeps its admitted evidence and must pass the new
catalog-wide gates without a special case.

### Wave 3: fleet-dependent managed resources

Migrate AP group, client, device, WLAN, port profile, port forward, firewall
policy, VPN server, and WireGuard peer. Port forward is the nested-schema
lighthouse. Device coverage remains controller-CRUD scoped unless a released
claim requires real device behavior.

### Wave 4: remaining managed catalog

Migrate account, BGP, client QoS, firewall rule, power supervisor, RADIUS
profile, RADIUS user, site-to-site VPN, static route, traffic route, and VPN
client. Lower fleet priority does not reduce their admission evidence.

### Wave 5: action and hardware-dependent claims

Qualify the port action and any released behavior that depends on adoption,
PoE, reboot, firmware, topology, or physical gateway behavior. Emulator
evidence covers protocol behavior. Hardware-in-the-loop evidence is required
only when locked controller or emulator targets cannot substantiate the
released claim.

### Wave 6: downstream and whole-provider qualification

- Emit contracts for the complete admitted catalog.
- Bring downstream differential behavior to parity for every consumed surface.
- Run v0.101.2-to-candidate migration, import, refresh, plan, and recovery
  suites from pre-upgrade snapshots.
- Rebuild the exact candidate twice without networking.
- Run Terraform and OpenTofu against the same installed provider binary.
- Run private fleet-informed soak scenarios and produce the release checkpoint.

Private review and merging may happen wave by wave. Public export and release
remain blocked until Wave 6 passes.

## Per-surface verification

### Static contract

- Complete Terraform and OpenTofu schema projection.
- Attribute types, nesting modes, descriptions, sensitivity, defaults,
  validators, replacement rules, and timeouts.
- Resource identity, import grammar, state versions, state upgraders, and every
  declared migration-manifest difference.
- Data-source results, list pagination and filtering, and action input/output.
- Generated documentation from the exact candidate binary.

### Mapping and presence

- Configuration to intent and intent to controller patch.
- Controller model to Terraform state.
- Omitted, null, unknown, empty, defaulted, and computed values.
- Ordered and unordered collections, nested objects, and nested lists or sets.
- Sensitive-value redaction and preserve-only fields.
- Stable identity across create, import, refresh, and replacement.

### Adapter differential

- Identical fixtures and HTTP responses for released and candidate adapters.
- Equivalent requests after permitted canonicalization.
- Equivalent normalized controller outcomes, diagnostics, and following plan.
  Terraform state is identical or matches its declared candidate transform.
- Compatible state written by either adapter round-trips through the other.
  Intentional state breaks pass through the declared forward and recovery
  recipes instead.
- Failed writes do not retry through or fall back to the other adapter.

### Locked-controller lifecycle

Apply the operations relevant to the surface: create, read, update, refresh,
import, no-op plan, replacement, delete, controller restart, supported target
upgrade, failed-write behavior, readiness, evidence redaction, and cleanup.
Use fresh targets where isolation is required. Retain every attempt.

### Provider configuration

Preserve the canonical provider address and released username/password,
API-key and hardware-ID, site, TLS, and legacy Connector configuration
semantics. A parity change cannot silently reroute an existing configuration or
infer a new credential mode. Any future provider-configuration redesign is a
separate project, not part of the catalog migration budget.

### Downstream contract

For consumed surfaces, compare enumeration, import identity, capture
eligibility, redaction, generated HCL, plan classification, coverage, and
receipt inputs. Binary, contract, schema, CLI, catalog, operation, or corpus
mismatch fails contract mode before controller access.

## Fleet reference policy

The fleet inventory is read-only input to planning. It supplies resource type
counts, configuration-shape coverage, dependency ordering, and scenario
requirements. Test fixtures replace names, addresses, identifiers, credentials,
and values with established synthetic vocabulary.

No fleet HCL, state, plan, controller response, host name, internal repository
identity, runner identity, or incident identifier enters a public artifact.
Production-controller writes are outside the parity program. Private evidence
may record digests and value-free coverage summaries, but not raw state or
configuration.

## Failure semantics

Every attempt ends in one of these classifications:

| Result | Meaning | Admission effect |
| --- | --- | --- |
| `pass` | All required assertions and cleanup passed | May advance |
| `uncovered` | Required evidence or scenario is absent | Block |
| `divergent` | Released and candidate behavior differ | Block |
| `inconclusive` | Target or runner could not establish behavior | Block |
| `invalid` | Evidence, input, checksum, safety, or cleanup failed | Block |

Retries append attempts. They do not erase failures. Unknown controller fields,
missing catalog references, stale policy, malformed receipts, mismatched
binaries, and unmanifested outputs fail closed. Cleanup failure invalidates a
lifecycle receipt even when CRUD assertions passed.

A migration dry run also fails when it cannot identify a released object, when
an HCL rewrite lacks a state-address operation, when an import bridge cannot
prove the refreshed identity, or when the first candidate plan contains an
undeclared replacement or deletion.

## Evidence and confidentiality

Public artifacts may contain synthetic fixtures, permitted structural
interoperability output, provider policy, generated code, canonical schemas,
digests, and generic build instructions.

Restricted evidence contains raw CLI envelopes, private fleet coverage,
controller attempt records, private CI receipts, and unpublished merge
identities. It must not retain credentials, raw state, proprietary controller
bytes, or unsanitized controller responses.

Every public-export branch is rebuilt from the current public base. It receives
only the approved public artifacts and clean commit messages. The private
carrier history is never pushed or mirrored. Secret scanning, the private
identifier denylist, file-type checks, and a manual provenance review must pass
before publication.

## Completion and release gate

Full-catalog parity is complete only when:

- The ledger accounts for 28 resources, 13 data sources, 25 list resources,
  and one action.
- Every entry is `release_ready`. No blocking state remains.
- The complete installed schema matches v0.101.2 except for differences listed
  in the accepted migration manifest under Terraform and OpenTofu.
- All required adapter, state, import, lifecycle, controller-profile,
  downstream, and hardware-specific receipts pass.
- Every v0.101.2 surface has a tested forward migration and recovery path.
  The first post-migration refresh and plan contain no undeclared replacement,
  deletion, or ownership loss.
- The exact candidate binary and generated documentation reproduce in two
  clean, network-disabled builds.
- The provider consumes a publishable canonical `go-unifi` dependency without
  a private replacement.
- A pre-upgrade snapshot and the declared recovery recipes can restore the
  previous provider release. Corrective binaries are tested only against the
  state shapes they claim to accept.
- A private fleet-informed soak produces no unexplained drift.
- The release evidence and public-export confidentiality gates pass.

The public handoff begins only after this gate. Clean public branches then land
in dependency order, and the final evidence is regenerated from the public
merge commits before tagging the provider.

## Planning decomposition

This program is too large for one implementation plan. Each wave receives a
separate plan and independent review gate. Wave plans may split again by
capability group when their resources do not share code or target state.

The first implementation plan covers Wave 0 only. It produces the catalog-wide
ledger, generalized formats, migration-manifest contract, completeness checks,
differential-runner interfaces, and evidence reconciliation needed by every
migration wave.
