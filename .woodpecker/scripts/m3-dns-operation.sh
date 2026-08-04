#!/usr/bin/env bash
set -euo pipefail

readonly expected_module=github.com/ubiquiti-community/go-unifi
readonly expected_replacement=github.com/jamesbraid/go-unifi
readonly expected_version=v1.102.0
readonly expected_sum='h1:mi7q/FGi/TIUiM2K5BLv5jdA3T4f1/Tf5icYjGnnd/I='
readonly expected_catalog_sha256=2bf9c410d98c5bc98d7d790ea9370dcd5e7a94630ee7dc5356d7bb5fa85625f6
readonly catalog_path=provider-codegen/catalog/go-unifi-v1.102.0-dns-record.catalog.json
readonly lifecycle_receipt=${M3_LIFECYCLE_RECEIPT:-}

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
test "$(jq -r '.Replace.Path' <<<"${module_json}")" = "${expected_replacement}"
test "$(jq -r '.Replace.Version' <<<"${module_json}")" = "${expected_version}"
test "$(jq -r '.Replace.Sum' <<<"${module_json}")" = "${expected_sum}"

test "$(sha256sum "${catalog_path}" | awk '{print $1}')" = "${expected_catalog_sha256}"

go test ./unifi \
    -run 'TestPrivateDNSRecordBackend|TestDNSRecordPatch|Test_dnsRecordFrameworkResource_modelToDNSRecordPatch' \
    -count=1

M1_EXECUTION="${M3_EXECUTION:-local-m3}" \
M1_RECEIPT_OUTPUT="${work_root}/schema-gate.json" \
TERRAFORM_BIN="${TERRAFORM_BIN:-terraform}" \
TOFU_BIN="${TOFU_BIN:-tofu}" \
    .woodpecker/scripts/m1-dns-compiler.sh

lifecycle_result=pending
lifecycle_sha256=''
if [[ -n ${lifecycle_receipt} ]]; then
    test -f "${lifecycle_receipt}"
    test "$(jq -r '.result' "${lifecycle_receipt}")" = pass
    lifecycle_result=pass
    lifecycle_sha256=$(sha256sum "${lifecycle_receipt}" | awk '{print $1}')
fi
readonly lifecycle_result lifecycle_sha256

jq --indent 2 --null-input \
    --arg source_commit "$(git rev-parse HEAD)" \
    --arg execution "${M3_EXECUTION:-local}" \
    --arg module_path "${expected_module}" \
    --arg replacement "${expected_replacement}" \
    --arg version "${expected_version}" \
    --arg sum "${expected_sum}" \
    --arg catalog_sha256 "${expected_catalog_sha256}" \
    --arg lifecycle_result "${lifecycle_result}" \
    --arg lifecycle_receipt "${lifecycle_receipt}" \
    --arg lifecycle_sha256 "${lifecycle_sha256}" \
    --slurpfile schema_gate "${work_root}/schema-gate.json" \
    '{
        format_version: 1,
        milestone: "M3",
        result: "local_pass",
        promotion: (if $lifecycle_result == "pass" then "qualified" else "pending_locked_target" end),
        execution: $execution,
        source_commit: $source_commit,
        go_unifi: {
            module: $module_path,
            replacement: $replacement,
            version: $version,
            sum: $sum
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
