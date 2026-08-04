#!/usr/bin/env bash
set -euo pipefail

script_directory=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
readonly script_directory
# shellcheck source=.woodpecker/scripts/m1-evidence-lib.sh
source "${script_directory}/m1-evidence-lib.sh"

readonly terraform_bin=${TERRAFORM_BIN:-terraform}
readonly tofu_bin=${TOFU_BIN:-tofu}
readonly provider_address=registry.terraform.io/ubiquiti-community/unifi
readonly provider_version=0.101.2
readonly released_provider_binary=${M1_RELEASED_PROVIDER_BINARY:?M1_RELEASED_PROVIDER_BINARY is required}
readonly released_provider_sha256=c580db83441331fb8c194b9c894730e624743cdaf73b02a990c0655b47fc2d48
readonly terraform_sha256=8b6cb96cd46080ee1287baf646c70078715a99123b9b3a6ce2a7fe3892ec703a
readonly tofu_sha256=b6cb308bd699c8b53882319986f339eeaa44f925a68f12d5b0a14e6caceedf23
readonly resource_digest=1bdb6740d88d68bf232d79874c34d0e3811d382f55948352add15c2a28e5e93c
readonly identity_digest=1a6e443309d9484e62e9f1fe71a83b60cf348f4acbe3a92d8f7b8bb7d3274d33
readonly list_digest=c914929e71ab8ce0e8977518615ee3cf81c31a411ec77c9f58a2350145c6ee95
readonly catalog_path=provider-codegen/catalog/go-unifi-v1.102.0-dns-record.catalog.json
readonly operation_path=provider-codegen/catalog/go-unifi-62add0c-dns-record.operation.json
readonly mapping_path=provider-codegen/generated/dns_record.mapping.json
started_at=$(date +%s)
readonly started_at

work_root=$(mktemp -d "${TMPDIR:-/tmp}/provider-m1.XXXXXX")
readonly work_root
cleanup() {
    local result=$?
    trap - EXIT
    rm -rf "${work_root}"
    exit "${result}"
}
trap cleanup EXIT

mkdir -p "${work_root}/before" "${work_root}/provider/head" \
    "${work_root}/provider/released" "${work_root}/fixture"
cp provider-codegen/generated/dns_record.provider-code-spec.json "${work_root}/before/"
cp provider-codegen/generated/dns_record.impact.json "${work_root}/before/"
cp provider-codegen/generated/dns_record.mapping.json "${work_root}/before/"
cp internal/generated/resource_dns_record/dns_record_resource_gen.go "${work_root}/before/"
find docs -type f -exec sha256sum {} + | LC_ALL=C sort >"${work_root}/before/docs.sha256"

go generate ./...
cmp "${work_root}/before/dns_record.provider-code-spec.json" provider-codegen/generated/dns_record.provider-code-spec.json
cmp "${work_root}/before/dns_record.impact.json" provider-codegen/generated/dns_record.impact.json
cmp "${work_root}/before/dns_record.mapping.json" provider-codegen/generated/dns_record.mapping.json
cmp "${work_root}/before/dns_record_resource_gen.go" internal/generated/resource_dns_record/dns_record_resource_gen.go
find docs -type f -exec sha256sum {} + | LC_ALL=C sort >"${work_root}/after-docs.sha256"
cmp "${work_root}/before/docs.sha256" "${work_root}/after-docs.sha256"

go test ./...
for build in one two; do
    CGO_ENABLED=0 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local \
        GOCACHE="${work_root}/go-build-${build}" \
        go build -trimpath -buildvcs=false \
        -o "${work_root}/provider/head/terraform-provider-unifi_v${provider_version}.${build}" .
done
cmp "${work_root}/provider/head/terraform-provider-unifi_v${provider_version}.one" \
    "${work_root}/provider/head/terraform-provider-unifi_v${provider_version}.two"
mv "${work_root}/provider/head/terraform-provider-unifi_v${provider_version}.one" \
    "${work_root}/provider/head/terraform-provider-unifi_v${provider_version}"
rm "${work_root}/provider/head/terraform-provider-unifi_v${provider_version}.two"
cp "${released_provider_binary}" \
    "${work_root}/provider/released/terraform-provider-unifi_v${provider_version}"
test "$(sha256sum "${work_root}/provider/released/terraform-provider-unifi_v${provider_version}" | awk '{print $1}')" = \
    "${released_provider_sha256}"
go build -trimpath -o "${work_root}/schema-baseline" ./cmd/schema-baseline

terraform_path=$(command -v "${terraform_bin}")
tofu_path=$(command -v "${tofu_bin}")
readonly terraform_path tofu_path
test "$(sha256sum "${terraform_path}" | awk '{print $1}')" = "${terraform_sha256}"
test "$(sha256sum "${tofu_path}" | awk '{print $1}')" = "${tofu_sha256}"
test "$("${terraform_bin}" version -json | jq -r .terraform_version)" = 1.15.8
test "$("${tofu_bin}" version -json | jq -r .terraform_version)" = 1.12.1

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
cat >"${work_root}/fixture/head.tfrc" <<EOF
provider_installation {
  dev_overrides {
    "${provider_address}" = "${work_root}/provider/head"
  }
  direct {}
}
EOF
cat >"${work_root}/fixture/released.tfrc" <<EOF
provider_installation {
  dev_overrides {
    "${provider_address}" = "${work_root}/provider/released"
  }
  direct {}
}
EOF

run_schema() {
    local cli=$1
    local name=$2
    local build=$3
    CHECKPOINT_DISABLE=1 TF_IN_AUTOMATION=1 \
        TF_CLI_CONFIG_FILE="${work_root}/fixture/${build}.tfrc" \
        TF_DATA_DIR="${work_root}/fixture/.${build}-${name}" \
        "${cli}" -chdir="${work_root}/fixture" providers schema -json \
        >"${work_root}/${build}.${name}.raw.json"
    "${work_root}/schema-baseline" \
        -input "${work_root}/${build}.${name}.raw.json" \
        -canonical-output "${work_root}/${build}.${name}.canonical.json" \
        -digests-output "${work_root}/${build}.${name}.digests.json"
}

for build in released head; do
    run_schema "${terraform_bin}" terraform "${build}"
    run_schema "${tofu_bin}" tofu "${build}"
done

cmp "${work_root}/released.terraform.canonical.json" "${work_root}/head.terraform.canonical.json"
cmp "${work_root}/released.tofu.canonical.json" "${work_root}/head.tofu.canonical.json"

test "$(jq -r '.schema_sha256["resource_schemas.unifi_dns_record"]' "${work_root}/head.terraform.digests.json")" = "${resource_digest}"
test "$(jq -r '.schema_sha256["resource_identity_schemas.unifi_dns_record"]' "${work_root}/head.terraform.digests.json")" = "${identity_digest}"
test "$(jq -r '.schema_sha256["list_resource_schemas.unifi_dns_record"]' "${work_root}/head.terraform.digests.json")" = "${list_digest}"
test "$(jq -r '.schema_sha256["resource_schemas.unifi_dns_record"]' "${work_root}/head.tofu.digests.json")" = "${resource_digest}"
test "$(jq -r '.schema_sha256["resource_identity_schemas.unifi_dns_record"]' "${work_root}/head.tofu.digests.json")" = "${identity_digest}"

for build in released head; do
    jq --sort-keys 'del(.action_schemas, .list_resource_schemas)' \
        "${work_root}/${build}.terraform.canonical.json" >"${work_root}/${build}.terraform.shared.json"
    jq --sort-keys . "${work_root}/${build}.tofu.canonical.json" >"${work_root}/${build}.tofu.shared.json"
    cmp "${work_root}/${build}.terraform.shared.json" "${work_root}/${build}.tofu.shared.json"
done

raw_retained=false
if [[ -n ${M1_EVIDENCE_OUTPUT_DIRECTORY:-} ]]; then
    repository_root=$(git rev-parse --show-toplevel)
    readonly repository_root
    evidence_directory=$(prepare_evidence_directory \
        "${M1_EVIDENCE_OUTPUT_DIRECTORY}" "${repository_root}")
    readonly evidence_directory
    mkdir -p "${evidence_directory}/raw" "${evidence_directory}/canonical"
    cp "${work_root}/head.terraform.raw.json" "${evidence_directory}/raw/terraform-1.15.8-head.json"
    cp "${work_root}/head.tofu.raw.json" "${evidence_directory}/raw/tofu-1.12.1-head.json"
    cp "${work_root}/released.terraform.raw.json" "${evidence_directory}/raw/terraform-1.15.8-released.json"
    cp "${work_root}/released.tofu.raw.json" "${evidence_directory}/raw/tofu-1.12.1-released.json"
    cp "${work_root}/head.terraform.canonical.json" "${evidence_directory}/canonical/terraform-1.15.8.json"
    cp "${work_root}/head.tofu.canonical.json" "${evidence_directory}/canonical/tofu-1.12.1.json"
    (
        cd "${evidence_directory}"
        find raw canonical -type f -exec sha256sum {} + | LC_ALL=C sort >SHA256SUMS
        sha256sum --check SHA256SUMS
    )
    raw_retained=true
fi
readonly raw_retained

provider_binary_sha256=$(sha256sum "${work_root}/provider/head/terraform-provider-unifi_v${provider_version}" | awk '{print $1}')
terraform_canonical_sha256=$(sha256sum "${work_root}/head.terraform.canonical.json" | awk '{print $1}')
tofu_canonical_sha256=$(sha256sum "${work_root}/head.tofu.canonical.json" | awk '{print $1}')
source_commit=$(git rev-parse HEAD)
readonly provider_binary_sha256 terraform_canonical_sha256 tofu_canonical_sha256 source_commit

management_sidecar_sha256=null
if [[ -n ${M1_LIFECYCLE_RECEIPT:-} ]]; then
    lifecycle_receipt=${M1_LIFECYCLE_RECEIPT}
    readonly lifecycle_receipt
    test "$(jq -r .result "${lifecycle_receipt}")" = pass
    test "$(jq -r .source_commit "${lifecycle_receipt}")" = "${source_commit}"
    test "$(jq -r .provider_binary_sha256 "${lifecycle_receipt}")" = "${provider_binary_sha256}"
    lifecycle_sha256=$(sha256sum "${lifecycle_receipt}" | awk '{print $1}')
    sidecar_output=${M1_MANAGEMENT_SIDECAR_OUTPUT:-${work_root}/management-sidecar.json}
    readonly lifecycle_sha256 sidecar_output
    jq --indent 2 --null-input \
        --arg address "${provider_address}" \
        --arg version "${provider_version}" \
        --arg source_commit "${source_commit}" \
        --arg provider_binary_sha256 "${provider_binary_sha256}" \
        --arg terraform_binary_sha256 "${terraform_sha256}" \
        --arg terraform_canonical_sha256 "${terraform_canonical_sha256}" \
        --arg tofu_binary_sha256 "${tofu_sha256}" \
        --arg tofu_canonical_sha256 "${tofu_canonical_sha256}" \
        --arg catalog_id "$(jq -r .catalog_id "${catalog_path}")" \
        --arg catalog_sha256 "$(sha256sum "${catalog_path}" | awk '{print $1}')" \
        --arg operation_sha256 "$(sha256sum "${operation_path}" | awk '{print $1}')" \
        --arg operation_digest "$(jq -r .operation_digest provider-codegen/policy/dns_record.json)" \
        --arg lifecycle_receipt "$(basename "${lifecycle_receipt}")" \
        --arg lifecycle_sha256 "${lifecycle_sha256}" \
        --arg policy_sha256 "$(sha256sum provider-codegen/policy/dns_record.json | awk '{print $1}')" \
        --arg provider_code_spec_sha256 "$(sha256sum provider-codegen/generated/dns_record.provider-code-spec.json | awk '{print $1}')" \
        --arg mapping_sha256 "$(sha256sum "${mapping_path}" | awk '{print $1}')" \
        '{format_version: 1, contract_id: "unifi_dns_record@exact-binary", mode: "provider_projection_required", provider: {address: $address, version: $version, source_commit: $source_commit, binary: {platform: "linux/amd64", sha256: $provider_binary_sha256}, schema: {toolchains: {terraform: {version: "1.15.8", binary_sha256: $terraform_binary_sha256, canonical_schema: "canonical/terraform-1.15.8.json", canonical_schema_sha256: $terraform_canonical_sha256}, tofu: {version: "1.12.1", binary_sha256: $tofu_binary_sha256, canonical_schema: "canonical/tofu-1.12.1.json", canonical_schema_sha256: $tofu_canonical_sha256}}}}, catalog: {id: $catalog_id, sha256: $catalog_sha256}, operation: {artifact_sha256: $operation_sha256, digest: $operation_digest}, lifecycle: {receipt: $lifecycle_receipt, receipt_sha256: $lifecycle_sha256, result: "pass"}, provenance: {policy_sha256: $policy_sha256, provider_code_spec_sha256: $provider_code_spec_sha256, mapping_sha256: $mapping_sha256}, trust: {kind: "operator-pinned-checksum", signed: false}}' \
        >"${sidecar_output}"
    management_sidecar_sha256=$(sha256sum "${sidecar_output}" | awk '{print $1}')
fi
readonly management_sidecar_sha256

finished_at=$(date +%s)
readonly finished_at
receipt_output=${M1_RECEIPT_OUTPUT:-${work_root}/dns-compiler-receipt.json}
readonly receipt_output
jq --indent 2 --null-input \
    --arg source_commit "${source_commit}" \
    --arg execution "${M1_EXECUTION:-local}" \
    --arg go_version "$(go version)" \
    --arg goos "$(go env GOOS)" \
    --arg goarch "$(go env GOARCH)" \
    --arg terraform_version "$("${terraform_bin}" version -json | jq -r .terraform_version)" \
    --arg tofu_version "$("${tofu_bin}" version -json | jq -r .terraform_version)" \
    --arg provider_binary_sha256 "${provider_binary_sha256}" \
    --arg released_provider_binary_sha256 "${released_provider_sha256}" \
    --arg terraform_binary_sha256 "${terraform_sha256}" \
    --arg tofu_binary_sha256 "${tofu_sha256}" \
    --arg terraform_raw_sha256 "$(sha256sum "${work_root}/head.terraform.raw.json" | awk '{print $1}')" \
    --arg tofu_raw_sha256 "$(sha256sum "${work_root}/head.tofu.raw.json" | awk '{print $1}')" \
    --arg terraform_canonical_sha256 "${terraform_canonical_sha256}" \
    --arg tofu_canonical_sha256 "${tofu_canonical_sha256}" \
    --arg shared_schema_sha256 "$(sha256sum "${work_root}/head.terraform.shared.json" | awk '{print $1}')" \
    --arg provider_code_spec_sha256 "$(sha256sum provider-codegen/generated/dns_record.provider-code-spec.json | awk '{print $1}')" \
    --arg impact_sha256 "$(sha256sum provider-codegen/generated/dns_record.impact.json | awk '{print $1}')" \
    --arg mapping_sha256 "$(sha256sum provider-codegen/generated/dns_record.mapping.json | awk '{print $1}')" \
    --arg generated_go_sha256 "$(sha256sum internal/generated/resource_dns_record/dns_record_resource_gen.go | awk '{print $1}')" \
    --arg resource_digest "${resource_digest}" \
    --arg identity_digest "${identity_digest}" \
    --arg list_digest "${list_digest}" \
    --arg management_sidecar_sha256 "${management_sidecar_sha256}" \
    --argjson raw_retained "${raw_retained}" \
    --argjson elapsed_seconds "$((finished_at - started_at))" \
    '{format_version: 1, milestone: "M1", result: "passed", execution: $execution, source_commit: $source_commit, platform: ($goos + "/" + $goarch), go_version: $go_version, terraform_version: $terraform_version, tofu_version: $tofu_version, provider_binary_sha256: $provider_binary_sha256, released_provider_binary_sha256: $released_provider_binary_sha256, management_sidecar_sha256: (if $management_sidecar_sha256 == "null" then null else $management_sidecar_sha256 end), generator: {module: "github.com/hashicorp/terraform-plugin-codegen-framework", version: "v0.4.1", commit: "eea0e9d6b59b4e678cac5cda2d2c5d852f8679f2"}, outputs: {provider_code_spec_sha256: $provider_code_spec_sha256, impact_sha256: $impact_sha256, mapping_sha256: $mapping_sha256, generated_go_sha256: $generated_go_sha256}, schema_sha256: {resource: $resource_digest, identity: $identity_digest, list_resource: $list_digest}, schema_evidence: {terraform: {version: $terraform_version, binary_sha256: $terraform_binary_sha256, raw_sha256: $terraform_raw_sha256, canonical_sha256: $terraform_canonical_sha256}, tofu: {version: $tofu_version, binary_sha256: $tofu_binary_sha256, raw_sha256: $tofu_raw_sha256, canonical_sha256: $tofu_canonical_sha256}, shared_sha256: $shared_schema_sha256, raw_linux_cli_outputs_retained: $raw_retained}, clean_builds: 2, build_network: "none", release_to_head: true, deterministic_provider_binary: true, generated_docs_unchanged: true, deterministic_regeneration: true, shared_cli_projection_equal: true, full_cli_projection_equal: false, terraform_only_categories: ["action_schemas", "list_resource_schemas"], lifecycle_evidence: {receipt: "build/m0/network-dns-qualification.json", reused: true, reason: "schema-only cutover left runtime paths unchanged"}, human_decisions: ["field dispositions", "key to name mapping", "integer seconds to duration string", "provider-owned identity site and timeouts", "defaults", "validators", "replacement modifiers", "handwritten state list and lifecycle seams"], elapsed_seconds: $elapsed_seconds}' \
    >"${receipt_output}"

if [[ ${raw_retained} = true ]]; then
    cp "${receipt_output}" "${evidence_directory}/receipt.json"
    if [[ ${management_sidecar_sha256} != null ]]; then
        cp "${sidecar_output}" "${evidence_directory}/management-sidecar.json"
    fi
    (
        cd "${evidence_directory}"
        find raw canonical -type f -exec sha256sum {} + >SHA256SUMS.unsorted
        sha256sum receipt.json >>SHA256SUMS.unsorted
        if [[ -f management-sidecar.json ]]; then
            sha256sum management-sidecar.json >>SHA256SUMS.unsorted
        fi
        LC_ALL=C sort SHA256SUMS.unsorted >SHA256SUMS
        rm SHA256SUMS.unsorted
        sha256sum --check SHA256SUMS
    )
    if [[ -n ${M1_EVIDENCE_ARCHIVE:-} ]]; then
        archive_evidence_directory "${evidence_directory}" "${M1_EVIDENCE_ARCHIVE}"
        if [[ ${M1_PRINT_EVIDENCE_BUNDLE:-false} = true ]]; then
            archive_size=$(wc -c <"${M1_EVIDENCE_ARCHIVE}")
            readonly archive_size
            if (( archive_size > 786432 )); then
                echo "M1 evidence archive exceeds the 768 KiB transport limit" >&2
                exit 1
            fi
            printf 'M1_EVIDENCE_ARCHIVE_SHA256=%s\n' \
                "$(sha256sum "${M1_EVIDENCE_ARCHIVE}" | awk '{print $1}')"
            printf 'M1_EVIDENCE_BUNDLE_BASE64_BEGIN\n'
            base64 "${M1_EVIDENCE_ARCHIVE}"
            printf 'M1_EVIDENCE_BUNDLE_BASE64_END\n'
        fi
    fi
fi

if [[ -z ${M1_RECEIPT_OUTPUT:-} ]]; then
    cat "${receipt_output}"
fi
