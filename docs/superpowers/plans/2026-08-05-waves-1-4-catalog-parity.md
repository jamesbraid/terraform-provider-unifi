# Waves 1-4 Catalog Parity Implementation Plan

> Execute this plan continuously after Wave 0. A wave may complete its static
> construction locally while promotion remains blocked on retained controller
> evidence. Never turn missing evidence into a passing receipt.

**Goal:** Build a deterministic full-catalog contract corpus, carry every read
surface and every managed resource through its assigned Wave 1-4 static gate,
and leave an exact machine-readable account of what is ready and what still
needs adapter or controller evidence.

**Compatibility stance:** The current provider runtime remains the released
compatibility implementation until a surface reaches the existing admission
gate. Static completion advances a surface to `policy_complete`; it does not
claim runtime cutover, differential parity, or lifecycle qualification. DNS
retains its admitted status and port forward retains its shadow status.

## Task 1: Compile the full surface-contract corpus

Create `internal/catalogparity/waves.go` and tests for the exact Wave 1-5
partition. Create a strict policy at `provider-codegen/policy/catalog.json` and
extend `cmd/catalog-parity` to emit
`provider-codegen/generated/catalog-surface-contracts.json`.

Each contract binds surface kind/name, assigned wave, released schema digest,
schema version, required parity dimensions, and evidence gates. Reject missing,
duplicate, unknown, or multiply assigned surfaces and any unexpected generated
catalog artifact. Start with failing completeness and determinism tests, then
regenerate twice and require a clean diff.

## Task 2: Complete Wave 1 static read contracts

Update the status overlay so all 13 data sources and 25 list resources are
`policy_complete`, except the DNS list resource remains `shadow_only` until its
adapter evidence exists. Generate `build/wave1/read-surfaces.json` with exact
counts, schema and contract digests, and explicit blocker counts.

Test schema identity, version coverage, read/list parity dimensions,
empty-result coverage, and list pagination/filter requirements. The receipt
must say `static_pass` and `promotion: blocked_evidence`; it must not claim a
controller run.

## Task 3: Complete Wave 2 static foundation contracts

Advance site, setting, network, WAN, dynamic DNS, firewall zone, and firewall
group to `policy_complete`. Keep DNS record `admitted` and bind it to its
existing receipt. Generate `build/wave2/fleet-foundations.json` and test the
exact eight-surface set, migration entries, provider address, and blocker
classification.

## Task 4: Complete Wave 3 static fleet-dependent contracts

Advance AP group, client, device, WLAN, port profile, firewall policy, VPN
server, and WireGuard peer to `policy_complete`. Keep port forward
`shadow_only`. Generate `build/wave3/fleet-dependent.json`; require the nested
port-forward schema digest and shadow receipt without making a lifecycle claim.

## Task 5: Complete Wave 4 static remaining-managed contracts

Advance account, BGP, client QoS, firewall rule, power supervisor, RADIUS
profile, RADIUS user, site-to-site VPN, static route, traffic route, and VPN
client to `policy_complete`. Generate `build/wave4/remaining-managed.json` and
test the exact eleven-surface partition and blocker accounting.

At this point all 66 Wave 1-4 surfaces must be one of `policy_complete`,
`shadow_only`, or `admitted`; the port action remains
`legacy_authoritative` for Wave 5. No Wave 1-4 receipt may say `release_ready`.

## Task 6: Verify the Wave 4 checkpoint

Run the pinned full generator twice, `go test ./... -count=1`, `go vet ./...`,
and `git diff --check`. Confirm the provider registration lists and runtime
files are unchanged from the Wave 0 boundary. Run the public-artifact private
locator checks over the new policy, corpus, and receipts.

Commit each task independently. After the Wave 4 checkpoint, continue into
adapter/controller evidence or Wave 5 static construction where the available
local and Skunkworks runners can produce honest retained evidence.
