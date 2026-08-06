# Restricted catalog evidence

Files in this directory belong to the private carrier. Public-export tooling
must exclude the directory.

`catalog-fleet-gap-summary.json` contains only provider surface keys, declared
instance counts, and configured attribute names for the Wave 1-4 evidence
gaps. It contains no paths, labels, values, state, plans, controller output, or
network identifiers. A zero count means the inspected fleet has no declaration
for that surface. It is not a controller capability claim.

`catalog-pragmatic-resolution.json` binds that summary to the public evidence
inventory and the reference policy. The resolution accepts references only for
source-identical runtime paths and only when the referenced surface has no
missing base evidence. It resolves nine Wave 1-4 signals. The Wave 5 action is
exercised against the synthetic controller separately. Only its physical
hardware claim remains blocked.

Regenerate the resolution after either input changes:

```sh
go run ./cmd/catalog-pragmatic-evidence \
  -inventory build/release-ready/catalog-evidence-inventory.json \
  -fleet-summary build/restricted/catalog-fleet-gap-summary.json \
  -references provider-codegen/policy/catalog-pragmatic-references.json \
  -output build/restricted/catalog-pragmatic-resolution.json
```

This resolution is an input to admission. It does not set ledger state or
claim `release_ready`.
