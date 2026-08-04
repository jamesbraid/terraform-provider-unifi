# Milestone 0 provider schema baseline

`cmd/schema-baseline` turns the raw output from either
`terraform providers schema -json` or `tofu providers schema -json` into the
projection for `registry.terraform.io/ubiquiti-community/unifi`.

The command removes only the CLI envelope. It retains the complete provider
projection, including any resource, data source, list resource, function,
action, or ephemeral-resource schemas returned by the CLI. The companion
digest manifest records the complete projection digest and one digest for each
named schema.

Raw CLI output belongs in the restricted evidence store. It is useful for
diagnosing a mismatch but is not a committed compatibility contract.

The accepted M0c manifest is `provider-baseline.json`; per-schema hashes are in
`provider-schema-digests.json`. Terraform 1.15.8 returns action and list-resource
schema categories that OpenTofu 1.12.1 does not return. Both full projections
are retained and hashed. The gate compares release to HEAD within each CLI and
also compares the shared projection exactly.

The first passing private carrier retained the raw outputs only for the life of
the job. That is recorded as an evidence limitation in the baseline manifest;
a later evidence-carrier update must persist the raw diagnostic envelopes
before release promotion.

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
