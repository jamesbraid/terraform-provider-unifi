#!/usr/bin/env bash
set -euo pipefail

repository_root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repository_root
# shellcheck source=.woodpecker/scripts/m1-evidence-lib.sh
source "${repository_root}/.woodpecker/scripts/m1-evidence-lib.sh"

# Before the toolchain is touched. This script writes the schema digests every
# other gate compares against, so a dirty run poisons the comparison rather than
# just one artifact -- and it would do it while every downstream check stayed
# green, because they would all agree with the same wrong baseline.
# shellcheck source=.woodpecker/scripts/tree-state.sh
source "${repository_root}/.woodpecker/scripts/tree-state.sh"
evidence_tree_state "the released schema baseline and digests"

terraform_bin=${TERRAFORM_BIN:-terraform}
tofu_bin=${TOFU_BIN:-tofu}
output=${CATALOG_BUILD_SCHEMA_OUTPUT:?CATALOG_BUILD_SCHEMA_OUTPUT is required}
evidence_directory=${CATALOG_BUILD_SCHEMA_EVIDENCE_DIRECTORY:-}
if [[ -n ${evidence_directory} ]]; then
    evidence_directory=$(prepare_evidence_directory "${evidence_directory}" "${repository_root}")
fi
readonly terraform_bin tofu_bin output evidence_directory
baseline_manifest=${repository_root}/build/m0/provider-baseline.json
readonly baseline_manifest

work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-build-schema.XXXXXX")
readonly work_root
cleanup() {
    rm -rf "${work_root}"
}
trap cleanup EXIT
mkdir -p "${work_root}/released-source" "${work_root}/provider/released" \
    "${work_root}/provider/candidate" "${work_root}/fixture"

sha256_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

inventory=${work_root}/catalog-evidence-inventory.json
readonly inventory
CATALOG_EVIDENCE_OUTPUT=${inventory} \
    bash "${repository_root}/.woodpecker/scripts/catalog-evidence-inventory.sh"

released_commit=$(jq -r .provider.released_commit "${baseline_manifest}")
readonly released_commit
test "$(git -C "${repository_root}" rev-parse 'v0.101.2^{commit}')" = "${released_commit}"
git -C "${repository_root}" archive --format=tar v0.101.2 | \
    tar -xf - -C "${work_root}/released-source"

build_provider_twice() {
    local source_root=$1
    local label=$2
    local first=${work_root}/${label}-one
    local second=${work_root}/${label}-two
    local build
    for build in one two; do
        local target=${work_root}/${label}-${build}
        (
            cd "${source_root}"
            env CGO_ENABLED=0 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off \
                'GOVCS=*:off' GIT_TERMINAL_PROMPT=0 GOTOOLCHAIN=local \
                GOCACHE="${work_root}/go-build-${label}-${build}" \
                go build -trimpath -buildvcs=false -ldflags=-buildid= -o "${target}" .
        )
    done
    cmp "${first}" "${second}"
}

build_provider_twice "${work_root}/released-source" provider-released-source
build_provider_twice "${repository_root}" provider-candidate

released_source_binary=${work_root}/provider-released-source-one
candidate_binary=${work_root}/provider-candidate-one
released_authority=source_rebuild
released_authority_binary=${released_source_binary}
if [[ -n ${CATALOG_RELEASED_PROVIDER_BINARY:-} ]]; then
    released_authority=published_archive
    released_authority_binary=${CATALOG_RELEASED_PROVIDER_BINARY}
    test -x "${released_authority_binary}"
    test "$(sha256_file "${released_authority_binary}")" = \
        "$(jq -r .provider.release_binary_sha256 "${baseline_manifest}")"
fi
readonly released_source_binary candidate_binary released_authority released_authority_binary
cp "${released_authority_binary}" "${work_root}/provider/released/terraform-provider-unifi_v0.101.2"
cp "${candidate_binary}" "${work_root}/provider/candidate/terraform-provider-unifi_v0.101.2"

(
    cd "${repository_root}"
    env GOPROXY=off GOSUMDB=off 'GOVCS=*:off' GIT_TERMINAL_PROMPT=0 \
        GOTOOLCHAIN=local GOCACHE="${work_root}/go-build-schema-baseline" \
        go build -trimpath -buildvcs=false -o "${work_root}/schema-baseline" \
        ./cmd/schema-baseline
)

cat >"${work_root}/fixture/main.tf" <<'EOF'
terraform {
  required_providers {
    unifi = {
      source  = "registry.terraform.io/ubiquiti-community/unifi"
      version = "0.101.2"
    }
  }
}

provider "unifi" {}
EOF
for build in released candidate; do
    cat >"${work_root}/fixture/${build}.tfrc" <<EOF
provider_installation {
  dev_overrides {
    "registry.terraform.io/ubiquiti-community/unifi" = "${work_root}/provider/${build}"
  }
  direct {
    exclude = ["registry.terraform.io/ubiquiti-community/unifi"]
  }
}
EOF
done

run_schema() {
    local cli=$1
    local cli_name=$2
    local build=$3
    CHECKPOINT_DISABLE=1 TF_IN_AUTOMATION=1 GIT_TERMINAL_PROMPT=0 \
        TF_CLI_CONFIG_FILE="${work_root}/fixture/${build}.tfrc" \
        TF_DATA_DIR="${work_root}/fixture/.${build}-${cli_name}" \
        "${cli}" -chdir="${work_root}/fixture" providers schema -json \
        >"${work_root}/${build}.${cli_name}.raw.json"
    "${work_root}/schema-baseline" \
        -input "${work_root}/${build}.${cli_name}.raw.json" \
        -canonical-output "${work_root}/${build}.${cli_name}.canonical.json" \
        -digests-output "${work_root}/${build}.${cli_name}.digests.json"
}

for build in released candidate; do
    run_schema "${terraform_bin}" terraform "${build}"
    run_schema "${tofu_bin}" tofu "${build}"
done

# Parity is the ONE claim in this file that is no longer byte-identity, so it is
# the one claim that stopped being a cmp. A schema difference is allowed when the
# schema-change ledger declares that exact transition -- surface, attribute,
# field, old and new -- and forbidden otherwise. Everything else here still
# compares bytes and must keep doing so.
#
# cmp reported the first differing byte and stopped, so it could say
# "differ: char 139633, line 1" and nothing about which attribute, how many, or
# whether anyone intended it. The answer turned out to be six.
#
# THIS IS THE CALL SITE. Until it existed, internal/schemaparity was a producer
# nobody invoked: correct, unit-tested, and asked nothing.
#
# Each invocation is ONE LINE, deliberately. tree-state-coverage_test.sh finds
# call sites with a per-line grep, so a continuation would hide the -tree-state
# from it and report four call sites omitting a flag they in fact pass. Every
# other guarded invocation in this repository is a single line for the same
# reason.
#
# The `cd` is load-bearing. `go run` resolves a package against the CURRENT
# module, not against the path it is handed, so an absolute path works only when
# the caller already happens to be inside the repository. Proven from /tmp:
# "go.mod file not found in current directory or any parent directory".
#
# compare-only reads the projections this script ALREADY built and asserts
# nothing else. Running the full binary here would rebuild both providers and
# re-dump both CLIs, and the determinism, cross-CLI and inverted-control
# assertions below would then be judging bytes it never saw.
(cd "${repository_root}" && go run ./cmd/schema-parity -ledger "${repository_root}/provider-codegen/schema-changes/v0.101.2-to-next.json" -cli terraform -released-canonical "${work_root}/released.terraform.canonical.json" -candidate-canonical "${work_root}/candidate.terraform.canonical.json" -tree-state "$(evidence_tree_json)")
(cd "${repository_root}" && go run ./cmd/schema-parity -ledger "${repository_root}/provider-codegen/schema-changes/v0.101.2-to-next.json" -cli tofu -released-canonical "${work_root}/released.tofu.canonical.json" -candidate-canonical "${work_root}/candidate.tofu.canonical.json" -tree-state "$(evidence_tree_json)")
# The frozen released baseline against the built candidate: the same claim as
# the pair above by a second route, so it goes through the same ledger.
(cd "${repository_root}" && go run ./cmd/schema-parity -ledger "${repository_root}/provider-codegen/schema-changes/v0.101.2-to-next.json" -cli terraform-baseline -released-canonical "${repository_root}/provider-contracts/schema/terraform-1.15.8.json" -candidate-canonical "${work_root}/candidate.terraform.canonical.json" -tree-state "$(evidence_tree_json)")
(cd "${repository_root}" && go run ./cmd/schema-parity -ledger "${repository_root}/provider-codegen/schema-changes/v0.101.2-to-next.json" -cli tofu-baseline -released-canonical "${repository_root}/provider-contracts/schema/tofu-1.12.1.json" -candidate-canonical "${work_root}/candidate.tofu.canonical.json" -tree-state "$(evidence_tree_json)")
cmp "${repository_root}/build/m0/provider-schema-digests.json" \
    "${work_root}/candidate.terraform.digests.json"

for build in released candidate; do
    jq --sort-keys 'del(.action_schemas, .list_resource_schemas)' \
        "${work_root}/${build}.terraform.canonical.json" >"${work_root}/${build}.terraform.shared.json"
    jq --sort-keys . "${work_root}/${build}.tofu.canonical.json" \
        >"${work_root}/${build}.tofu.shared.json"
    cmp "${work_root}/${build}.terraform.shared.json" "${work_root}/${build}.tofu.shared.json"
done
if cmp -s "${work_root}/candidate.terraform.canonical.json" "${work_root}/candidate.tofu.canonical.json"; then
    echo "Terraform and OpenTofu unexpectedly returned the same full projection" >&2
    exit 1
fi
test "$(jq -r '.action_schemas | length' "${work_root}/candidate.terraform.canonical.json")" = 1
test "$(jq -r '.list_resource_schemas | length' "${work_root}/candidate.terraform.canonical.json")" = 25
test "$(jq -r 'has("action_schemas") or has("list_resource_schemas")' "${work_root}/candidate.tofu.canonical.json")" = false

terraform_path=$(command -v "${terraform_bin}")
tofu_path=$(command -v "${tofu_bin}")
terraform_version=$("${terraform_bin}" version -json | jq -r .terraform_version)
tofu_version=$("${tofu_bin}" version -json | jq -r .terraform_version)
go_version=$(go env GOVERSION)
platform=$(go env GOOS)/$(go env GOARCH)
readonly terraform_path tofu_path terraform_version tofu_version go_version platform

promotion_blockers=()
[[ ${go_version} == "$(jq -r .toolchain.go_version "${baseline_manifest}")" ]] || promotion_blockers+=(go_version)
[[ ${platform} == "$(jq -r .provider.platform "${baseline_manifest}")" ]] || promotion_blockers+=(platform)
[[ ${terraform_version} == "$(jq -r .clients.terraform.version "${baseline_manifest}")" ]] || promotion_blockers+=(terraform_version)
[[ $(sha256_file "${terraform_path}") == "$(jq -r .clients.terraform.binary_sha256 "${baseline_manifest}")" ]] || promotion_blockers+=(terraform_binary)
[[ ${tofu_version} == "$(jq -r .clients.opentofu.version "${baseline_manifest}")" ]] || promotion_blockers+=(tofu_version)
[[ $(sha256_file "${tofu_path}") == "$(jq -r .clients.opentofu.binary_sha256 "${baseline_manifest}")" ]] || promotion_blockers+=(tofu_binary)
[[ ${released_authority} == published_archive ]] || promotion_blockers+=(released_binary_authority)

result=pass
if (( ${#promotion_blockers[@]} > 0 )); then
    if [[ ${CATALOG_ALLOW_DIAGNOSTIC_TOOLCHAIN:-false} != true ]]; then
        printf 'catalog build/schema promotion blockers: %s\n' "${promotion_blockers[*]}" >&2
        exit 1
    fi
    result=diagnostic_pass
fi
promotion_blockers_json=$(jq --null-input --compact-output --args '$ARGS.positional' "${promotion_blockers[@]}")
readonly result promotion_blockers_json

mkdir -p "$(dirname -- "${output}")"
jq --indent 2 --null-input \
    --arg result "${result}" \
    --argjson promotion_blockers "${promotion_blockers_json}" \
    --arg source_commit "$(git -C "${repository_root}" rev-parse HEAD)" \
    --argjson tree_state "$(evidence_tree_json)" \
    --arg released_commit "${released_commit}" \
    --arg platform "${platform}" \
    --arg go_version "${go_version}" \
    --arg released_source_binary_sha256 "$(sha256_file "${released_source_binary}")" \
    --arg released_authority "${released_authority}" \
    --arg released_authority_binary_sha256 "$(sha256_file "${released_authority_binary}")" \
    --arg candidate_binary_sha256 "$(sha256_file "${candidate_binary}")" \
    --arg inventory_sha256 "$(sha256_file "${inventory}")" \
    --arg terraform_version "${terraform_version}" \
    --arg terraform_binary_sha256 "$(sha256_file "${terraform_path}")" \
    --arg tofu_version "${tofu_version}" \
    --arg tofu_binary_sha256 "$(sha256_file "${tofu_path}")" \
    --arg terraform_released_raw_sha256 "$(sha256_file "${work_root}/released.terraform.raw.json")" \
    --arg terraform_candidate_raw_sha256 "$(sha256_file "${work_root}/candidate.terraform.raw.json")" \
    --arg terraform_canonical_sha256 "$(sha256_file "${work_root}/candidate.terraform.canonical.json")" \
    --arg tofu_released_raw_sha256 "$(sha256_file "${work_root}/released.tofu.raw.json")" \
    --arg tofu_candidate_raw_sha256 "$(sha256_file "${work_root}/candidate.tofu.raw.json")" \
    --arg tofu_canonical_sha256 "$(sha256_file "${work_root}/candidate.tofu.canonical.json")" \
    --arg shared_schema_sha256 "$(sha256_file "${work_root}/candidate.terraform.shared.json")" \
    '{format_version: 1, gate: "catalog-build-schema", result: $result, promotion_blockers: $promotion_blockers, source_commit: $source_commit, tree_state: $tree_state, released_commit: $released_commit, platform: $platform, go_version: $go_version, build_network: "none", clean_builds: {released: 2, candidate: 2}, provider_binaries: {released_source_rebuild_sha256: $released_source_binary_sha256, released_authority: $released_authority, released_authority_sha256: $released_authority_binary_sha256, candidate_sha256: $candidate_binary_sha256}, catalog_evidence_inventory_sha256: $inventory_sha256, schema_evidence: {terraform: {version: $terraform_version, binary_sha256: $terraform_binary_sha256, released_raw_sha256: $terraform_released_raw_sha256, candidate_raw_sha256: $terraform_candidate_raw_sha256, canonical_sha256: $terraform_canonical_sha256}, tofu: {version: $tofu_version, binary_sha256: $tofu_binary_sha256, released_raw_sha256: $tofu_released_raw_sha256, candidate_raw_sha256: $tofu_candidate_raw_sha256, canonical_sha256: $tofu_canonical_sha256}, release_to_candidate_within_cli: true, shared_cli_projection_equal: true, full_cli_projection_equal: false, shared_schema_sha256: $shared_schema_sha256, terraform_only_categories: ["action_schemas", "list_resource_schemas"]}}' \
    >"${output}"

if [[ -n ${evidence_directory} ]]; then
    install -m 0755 "${candidate_binary}" "${evidence_directory}/terraform-provider-unifi"
    install -m 0600 "${work_root}/candidate.terraform.canonical.json" \
        "${evidence_directory}/terraform-schema.json"
    install -m 0600 "${work_root}/candidate.tofu.canonical.json" \
        "${evidence_directory}/tofu-schema.json"
    (
        cd "${evidence_directory}"
        sha256sum terraform-provider-unifi terraform-schema.json tofu-schema.json >SHA256SUMS
        chmod 0600 SHA256SUMS
    )
fi
