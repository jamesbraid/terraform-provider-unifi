# Contributing

See [architecture.md](development/architecture.md) for how the provider is put
together, and [new-controller.md](development/new-controller.md) for a new controller release.

## Build

```
go build ./...
```

## Generate

```
go generate ./...
```

Run after changing a policy file, the pinned go-unifi commit, or a generator
tool, and commit the result; CI fails a PR whose output doesn't match a
clean-tree run.

## Test

```
go test ./...
```

`go test ./unifi` and `go test ./internal/resourcekit` also run every
kit-served resource's conformance instruments.

## Acceptance tests

```
export TF_ACC=1 UNIFI_TEST_HERDER_BIN=/path/to/unifi-emu-herder
go test -tags acceptance ./unifi -count 1 -timeout 900s
```

Non-default Docker socket: set `DOCKER_HOST`/`TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE`.
Against your own controller: set `UNIFI_SKIP_CONTAINER=1` plus `UNIFI_USERNAME`,
`UNIFI_PASSWORD`, and `UNIFI_API`.

## Lint

CI runs `golangci-lint`; run it locally before opening a pull request.
