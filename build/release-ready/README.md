# Release-ready catalog evidence

`catalog-evidence-inventory.json` compares every released surface with the
v0.101.2 source tree and assigns its current test file as the scenario owner.
It records file digests for both trees and pins the v1.101.0 and v1.102.0
`go-unifi` module archives.

Run the inventory gate from a clone that already has the v0.101.2 tag and both
module archives in its Go cache:

```sh
bash .woodpecker/scripts/catalog-evidence-inventory_test.sh
```

The script disables module lookup and VCS fetching. It extracts the released
tree from the local tag, checks the cached module archives against
`provider-codegen/policy/catalog-evidence.json`, regenerates the inventory, and
compares it byte-for-byte with the tracked artifact.

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
disabled. The port action adopts a synthetic switch, runs through Terraform's
action trigger, and must persist the requested port override in the controller.
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
