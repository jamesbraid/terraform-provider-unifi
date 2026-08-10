#!/usr/bin/env bash
set -euo pipefail

m3_repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly m3_repository_root
# shellcheck source=.woodpecker/scripts/go-unifi-pin.sh
source "${m3_repository_root}/.woodpecker/scripts/go-unifi-pin.sh"

# This receipt attests a controller operation actually ran. Generated from a
# dirty tree it would attest the run against code that is in no commit, which is
# the one claim an operation receipt exists to make.
# shellcheck source=.woodpecker/scripts/tree-state.sh
source "${m3_repository_root}/.woodpecker/scripts/tree-state.sh"
evidence_tree_state "the M3 DNS operation receipt"

# The module version and its hash are read from go.mod and go.sum. They used to
# be pinned here as well, which made this the fourth home for one fact and the
# reason a repoint that updated go.mod left this script asserting the previous
# release. The catalog path below stays literal on purpose: that artifact is
# keyed on the controller it was captured from, not on the module.
readonly expected_module=${go_unifi_module_path}
expected_version=$(go_unifi_declared_version "${m3_repository_root}")
readonly expected_version
expected_sum=$(go_unifi_declared_sum "${m3_repository_root}" "${expected_version}")
readonly expected_sum
readonly expected_catalog_source_commit=62add0c72d932aba663164fd794d510fb56aebae
readonly expected_catalog_sha256=a07b8a4b91d68aaaf35b7bb8a2d8f3d877531ffff64cbd44a44748ab736b9ac1
readonly expected_operation_sha256=d004069d8f4d911de2d893c3183e0d9e59a67d66425fcb3d1551caa55c623cbd
readonly expected_operation_digest=199c002d9a1229aa7e43a94b12da6f33c7ce612ecb471eebcd6896d985b161f8
readonly expected_controller_index=sha256:584be3a2e45c4913e1bc373eff9c7330609c82085d4fc6f5ea365abdcdb3e664
readonly expected_controller_manifest=sha256:9d19c8d03948a77d28181743fb81a515aa51a5cea0856bc0491b34d92a01bcf4
readonly catalog_path=provider-codegen/catalog/go-unifi-v1.102.0-dns-record.catalog.json
readonly operation_path=provider-codegen/catalog/go-unifi-62add0c-dns-record.operation.json
readonly policy_path=provider-codegen/policy/dns_record.json
readonly lifecycle_receipt=${M3_LIFECYCLE_RECEIPT:?M3_LIFECYCLE_RECEIPT is required}
source_commit=$(git rev-parse HEAD)
readonly source_commit
readonly terraform_bin=${TERRAFORM_BIN:-terraform}
readonly tofu_bin=${TOFU_BIN:-tofu}

work_root=$(mktemp -d "${TMPDIR:-/tmp}/provider-m3.XXXXXX")
readonly work_root
cleanup() {
    local result=$?
    trap - EXIT
    rm -rf "${work_root}"
    exit "${result}"
}
trap cleanup EXIT

module_json=$(go list -m -json all | jq --compact-output \
    --arg module_path "${expected_module}" \
    'select(.Path == $module_path)')
readonly module_json
test "$(jq -r 'has("Replace")' <<<"${module_json}")" = false
test "$(jq -r .Version <<<"${module_json}")" = "${expected_version}"
test "$(jq -r .Sum <<<"${module_json}")" = "${expected_sum}"

test "$(sha256sum "${catalog_path}" | awk '{print $1}')" = "${expected_catalog_sha256}"
test "$(sha256sum "${operation_path}" | awk '{print $1}')" = "${expected_operation_sha256}"
test "$(jq -r '.admission.state' "${catalog_path}")" = admitted
test "$(jq -r '.admission.operation_digest' "${catalog_path}")" = "${expected_operation_digest}"
test "$(jq -r '.catalog_sha256' "${policy_path}")" = "${expected_catalog_sha256}"
test "$(jq -r '.catalog_source.commit' "${policy_path}")" = "${expected_catalog_source_commit}"
test "$(jq -r '.operation_digest' "${policy_path}")" = "${expected_operation_digest}"
test "$(jq -r '.target.image_index_sha256' "${catalog_path}")" = "${expected_controller_index}"
test "$(jq -r '.target.image_manifest_sha256' "${catalog_path}")" = "${expected_controller_manifest}"

test -f "${lifecycle_receipt}"
jq -e \
    --arg source_commit "${source_commit}" \
    --arg index "${expected_controller_index}" \
    --arg manifest "${expected_controller_manifest}" \
    '.format_version == 1 and
     .result == "pass" and
     .source_commit == $source_commit and
     .platform == "linux/amd64" and
     .provider_version == "0.101.2" and
     .terraform.version == "1.15.8" and
     .tofu.version == "1.12.1" and
     .target.product == "UniFi Network" and
     .target.version == "10.4.57" and
     .target.index_sha256 == $index and
     .target.platform_manifest_sha256 == $manifest and
     ([.lifecycle.fresh_target_per_cli_and_adapter,
       .lifecycle.create,
       .lifecycle.update,
       .lifecycle.omitted_optional_fields,
       .lifecycle.configured_optional_fields,
       .lifecycle.replacement_plan,
       .lifecycle.restart_refresh,
       .lifecycle.import,
       .lifecycle.v0_integer_ttl_state_upgrade,
       .lifecycle.no_op_plan,
       .lifecycle.delete,
       .lifecycle.cleanup,
       .lifecycle.bidirectional_adapter_state_round_trip] | all)' \
    "${lifecycle_receipt}" >/dev/null

terraform_path=$(command -v "${terraform_bin}")
tofu_path=$(command -v "${tofu_bin}")
readonly terraform_path tofu_path
terraform_sha256=$(sha256sum "${terraform_path}" | awk '{print $1}')
tofu_sha256=$(sha256sum "${tofu_path}" | awk '{print $1}')
readonly terraform_sha256 tofu_sha256
test "$("${terraform_bin}" version -json | jq -r '.terraform_version')" = 1.15.8
test "$("${tofu_bin}" version -json | jq -r '.terraform_version')" = 1.12.1
test "$(jq -r '.terraform.binary_sha256' "${lifecycle_receipt}")" = "${terraform_sha256}"
test "$(jq -r '.tofu.binary_sha256' "${lifecycle_receipt}")" = "${tofu_sha256}"

go test ./unifi \
    -run 'TestPrivateDNSRecordBackend|TestDNSRecordPatch|Test_dnsRecordFrameworkResource_modelToDNSRecordPatch' \
    -count=1

M1_EXECUTION="${M3_EXECUTION:-local-m3}" \
M1_RECEIPT_OUTPUT="${work_root}/schema-gate.json" \
M1_LIFECYCLE_RECEIPT="${lifecycle_receipt}" \
M1_MANAGEMENT_SIDECAR_OUTPUT="${M3_MANAGEMENT_SIDECAR_OUTPUT:-${work_root}/management-sidecar.json}" \
TERRAFORM_BIN="${terraform_bin}" \
TOFU_BIN="${tofu_bin}" \
    .woodpecker/scripts/m1-dns-compiler.sh

test "$(jq -r '.source_commit' "${work_root}/schema-gate.json")" = "${source_commit}"
test "$(jq -r '.terraform_version' "${work_root}/schema-gate.json")" = 1.15.8
test "$(jq -r '.tofu_version' "${work_root}/schema-gate.json")" = 1.12.1
test "$(jq -r '.provider_binary_sha256' "${work_root}/schema-gate.json")" = \
    "$(jq -r '.provider_binary_sha256' "${lifecycle_receipt}")"
management_sidecar_sha256=$(sha256sum \
    "${M3_MANAGEMENT_SIDECAR_OUTPUT:-${work_root}/management-sidecar.json}" | awk '{print $1}')
readonly management_sidecar_sha256
test "$(jq -r '.management_sidecar_sha256' "${work_root}/schema-gate.json")" = \
    "${management_sidecar_sha256}"
lifecycle_result=pass
lifecycle_sha256=$(sha256sum "${lifecycle_receipt}" | awk '{print $1}')
readonly lifecycle_result lifecycle_sha256

jq --indent 2 --null-input \
    --arg source_commit "${source_commit}" \
    --arg execution "${M3_EXECUTION:-local}" \
    --arg module_path "${expected_module}" \
    --arg version "${expected_version}" \
    --arg sum "${expected_sum}" \
    --arg catalog_sha256 "${expected_catalog_sha256}" \
    --arg catalog_source_commit "${expected_catalog_source_commit}" \
    --arg operation_sha256 "${expected_operation_sha256}" \
    --arg operation_digest "${expected_operation_digest}" \
    --arg terraform_sha256 "${terraform_sha256}" \
    --arg tofu_sha256 "${tofu_sha256}" \
    --arg controller_index "${expected_controller_index}" \
    --arg controller_manifest "${expected_controller_manifest}" \
    --arg lifecycle_result "${lifecycle_result}" \
    --arg lifecycle_receipt "${lifecycle_receipt}" \
    --arg lifecycle_sha256 "${lifecycle_sha256}" \
    --arg management_sidecar_sha256 "${management_sidecar_sha256}" \
    --slurpfile schema_gate "${work_root}/schema-gate.json" \
    '{
        format_version: 1,
        milestone: "M3",
        result: "local_pass",
        promotion: "qualified",
        execution: $execution,
        source_commit: $source_commit,
        go_unifi: {
            module: $module_path,
            replacement: null,
            version: $version,
            sum: $sum
        },
        inputs: {
            catalog: {sha256: $catalog_sha256, source_commit: $catalog_source_commit},
            operation: {artifact_sha256: $operation_sha256, normalized_digest: $operation_digest},
            target: {index_sha256: $controller_index, platform_manifest_sha256: $controller_manifest},
            terraform: {version: "1.15.8", binary_sha256: $terraform_sha256},
            tofu: {version: "1.12.1", binary_sha256: $tofu_sha256}
        },
        catalog_sha256: $catalog_sha256,
        adapter: {
            model_intent_patch: true,
            masked_private_write: true,
            unsafe_mask_fails_before_request: true,
            failed_write_fallback: false,
            focused_tests: "passed"
        },
        outputs: $schema_gate[0].outputs,
        schema_sha256: $schema_gate[0].schema_sha256,
        deterministic_regeneration: $schema_gate[0].deterministic_regeneration,
        shared_cli_projection_equal: $schema_gate[0].shared_cli_projection_equal,
        provider_binary_sha256: $schema_gate[0].provider_binary_sha256,
        management_sidecar_sha256: $management_sidecar_sha256,
        lifecycle: {
            unit_and_http_boundary: "passed",
            baseline_receipt: "build/m0/network-dns-qualification.json",
            locked_target_result: $lifecycle_result,
            locked_target_receipt: (if $lifecycle_receipt == "" then null else $lifecycle_receipt end),
            locked_target_receipt_sha256: (if $lifecycle_sha256 == "" then null else $lifecycle_sha256 end)
        }
    }' >"${M3_RECEIPT_OUTPUT:-${work_root}/dns-operation-receipt.json}"

if [[ -z ${M3_RECEIPT_OUTPUT:-} ]]; then
    cat "${work_root}/dns-operation-receipt.json"
fi
