# DNS Compiler Lighthouse Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Compile `unifi_dns_record` from pinned go-unifi structural facts and explicit provider policy into deterministic Framework schema code without changing its released behavior.

**Architecture:** A provider-local compiler reads a DNS-only projection of go-unifi v1.102.0 `specification.json`, a hand-owned JSON policy, and the M0 schema digest. It emits a resolved Provider Code Specification plus impact and mapping reports, then invokes `tfplugingen-framework` v0.4.1. The generated schema replaces only the handwritten attribute construction. Identity, timeouts, list support, state upgrade, imports, registration, and CRUD remain handwritten seams.

**Tech Stack:** Go 1.25.8, Terraform Plugin Framework, `tfplugingen-framework` v0.4.1, JSON, Terraform 1.15.8, OpenTofu 1.12.1.

## Global Constraints

- Keep the provider runtime dependency at go-unifi v1.101.0.
- Use go-unifi v1.102.0 commit `e255518385e0104eb838be56c2a491de158f3194` only as the bootstrap structural source.
- Pin bootstrap `specification.json` SHA-256 `3ddcc597a631259089c823553f3bf696725ad0bbf7d78d2f412b111e8e3427ad`.
- Pin `tfplugingen-framework` v0.4.1, module sum `h1:eaI/3dsu2T5QAXbA+7N+B+UBj20GdtYnsRuYypKh3S4=`, commit `eea0e9d6b59b4e678cac5cda2d2c5d852f8679f2`.
- Preserve resource schema digest `1bdb6740d88d68bf232d79874c34d0e3811d382f55948352add15c2a28e5e93c`.
- Preserve identity schema digest `1a6e443309d9484e62e9f1fe71a83b60cf348f4acbe3a92d8f7b8bb7d3274d33`.
- Preserve list-resource schema digest `c914929e71ab8ce0e8977518615ee3cf81c31a411ec77c9f58a2350145c6ee95`.
- Do not generate CRUD, registration, imports, identity, state upgrade, list execution, or lifecycle orchestration.
- Public documentation continues to come from the built provider.

---

### Task 1: Typed compiler and fail-closed policy resolution

**Files:**
- Create: `internal/providercompiler/types.go`
- Create: `internal/providercompiler/compile.go`
- Create: `internal/providercompiler/compile_test.go`
- Create: `provider-codegen/bootstrap/go-unifi-v1.102.0-dns-record.json`
- Create: `provider-codegen/policy/dns_record.json`

**Interfaces:**
- Consumes: `CompileInput{Bootstrap []byte, Policy []byte, BaselineDigests []byte}`.
- Produces: `Compile(input CompileInput) (Result, error)`, where `Result` contains `ProviderCodeSpec`, `ImpactReport`, and `MappingReport` byte slices.

- [ ] **Step 1: Add failing policy-resolution tests**

Test a valid eight-field DNS projection, an unclassified `new_field`, a policy entry pointing at `removed_field`, a duplicate Terraform name, and a bootstrap digest mismatch. Assert the errors contain `unclassified structural field`, `stale policy field`, `duplicate terraform attribute`, and `bootstrap digest mismatch` respectively.

- [ ] **Step 2: Run the focused test and observe the missing compiler**

Run: `go test ./internal/providercompiler -run 'TestCompile' -v`

Expected: FAIL because `Compile` and its types do not exist.

- [ ] **Step 3: Implement the minimal typed compiler**

Define these exact public types:

```go
type CompileInput struct {
    Bootstrap       []byte
    Policy          []byte
    BaselineDigests []byte
}

type Result struct {
    ProviderCodeSpec []byte
    ImpactReport     []byte
    MappingReport    []byte
}

func Compile(input CompileInput) (Result, error)
```

Accept only dispositions `managed`, `computed`, `preserve_only`, and `omitted`. Sort all emitted arrays by stable field or attribute name and encode JSON with two-space indentation plus one trailing newline.

- [ ] **Step 4: Author the pinned bootstrap projection and complete DNS policy**

The projection carries the source repository, commit, full-spec digest, and resource name.
It includes all eight structural fields: `enabled`, `key`, `port`, `priority`, `record_type`, `ttl`, `value`, and `weight`.
The policy maps `key` to `name` and changes `ttl` from controller integer seconds to a Terraform Go-duration string.
It explicitly classifies every field as `managed`.
It also declares provider-owned `id`, `site`, and `timeouts` seams and records the three M0 schema digests.

- [ ] **Step 5: Run focused and package tests**

Run: `go test ./internal/providercompiler -v`

Expected: PASS, including both fail-closed cases.

- [ ] **Step 6: Commit**

Stage the five task files and commit with subject `providercompiler: resolve DNS policy fail closed`.

### Task 2: Deterministic outputs and pinned Framework generation

**Files:**
- Create: `cmd/provider-spec-compiler/main.go`
- Create: `cmd/provider-spec-compiler/main_test.go`
- Create: `provider-codegen/generated/dns_record.provider-code-spec.json`
- Create: `provider-codegen/generated/dns_record.impact.json`
- Create: `provider-codegen/generated/dns_record.mapping.json`
- Create: `internal/generated/resource_dns_record/dns_record_resource_gen.go`
- Modify: `tools/tools.go`
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `provider-codegen/generate.go`

**Interfaces:**
- Consumes: `provider-spec-compiler -bootstrap <path> -policy <path> -baseline <path> -output-dir <path>`.
- Produces: the three canonical JSON files and generator-owned `resource_dns_record.DnsRecordResourceSchema(context.Context) schema.Schema`.

- [ ] **Step 1: Add failing command tests**

Run the command twice into separate temporary directories and compare every byte. Assert `dns_record.mapping.json` lists all eight structural fields and the provider-owned seams, and that the impact report contains zero unresolved or stale entries.

- [ ] **Step 2: Run the command test and observe missing output**

Run: `go test ./cmd/provider-spec-compiler -v`

Expected: FAIL because the command does not exist.

- [ ] **Step 3: Implement atomic canonical writes**

Use `providercompiler.Compile`, write each result to a temporary file in the destination directory, `fsync`, and rename. Never mutate the policy or bootstrap input.

- [ ] **Step 4: Pin and run the Framework generator**

Add the tool dependency at v0.4.1 and a `go:generate` entry that runs:

```text
go run github.com/hashicorp/terraform-plugin-codegen-framework/cmd/tfplugingen-framework@v0.4.1 generate resources --input provider-codegen/generated/dns_record.provider-code-spec.json --output internal/generated --package resource_dns_record
```

Generate only `unifi_dns_record`. The resolved specification supplies generator expressions for descriptions, validators, defaults, the custom duration type, and plan modifiers. Keep `timeouts` outside the HashiCorp generator.

- [ ] **Step 5: Verify deterministic generation**

Run the compiler and Framework generator twice, hash the four generated outputs, and compare the hashes. Then run `gofmt` on generated Go and `git diff --check`.

Expected: both runs are byte-identical.

- [ ] **Step 6: Commit**

Stage the command, tool pin, generator entrypoint, and generated artifacts. Commit with subject `build: generate the resolved DNS schema`.

### Task 3: Use generated schema without moving the lifecycle kernel

**Files:**
- Modify: `unifi/dns_record_resource.go`
- Modify: `unifi/dns_record_resource_test.go`
- Create: `unifi/dns_record_schema_test.go`

**Interfaces:**
- Consumes: `resource_dns_record.DnsRecordResourceSchema(ctx)`.
- Produces: the existing `dnsRecordFrameworkResource.Schema` behavior with handwritten timeout attachment and unchanged resource methods.

- [ ] **Step 1: Add a schema-equivalence regression test**

Build the schema through `dnsRecordFrameworkResource.Schema`. Canonicalize `unifi_dns_record`, its identity schema, and its list-resource schema. Assert the three M0 digests. Also assert that `UpgradeState` exposes version 0 and both import forms populate the same fields.

- [ ] **Step 2: Run the regression test before integration**

Run: `go test ./unifi -run 'TestDNSRecord.*Schema|TestDNSRecord.*Import|TestDNSRecord.*Upgrade' -v`

Expected: the digest test fails until the generated schema is wired in.

- [ ] **Step 3: Replace only handwritten attribute construction**

Set `resp.Schema = resource_dns_record.DnsRecordResourceSchema(ctx)`, then attach `Version: 1` and the `timeouts.Attributes` result in the handwritten method. Do not edit `Create`, `Read`, `Update`, `Delete`, `ImportState`, `UpgradeState`, identity, list configuration, or list execution.

- [ ] **Step 4: Run resource and full provider tests**

Run: `go test ./unifi -run 'TestDNSRecord' -v`

Run: `go test ./...`

Expected: PASS with no runtime-resource test change.

- [ ] **Step 5: Commit**

Stage the DNS resource and tests. Commit with subject `dns-record: consume the generated schema`.

### Task 4: Regeneration, binary schema parity, and M1 receipt

**Files:**
- Create: `.woodpecker/m1-dns-compiler.yml`
- Create: `.woodpecker/scripts/m1-dns-compiler.sh`
- Create: `build/m1/README.md`
- Create: `build/m1/dns-compiler-receipt.json`

**Interfaces:**
- Consumes: the checked-in compiler inputs/outputs and M0 CLI/build pins.
- Produces: a receipt binding input digests, compiler commit, generator version, output digests, elapsed time, human decision count, and Terraform/OpenTofu schema results.

- [ ] **Step 1: Add workflow contract checks**

Assert the workflow runs on `pool: skunkworks`, uses Go 1.25.8, Terraform 1.15.8, OpenTofu 1.12.1, and invokes the M1 script with networking disabled after dependency preparation.

- [ ] **Step 2: Implement the M1 gate**

The script performs two clean regenerations, compares generated bytes, and runs `go test ./...`.
It builds one Linux/amd64 provider binary and canonicalizes both CLI schema outputs.
It checks the three DNS digests against M0.
It also runs the existing DNS lifecycle and v0 TTL upgrade fixture without changing HCL.

- [ ] **Step 3: Record the autonomy baseline**

The receipt records elapsed generation time and these initial human decisions: field dispositions, `key` to `name`, integer seconds to duration string, provider-owned identity/site/timeouts, defaults, validators, replacement modifiers, and the handwritten state/list/lifecycle seams.

- [ ] **Step 4: Run and retain the gate**

Run the workflow on private Forgejo/Woodpecker. Copy the compact receipt to `build/m1/dns-compiler-receipt.json` and validate it with `jq empty`.

- [ ] **Step 5: Final verification and commit**

Run: `go generate ./... && git diff --exit-code`

Run: `go test ./...`

Run: `git diff --check`

Expected: all commands exit zero, and the provider dependency, registration, CRUD, imports, state, and public schema remain unchanged.

Commit with subject `build: record the DNS compiler lighthouse`.
