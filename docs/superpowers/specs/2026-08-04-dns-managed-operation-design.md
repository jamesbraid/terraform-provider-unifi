# DNS managed operation design

## Scope

Milestone 3 migrates only `unifi_dns_record` behind a provider-local backend.
Its Terraform name, schema, identity, state version, import grammar, list
surface, timeouts, and lifecycle orchestration remain unchanged. The backend
continues to use the existing private Network API. It does not select a
transport at runtime and never retries a failed write through another path.

The provider advances its existing `github.com/jamesbraid/go-unifi`
replacement from v1.101.0 to v1.102.0. That release contains the named-field
write support required for an explicit patch without coupling the provider to
the unpublished local catalog branch.

## Boundary and data flow

An unexported `dnsRecordBackend` interface owns create, read, update, delete,
and list calls for this resource. `dnsRecordModel` represents controller state,
`dnsRecordIntent` represents create ownership, and `dnsRecordPatch` carries an
ID, values, and the exact managed fields present in an update. The concrete
private backend translates those types to `go-unifi` v1.102.0 calls.

Create retains the legacy full create request. Read, delete, and list retain
their legacy private API calls. Update derives its mask before merging plan
values into state. Required and defaulted values are selected when known.
optional null or unknown plan values are omitted, matching the existing
preserve behavior. `UpdateDNSRecordFields` then writes only the selected
provider-owned wire fields. No catalog or policy artifact is loaded at runtime.

The resource stores only the backend and default site after configuration.
This keeps lifecycle code independent of the transport implementation and
allows the tests to compare a legacy full-write adapter with the normalized
adapter over the same in-memory controller.

## Errors and compatibility

The backend returns controller errors without fallback. The resource preserves
the existing not-found behavior by recognizing `go-unifi`'s `NotFoundError`.
Unknown or empty patch field names fail before a request. A failed update leaves
Terraform state untouched, as it does today.

The provider's schema and generated compiler artifacts must remain byte
identical. State produced through either adapter must normalize to the same
Terraform model. The v1.102.0 dependency change is accepted only with complete
provider tests, deterministic regeneration, Terraform/OpenTofu schema parity,
and DNS lifecycle qualification against the locked target.

## Verification and rollback

Unit tests pin Model -> Intent -> Patch conversion, optional-field presence,
the exact named-field request, no write fallback, and legacy/new normalized
outcome parity. Existing resource tests pin schema, import, state upgrade, and
model conversion. The M1 executable gate rebuilds the provider and checks both
CLI schema projections.

The locked controller campaign covers create, update, refresh, delete, import,
no-op plan, state upgrade, restart, and persisted refresh. Rollback changes the
resource constructor back to the legacy backend while retaining v1 state and
the same public schema. No state migration is needed.
