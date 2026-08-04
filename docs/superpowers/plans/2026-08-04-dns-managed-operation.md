# DNS Managed Operation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate `unifi_dns_record` to an explicit private Model -> Intent -> Patch backend without changing its Terraform contract.

**Architecture:** A provider-local interface separates the handwritten lifecycle kernel from `go-unifi`. The concrete adapter uses v1.102.0 named-field updates. The resource derives patch presence from the Terraform plan and retains all existing public behavior.

**Tech Stack:** Go 1.26.5, terraform-plugin-framework, go-unifi v1.102.0, Terraform 1.15.8, OpenTofu 1.12.1.

## Global Constraints

- Keep the provider address `registry.terraform.io/ubiquiti-community/unifi`.
- Preserve the DNS schema, identity, state version, import grammar, timeouts, list behavior, and private API routing.
- Never fall back after a failed write.
- Keep all catalog and compiler artifacts build-time only.
- Run tests test-first and retain the legacy adapter until parity evidence passes.

---

### Task 1: Pin the operation-capable go-unifi release

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Test: `unifi/dns_record_backend_test.go`

**Interfaces:**
- Consumes: `github.com/jamesbraid/go-unifi v1.102.0` through the existing module replacement.
- Produces: `(*unifi.ApiClient).UpdateDNSRecordFields(context.Context, string, *unifi.DNSRecord, ...string)`.

- [ ] **Step 1: Write a compile-time test for named-field updates**

Add an interface assertion in `unifi/dns_record_backend_test.go`:

```go
type dnsRecordFieldUpdater interface {
    UpdateDNSRecordFields(context.Context, string, *ui.DNSRecord, ...string) (*ui.DNSRecord, error)
}
var _ dnsRecordFieldUpdater = (*ui.ApiClient)(nil)
```

- [ ] **Step 2: Run the test and verify it fails**

Run: `GOCACHE=/private/tmp/m3-provider-go-cache go test ./unifi -run TestDNSRecordBackendDependency -count=1`

Expected: FAIL because the v1.101.0 replacement has no named-field method.

- [ ] **Step 3: Update the replacement and sums**

Set the existing replacement to `github.com/jamesbraid/go-unifi v1.102.0`, then download only that module version so `go.sum` records its checksums.

- [ ] **Step 4: Run the focused test**

Run: `GOCACHE=/private/tmp/m3-provider-go-cache go test ./unifi -run TestDNSRecordBackendDependency -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

Commit subject: `deps: update go-unifi for masked DNS writes`.

### Task 2: Add the normalized DNS backend

**Files:**
- Create: `unifi/dns_record_backend.go`
- Create: `unifi/dns_record_backend_test.go`

**Interfaces:**
- Consumes: `*unifi.ApiClient` and the existing private DNS methods.
- Produces: `dnsRecordBackend`, `dnsRecordModel`, `dnsRecordIntent`, `dnsRecordPatch`, and `newPrivateDNSRecordBackend(*unifi.ApiClient)`.

- [ ] **Step 1: Write failing adapter tests**

Test that create, read, delete, and list normalize the legacy result. Assert
that update sends exactly the patch field mask. An empty or unknown mask must
fail without issuing a request. Use an `httptest.Server` so assertions cover
the real `go-unifi` encoder and request path.

- [ ] **Step 2: Verify the focused tests fail**

Run: `GOCACHE=/private/tmp/m3-provider-go-cache go test ./unifi -run 'TestPrivateDNSRecordBackend|TestDNSRecordPatch' -count=1 -v`

Expected: FAIL because the backend types do not exist.

- [ ] **Step 3: Implement the minimal backend**

Define:

```go
type dnsRecordBackend interface {
    Create(context.Context, string, dnsRecordIntent) (dnsRecordModel, error)
    Read(context.Context, string, string) (dnsRecordModel, error)
    Update(context.Context, string, dnsRecordPatch) (dnsRecordModel, error)
    Delete(context.Context, string, string) error
    List(context.Context, string) ([]dnsRecordModel, error)
}
```

Translate only the eight catalog-managed fields. Validate patch fields against
that fixed set before calling `UpdateDNSRecordFields`. Pass controller errors
through unchanged.

- [ ] **Step 4: Run the focused backend tests**

Run: `GOCACHE=/private/tmp/m3-provider-go-cache go test ./unifi -run 'TestPrivateDNSRecordBackend|TestDNSRecordPatch' -count=1 -v`

Expected: PASS.

- [ ] **Step 5: Commit**

Commit subject: `dns-record: add the normalized private backend`.

### Task 3: Migrate the lifecycle kernel

**Files:**
- Modify: `unifi/dns_record_resource.go`
- Modify: `unifi/dns_record_resource_test.go`

**Interfaces:**
- Consumes: `dnsRecordBackend` and normalized DNS types from Task 2.
- Produces: a DNS resource whose runtime calls only the narrow backend.

- [ ] **Step 1: Write failing lifecycle tests**

Inject a recording backend and assert create, read, update, delete, and list use
it. For update, assert optional null or unknown values are absent from the patch
and known changed values are present. Make the recording backend return a write
error and assert it receives one update call.

- [ ] **Step 2: Verify the tests fail**

Run: `GOCACHE=/private/tmp/m3-provider-go-cache go test ./unifi -run 'TestDNSRecord.*Backend|TestDNSRecord.*Patch' -count=1 -v`

Expected: FAIL because the resource still calls `*Client` directly.

- [ ] **Step 3: Cut the resource over**

Store `dnsRecordBackend` and `defaultSite` on the resource. Configure them from
the provider client. Convert Terraform plans to intents and patches before the
existing state merge, then convert normalized controller models back to the
unchanged Terraform model. Keep import, schema, state upgrade, timeouts, and
diagnostic text unchanged.

- [ ] **Step 4: Run focused and complete tests**

Run: `GOCACHE=/private/tmp/m3-provider-go-cache go test ./unifi -count=1`

Run: `GOCACHE=/private/tmp/m3-provider-go-cache go test ./... -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

Commit subject: `dns-record: route lifecycle through the normalized backend`.

### Task 4: Record parity evidence

**Files:**
- Create: `.woodpecker/scripts/m3-dns-operation.sh`
- Create: `.woodpecker/m3-dns-operation.yml`
- Create: `build/m3/README.md`
- Create: `build/m3/dns-operation-receipt.json`

**Interfaces:**
- Consumes: the M3 provider binary, locked M0 targets, and pinned Terraform/OpenTofu CLIs.
- Produces: a receipt binding the dependency, provider commit, catalog digest, adapter parity results, lifecycle evidence, and unchanged schema hashes.

- [ ] **Step 1: Write the gate script assertions**

Require deterministic generation, `go test ./...`, the DNS adapter tests, one
provider binary, both CLI schema checks, the M0 schema digests, and the declared
DNS lifecycle fixture. Mark live locked-target evidence pending unless the
controller profile actually ran.

- [ ] **Step 2: Run the local gate**

Run with pinned CLI paths and a temporary receipt. Expected: PASS with the
resource, identity, and list-resource hashes equal to M0.

- [ ] **Step 3: Validate workflow and evidence**

Run `shellcheck`, parse the workflow, run `jq empty` on the receipt, and verify
every recorded digest against its artifact.

- [ ] **Step 4: Commit**

Commit subject: `build: record the DNS managed operation migration`.
