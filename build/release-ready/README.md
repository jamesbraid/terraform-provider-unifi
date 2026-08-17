# Release-ready catalog evidence

`catalog-evidence-inventory.json` compares every released surface with the
v0.101.2 source tree and assigns its current test file as the scenario owner.
It records file digests for both trees and pins the v1.101.0 and v1.102.0
`go-unifi` module archives.

The inventory gate is regenerate-and-compare: rebuild the artifact and require
it to match the tracked one byte for byte. It needs a clone that already has
the v0.101.2 tag and both module archives in its Go cache, because it extracts
the released tree from the local tag and checks the cached archives against
`provider-codegen/policy/catalog-evidence.json` with module lookup and VCS
fetching disabled.

The shell self-test that used to be named here has been deleted; its coverage
is in `go test ./...` and the regeneration runs in CI. The workflow step is the
authority on how it is invoked -- naming a command here would be a second home
for that, and this file cannot notice when it goes stale.

The missing-signal list drives the controller campaign. A source-identical file
or an existing acceptance test does not satisfy adapter parity, admission,
contract parity, or `release_ready`.

`catalog-build-schema.sh` builds the released source and candidate twice, then
drives both binaries through Terraform and OpenTofu development overrides. It
requires the pinned Linux toolchain and published release binary for a passing
promotion receipt. A developer can set `CATALOG_ALLOW_DIAGNOSTIC_TOOLCHAIN=true`
to inspect another platform or CLI patch level. That receipt is marked
`diagnostic_pass` and lists every promotion blocker.

```sh
CATALOG_ALLOW_DIAGNOSTIC_TOOLCHAIN=true \
CATALOG_BUILD_SCHEMA_OUTPUT=/tmp/catalog-build-schema.json \
TERRAFORM_BIN=/path/to/terraform \
TOFU_BIN=/path/to/tofu \
bash .woodpecker/scripts/catalog-build-schema.sh
```

The gate performs no dependency or VCS fetch. Final evidence must also set
`CATALOG_RELEASED_PROVIDER_BINARY` to the verified v0.101.2 binary retained in
the private evidence store.

`catalog-unit-differential.sh` extracts the exact v0.101.2 source tree and runs
the complete released and candidate Go suites. `TF_ACC` is removed so this
layer covers unit and in-process HTTP-boundary tests without a controller. The
runner disables module, checksum-database, toolchain, and VCS acquisition,
keeps raw JSON logs outside the repository, and emits only counts plus hashes
of the raw logs and normalized package/test outcomes. As with the build/schema
gate, a non-baseline local toolchain can only produce `diagnostic_pass`.

`catalog-controller-differential.sh` plans the Wave 1-5 acceptance corpus and
runs those test names against the released and candidate source trees. Both
attempts use the same digest-pinned, locally cached controller and the same
source-pinned synthetic fleet and Ryuk helper. Controller registry pulls are
disabled. The candidate must pass every planned scenario outside the exact
declared skip set. A released-only failure is accepted only when the plan names
that specific v0.101.2 limitation and the candidate passes the scenario. The
receipt reports `accepted_limitation`, not `pass`. The only current limitation
is `TestAccDeviceFramework_basic`, which reproduces v0.101.2 returning the
controller's stale model-default name after apply. The port action adopts a
synthetic switch, runs through Terraform's action trigger, and must persist the
requested port override in the controller.
That proves the action protocol, not electrical PoE behavior. A passing
differential can still report `blocked_evidence`: missing acceptance, import,
list, or hardware signals remain blockers until a scenario or a pragmatic
fleet reference covers them. Only `CATALOG_REQUIRE_COMPLETE=true` turns the
diagnostic into a promotion gate.

The private carrier reconciles non-lifecycle Wave 1-4 gaps with
`cmd/catalog-pragmatic-evidence`. The reconciler requires a value-free fleet
summary, a digest-bound reference policy, source-identical target runtime, and
a fully covered source surface. It cannot resolve the Wave 5 hardware claim or
promote a ledger entry.

`cmd/catalog-admission` is the next fail-closed boundary. It accepts only
promotable build/schema and unit receipts, a complete released/candidate
controller differential, and a pragmatic resolution bound to that controller
receipt. Each surface gets separate `adapter_parity` and `admitted` digests.
The catalog can be admitted with the port action's physical hardware claim
carried forward as a release blocker. No other unresolved signal is allowed.

`cmd/catalog-management-contract` binds the admission receipt back to the
exact provider binary and Terraform/OpenTofu schema toolchains. Its 67-surface
projection marks all 28 managed resources for downstream capture verification
and marks the remaining data, list, and action surfaces `not_applicable` for
capture. The legacy authority is an immutable `ubitofu` manifest commit and
file digest. Endpoint and identity policy are not duplicated into this
repository during the parity check.

`cmd/catalog-migration-recovery` binds the complete admission, build/schema,
controller, inventory, and migration manifest to the M3 DNS lifecycle receipt.
The DNS resource carries bidirectional v0.101.2/candidate state, import,
restart, no-op plan, deletion, and v0 integer-TTL upgrade evidence. The DNS list
surface carries the matching controller differential. The other 65 surfaces
must be source-identical to v0.101.2 and retain their explicit identity and
snapshot-recovery entries. This gate produces `migration_recovery_pass`. It
does not claim downstream contract parity, fleet soak, or `release_ready`.
