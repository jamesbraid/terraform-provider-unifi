# Kitfix Delivery to v0.103.0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reconcile `sweep/kitfix` with `forgejo/main`, remove the dead code the kit migration left behind, get every enforced gate green, and cut release v0.103.0 on the private forge.

**Architecture:** The provider binary is `unifi/` + `internal/resourcekit` + `internal/generated/*` behind `providerserver.Serve`. Everything else in-tree is campaign apparatus (catalog machinery, evidence, Woodpecker pipelines) that is deliberately private, manual, and out of the release path. Delivery = one merge, one small semantic-conflict fix, four deletion passes, gate verification, changelog, tag.

**Tech Stack:** Go 1.25.8, terraform-plugin-framework v1.19.0, go-unifi v1.103.0, golangci-lint v2.12.2 (pinned fast-loop gate), goreleaser (GitHub only — not used here).

**Spec:** Ground-truth review performed 2026-08-23 in this session (summarized in "State of the world" below). The 2026-08-08 public-release plan (`docs/superpowers/plans/2026-08-08-public-provider-release.md`) is historical context only — events superseded it (v0.102.0 was cut on the forgejo line 2026-08-16, not on GitHub).

## State of the world (measured 2026-08-23)

- Branch `sweep/kitfix` is 3 ahead / 7 behind `forgejo/main`. Test-merge: **clean textual merge, builds, generate-clean, exactly 2 test failures** — both in `unifi/write_paths_test.go`, caused by `forgejo/main` moving `client` to the resource kit while our branch pins write-path classifications that predate that move.
- On the branch alone: `go build` ✅, `go vet` ✅, `go test ./...` ✅, fast-loop lint (v2.12.2, 8 pinned linters, zero tolerance) ✅ on merged tree, `go generate ./... && git diff --exit-code` ✅ on merged tree.
- Repo's own `.golangci.yaml` full run: 72 issues (not an enforced gate — GitHub CI is only-new-issues; fast-loop is the 8-linter subset). The 9 `unused` findings are genuine dead code.
- Releases: last public GitHub release **v0.101.2**; **v0.102.0 (2026-08-16) exists only on the forgejo line** (tag on merge cfc2bdf4). No pipeline tags releases on forgejo — a release there is: merge to main + tag + changelog section (update-release-notes automation exists only on GitHub).
- CHANGELOG `[Unreleased]` already documents the bulk of post-v0.102.0 user-visible work (six controller-owned-attribute fixes, dhcp_v6_server, wlan fixes, six declared schema changes, known issue: vpn_server DNS slots 3–4 — SDK-blocked, fix written but untagged).
- Dead-code candidates verified: `unifi/models` (1,214 lines, one importer using exactly 2 funcs), 9 lint-`unused` symbols, `unifi/util/retry` (near-duplicate of `unifi/util`, 1 prod importer), root `main_test.go` (empty table, asserts nothing), 9 zero-consumer `build/` evidence files (m1/m2/m3/m5/m6/m7 receipts + 2 migration-baseline + m0/uos).
- Deliberately KEPT (decision, not oversight): `internal/blastradius` + `internal/testaudit` (zero-importer but live check suites executed by `go test ./...` — they are gates, not dead code); `cmd/export-gate` + `internal/exportgate` (the future public-tree sanitization gate, declared in `knownUnreachableChecks`); `cmd/{policy,list-policy}-scaffold`, `cmd/schema-baseline` (declared hand-run authoring tools); all catalog apparatus wired into `go:generate` or `.woodpecker/`.

## Global Constraints

- **NEVER push to `origin` (github.com/jamesbraid) or `upstream` (ubiquiti-community).** Delivery target is `forgejo` (git.octanix.dev/infra/terraform-provider-unifi) only. Public GitHub publication is a separate James-approval follow-up. (The stray `emdash/delivery-192kb` branch on origin is NOT an invitation to push there.)
- Gates that must be green before tagging, run from repo root:
  1. `go build ./...`
  2. `go vet ./...`
  3. `CGO_ENABLED=1 go test -race -count=1 ./...`
  4. `golangci-lint run --no-config --default=none --enable=ineffassign --enable=makezero --enable=misspell --enable=unconvert --enable=copyloopvar --enable=durationcheck --enable=usetesting --enable=forcetypeassert --timeout 5m ./... --max-issues-per-linter=0 --max-same-issues=0` (local golangci-lint is already the pinned v2.12.2)
  5. `go generate ./... && git diff --exit-code`
- Every deletion is proven by compiler + full test suite, never by grep alone (grep produces candidates; the build arbitrates).
- Version: **v0.103.0** (the fork's line tracks the go-unifi minor: v0.101.x↔1.101.0, v0.102.0↔1.102.0; go.mod pins v1.103.0). Date: 2026-08-23.
- Commit messages follow the repo's existing style: component-prefixed lowercase imperative subject, prose body explaining why (commit-messages skill).
- CHANGELOG sections must terminate with a literal `---` line (`changelog_test.go` enforces; the release-notes awk splits on it).
- Capture exit codes as `out=$(cmd 2>&1); rc=$?` — never `$?` after a pipe (three prior misreads in this repo came from that).

---

### Task 1: Merge forgejo/main and fold in the semantic resolution

**Files:**
- Modify: `unifi/write_paths_test.go` (two edits below)
- Merge: everything `forgejo/main` brings (client kit migration, acceptance harness build tag, scattered-Decode fix, fsync fix)

**Interfaces:**
- Consumes: `clientKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Client]` — arrives from forgejo/main's `unifi/client_descriptor.go:70`; its `Create` is whole-object, its update is `UpdateFields` (masked), so it satisfies both pinned properties.
- Produces: a merged tree where `go test ./unifi` is green; later tasks build on it.

- [ ] **Step 1: Merge without committing**

```bash
git merge --no-ff --no-commit forgejo/main
```
Expected: auto-merge succeeds (verified 2026-08-23 in a scratch worktree; only `unifi/port_forward_descriptor.go` and `unifi/schema_model_agreement_test.go` overlap and both auto-merge).

- [ ] **Step 2: Run the two failing tests to see the failure**

```bash
go test ./unifi -run 'TestEveryKitWritePathIsClassified|TestTheUnmaskedHandWrittenSurfacesAreTheOnesWeThinkTheyAre'
```
Expected: FAIL — "classified 19 write path(s) against 20 kit-served surface(s)" and "unmasked hand-written surfaces = [bgp dynamic_dns power_supervisor setting site wireguard_peer], pinned as [bgp client …]".

- [ ] **Step 3: Classify client's write path**

In `unifi/write_paths_test.go`, inside `TestEveryKitWritePathIsClassified`, insert a block in alphabetical position (before the `clientQosRateKitBackend` block):

```go
	{
		backend := clientKitBackend(api)
		record("client", backend.Create != nil, backend.CreateFields != nil,
			backend.Update != nil, backend.UpdateFields != nil)
	}
```

- [ ] **Step 4: Drop client from the unmasked hand-written pin**

In `TestTheUnmaskedHandWrittenSurfacesAreTheOnesWeThinkTheyAre`, remove the `"client",` line from `want`. (The derivation walks `*_resource.go` for whole-object `client.Update*(` calls; main gutted `client_resource.go`, so the derived list already lost client.)

- [ ] **Step 5: Update the file's header picture**

The doc comment says "update masked on all nineteen kit surfaces" and "create unmasked on eighteen". Change to twenty / nineteen. Client's create is whole-object (a client is genuinely made, not adopted), so device stays the only patching create — no other comment claims change.

- [ ] **Step 6: Verify the package is green**

```bash
go build ./... && go test ./unifi
```
Expected: PASS.

- [ ] **Step 7: Commit the merge**

Commit with a body that names the semantic conflict: main moved client onto the kit; this branch pinned the write-path classification; the merge commit carries both plus the one classification the pin now requires.

---

### Task 2: Delete the lint-dead kit-migration leftovers

**Files:**
- Modify: `unifi/vpn_server_resource.go` (delete types `vpnServerIdentityModel`, `vpnServerListConfigModel`, `vpnServerListFilterModel` at :53–:64)
- Delete or trim: `unifi/vpn_server_wire_fields.go` (69 lines; `vpnServerWireFields` is its only symbol reference in the tree — if the file holds only that func + comments, delete the file)
- Modify: `unifi/wlan_resource.go` (delete types `wlanListConfigModel` :90, `wlanListFilterModel` :96)
- Modify: `unifi/network_conditional_wires_test.go` (delete funcs `wiresWrittenByEncode` :201, `marshalKeys` :223, `sentinelFill` :238)

**Interfaces:**
- Consumes: the merged tree from Task 1.
- Produces: a tree where `golangci-lint run` (repo config) reports zero `unused` findings in `unifi/`.

- [ ] **Step 1: Delete the symbols listed above.** Read each site first; take trailing comment blocks that describe only the deleted symbol.
- [ ] **Step 2: Verify**

```bash
go build ./... && go test ./unifi && golangci-lint run --timeout 5m 2>&1 | grep -c "unused)"
```
Expected: build + tests pass; unused count drops from 9 to ≤ 1 (the remaining `network_data_source.go` finding, if it survives, gets the same treatment).

- [ ] **Step 3: Commit** (`unifi: delete the list models and helpers the kit migration orphaned`).

---

### Task 3: Strip unifi/models to what its one consumer calls

**Files:**
- Modify: `unifi/models/client_info.go` (454 lines — keep `AttributeTypes`, `ClientInfoAttrValues`, and their transitive private deps; delete exports `ClientInfoObjectType`, `ClientInfoObjectValue`, `NewClientInfoObjectType`, `NewClientInfoObjectTypeFromData`, `ClientInfoValue`, `Attributes`, `ClientInfoDataSourceSchema`, `ClientInfoListAttribute`)
- Modify or delete: `unifi/models/client_info_list.go` (96 lines — `ClientInfoListType`, `ClientInfoListValue`, `ClientListValue` all unreferenced outside the package)
- Trim: `unifi/models/client_info_test.go`, `unifi/models/client_info_list_test.go` (664 lines — keep only tests of the two surviving funcs)

**Interfaces:**
- Consumes: sole importer `unifi/client_info_list_data_source.go` calls exactly `models.ClientInfoAttrValues(ctx, &ci)` (:120) and `models.AttributeTypes()` (:121, :130).
- Produces: a `models` package that is only those two functions plus what they need.

This is the hand-written twin of the generated value layer commit 3d4998ac stripped: an `attr.Type`/`attr.Value` object implementation nothing constructs.

- [ ] **Step 1: Delete the unreferenced exports and their private-only helpers.** Work function-by-function; after each removal `go build ./unifi/...`.
- [ ] **Step 2: Trim the two test files to the surviving API.** Delete tests that construct deleted types. Keep any test exercising `AttributeTypes`/`ClientInfoAttrValues`.
- [ ] **Step 3: Verify**

```bash
go build ./... && go test ./unifi/... && go vet ./unifi/models
```
Expected: PASS; `grep -rn "models\." unifi --include="*.go" | grep -v "^unifi/models/"` still shows only the three call sites.

- [ ] **Step 4: Commit** (`unifi/models: strip the hand-written value layer nothing calls`).

---

### Task 4: Fold unifi/util/retry into unifi/util, or record why not

**Files:**
- Inspect: `unifi/util/retry/` (wait.go 116 + state.go + tests ≈ 1,100 lines; `WaitForState`/`Retry` carry `Deprecated:` markers), `unifi/util/` (26 prod importers)
- Modify: the single non-test importer of `unifi/util/retry` (find with `grep -rln 'unifi/util/retry"' unifi --include='*.go' | grep -v _test`)

**Interfaces:**
- Consumes: Task 1's merged tree.
- Produces: either one retry implementation or a one-paragraph record (in the commit that closes this task) of why two must exist.

- [ ] **Step 1: Diff the two packages' overlapping API** (`StateChangeConf`, `RetryContext`, etc.). They are near-duplicates by inspection; measure which functions the single importer uses.
- [ ] **Step 2:** If `unifi/util` provides equivalents: repoint the importer, delete `unifi/util/retry/`, build + test. If the APIs genuinely diverge where the importer stands, leave the package and write the reason into the task-close commit body instead.
- [ ] **Step 3: Verify** `go build ./... && go test ./unifi/...` — PASS.
- [ ] **Step 4: Commit** (`unifi/util: fold the retry duplicate onto the surviving copy` or the recorded-reason variant).

---

### Task 5: Hygiene sweep — unfailable test, stray binaries, orphan evidence

**Files:**
- Delete: `main_test.go` (root; `Test_main` has an empty table and asserts nothing — an unfailable test)
- Check first: `internal/testaudit/unfailable-tests.txt` — if it ledgers `Test_main`, remove that line in the same commit
- Disk only (untracked, gitignored): `rm -f generated-value-strip` (7.1 MB stray build output; `dist/` may stay, it is gitignored working output)
- Evaluate for deletion (zero consumers found 2026-08-23; re-verify each): `build/m1/dns-compiler-receipt.json`, `build/m2/dns-catalog-cutover.json`, `build/m3/dns-operation-receipt.json`, `build/m5/provider-contract-lighthouse.json`, `build/m6/capability-expansion.json`, `build/m7/hil-promotion.json`, `build/migration-baseline/final-go-vs-shell-differential.json`, `build/migration-baseline/m1-dns-compiler.json`, `build/m0/uos-dns-qualification.json`

**Interfaces:**
- Consumes: nothing from other tasks (independent).
- Produces: a tree where every committed artifact has a consumer or a README naming its purpose.

- [ ] **Step 1: Delete `main_test.go`; update the testaudit ledger if it names `Test_main`.**
- [ ] **Step 2: Per orphan evidence file, run the consumer check before deleting:**

```bash
f=build/m5/provider-contract-lighthouse.json
grep -rn "$(basename $f)" --include='*.go' --include='*.yml' --include='*.yaml' --include='*.sh' --include='Makefile' --include='*.json' . | grep -v "^Binary\|^docs/superpowers\|CHANGELOG"
```
Delete only on zero hits (docs/superpowers prose references do not count — those are records about the file, and `check_digest_reproducibility_test.go` pins digests over m0/wave*/release-ready/restricted, NOT these). A file with any Go/CI/json consumer stays. Note: `build/m0/uos-dns-qualification.json` is referenced by `build/m0/network-dns-qualification.json` — trace whether anything reads that reference before touching it; when in doubt, keep and say so.

- [ ] **Step 3: Verify the check suite still passes** (these tests read `build/`):

```bash
go test . ./internal/catalogparity ./internal/upgradeplan ./internal/exportgate
```
Expected: PASS.

- [ ] **Step 4: Commit** (`build: retire the milestone receipts nothing reads`; separate commit for `main_test.go`: `main: delete the test with an empty table`).

---

### Task 6: Full gate run on the finished tree

**Files:** none modified (fix-forward only if a gate reddens).

- [ ] **Step 1–5: Run the five Global Constraints gates in order**, capturing exit codes without pipes. `-race` is the long pole (CI allows 120m; locally expect ~10–20m).
- [ ] **Step 6:** If any gate fails: systematic-debugging skill, fix, re-run that gate plus `go test ./...`, amend or add a commit.

Expected: all five green. This is the release bar; nothing in Tasks 7–9 proceeds red.

---

### Task 7: Acceptance attempt (best effort, honestly reported)

**Files:** none.

Acceptance is the only gate class not yet exercised (GitHub runs it nightly/post-release; forgejo never does). Memory `acceptance-tests-colima-docker-host.md` records the local recipe: TestMain self-manages the controller via docker-compose; needs `DOCKER_HOST` (Colima) and `UNIFI_TEST_HERDER_BIN`.

- [ ] **Step 1:** Read that memory file; check `docker info` / `colima status` and that the herder binary exists.
- [ ] **Step 2:** If the environment is present: `make testacc TEST_TIMEOUT=1h` (background, generous timeout; retry once on timeout as CI does).
- [ ] **Step 3:** Record the verbatim outcome in the final report. If the environment is absent or the run fails for environment reasons, the release proceeds (v0.102.0 set the precedent — no acceptance pipeline exists on the forge) but the report must say exactly what did and did not run. Provider-behaviour failures, by contrast, block: fix or pull the offending change before tagging.

---

### Task 8: Changelog audit and release section

**Files:**
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: the finished tree (Tasks 1–6), `git log v0.102.0..HEAD` (≈470 commits).
- Produces: a `## [v0.103.0] - 2026-08-23` section that `changelog_test.go` accepts.

- [ ] **Step 1: Audit coverage.** List first-parent merge/commit subjects since the tag: `git log --first-parent --oneline v0.102.0..HEAD`. The `[Unreleased]` section already covers the six-defaults fix, dhcp_v6_server, wlan fixes, schema declarations, known issue, and the migration-manifest gate. Check for missing user-visible items — candidates from this branch: client/vpn_client/vpn_server served from the resource kit (behaviour-preserving? verify the conformance tests' claim before writing "no behaviour change"), the ~53k-line dead-code removal (Maintenance), the write-path classification (Maintenance). Anything user-visible and uncovered gets an entry in the repo's long-form style (release-notes + prose-style skills).
- [ ] **Step 2: Cut the section.** Check how v0.102.0 did it (`git log -p --follow -1 v0.102.0 -- CHANGELOG.md` region): retitle `[Unreleased]` → `## [v0.103.0] - 2026-08-23`, terminated by `---`. Recreate an empty `[Unreleased]` only if the v0.102.0 cut did.
- [ ] **Step 3: Verify** `go test . -run TestEveryChangelogSectionIsTerminated` — PASS.
- [ ] **Step 4: Commit** (`changelog: cut v0.103.0`).

---

### Task 9: Deliver on the forge

**Files:** none (git operations).

Current blocker: forgejo HTTPS auth fails locally ("could not read Username"); memory `forge-push-blocked-by-tokenless-tea-login.md` says this is a local credential fault (the forge answers in 46ms). The homelab-access skill owns the fix (op-backed token, tea login).

- [ ] **Step 1:** Load the homelab-access skill; restore forgejo credentials.
- [ ] **Step 2:** Push the branch: `git push forgejo sweep/kitfix`.
- [ ] **Step 3:** Open the PR with tea (base `main`), title/body via pr-descriptions skill, then merge it (repo history shows self-merged PRs are the convention: #5).
- [ ] **Step 4:** Tag the merge commit `v0.103.0` (annotated, message = one-line release name) and `git push forgejo v0.103.0`.
- [ ] **Step 5:** If Step 1 fails after honest attempts: create the tag locally on the finished merge of `sweep/kitfix` into a local `main` fast-forwarded from `forgejo/main`, and end the run reporting the exact three commands James must run. Do NOT route around via origin.
- [ ] **Step 6 (explicitly deferred):** GitHub publication (push main + tag to origin → goreleaser). Requires James's approval per standing rule; the report offers it as the next action.

---

### Task 10: Wrap up

- [ ] **Step 1:** Remove the scratch worktree: `git worktree remove --force <scratchpad>/mergecheck`.
- [ ] **Step 2:** Update memory: delivery record (what shipped in v0.103.0, where the public-release decision stands), correct any memory the run falsified.
- [ ] **Step 3:** Final report: gates table with verbatim results, line-count delta, what was deleted vs deliberately kept, acceptance outcome, and the deferred public-publication decision.

## Self-review (done at write time)

- Spec coverage: every red gate and every verified dead-code candidate from the review has a task; deliberately-kept items are recorded with reasons rather than silently skipped.
- No placeholders: the one code change (Task 1) is written out; deletion tasks name exact symbols/lines measured this session.
- Type consistency: `clientKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Client]` matches forgejo/main's `unifi/client_descriptor.go:70`; the two surviving models funcs match the call sites at `client_info_list_data_source.go:120–130`.

## Postscript (2026-08-23)

Task 4 landed as a trim rather than a fold: the single importer's API needs stayed on `unifi/util/retry`, so the deprecated `Retry`/`WaitForState` wrappers and their unfailable empty-table tests were deleted instead of merging the package into `unifi/util`. Task 5's nine orphan `build/` evidence files were deliberately kept, each with its keep-decision recorded rather than deleted, once the consumer check turned up none for the check suite itself. Task 7 grew past its original best-effort acceptance run into the full acceptance-regression campaign the changelog's v0.103.0 section now describes, since the first live run against a controller found 38 failing tests across the migrated surfaces rather than a clean pass.
