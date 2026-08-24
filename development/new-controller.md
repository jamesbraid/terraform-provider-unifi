# Adding support for a new controller release

This is the playbook for moving the provider onto a new UniFi Network
controller release: what has to change, in what order, and how to tell the
result is actually correct before it ships. It spans three repositories —
[go-unifi](https://github.com/ubiquiti-community/go-unifi) (the Go SDK this
provider is built on), this provider, and the emulated controller image the
acceptance suite runs against — and answers the question "can someone outside
this project reproduce a controller bump." Read
[architecture.md](architecture.md) first if you have not; this document
assumes you know what a descriptor, a bootstrap, and a policy file are.

## 1. go-unifi: regenerate the SDK

The provider never talks to a controller's HTTP API directly — every field it
reads or writes goes through a generated Go struct in go-unifi. A new
controller release changes that generation's input, so the first step happens
in that repository, not this one: regenerate the SDK structs from the new
release's field definitions, and tag it.

This provider's `go.mod` tracks a fork of go-unifi (see the root `README.md`
for why, and the `go mod edit -replace` invocation that moves it). Nothing
downstream of this step can start until a tag exists to depend on.

## 2. Provider: regenerate the schema

With the new go-unifi tag available:

1. **Bump the dependency.**

   ```
   go mod edit -replace github.com/ubiquiti-community/go-unifi=github.com/jamesbraid/go-unifi@vX.Y.Z
   go mod tidy
   ```

2. **Re-derive bootstraps for every affected surface.** Each surface's
   `provider-codegen/bootstrap/*.json` is generated from the SDK struct by
   `cmd/sdk-bootstrap`, invoked through the `go:generate` directives in
   `provider-codegen/generate.go`. Re-running generation (step 4) regenerates
   every bootstrap from the new SDK commit; a struct whose fields changed
   shape produces a different bootstrap, which is the signal that surface
   needs attention below.

3. **Adjust policy.** This is the one step that takes human judgment and the
   one no tool in this pipeline attempts to automate: for each surface whose
   bootstrap changed, open `provider-codegen/policy/<surface>.json` and decide
   what a new, renamed, or removed SDK field means for the Terraform schema —
   expose it as a new attribute, fold it into an existing one, or deliberately
   omit it. Every surface here already serves a generated schema, so edit the
   policy directly.

   Deliberately omitting a field with nothing else to say about it is a
   one-line append to the policy's `omitted` list, not a new object in
   `fields`:

   ```json
   "omitted": ["new_field_name"]
   ```

   `omitted` is only for that bare case. A field that takes an `attribute`, a
   `semantic_id`, or per-member decisions of its own — omitted or not —
   still goes in `fields`, the same as before.

4. **Regenerate.**

   ```
   go generate ./...
   ```

   This re-runs the whole pipeline described in
   [architecture.md](architecture.md#the-generated-schema-relationship) —
   bootstraps, policy compilation, `tfplugingen-framework`, and the small
   tools that patch its output — and writes the result under
   `internal/generated/`. Review the diff the way you would review any other
   generated-code change: a surface with no policy change should regenerate
   byte-identical.

5. **Update descriptors.** A new attribute needs a new `Field` entry (or, for
   a value the schema alone can't express, a hook — see
   [architecture.md](architecture.md#the-three-hooks)) in the surface's
   `unifi/<surface>_descriptor.go`. A field that changed shape (renamed on the
   wire, became a pointer, changed unit) needs its existing entry updated.
   Run the conformance instruments for that surface
   (`go test ./unifi -run <Surface>`) before writing a single acceptance
   test — they catch a wrong `Elide`, a wrong wire name, or an unmodelled
   force-emitted nested member without needing a controller at all.

6. **Update the golden schema snapshot.**

   ```
   UPDATE_SCHEMA_SNAPSHOT=1 go test ./unifi/ -run TestProviderSchemaSnapshot
   ```

   Review the diff to `unifi/testdata/schema-snapshot.json` the same way you
   would review a schema change to any released provider: it is the record of
   what changed and the thing a future run diffs against. Commit it alongside
   the descriptor and policy changes that caused it.

## 3. Validate

**Unit and conformance suites** need no controller:

```
go build ./...
go test ./unifi ./internal/resourcekit
```

**A new controller image.** The acceptance suite runs against a pinned,
emulated controller rather than a live one — `docker-compose.yaml` pins the
image tag (`UNIFI_TEST_CONTROLLER_IMAGE`, defaulting to a tag matching the
go-unifi commit the SDK was generated from) and CI separately pins the
version of `unifi-emu-herder`, which drives the simulated devices the
controller adopts. Both need to move together with a controller bump: build
and publish an image for the new release, then update the default tag in
`docker-compose.yaml` and, if the new release needs device behavior the
emulator doesn't yet simulate, the herder pin in
`.github/workflows/acctest.yaml`.

**The acceptance run.** Each invocation boots its own controller and takes a
few minutes:

```
export DOCKER_HOST=unix://$HOME/.colima/default/docker.sock \
  TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
  UNIFI_TEST_HERDER_BIN=/path/to/unifi-emu-herder \
  TF_ACC=1
go test -tags acceptance ./unifi -count 1 -timeout 900s
```

`-tags acceptance` is mandatory — the harness that starts a controller lives
behind that build tag on purpose (see
`unifi/provider_acceptance_harness_test.go`), so a plain `go test ./unifi`
never pulls in a container runtime at all, and `TF_ACC=1 go test ./unifi`
without the tag refuses to run at all rather than silently skipping every
acceptance test (`unifi/provider_acceptance_stub_test.go`). To validate
against a controller you started yourself instead of the harness's own
docker-compose lifecycle — real hardware, or a manually run instance of the
new image — set `UNIFI_SKIP_CONTAINER=1` alongside `UNIFI_USERNAME`,
`UNIFI_PASSWORD`, and `UNIFI_API` pointing at it (`unifi/provider_test.go`).

## Gate before shipping a controller bump

1. `go build ./...`
2. `go test ./unifi ./internal/resourcekit` — every conformance instrument and
   unit test green.
3. `go generate ./...` produces no diff against the committed tree (this is
   what CI's `generate` job checks on every change; a controller bump is the
   change most likely to fail it).
4. The full acceptance suite, green against the newly pinned controller
   image.
5. `unifi/testdata/schema-snapshot.json` updated and reviewed if the served
   schema changed at all, committed in the same change as the descriptor
   updates that caused it.

That is the whole answer to "can the next controller release be supported
from what is published here": every step above runs from this repository, the
go-unifi tag it depends on, and a publicly published emulated-controller
image — nothing else.
