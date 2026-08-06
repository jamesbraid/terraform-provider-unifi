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
