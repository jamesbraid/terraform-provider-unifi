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
git -C "${work_root}/source" tag v1.103.0
readonly commit=$(git -C "${work_root}/source" rev-parse HEAD)

GO_UNIFI_SOURCE_ROOT="${work_root}/source" \
GO_UNIFI_PROXY_ROOT="${work_root}/proxy" \
GO_UNIFI_EXPECTED_COMMIT="${commit}" \
GO_UNIFI_EXPECTED_SUM=skip \
    bash "${script_directory}/bootstrap-go-unifi-proxy.sh"

# Assigned first and made readonly on its own line, as the production script
# does at bootstrap-go-unifi-proxy.sh:67-71.
#
# `readonly x=$(cmd)` DOES NOT TRIP set -e. The exit status of the whole
# statement is readonly's, not the substitution's, so a failing go mod download
# was reported as a success and execution continued:
#
#   bash -c 'set -euo pipefail; readonly x=$(false); echo SURVIVED'  -> SURVIVED, rc 0
#   bash -c 'set -euo pipefail; x=$(false); echo NOPRINT'            -> rc 1
# Captured with `if !` rather than left to set -e. set -e would abort here
# correctly but SILENTLY, and a gate whose whole defect was reporting nothing
# should not fail the same way it used to pass.
if ! module_json=$(cd "${work_root}" && env GOMODCACHE="${work_root}/cache" \
    GOPROXY="file://${work_root}/proxy" GOSUMDB=off 'GOVCS=*:off' \
    GIT_TERMINAL_PROMPT=0 GOTOOLCHAIN=local \
    go mod download -json github.com/ubiquiti-community/go-unifi@v1.103.0); then
    echo "go mod download could not fetch the module from the proxy this test" >&2
    echo "  just built. THE PROXY IS BROKEN, not this check. It said:" >&2
    printf '%s\n' "${module_json}" | sed 's/^/    /' >&2
    exit 1
fi
readonly module_json
test "$(jq -r .Path <<<"${module_json}")" = github.com/ubiquiti-community/go-unifi
test "$(jq -r .Version <<<"${module_json}")" = v1.103.0
# .Sum is asserted because it is the only field here that a FAILED download does
# not emit. go mod download -json prints .Path and .Version even when it exits 1
# -- they are echoed from the argument, not learned from the proxy -- so the two
# assertions above pass just as readily on a proxy that served nothing:
#
#   {"Path": "...", "Version": "v1.103.0", "Error": "module lookup disabled ..."}
#
# Relying on set -e alone would make this check correct but silent about why,
# and would break again the moment someone collapses the assignment above.
# Asserting a field that only exists on success makes the assertions themselves
# discriminate.
if [ -z "$(jq -r '.Sum // empty' <<<"${module_json}")" ] ||
    [ -n "$(jq -r '.Error // empty' <<<"${module_json}")" ]; then
    echo "the proxy did not serve the module; this gate is reporting the PROXY" >&2
    echo "  as broken, not itself. go mod download said:" >&2
    jq . <<<"${module_json}" 2>/dev/null | sed 's/^/    /' >&2 ||
        printf '    %s\n' "${module_json}" >&2
    exit 1
fi
