# Public provider release plan

Scope: the provider only. Out of scope by direction: ubitofu, the release
carrier, the ansible fleet soak, and the catalog release-ready campaign.
Nothing is built for release or published before the Phase 6 review.

## The reframe

The release payload is not this branch. `origin/main` (`26bcad84`) is
`v0.101.2` plus one CI commit and is an ancestor of HEAD, so what ships is
`origin/main..HEAD`, and in provider terms that is six files:

```
internal/generated/resource_dns_record/dns_record_resource_gen.go  new, 120
unifi/dns_record_backend.go                                        new, 194
unifi/dns_record_resource.go                                       221 touched
unifi/device_resource.go                                            +42
unifi/port_action.go                                                40 touched
unifi/firewall_zone_resource.go                                      +3
                                              6 files, +451 / -169
```

Five net commits, whose messages already read as public work. Since
`v0.101.2` there are zero changes to `docs/`, `examples/`, or the schema,
which is consistent with the proven schema parity.

Four user-visible behaviours:

1. `unifi_port` compared a value against a pointer, so every invocation
   appended a duplicate port override instead of replacing one. A real bug.
2. `unifi_firewall_zone` delete treats not-found as success.
3. `unifi_device` keeps a planned name across the post-write read and
   asserts adoption after create.
4. `unifi_dns_record` moves to a generated schema and a normalized backend
   doing masked field writes. This is the only consumer of the forked SDK's
   `UpdateDNSRecordFields`.

## Scope: three tiers

**Ships.** `unifi/` runtime and tests, `internal/generated/resource_dns_record`,
`internal/controllertest`, `docs/`, `examples/`, `templates/`, `.github/`,
`.goreleaser.yml`, `Makefile`, README, CHANGELOG, go.mod/go.sum, lint and
codecov config, registry manifest.

**Stays private and frozen.** The apparatus: `internal/catalogparity`,
`releasequalification`, `providercompiler`, `managementcontract`,
`paritydiff`, `schemabaseline`, the ten `cmd/catalog-*` tools,
`provider-codegen/`, `provider-contracts/`, `build/`, `.woodpecker/`,
`docs/superpowers/`, `docs/architecture/`. About 16,000 lines built to prove
450 lines.

Publishing it would triple the review surface of the change, drag ten `main`
packages and two codegen dependencies into the public module, and oblige
every outside contributor to keep receipts and ledgers consistent. Extracting
it to its own repository is real work with real coupling and buys nothing for
this release. Freeze it instead: tag the private branch and stop developing
there.

Trust is better served by conclusions plus one reproducible recipe than by
shipping machinery nobody outside can run: publish the canonical schema
digest and the four commands that reproduce it, publish the deterministic
build recipe and the resulting binary digests, and state the campaign results
as claims with the method described.

**Never publishes.** `build/restricted/`, and anything naming the carrier,
ubitofu, or the fleet soak.

## Dependency

`go.mod` requires `github.com/ubiquiti-community/go-unifi v1.102.0` with no
`replace`. That version does not exist at that path; it resolves here only
because the module cache and the internal proxy are primed. Nobody outside
can build this tree.

Upstream's SDK is not an escape route: the DNS backend calls
`UpdateDNSRecordFields`, which no canonical version has.

So, in go-unifi: rename the module to `github.com/jamesbraid/go-unifi`
including `cmd/fields/api.go.tmpl`, which emits imports into generated code
and will otherwise reintroduce the old path on every regeneration. Tag
`v1.103.0`; `v1.102.0` is burnt at both paths because the proxy caches the
failure. Before tagging, resolve a branch pseudo-version through
`proxy.golang.org` to prove the tag will resolve.

The gate that matters, run with a cold cache and no private proxy:

```
GOMODCACHE=$(mktemp -d) GOFLAGS=-mod=readonly \
GOPROXY=https://proxy.golang.org,direct GOSUMDB=sum.golang.org \
  go mod download github.com/jamesbraid/go-unifi@v1.103.0
```

If that needs a private proxy, `GOPRIVATE`, or a `replace`, the provider is
not publicly releasable. Internal forgejo paths are fine for iterating; they
must not appear in the released `go.mod`.

Rename the provider's own module path too, in the same commit, and update
`main.go`'s served address.

## Branch strategy: merge once, replay five

Do not rebase 150 commits. They are apparatus, and rebasing them produces a
tree that is not shipped.

**Step A.** Branch off `origin/main`, merge `upstream/main`.

The cost was measured with a throwaway merge on 2026-08-08 and is much lower
than predicted, because the base is `origin/main` (v0.101.2) rather than the
work branch. Earlier estimates of seven conflicts came from merging into
HEAD, which is not what this does.

Measured: **four conflicted files, five hunks.**

| file | hunks | nature |
|---|---|---|
| `unifi/device_resource.go` | 3 | upstream's UDM payload fix and port-override set modelling |
| `unifi/device_resource_test.go` | 1 | matching test changes |
| `go.sum` | 1 | dependency lines |
| `CHANGELOG.md` | — | upstream's `Unreleased` section against the fork's `v0.101.x` block |

Auto-merged but semantically overlapping, so each still needs reading:
`unifi/port_profile_resource.go`, `unifi/setting_resource.go`,
`unifi/wlan_resource.go` and their tests.

The overlapping fixes are distinct bugs, not duplicate upstreaming — keep
both sides. `go.sum` resolves by regenerating rather than hand-merging.
`CHANGELOG.md` keeps both sections with the fork's block on top and a note
that the version lines are independent.

Land as its own pull request with its own acceptance run, released as
`v0.101.3`. Doing this first and alone is the single biggest risk reduction
available, and at four conflicts it is roughly a half-day rather than the one
to two days first estimated.

**Step B.** Branch off that merge and replay seven commits: the module path
rename; the DNS backend; the device name preservation; the port override
fix; the firewall zone delete; the fixture changes the acceptance tests
genuinely need; docs. Split the port and zone fixes apart — they are
unrelated bugs currently sharing a commit.

Sanitization then holds by construction: none of the private material is ever
on the branch, so the checker proves a property rather than filtering a mess.
Triage the 24 test files individually — tests covering the five behaviours
ship, tests that exist to feed the campaign do not.

## Sanitization

Fix what ships and is wrong: the README documents a `replace` that no longer
exists and points its badges and install instructions at upstream; `main.go`
hardcodes the upstream registry address; `FUNDING.yml` names upstream's
maintainer.

The existing confidentiality gate cannot be reused. It ends by emitting four
literal `true` values, two of which — the private identifier scan and the
provenance review — correspond to no code at all, and the release qualifier
then checks that those constants are true. Its symlink, file-type, MIME and
gitleaks checks are worth porting; its receipt is not.

Write `scripts/check-public-tree.sh` in the provider repo. It operates on
`git archive HEAD` rather than the working tree, asserts the denied paths are
absent, runs a committed denylist with per-term permitted-path globs, scans
commit messages over the published range, keeps the symlink and file-type and
MIME checks, runs gitleaks, and exits 0 or 1 without emitting a receipt.

Ship `scripts/check-public-tree_test.sh` alongside it: for each denied term
and path, build a tree containing exactly that violation and assert the
checker fails. That self-test is the difference between a gate and a
decoration.

## Verification

**Public.** A stranger with a clean checkout must be able to run `go mod
download`, `go build ./...`, `go test ./...`, `golangci-lint run`, `go
generate ./... && git diff --exit-code`, and `make testacc` if they have
Docker and the public images. Publish the schema reproduction recipe and its
expected digest.

**Private, schema parity.** Re-run the existing gate with the baseline
rebound from `v0.101.2` to the Step A merge commit. Keeping the old baseline
would fail for the wrong reason, because upstream added schedules and a
write-only key. Rebound, "released versus candidate schema is byte-identical"
becomes a statement about this change.

**Private, controller differential.** Same rebinding, same expected result.

**A/B real-controller dry run.** Provider only, no ubitofu, after schema
parity passes, with human sign-off.

Released binary is the published `v0.101.2` archive verified against its
`SHA256SUMS` — what users actually have, not a rebuild. Candidate is a
deterministic build made twice and compared. Copy the config and a state
snapshot into one `mktemp -d`; never point at the live backend; neutralize
any remote backend with an override and assert none survives.

Per side, twice:

```
terraform -chdir=$w plan -refresh-only -lock=false -input=false -no-color \
          -out=$w/$side.$run.tfplan
```

`-refresh-only` cannot propose changes, `-lock=false` never takes the state
lock, `-out` keeps the plan inside the scratch directory, and apply is never
invoked. Verify the state digest is unchanged after each run rather than
assuming it.

Two runs per side is not optional: a live controller returns volatile fields,
and without a same-side determinism check you cannot separate provider
divergence from controller noise. Derive the volatile allowlist from the
observed released-side delta, not from a guess.

Assert: both sides propose only no-op and read; each side is deterministic
across its two runs; the drift address sets match; and for every address
outside `unifi_dns_record` and `unifi_device` the normalized before/after are
byte-identical between sides. That last one is the core claim — the two
providers read the world identically everywhere this change did not touch.
Print the DNS and device diffs for sign-off; the expected answer is no
difference, since both changes are on write paths.

## Release mechanics

Version `v0.102.0`. The fork's line is already independent; do not renumber
to align with upstream, and say in the README that the numbers are not
comparable. If the upstream merge ships separately, tag it `v0.101.3` so the
`v0.102.0` notes contain only the five behaviours.

Replace goreleaser's `go mod tidy` prehook with `go mod verify` — a release
must not be able to rewrite its own manifest. Confirm the signing secrets
resolve and that the assets really include a detached signature.

Registry publication requires the namespace to match the account, so
`jamesbraid/unifi`, and that forces every user to run `state
replace-provider`. Registry versions cannot be unpublished, so do the GitHub
release first and publish to the registry as a separate, later action.

## Sequence

Phase 0, owner decisions, blocking. Phase 1, go-unifi publishable, about a
day, parallel with Phase 2. Phase 2, upstream catch-up merge, one to two
days, highest risk, parallel with Phase 1. Phase 3, compose the candidate,
about a day plus half a day of test triage. Phase 4, sanitization gate,
written in parallel with Phase 3 and run after it. Phase 5, verification, two
to three days wall time. Phase 6, owner review checkpoint, blocking. Phase 7,
release. Phase 8, archive the private branch.

Critical path is 2 to 3 to 4 to 5 to 6 to 7, with 1 landing before 3. Roughly
six to nine working days plus review.

## Decisions taken (2026-08-08)

1. **Distribution: publish to the Terraform Registry as `jamesbraid/unifi`.**
   `main.go`'s served address changes accordingly, and the release notes must
   carry the `terraform state replace-provider` migration prominently, because
   every existing user has to run it. Registry versions cannot be withdrawn,
   so the assets are verified before submission.
2. **Rename the provider module path** to match the repository. Required by
   the registry decision and correct regardless.
3. **Upstream lands as its own release first.** Merge `upstream/main`, ship it
   as `v0.101.3` containing only upstream's fixes, then build `v0.102.0` on
   top carrying only the four behaviours. Isolates the risky merge and keeps
   the release notes honest.
4. **Acceptance suite stays public, documented with a caveat.** Verified: the
   controller image is anonymously pullable, `jamesbraid/unifi-emu` is public
   MIT, and the image is built from `jamesbraid/unifi-containers`, which is
   public MIT ("version-pinned multi-arch UniFi controller images as test
   targets"). So the pull is reviewable. Document what the suite pulls, name
   the source repository, and state that the image packages Ubiquiti's
   Network Application which this project does not relicense. Pull requests
   run unit, lint, build and generate; acceptance runs on schedule and on
   release.
5. **The apparatus stays private and frozen.**
6. **Publish the canonical schema digest** as a release asset.

## Risks

The upstream merge in `device_resource.go` is the highest risk; isolating it
in its own pull request is the mitigation. Burning `v1.103.0` the way
`v1.102.0` was burnt is avoided by resolving a pseudo-version first. Registry
publication is irreversible per version. Live-controller noise can swamp the
A/B comparison, which the two-runs-per-side rule addresses. Test triage could
leak campaign scaffolding, which the path-absence assertions catch.
