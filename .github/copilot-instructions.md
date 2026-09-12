# Working in this provider

This is the UniFi Terraform provider. It is not a hand-written
plugin-framework provider, and treating it as one is the most common way
to do the wrong thing here. Resources are DERIVED from the controller's
own definitions through a codegen pipeline; you edit policy and judgment,
almost never schema code.

## The pipeline

SDK definitions -> bootstrap -> policy -> compiler -> provider-code-spec ->
tfplugingen-framework -> generated schema + emitted descriptor halves ->
hand judgment.

- The pinned `go-unifi` SDK is the source of field names, wire (JSON) tags,
  Go types and pointer-ness, plus exported constraint and measured-behaviour
  facts (`FieldConstraints`, Min/Max consts, `Values` vars, `behavior.json`).
- `cmd/sdk-bootstrap` reads the SDK structs and constraints into a bootstrap
  JSON. `cmd/provider-spec-compiler` combines that with the hand-written
  policy in `provider-codegen/policy/*.json` and emits provider-code-spec.
- HashiCorp's `tfplugingen-framework` generates the schema from that spec.
  We use the official generator for the half it is good at.
- `cmd/descriptor-emitter` emits the mechanical half of each kit descriptor
  (field wiring, models, attr-type maps). Hand descriptor files carry only
  judgment: hooks, Backends, section semantics, deliberate divergences.
- `resourcekit` (internal/) is the engine every resource composes.
- `go generate ./...` rebuilds all of it and must be byte-identical on a
  re-run; CI fails on any drift.

## The rule that governs everything

No hand-carried fact the SDK already derives or measures. Validators come
from the SDK's exported constraints, sensitivity from its declared list,
required-on-create and empty/clearing semantics from `behavior.json`. If
you find yourself transcribing a bound, an enum, or a controller behaviour
into policy by hand, stop -- either the SDK exports it (derive) or it is a
capture-stage requirement (ask), not a constant to type.

Policy IS yours: expose/omit, attribute names, descriptions. Those are
decisions the controller cannot make. They live in `provider-codegen/policy`.

## Why the front half exists (do not "simplify" it away)

The provider's correctness model is keyed on WIRE names and is
pointer-ness-aware: masked writes name wire fields, the conformance
censuses key on them, `behavior.json` keys on them, and `*int64` vs
`int64` decides `OmitZero`. The provider-code-spec format has no slot for
a wire name distinct from the attribute name, nor for pointer-ness -- it
describes a Terraform schema, not a controller wire contract. That is why
the bootstrap reads the SDK structs rather than consuming a spec. The
missing artifact all along was a wire contract (now the SDK's
`behavior.json` + `FieldConstraints`), not a schema spec.

## Gates before anything merges

- `unifi/testdata/schema-snapshot.json` byte-identical unless you intend a
  schema change, and then the diff is exactly that change.
- The conformance instruments in `go test ./unifi ./internal/resourcekit`
  pass with assertions UNCHANGED. If an instrument disagrees with your
  work, your work is wrong; do not edit the assertion to fit.
- `go generate ./...` byte-identical on a double run.
- Acceptance (`-tags acceptance`) proves live behaviour against the pinned
  emulated controller; run it for anything that touches a write path.

## Where to look

- A resource's shape: `unifi/<name>_descriptor.go` (judgment) +
  `unifi/<name>_descriptor_gen.go` (emitted).
- Its policy: `provider-codegen/policy/<name>.json`.
- The engine: `internal/resourcekit`.
- The tools: `cmd/` (sdk-bootstrap, provider-spec-compiler,
  descriptor-emitter, list-resource-gen).
- Architecture rationale in depth: `development/architecture.md`.
