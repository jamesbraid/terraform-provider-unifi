#!/usr/bin/env bash
set -euo pipefail

readonly terraform_bin=${TERRAFORM_BIN:-terraform}
readonly tofu_bin=${TOFU_BIN:-tofu}
readonly provider_address=registry.terraform.io/ubiquiti-community/unifi
readonly provider_version=0.101.2
readonly resource_digest=1bdb6740d88d68bf232d79874c34d0e3811d382f55948352add15c2a28e5e93c
readonly identity_digest=1a6e443309d9484e62e9f1fe71a83b60cf348f4acbe3a92d8f7b8bb7d3274d33
readonly list_digest=c914929e71ab8ce0e8977518615ee3cf81c31a411ec77c9f58a2350145c6ee95
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

mkdir -p "${work_root}/before" "${work_root}/provider" "${work_root}/fixture"
cp provider-codegen/generated/dns_record.provider-code-spec.json "${work_root}/before/"
cp provider-codegen/generated/dns_record.impact.json "${work_root}/before/"
cp provider-codegen/generated/dns_record.mapping.json "${work_root}/before/"
cp internal/generated/resource_dns_record/dns_record_resource_gen.go "${work_root}/before/"

go generate ./provider-codegen
cmp "${work_root}/before/dns_record.provider-code-spec.json" provider-codegen/generated/dns_record.provider-code-spec.json
cmp "${work_root}/before/dns_record.impact.json" provider-codegen/generated/dns_record.impact.json
cmp "${work_root}/before/dns_record.mapping.json" provider-codegen/generated/dns_record.mapping.json
cmp "${work_root}/before/dns_record_resource_gen.go" internal/generated/resource_dns_record/dns_record_resource_gen.go

go test ./...
CGO_ENABLED=0 go build -trimpath -o "${work_root}/provider/terraform-provider-unifi_v${provider_version}" .
go build -trimpath -o "${work_root}/schema-baseline" ./cmd/schema-baseline

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
cat >"${work_root}/fixture/cli.tfrc" <<EOF
provider_installation {
  dev_overrides {
    "${provider_address}" = "${work_root}/provider"
  }
  direct {}
}
EOF

run_schema() {
    local cli=$1
    local name=$2
    CHECKPOINT_DISABLE=1 TF_IN_AUTOMATION=1 \
        TF_CLI_CONFIG_FILE="${work_root}/fixture/cli.tfrc" \
        TF_DATA_DIR="${work_root}/fixture/.${name}" \
        "${cli}" -chdir="${work_root}/fixture" providers schema -json \
        >"${work_root}/${name}.raw.json"
    "${work_root}/schema-baseline" \
        -input "${work_root}/${name}.raw.json" \
        -canonical-output "${work_root}/${name}.canonical.json" \
        -digests-output "${work_root}/${name}.digests.json"
}

run_schema "${terraform_bin}" terraform
run_schema "${tofu_bin}" tofu

test "$(jq -r '.schema_sha256["resource_schemas.unifi_dns_record"]' "${work_root}/terraform.digests.json")" = "${resource_digest}"
test "$(jq -r '.schema_sha256["resource_identity_schemas.unifi_dns_record"]' "${work_root}/terraform.digests.json")" = "${identity_digest}"
test "$(jq -r '.schema_sha256["list_resource_schemas.unifi_dns_record"]' "${work_root}/terraform.digests.json")" = "${list_digest}"
test "$(jq -r '.schema_sha256["resource_schemas.unifi_dns_record"]' "${work_root}/tofu.digests.json")" = "${resource_digest}"
test "$(jq -r '.schema_sha256["resource_identity_schemas.unifi_dns_record"]' "${work_root}/tofu.digests.json")" = "${identity_digest}"

jq --sort-keys 'del(.action_schemas, .list_resource_schemas)' "${work_root}/terraform.canonical.json" >"${work_root}/terraform.shared.json"
jq --sort-keys . "${work_root}/tofu.canonical.json" >"${work_root}/tofu.shared.json"
cmp "${work_root}/terraform.shared.json" "${work_root}/tofu.shared.json"

finished_at=$(date +%s)
readonly finished_at
jq --indent 2 --null-input \
    --arg source_commit "$(git rev-parse HEAD)" \
    --arg execution "${M1_EXECUTION:-local}" \
    --arg go_version "$(go version)" \
    --arg goos "$(go env GOOS)" \
    --arg goarch "$(go env GOARCH)" \
    --arg terraform_version "$("${terraform_bin}" version -json | jq -r .terraform_version)" \
    --arg tofu_version "$("${tofu_bin}" version -json | jq -r .terraform_version)" \
    --arg provider_binary_sha256 "$(sha256sum "${work_root}/provider/terraform-provider-unifi_v${provider_version}" | awk '{print $1}')" \
    --arg provider_code_spec_sha256 "$(sha256sum provider-codegen/generated/dns_record.provider-code-spec.json | awk '{print $1}')" \
    --arg impact_sha256 "$(sha256sum provider-codegen/generated/dns_record.impact.json | awk '{print $1}')" \
    --arg mapping_sha256 "$(sha256sum provider-codegen/generated/dns_record.mapping.json | awk '{print $1}')" \
    --arg generated_go_sha256 "$(sha256sum internal/generated/resource_dns_record/dns_record_resource_gen.go | awk '{print $1}')" \
    --arg resource_digest "${resource_digest}" \
    --arg identity_digest "${identity_digest}" \
    --arg list_digest "${list_digest}" \
    --argjson elapsed_seconds "$((finished_at - started_at))" \
    '{format_version: 1, milestone: "M1", result: "passed", execution: $execution, source_commit: $source_commit, platform: $goos + "/" + $goarch, go_version: $go_version, terraform_version: $terraform_version, tofu_version: $tofu_version, provider_binary_sha256: $provider_binary_sha256, generator: {module: "github.com/hashicorp/terraform-plugin-codegen-framework", version: "v0.4.1", commit: "eea0e9d6b59b4e678cac5cda2d2c5d852f8679f2"}, outputs: {provider_code_spec_sha256: $provider_code_spec_sha256, impact_sha256: $impact_sha256, mapping_sha256: $mapping_sha256, generated_go_sha256: $generated_go_sha256}, schema_sha256: {resource: $resource_digest, identity: $identity_digest, list_resource: $list_digest}, deterministic_regeneration: true, shared_cli_projection_equal: true, full_cli_projection_equal: false, terraform_only_categories: ["action_schemas", "list_resource_schemas"], lifecycle_evidence: {receipt: "build/m0/network-dns-qualification.json", reused: true, reason: "schema-only cutover left runtime paths unchanged"}, human_decisions: ["field dispositions", "key to name mapping", "integer seconds to duration string", "provider-owned identity site and timeouts", "defaults", "validators", "replacement modifiers", "handwritten state list and lifecycle seams"], elapsed_seconds: $elapsed_seconds}' \
    >"${M1_RECEIPT_OUTPUT:-${work_root}/dns-compiler-receipt.json}"

if [[ -z ${M1_RECEIPT_OUTPUT:-} ]]; then
    cat "${work_root}/dns-compiler-receipt.json"
fi
