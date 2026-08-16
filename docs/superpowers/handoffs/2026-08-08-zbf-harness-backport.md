# Handoff: backport ZBF fixture improvements to go-unifi

Date: 2026-08-08. Owner: next go-unifi revision after v1.102.0 publishes at its
canonical origin. Do not act on this before the provider release qualifies —
any go-unifi commit mints a new module version and invalidates the locked
v1.102.0 evidence chain (module zip sha, dependency-publishability receipt,
go.mod pins).

## What diverged

The provider's `internal/controllertest.MigrateZoneBasedFirewall` gained,
during the 2026-08-08 release push, capabilities go-unifi's controllertest
fixture does not have:

- an API-style probe (GET `/`, 200 = UniFi OS console paths, 302 = standalone
  Network controller paths) so the helper works against both controller
  generations instead of hardcoding one login/migrate path;
- the migrate POST body as the JSON literal `null`, matching the proven call
  in go-unifi's `cmd/fields` drift harness (`migrateZoneBasedFirewall`,
  drift_integration_test.go) rather than a zero-byte body;
- a post-migration zone-collection read-back that fails loudly when the
  controller answers 204 without migrating (the documented no-op migration
  service on non-mongo DB modes) instead of trusting the status code;
- response-body snippets in every error so controller-side failures
  self-diagnose in CI logs.

go-unifi's own knowledge of these semantics lives in its drift harness, not
its controllertest fixture; its `internal/controllertest/session.go` hardcodes
the classic `/api/login` path.

## The obligation

The two controllertest packages are documented as "same shape, must not
drift" (provider `internal/controllertest` package doc). Backport the fixture-
level migrate-and-verify helper into go-unifi's controllertest (or hoist the
drift harness helper there) so both repositories drive controllers through
one proven code shape. Provider side then reconciles on the next go-unifi
version bump.
