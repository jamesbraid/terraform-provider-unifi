# Milestone 2 DNS catalog evidence

`dns-catalog-cutover.json` binds the value-free `go-unifi` observation catalog
to the provider compiler and the accepted M0 schema hashes.

The provider policy pins both the catalog ID and operation digest. That pin is
the explicit provider-side admission for this candidate catalog. The compiler
also rejects unresolved conflicts, missing coverage, unstable semantic IDs,
and structural-source digest drift.

The M1 gate remains the executable provider check. Run it with the pinned CLIs
after regenerating from the catalog:

> The command below no longer runs: `m1-dns-compiler.sh` was deleted once the
> proof had finished its job -- nothing outside its own workflow read its
> receipt. The receipt is kept as the record of a run that happened; it is
> no longer reproducible from this tree.

```sh
TERRAFORM_BIN=/path/to/terraform \
TOFU_BIN=/path/to/tofu \
M1_EXECUTION=local-m2 \
M1_RECEIPT_OUTPUT=/tmp/m2-provider-gate-receipt.json \
.woodpecker/scripts/m1-dns-compiler.sh
```
