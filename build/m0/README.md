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
OpenTofu 1.12.1 raw files in the private evidence repository. The M1 script
rejects an evidence output directory inside this repository.

M1 builds the same HEAD provider binary twice with fresh Go build caches,
networked module lookup disabled, and VCS embedding disabled. The lifecycle
builder uses the same flags even though it builds from a Git archive. M1 drives
both schema CLIs through development overrides against that binary, then
repeats the queries against the released v0.101.2 binary. Release and HEAD must
match within each CLI. Terraform and OpenTofu must also match after removing
only the categories absent from OpenTofu 1.12.1.

Set `M1_EVIDENCE_OUTPUT_DIRECTORY` to a restricted external directory to retain
the four raw envelopes, the two canonical HEAD projections, checksums, and the
M1 receipt. `M1_EVIDENCE_ARCHIVE` optionally writes the same verified bundle as
a compressed archive. Set `M1_LIFECYCLE_RECEIPT` during qualification to emit
an exact-binary management sidecar; its source commit and provider hash must
match the lifecycle receipt.

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
