# Milestone 1 DNS compiler evidence

`dns-compiler-receipt.json` binds the DNS-only structural bootstrap, provider
policy, resolved Provider Code Specification, HashiCorp generator output, and
the M0 compatibility hashes.

Run the local gate with the pinned CLIs:

```sh
TERRAFORM_BIN=/path/to/terraform \
TOFU_BIN=/path/to/tofu \
M1_RECEIPT_OUTPUT=/tmp/dns-compiler-receipt.json \
.woodpecker/scripts/m1-dns-compiler.sh
```

The gate regenerates the compiler artifacts, runs the complete Go suite, builds
one provider binary, and checks the DNS resource and identity schemas through
Terraform 1.15.8 and OpenTofu 1.12.1. Terraform also supplies the list-resource
schema hash because OpenTofu 1.12.1 does not expose that category.

