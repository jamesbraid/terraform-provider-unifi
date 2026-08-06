#!/usr/bin/env bash
set -euo pipefail

readonly script_directory=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
readonly work_root=$(mktemp -d /tmp/bootstrap-go-unifi-proxy-test.XXXXXX)
cleanup() {
    chmod -R u+w "${work_root}" 2>/dev/null || true
    rm -rf "${work_root}"
}
trap cleanup EXIT

mkdir -p "${work_root}/source"
git -C "${work_root}/source" init --quiet
printf '%s\n' 'module github.com/ubiquiti-community/go-unifi' 'go 1.25.0' \
    >"${work_root}/source/go.mod"
printf '%s\n' 'package fixture' >"${work_root}/source/fixture.go"
git -C "${work_root}/source" add go.mod fixture.go
GIT_AUTHOR_NAME='module proxy fixture' \
GIT_AUTHOR_EMAIL='fixture@example.invalid' \
GIT_COMMITTER_NAME='module proxy fixture' \
GIT_COMMITTER_EMAIL='fixture@example.invalid' \
    git -C "${work_root}/source" commit --quiet -m fixture
git -C "${work_root}/source" tag v1.102.0
readonly commit=$(git -C "${work_root}/source" rev-parse HEAD)

GO_UNIFI_SOURCE_ROOT="${work_root}/source" \
GO_UNIFI_PROXY_ROOT="${work_root}/proxy" \
GO_UNIFI_EXPECTED_COMMIT="${commit}" \
GO_UNIFI_EXPECTED_SUM=skip \
    bash "${script_directory}/bootstrap-go-unifi-proxy.sh"

readonly module_json=$(cd "${work_root}" && env GOMODCACHE="${work_root}/cache" \
    GOPROXY="file://${work_root}/proxy" GOSUMDB=off 'GOVCS=*:off' \
    GIT_TERMINAL_PROMPT=0 GOTOOLCHAIN=local \
    go mod download -json github.com/ubiquiti-community/go-unifi@v1.102.0)
test "$(jq -r .Path <<<"${module_json}")" = github.com/ubiquiti-community/go-unifi
test "$(jq -r .Version <<<"${module_json}")" = v1.102.0
