# Milestone 0 provider schema baseline

`cmd/schema-baseline` turns the raw output from either
`terraform providers schema -json` or `tofu providers schema -json` into the
projection for `registry.terraform.io/ubiquiti-community/unifi`.

The command removes only the CLI envelope. It retains the complete provider
projection, including any resource, data source, list resource, function,
action, or ephemeral-resource schemas returned by the CLI. The companion
digest manifest records the complete projection digest and one digest for each
named schema.

Raw CLI output belongs in the restricted `infra/unifi-build-evidence`
repository. It is useful for diagnosing a mismatch but is not a committed
public compatibility contract.

The accepted M0c manifest is `provider-baseline.json`; per-schema hashes are in
`provider-schema-digests.json`. Terraform 1.15.8 returns action and list-resource
schema categories that OpenTofu 1.12.1 does not return. Both full projections
are retained and hashed. The gate compares release to HEAD within each CLI and
also compares the shared projection exactly.

The first passing private carrier retained the raw outputs only for the life of
the job. The baseline keeps `raw_linux_cli_outputs_retained` false until
`receipts/provider-baseline/receipt.json` verifies the Terraform 1.15.8 and
OpenTofu 1.12.1 raw files in the private evidence repository.

WHAT M1 DID, PAST TENSE. `m1-dns-compiler.sh` has been deleted; the paragraphs
below described how to run it and are kept as a record of what produced
`build/m1/dns-compiler-receipt.json`, not as instructions. Nothing reads
`M1_EVIDENCE_OUTPUT_DIRECTORY`, `M1_EVIDENCE_ARCHIVE` or `M1_LIFECYCLE_RECEIPT`
any more -- measured across the Go, the shell and the workflows -- so setting
them does nothing.

M1 built the same HEAD provider binary twice with fresh Go build caches,
networked module lookup disabled, and VCS embedding disabled. It drove both
schema CLIs through development overrides against that binary, then repeated the
queries against the released v0.101.2 binary, requiring release and HEAD to
match within each CLI and Terraform and OpenTofu to match after removing only
the categories absent from OpenTofu 1.12.1. It refused an evidence output
directory inside this repository.

The schema comparison itself survives and is not M1's: `cmd/schema-parity`
performs it, and `internal/schemaparity` holds the frozen contract it is checked
against.

`port-forward-shadow.json` records the nested-shape and mapping-test shadow
baseline. It deliberately does not claim acceptance, lifecycle, migration, or
compiler admission for `unifi_port_forward`.

`network-dns-qualification.json` records the passing Terraform/OpenTofu DNS
lifecycle against the locked standalone Network target.

`uos-dns-qualification.json` records the complementary native-arm64 run against
the digest-pinned UOS 5.1.21 target. Each CLI used a fresh persisted volume,
created and deleted the same synthetic DNS record, restarted the controller,
and produced the same normalized state after refresh and a no-op plan. The
per-volume API key was verified across restart but was not retained. Together
the Network and UOS receipts close the controller coverage recorded by the M0
checkpoint.

```sh
go run ./cmd/schema-baseline \
  -input terraform.raw.json \
  -canonical-output terraform.canonical.json \
  -digests-output terraform.digests.json
```
