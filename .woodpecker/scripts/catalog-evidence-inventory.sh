#!/usr/bin/env bash
set -euo pipefail

repository_root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repository_root
policy=${repository_root}/provider-codegen/policy/catalog-evidence.json
readonly policy
output=${CATALOG_EVIDENCE_OUTPUT:-${repository_root}/build/release-ready/catalog-evidence-inventory.json}
readonly output

work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-evidence.XXXXXX")
readonly work_root
cleanup() {
    rm -rf "${work_root}"
}
trap cleanup EXIT
mkdir -p "${work_root}/released"

released_commit=$(jq -r .released_provider.commit "${policy}")
readonly released_commit
test "$(git -C "${repository_root}" rev-parse 'v0.101.2^{commit}')" = "${released_commit}"
git -C "${repository_root}" archive --format=tar v0.101.2 | tar -xf - -C "${work_root}/released"

sha256_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

module_cache=${GOMODCACHE:-$(go env GOMODCACHE)}
download_root=${CATALOG_GO_UNIFI_DOWNLOAD_ROOT:-${module_cache}/cache/download/github.com/jamesbraid/go-unifi/@v}
readonly module_cache download_root
for version in released candidate; do
    module_version=$(jq -r ".sdk.${version}_version" "${policy}")
    expected_sha256=$(jq -r ".sdk.${version}_archive_sha256" "${policy}")
    archive=${download_root}/${module_version}.zip
    test -f "${archive}"
    test "$(sha256_file "${archive}")" = "${expected_sha256}"
done

(
    cd "${repository_root}"
    env GOPROXY=off GOSUMDB=off 'GOVCS=*:off' GIT_TERMINAL_PROMPT=0 \
        GOFLAGS=-mod=readonly GOTOOLCHAIN=local \
        GOCACHE="${GOCACHE:-${work_root}/go-build}" \
        go run ./cmd/catalog-evidence \
        -baseline build/m0/provider-schema-digests.json \
        -contracts provider-codegen/generated/catalog-surface-contracts.json \
        -policy provider-codegen/policy/catalog-evidence.json \
        -released-root "${work_root}/released" \
        -candidate-root . \
        -output "${output}"
)
