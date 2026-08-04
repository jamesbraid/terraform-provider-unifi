# Milestone 3 DNS operation evidence

`dns-operation-receipt.json` binds the normalized DNS backend to go-unifi
v1.102.0, the admitted catalog, deterministic generation, and the accepted
Terraform/OpenTofu schema hashes.

Run the local gate with the pinned CLIs:

```sh
TERRAFORM_BIN=/path/to/terraform \
TOFU_BIN=/path/to/tofu \
M3_RECEIPT_OUTPUT=/tmp/dns-operation-receipt.json \
.woodpecker/scripts/m3-dns-operation.sh
```

The local receipt remains `pending_locked_target` until the migrated provider
runs the complete DNS lifecycle against the locked Network target. Pass that
redacted receipt through `M3_LIFECYCLE_RECEIPT` to produce qualified evidence.
