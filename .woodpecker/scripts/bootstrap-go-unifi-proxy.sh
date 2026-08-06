#!/usr/bin/env bash
set -euo pipefail

readonly module_path=github.com/ubiquiti-community/go-unifi
readonly module_version=v1.102.0
readonly source_url=https://github.com/jamesbraid/go-unifi.git
readonly expected_commit=${GO_UNIFI_EXPECTED_COMMIT:-e255518385e0104eb838be56c2a491de158f3194}
readonly expected_sum=${GO_UNIFI_EXPECTED_SUM:-h1:/CSJB4rm9aqDrkDB4yw6xAgY8dYAohcv8N4TC/ugLRU=}
readonly source_root=${GO_UNIFI_SOURCE_ROOT:-/tmp/go-unifi-v1.102.0-source}
readonly proxy_root=${GO_UNIFI_PROXY_ROOT:-/tmp/go-unifi-proxy}

case ${source_root} in
    /*) ;;
    *) echo "source root must be absolute" >&2; exit 2 ;;
esac
case ${proxy_root} in
    /tmp/*) ;;
    *) echo "proxy root must be under /tmp" >&2; exit 2 ;;
esac
test "${proxy_root}" != /tmp
test "${proxy_root}" != /

if [[ ! -d ${source_root}/.git ]]; then
    test "${source_root}" = /tmp/go-unifi-v1.102.0-source
    git clone --branch "${module_version}" --depth 1 "${source_url}" "${source_root}"
fi

test "$(git -C "${source_root}" rev-parse HEAD)" = "${expected_commit}"
test "$(git -C "${source_root}" rev-parse "${module_version}^{commit}")" = "${expected_commit}"
test "$(awk '$1 == "module" {print $2; exit}' "${source_root}/go.mod")" = "${module_path}"
test -z "$(git -C "${source_root}" status --porcelain --untracked-files=no)"

rm -rf "${proxy_root}"
readonly version_root=${proxy_root}/${module_path}/@v
mkdir -p "${version_root}"
cp "${source_root}/go.mod" "${version_root}/${module_version}.mod"
git -C "${source_root}" archive \
    --format=zip \
    --prefix="${module_path}@${module_version}/" \
    --output="${version_root}/${module_version}.zip" \
    "${module_version}"
printf '%s\n' "${module_version}" >"${version_root}/list"
printf '%s\n' \
    "{\"Version\":\"${module_version}\",\"Time\":\"2026-08-03T04:59:13Z\",\"Origin\":{\"VCS\":\"git\",\"URL\":\"https://github.com/ubiquiti-community/go-unifi\",\"Hash\":\"${expected_commit}\",\"Ref\":\"refs/tags/${module_version}\"}}" \
    >"${version_root}/${module_version}.info"

validation_cache=$(mktemp -d /tmp/go-unifi-proxy-validation.XXXXXX)
readonly validation_cache
cleanup() {
    chmod -R u+w "${validation_cache}" 2>/dev/null || true
    rm -rf "${validation_cache}"
}
trap cleanup EXIT
module_json=$(cd /tmp && env GOMODCACHE="${validation_cache}" \
    GOPROXY="file://${proxy_root}" GOSUMDB=off 'GOVCS=*:off' \
    GIT_TERMINAL_PROMPT=0 GOTOOLCHAIN=local \
    go mod download -json "${module_path}@${module_version}")
readonly module_json
test "$(jq -r .Path <<<"${module_json}")" = "${module_path}"
test "$(jq -r .Version <<<"${module_json}")" = "${module_version}"
if [[ ${expected_sum} != skip ]]; then
    test "$(jq -r .Sum <<<"${module_json}")" = "${expected_sum}"
fi
