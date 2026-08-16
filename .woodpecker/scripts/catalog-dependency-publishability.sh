#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root

# The receipt stamps provider_commit from git rev-parse HEAD, so on a dirty tree
# it names a commit that does not contain the go.mod this gate just read. Take
# the tree state first, before the pin is even sourced.
# shellcheck source=.woodpecker/scripts/tree-state.sh
source "${repository_root}/.woodpecker/scripts/tree-state.sh"
evidence_tree_state "the dependency publishability receipt"

# shellcheck source=.woodpecker/scripts/go-unifi-pin.sh
source "${repository_root}/.woodpecker/scripts/go-unifi-pin.sh"

readonly output=${CATALOG_DEPENDENCY_OUTPUT:?CATALOG_DEPENDENCY_OUTPUT is required}

# Every check below reports what it wanted and what it found. This gate used to
# be a column of bare `test` calls under set -e, so a failure ended the step in
# silence and the operator could not tell whether the script had even run.
fail() {
    printf 'dependency publishability: %s\n' "$1" >&2
    exit 1
}

expect() {
    local what=$1 want=$2 got=$3
    if [[ ${got} != "${want}" ]]; then
        fail "${what} is ${got:-empty}, want ${want}"
    fi
}

module_path=${go_unifi_module_path}
readonly module_path
declared_version=$(go_unifi_declared_version "${repository_root}")
declared_sum=$(go_unifi_declared_sum "${repository_root}" "${declared_version}")
readonly declared_version declared_sum

module_json=$(env GOPROXY=off GOSUMDB=off 'GOVCS=*:off' \
    GIT_TERMINAL_PROMPT=0 GOTOOLCHAIN=local \
    go list -mod=readonly -m -json "${module_path}")
readonly module_json

expect "resolved module path" "${module_path}" "$(jq -r .Path <<<"${module_json}")"

# Declared against resolved, not against a constant. They differ exactly when
# minimal version selection upgraded the module or a replace redirected it,
# which is what makes a dependency unpublishable and what a hardcoded copy
# could never detect.
expect "resolved module version (go.mod declares ${declared_version})" \
    "${declared_version}" "$(jq -r .Version <<<"${module_json}")"
expect "resolved module sum (go.sum records ${declared_sum})" \
    "${declared_sum}" "$(jq -r .Sum <<<"${module_json}")"
expect "replace directive present" false "$(jq -r 'has("Replace")' <<<"${module_json}")"

module_cache=${GOMODCACHE:-$(go env GOMODCACHE)}
readonly module_cache
module_dir=$(jq -r .Dir <<<"${module_json}")
readonly module_dir
readonly version_root=${module_cache}/cache/download/${module_path}/@v
readonly module_zip=${version_root}/${declared_version}.zip
readonly module_info=${version_root}/${declared_version}.info
[[ -d ${module_dir} ]] || fail "extracted module directory ${module_dir} is absent"
[[ -f ${module_zip} ]] || fail "module archive ${module_zip} is absent"
[[ -f ${module_info} ]] || fail "module metadata ${module_info} is absent"

# The commit is a claim about the tag rather than a restatement of it: a tag
# moved underneath us keeps its version and changes its commit.
expect "module origin commit" "${go_unifi_expected_commit}" "$(jq -r .Origin.Hash "${module_info}")"
expect "module origin URL" "${go_unifi_module_origin}" "$(jq -r .Origin.URL "${module_info}")"

module_zip_sha256=$(sha256sum "${module_zip}" | awk '{print $1}')
module_dir_sha256=$(
    cd "${module_dir}"
    find . -type f -exec sha256sum {} + | LC_ALL=C sort | sha256sum | awk '{print $1}'
)
provider_commit=$(git -C "${repository_root}" rev-parse HEAD)
readonly module_zip_sha256 module_dir_sha256 provider_commit
expect "module archive digest length" 64 "${#module_zip_sha256}"
expect "module tree digest length" 64 "${#module_dir_sha256}"
expect "provider commit length" 40 "${#provider_commit}"

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
    --arg module_version "${declared_version}" \
    --arg module_commit "${go_unifi_expected_commit}" \
    --arg module_zip_sha256 "${module_zip_sha256}" \
    --arg module_dir_sha256 "${module_dir_sha256}" \
    --argjson tree "$(evidence_tree_json)" \
    '{
        format_version: 1,
        gate: "go-unifi-dependency-publishability",
        result: "pass",
        provider_commit: $provider_commit,
        tree: $tree,
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
