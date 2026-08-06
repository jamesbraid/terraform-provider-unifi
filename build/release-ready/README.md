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

`catalog-controller-differential.sh` plans the Wave 1-4 acceptance corpus and
runs those test names against the released and candidate source trees. Both
attempts use the same digest-pinned, locally cached controller and the same
source-pinned synthetic fleet and Ryuk helper; controller registry pulls are
disabled. A
passing differential can still report `blocked_evidence`: missing acceptance,
import, list, or hardware signals remain blockers until a scenario or a
pragmatic fleet reference covers them. Only `CATALOG_REQUIRE_COMPLETE=true`
turns the diagnostic into a promotion gate.
