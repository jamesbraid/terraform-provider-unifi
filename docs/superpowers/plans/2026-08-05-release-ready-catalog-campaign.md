# Release-Ready Catalog Campaign

**Goal:** Promote all 67 released provider surfaces to `release_ready` with
evidence tied to exact released and candidate binaries. The Wave 1-4
`static_pass` receipts are inputs to this campaign, not completion claims.

## Promotion rule

For each surface:

1. `adapter_parity` requires released-versus-candidate schema, unit,
   HTTP-boundary, state/import, and applicable controller scenarios to pass.
2. `admitted` binds that pass to the source commits, provider binaries,
   go-unifi modules, CLI toolchains, target manifests, and scenario corpus.
3. `contract_parity` requires the exact candidate management contract to agree
   with the downstream legacy contract after declared migration transforms.
4. `release_ready` requires the whole-provider migration/recovery, dual-CLI,
   deterministic-build, fleet-soak, and applicable hardware gates.

No `static_pass`, source-identity result, unit suite, or single controller run
can skip a promotion state. Missing evidence remains `uncovered`.

## Task 1: Inventory the real runtime delta and evidence coverage

Generate a per-surface evidence matrix from the v0.101.2 tag, current source,
v1.101.0 and v1.102.0 go-unifi module archives, canonical schema, provider test
corpus, and acceptance-test inventory. Record source-identical provider paths
separately from changed SDK/model paths. Fail when a surface has no scenario
owner, import/state coverage where applicable, or required read/list/action
coverage.

## Task 2: Produce exact local released and candidate evidence

Build both provider versions twice with the same pinned toolchain and network
disabled. Run both complete unit and HTTP-boundary suites. Install each binary
through isolated development overrides and capture Terraform and OpenTofu
schemas. Canonical projections must agree with the migration manifest.

## Task 3: Run controller differential campaigns

Use the digest-pinned Network and UOS targets on the Skunkworks runners. Run
the same surface scenario corpus against released and candidate binaries,
retaining append-only attempts. Cover CRUD/read/list, import, refresh, no-op
plan, replacement, failed writes, restart, cleanup, and redaction as applicable.
Do not promote uncovered surfaces merely because their provider source file is
unchanged; the go-unifi dependency changed across the whole catalog.

## Task 4: Admit wave groups

Promote a wave surface to `adapter_parity` only from passing differential
evidence, then to `admitted` with a digest-bound receipt. Once admitted, its
candidate or accepted terminal implementation is the only registered runtime
path. Wave 1 has 38 read surfaces; Wave 2 has eight foundations; Wave 3 has nine
fleet-dependent resources; Wave 4 has eleven remaining resources; Wave 5 has
the port action and hardware-specific claims.

## Task 5: Prove downstream contract parity

Emit complete exact-binary management contracts for admitted surfaces and run
the downstream contract suite against its legacy manifest. Enumeration,
identity, capture eligibility, redaction, generated HCL, plan classification,
coverage, and receipt inputs must match after declared transforms.

## Task 6: Promote the catalog to release-ready

Run v0.101.2-to-candidate migration and recovery from pre-upgrade snapshots,
Terraform and OpenTofu against the same candidate binary, two clean
network-disabled builds, locked-controller restart/upgrade scenarios, private
fleet-informed soak, HIL where protocol/controller evidence is insufficient,
and the public-export confidentiality gate. Only then set all 67 ledger entries
to `release_ready` and pass `VerifyCatalogPromotion`.

Dependency promotion is part of this gate. The candidate consumes
`github.com/ubiquiti-community/go-unifi v1.102.0` directly, with no `replace`.
Until the public handoff publishes that tag at its canonical GitHub origin,
Skunkworks constructs a local Go module-proxy entry from the exact v1.102.0
source commit and verifies the canonical module declaration and content sum.
That private proxy is staging transport, not a claim that the public module is
already available. The laptop does not contact GitHub; release downloads and
source acquisition run only on Skunkworks. The public handoff must publish the
same commit and reproduce the receipt before the provider is tagged.
