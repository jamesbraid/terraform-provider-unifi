#!/usr/bin/env bash
set -euo pipefail

repository_root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repository_root
work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-build-schema-test.XXXXXX")
readonly work_root
: "${TERRAFORM_BIN:?TERRAFORM_BIN is required}"
: "${TOFU_BIN:?TOFU_BIN is required}"
cleanup() {
    rm -rf "${work_root}"
}
trap cleanup EXIT

(
    cd "${work_root}"
    CATALOG_ALLOW_DIAGNOSTIC_TOOLCHAIN=true \
    CATALOG_BUILD_SCHEMA_OUTPUT=${work_root}/receipt.json \
    CATALOG_BUILD_SCHEMA_EVIDENCE_DIRECTORY=${work_root}/evidence \
    TERRAFORM_BIN=${TERRAFORM_BIN} \
    TOFU_BIN=${TOFU_BIN} \
        bash "${repository_root}/.woodpecker/scripts/catalog-build-schema.sh"
)

jq -e '
    (.result == "pass" or .result == "diagnostic_pass") and
    .build_network == "none" and
    .clean_builds.released == 2 and
    .clean_builds.candidate == 2 and
    .schema_evidence.release_to_candidate_within_cli == true and
    .schema_evidence.shared_cli_projection_equal == true and
    .schema_evidence.full_cli_projection_equal == false and
    .schema_evidence.terraform_only_categories == ["action_schemas", "list_resource_schemas"]
' "${work_root}/receipt.json" >/dev/null

test -x "${work_root}/evidence/terraform-provider-unifi"
test -f "${work_root}/evidence/terraform-schema.json"
test -f "${work_root}/evidence/tofu-schema.json"
test -f "${work_root}/evidence/SHA256SUMS"
(
    cd "${work_root}/evidence"
    sha256sum --check SHA256SUMS
)
test "$(sha256sum "${work_root}/evidence/terraform-provider-unifi" | awk '{print $1}')" = \
    "$(jq -r .provider_binaries.candidate_sha256 "${work_root}/receipt.json")"
test "$(sha256sum "${work_root}/evidence/terraform-schema.json" | awk '{print $1}')" = \
    "$(jq -r .schema_evidence.terraform.canonical_sha256 "${work_root}/receipt.json")"
test "$(sha256sum "${work_root}/evidence/tofu-schema.json" | awk '{print $1}')" = \
    "$(jq -r .schema_evidence.tofu.canonical_sha256 "${work_root}/receipt.json")"
