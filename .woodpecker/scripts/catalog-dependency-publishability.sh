#!/usr/bin/env bash
set -euo pipefail

readonly module_path=github.com/ubiquiti-community/go-unifi
readonly module_version=v1.102.0
readonly module_commit=e255518385e0104eb838be56c2a491de158f3194
readonly module_sum='h1:/CSJB4rm9aqDrkDB4yw6xAgY8dYAohcv8N4TC/ugLRU='
readonly output=${CATALOG_DEPENDENCY_OUTPUT:?CATALOG_DEPENDENCY_OUTPUT is required}

module_json=$(env GOPROXY=off GOSUMDB=off 'GOVCS=*:off' \
    GIT_TERMINAL_PROMPT=0 GOTOOLCHAIN=local \
    go list -mod=readonly -m -json "${module_path}")
readonly module_json
test "$(jq -r .Path <<<"${module_json}")" = "${module_path}"
test "$(jq -r .Version <<<"${module_json}")" = "${module_version}"
test "$(jq -r .Sum <<<"${module_json}")" = "${module_sum}"
test "$(jq -r 'has("Replace")' <<<"${module_json}")" = false

module_cache=${GOMODCACHE:-$(go env GOMODCACHE)}
readonly module_cache
readonly module_dir=$(jq -r .Dir <<<"${module_json}")
readonly version_root=${module_cache}/cache/download/${module_path}/@v
readonly module_zip=${version_root}/${module_version}.zip
readonly module_info=${version_root}/${module_version}.info
test -d "${module_dir}"
test -f "${module_zip}"
test -f "${module_info}"
test "$(jq -r .Origin.Hash "${module_info}")" = "${module_commit}"
test "$(jq -r .Origin.URL "${module_info}")" = "https://github.com/ubiquiti-community/go-unifi"

module_zip_sha256=$(sha256sum "${module_zip}" | awk '{print $1}')
module_dir_sha256=$(
    cd "${module_dir}"
    find . -type f -exec sha256sum {} + | LC_ALL=C sort | sha256sum | awk '{print $1}'
)
provider_commit=$(git rev-parse HEAD)
readonly module_zip_sha256 module_dir_sha256 provider_commit
test "${#module_zip_sha256}" = 64
test "${#module_dir_sha256}" = 64
test "${#provider_commit}" = 40

mkdir -p "$(dirname -- "${output}")"
temporary=$(mktemp "$(dirname -- "${output}")/.catalog-dependency-publishability.XXXXXX")
readonly temporary
cleanup() {
    rm -f "${temporary}"
}
trap cleanup EXIT
jq --compact-output --null-input \
    --arg provider_commit "${provider_commit}" \
    --arg module_path "${module_path}" \
    --arg module_version "${module_version}" \
    --arg module_commit "${module_commit}" \
    --arg module_zip_sha256 "${module_zip_sha256}" \
    --arg module_dir_sha256 "${module_dir_sha256}" \
    '{
        format_version: 1,
        gate: "go-unifi-dependency-publishability",
        result: "pass",
        provider_commit: $provider_commit,
        module_path: $module_path,
        module_version: $module_version,
        module_commit: $module_commit,
        module_zip_sha256: $module_zip_sha256,
        module_dir_sha256: $module_dir_sha256,
        replace_present: false,
        resolution_runner: "remote_ci",
        network_boundary: "remote_ci_only"
    }' >"${temporary}"
chmod 0600 "${temporary}"
mv "${temporary}" "${output}"
trap - EXIT
